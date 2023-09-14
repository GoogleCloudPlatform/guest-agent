// Copyright 2023 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
	// Events return a slice with all the event types a given Watcher handles.
	Events() []string
	// Run implements the actuall "listening" strategy and emits a event "signal".
	// It must return:
	//   - [bool] if the watcher should renew(run again).
	//   - [interface{}] a event context data pointer further describing the event(if needed).
	//   - [err] error case the Watcher failed and wants to notify subscribers(see EventData).
	Run(ctx context.Context, evType string) (bool, interface{}, error)
}

// Manager defines the interface between events management layer and the
// core guest agent implementation.
type Manager struct {
	// watchers maps the registered watchers.
	watchers map[string]Watcher

	// subscribers maps the subscribed callbacks.
	subscribers map[string][]*eventSubscriber

	// queue queue struct manages the running watchers, when it gets to len()
	// down to zero means all watchers are done and we can signal the other
	// control go routines to leave(given we don't have any more job left to
	// process).
	queue *watcherQueue
}

// watcherQueue wraps the watchers <-> callbacks communication as well as the
// communication/coordination of the multiple control go routine i.e. the one
// responsible to calling callbacks after a event is produced by the watcher etc.
type watcherQueue struct {
	// queueMutex protects the access to watchersMap
	queueMutex sync.RWMutex

	// watchersMap maps the currently running watchers.
	watchersMap map[string]bool

	// finishContextHandler is a channel used to communicate with the context handling
	// go routine that it should finish/end its job (usually after all watchers are done).
	finishContextHandler chan bool

	// finishCallbackHandler is a channel used to communicate with the callback handling
	// go routine that it should finish/end its job (usually after all watchers are done).
	finishCallbackHandler chan bool

	// watcherDone is a channel used to communicate that a given watcher is finished/done.
	watcherDone chan string

	// dataBus is the channel used to communicate between watchers (event producer) and the
	// callback handler (event consumer managing go routine).
	dataBus chan eventBusData

	// leaving is a flag that indicates no more job should be processed as we are done
	// with all watchers and callbacks.
	leaving bool
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

// WatcherEventType wraps/couples together a Watcher and an event type.
type WatcherEventType struct {
	// watcher is the watcher implementation for a given event type.
	watcher Watcher
	// evType idenfities the event type this object refences to.
	evType string
}

type eventSubscriber struct {
	data interface{}
	cb   EventCb
}

type eventBusData struct {
	evType string
	data   *EventData
}

// EventCb defines the callback interface between watchers and subscribers. The arguments are:
//   - ctx the app' context passed in from the manager's Run() call.
//   - evType a string defining the what event type triggered the call.
//   - data a user context pointer to be consumed by the callback.
//   - evData a event specific data pointer.
//
// The callback should return true if it wants to renew, returning false will case the callback
// to be unregistered/unsubscribed.
type EventCb func(ctx context.Context, evType string, data interface{}, evData *EventData) bool

// length returns how many watchers are currently running.
func (ep *watcherQueue) length() int {
	ep.queueMutex.RLock()
	defer ep.queueMutex.RUnlock()
	return len(ep.watchersMap)
}

// add adds a new watcher to the queue.
func (ep *watcherQueue) add(evType string) {
	ep.queueMutex.Lock()
	defer ep.queueMutex.Unlock()
	ep.watchersMap[evType] = true
}

// del removes a watcher from the queue.
func (ep *watcherQueue) del(evType string) int {
	ep.queueMutex.Lock()
	defer ep.queueMutex.Unlock()
	delete(ep.watchersMap, evType)
	return len(ep.watchersMap)
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
		queue: &watcherQueue{
			watchersMap:           make(map[string]bool),
			dataBus:               make(chan eventBusData),
			finishCallbackHandler: make(chan bool),
			finishContextHandler:  make(chan bool),
			watcherDone:           make(chan string),
		},
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

func (mngr *Manager) eventTypes() []*WatcherEventType {
	var res []*WatcherEventType
	for _, watcher := range mngr.watchers {
		for _, evType := range watcher.Events() {
			res = append(res, &WatcherEventType{watcher, evType})
		}
	}
	return res
}

func (mngr *Manager) runWatcher(ctx context.Context, watcher Watcher, evType string) {
	for renew := true; renew; {
		var evData interface{}
		var err error

		renew, evData, err = watcher.Run(ctx, evType)

		logger.Debugf("Watcher(%s) returned event: %q, should renew?: %t", watcher.ID(), evType, renew)

		if mngr.queue.leaving {
			break
		}

		mngr.queue.dataBus <- eventBusData{
			evType: evType,
			data: &EventData{
				Data:  evData,
				Error: err,
			},
		}
	}

	logger.Debugf("watcher finishing: %s", evType)
	mngr.queue.watcherDone <- evType
}

// Run runs the event manager, it will block until all watchers have given up/failed.
func (mngr *Manager) Run(ctx context.Context) {
	var wg sync.WaitGroup

	// Manages the context's done signal, pass it down to the other go routines to
	// finish its job and leave. Additionally, if the remaining go routines are leaving
	// we get it handled via dataBus channel and drop this go routine as well.
	wg.Add(1)
	go func(done <-chan struct{}, finishContextHandler <-chan bool, finishCallbackHandler chan<- bool) {
		defer wg.Done()

		for {
			select {
			case <-done:
				logger.Debugf("Got context's Done() signal, leaving.")
				mngr.queue.leaving = true
				finishCallbackHandler <- true
				return
			case <-finishContextHandler:
				mngr.queue.leaving = true
				return
			}
		}
	}(ctx.Done(), mngr.queue.finishContextHandler, mngr.queue.finishCallbackHandler)

	// Manages the event processing avoiding blocking the watcher's go routines.
	// This will listen to dataBus and call the events handlers/callbacks.
	wg.Add(1)
	go func(bus <-chan eventBusData, finishCallbackHandler <-chan bool) {
		defer wg.Done()

		for {
			select {
			case <-finishCallbackHandler:
				return
			case busData := <-bus:
				subscribers, found := mngr.subscribers[busData.evType]
				if !found || len(subscribers) == 0 {
					logger.Debugf("No subscriber found for event: %s, returning.", busData.evType)
					continue
				}

				keepMe := make([]*eventSubscriber, 0)
				for _, curr := range mngr.subscribers[busData.evType] {
					logger.Debugf("Running registered callback for event: %s", busData.evType)
					renew := curr.cb(ctx, busData.evType, curr.data, busData.data)
					if renew {
						keepMe = append(keepMe, curr)
					}
					logger.Debugf("Returning from event %q subscribed callback, should renew?: %t", busData.evType, renew)
				}

				mngr.subscribers[busData.evType] = keepMe

				// No more subscribers for this event type, delete it from the subscribers map.
				if len(keepMe) == 0 {
					logger.Debugf("No more subscribers left for evType: %s", busData.evType)
					delete(mngr.subscribers, busData.evType)
				}

				// No more subscribers at all, we have nothing more left to do here.
				if len(mngr.subscribers) == 0 {
					logger.Debugf("No subscribers left, leaving")
					break
				}
			}
		}
	}(mngr.queue.dataBus, mngr.queue.finishCallbackHandler)

	// Creates a goroutine for each registered watcher's event and keep handling its
	// execution until they give up/finishes their job by returning renew = false.
	for _, curr := range mngr.eventTypes() {
		mngr.queue.add(curr.evType)
		wg.Add(1)
		go func(watcher Watcher, evType string) {
			defer wg.Done()
			mngr.runWatcher(ctx, watcher, evType)
		}(curr.watcher, curr.evType)
	}

	// Controls the completion of the watcher go routines, their removal from the queue
	// and signals to context & callback control go routines about watchers completion.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for len := mngr.queue.length(); len > 0; {
			doneStr := <-mngr.queue.watcherDone
			len = mngr.queue.del(doneStr)
			if !mngr.queue.leaving && len == 0 {
				logger.Debugf("All watchers are finished, signaling to leave.")
				mngr.queue.finishContextHandler <- true
				mngr.queue.finishCallbackHandler <- true
			}
		}
	}()

	wg.Wait()
}
