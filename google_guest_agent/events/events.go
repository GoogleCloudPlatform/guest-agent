//  Copyright 2023 Google Inc. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

// Package events is a events processing layer.
package events

import (
	"context"
	"fmt"
	"sync"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/sshtrustedca"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	// availableWatchers mapps all kown available event watchers.
	availableWatchers = make(map[string]Watcher)
)

// Watcher defines the interface between the events manager and the actual
// watcher implementation.
type Watcher interface {
	// ID returns the watcher id.
	ID() string
	// Run implements the actuall "listening" strategy and emits a event "signal".
	// It must return:
	//   - [bool] if the watcher should renew(run again).
	//   - [string] the event type id.
	//   - [interface{}] a event context data pointer further describing the event(if needed).
	//   - [err] error case the Watcher failed and wants to notify subscribers(see EventData).
	Run(ctx context.Context) (bool, string, interface{}, error)
}

// Manager defines the interface between events management layer and the
// core guest agent implementation.
type Manager struct {
	watchers    map[string]Watcher
	subscribers map[string][]*eventSubscriber
}

// Config offers a mechanism for the consumers to configure the Manager behavior.
type Config struct {
	// Watchers lists the enabled watchers, of not provided all available watchers will be enabled.
	Watchers []string
}

// EventData wraps the data communicated from a Watcher to a Subscriber.
type EventData struct {
	// Data points to the Watcher provided data.
	Data interface{}
	// Error is used when a Watcher has failed and wants communicate its subscribers about the error.
	Error error
}

// EventCb defines the callback interface between watchers and subscribers. The arguments are:
//   - evType a string defining the what event type triggered the call.
//   - data a user context pointer to be consumed by the callback.
//   - evData a event specific data pointer.
//
// The callback should return true if it wants to renew, returning false will case the callback
// to be unregistered/unsubscribed.
type EventCb func(evType string, data interface{}, evData *EventData) bool

type eventSubscriber struct {
	data interface{}
	cb   EventCb
}

type eventBusData struct {
	leaving bool
	evType  string
	data    *EventData
}

func init() {
	err := initWatchers([]Watcher{
		metadata.New(),
		sshtrustedca.New(sshtrustedca.DefaultPipePath),
	})
	if err != nil {
		logger.Errorf("Failed to initialize watchers: %+v", err)
	}
}

// init initializes the known available event watchers.
func initWatchers(watchers []Watcher) error {
	for _, curr := range watchers {
		// Error if we are accidentaly not properly setting the id.
		if curr.ID() == "" {
			return fmt.Errorf("invalid event watcher id, skipping")
		}
		availableWatchers[curr.ID()] = curr
	}
	return nil
}

// New allocates and initializes a events Manager based on provided cfg.
func New(cfg *Config) (*Manager, error) {
	res := &Manager{
		watchers:    availableWatchers,
		subscribers: make(map[string][]*eventSubscriber),
	}

	// Align manager's config based on consumers provided watchers If it is
	// passing in wanted/expected watchers, otherwise use all available ones.
	if cfg != nil && len(cfg.Watchers) > 0 {
		res.watchers = make(map[string]Watcher)
		for _, curr := range cfg.Watchers {
			// Report back if we don't know the provided watcher id.
			if _, found := availableWatchers[curr]; !found {
				return nil, fmt.Errorf("invalid/unknown watcher id: %s", curr)
			}
			res.watchers[curr] = availableWatchers[curr]
		}
	}

	return res, nil
}

// Subscribe registers an event consumer/subscriber callback to a given event type, data
// is a context pointer provided by the caller to be passed down when calling cb when
// a new event happens.
func (mngr *Manager) Subscribe(evType string, data interface{}, cb EventCb) {
	mngr.subscribers[evType] = append(mngr.subscribers[evType],
		&eventSubscriber{
			data: data,
			cb:   cb,
		},
	)
}

type watcherQueue struct {
	mutex       sync.Mutex
	watchersMap map[string]bool
}

func (ep *watcherQueue) add(evType string) {
	ep.mutex.Lock()
	defer ep.mutex.Unlock()
	ep.watchersMap[evType] = true
}

func (ep *watcherQueue) del(evType string) int {
	ep.mutex.Lock()
	defer ep.mutex.Unlock()
	delete(ep.watchersMap, evType)
	return len(ep.watchersMap)
}

// Run runs the event manager, it will block until all watchers have given up/failed.
func (mngr *Manager) Run(ctx context.Context) {
	var wg sync.WaitGroup
	var leaving bool

	syncBus := make(chan eventBusData)
	defer close(syncBus)

	// Manages the context's done signal, pass it down to the other go routines to
	// finish its job and leave. Additionally, if the remaining go routines are leaving
	// we get it handled via syncBus channel and drop this go routine as well.
	wg.Add(1)
	go func() {
		defer wg.Done()

		select {
		case <-ctx.Done():
			logger.Debugf("Got context's Done() signal, leaving.")
			leaving = true
			syncBus <- eventBusData{
				leaving: true,
			}
			return
		case notif := <-syncBus:
			if notif.leaving {
				logger.Debugf("Got syncBus' leaving signal, leaving.")
				return
			}
		}
	}()

	// Manages the event processing avoiding blocking the watcher's go routines.
	// This will listen to syncBus and call the events handlers/callbacks.
	wg.Add(1)
	go func(bus <-chan eventBusData) {
		defer wg.Done()

		for busData := range bus {
			// We got a notification that we are leaving so drop it all.
			if busData.leaving {
				return
			}

			subscribers, found := mngr.subscribers[busData.evType]
			if !found || len(subscribers) == 0 {
				logger.Debugf("No subscriber found for event: %s, returning.", busData.evType)
				continue
			}

			keepMe := make([]*eventSubscriber, 0)
			for _, curr := range mngr.subscribers[busData.evType] {
				logger.Debugf("Running registered callback for event: %s", busData.evType)
				renew := curr.cb(busData.evType, curr.data, busData.data)
				if renew {
					keepMe = append(keepMe, curr)
				}
				logger.Debugf("Returning from event %q subscribed callback, should renew?: %+v", busData.evType, renew)
			}
			mngr.subscribers[busData.evType] = keepMe
		}
	}(syncBus)

	// This control struct manages the registered watchers, when it gets to len()
	// down to zero means all watchers are done and we can signal the other 2 control
	// go routines to leave(given we don't have any more job left to process).
	control := &watcherQueue{
		watchersMap: make(map[string]bool),
	}

	// Creates a goroutine for each registered watcher and keep handling its
	// execution until they give up/finishes their job by returning renew = false.
	for _, curr := range mngr.watchers {
		control.add(curr.ID())
		wg.Add(1)

		go func(bus chan<- eventBusData, watcher Watcher) {
			var evType string
			var evData interface{}
			var err error

			defer wg.Done()

			for renew := true; renew; {
				renew, evType, evData, err = watcher.Run(ctx)

				logger.Infof("Watcher(%s) returned event: %s, should renew?: %t", watcher.ID(), evType, renew)

				if leaving {
					break
				}

				bus <- eventBusData{
					evType: evType,
					data: &EventData{
						Data:  evData,
						Error: err,
					},
				}
			}

			if !leaving && control.del(watcher.ID()) == 0 {
				logger.Debugf("All watchers are finished, signaling to leave.")
				bus <- eventBusData{
					leaving: true,
				}
			}
		}(syncBus, curr)
	}

	wg.Wait()
}
