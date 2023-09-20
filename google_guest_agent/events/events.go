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
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	defaultWatchers = []Watcher{
		metadata.New(),
	}
	instance *Manager
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
	// watcherEvents maps the registered watchers and their events.
	watcherEvents []*WatcherEventType

	// watchersMap is a convenient manager's mapping of registered watcher instances.
	watchersMap map[string]bool

	// watchersMutex protects the watchers map.
	watchersMutex sync.Mutex

	// removingWatcherEvents is a map of watchers being removed.
	removingWatcherEvents map[string]bool

	// running is a flag indicating if the Run() was previously called.
	running bool

	// runningMutex protects the running flag.
	runningMutex sync.RWMutex

	// subscribers maps the subscribed callbacks.
	subscribers map[string][]*eventSubscriber

	// subscribersMutex protects subscribers member/map of the manager object.
	subscribersMutex sync.Mutex

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
	// removed is a channel used to communicate with the running watcher go routine
	// that it shouldn't renew even if the watcher requested a renew (in response of a
	// RemoveWatcher() call.
	removed chan bool
}

type eventSubscriber struct {
	data interface{}
	cb   *EventCb
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

// AddDefaultWatchers add the default watchers:
//   - metadata
func (mngr *Manager) AddDefaultWatchers(ctx context.Context) error {
	for _, curr := range defaultWatchers {
		if err := mngr.AddWatcher(ctx, curr); err != nil {
			return err
		}
	}
	return nil
}

// newManager allocates and initializes a events Manager.
func newManager() *Manager {
	return &Manager{
		watchersMap:           make(map[string]bool),
		removingWatcherEvents: make(map[string]bool),
		subscribers:           make(map[string][]*eventSubscriber),
		queue: &watcherQueue{
			watchersMap:           make(map[string]bool),
			dataBus:               make(chan eventBusData),
			finishCallbackHandler: make(chan bool),
			finishContextHandler:  make(chan bool),
			watcherDone:           make(chan string),
		},
	}
}

func init() {
	instance = newManager()
}

// Get allocates a new manager if one doesn't exists or returns the one previously allocated.
func Get() *Manager {
	if instance == nil {
		panic("The event's manager instance should had being initialized.")
	}
	return instance
}

// Subscribe registers an event consumer/subscriber callback to a given event type, data
// is a context pointer provided by the caller to be passed down when calling cb when
// a new event happens.
func (mngr *Manager) Subscribe(evType string, data interface{}, cb EventCb) {
	mngr.subscribersMutex.Lock()
	defer mngr.subscribersMutex.Unlock()
	mngr.subscribers[evType] = append(mngr.subscribers[evType],
		&eventSubscriber{
			data: data,
			cb:   &cb,
		},
	)
}

func (mngr *Manager) unsubscribe(evType string, cb *EventCb) {
	var keepMe []*eventSubscriber
	for _, curr := range mngr.subscribers[evType] {
		if curr.cb != cb {
			keepMe = append(keepMe, curr)
		}
	}

	mngr.subscribers[evType] = keepMe

	if len(keepMe) == 0 {
		logger.Debugf("No more subscribers left for evType: %s", evType)
		delete(mngr.subscribers, evType)
	}
}

// Unsubscribe removes the subscription of a given callback for a given event type.
func (mngr *Manager) Unsubscribe(evType string, cb EventCb) {
	mngr.subscribersMutex.Lock()
	defer mngr.subscribersMutex.Unlock()
	mngr.unsubscribe(evType, &cb)
}

// RemoveWatcher removes a watcher from the event manager. Each running watcher has its own
// context (derived from the one provided in the AddWatcher() call) and will have it canceled
// after calling this method.
func (mngr *Manager) RemoveWatcher(ctx context.Context, watcher Watcher) error {
	mngr.watchersMutex.Lock()
	defer mngr.watchersMutex.Unlock()

	id := watcher.ID()
	logger.Debugf("Got a request to remove watcher: %s", id)
	if _, found := mngr.watchersMap[id]; !found {
		return fmt.Errorf("unknown Watcher(%s)", id)
	}

	for _, curr := range mngr.watcherEvents {
		if _, found := mngr.removingWatcherEvents[curr.evType]; found {
			logger.Debugf("Watcher(%s) is being removed, skipping removal request: %s", id, curr.evType)
			continue
		}

		if curr.watcher.ID() == id {
			mngr.removingWatcherEvents[curr.evType] = true
			logger.Debugf("Removing watcher: %s, event type: %s", id, curr.evType)
			curr.removed <- true
		}
	}

	return nil
}

// AddWatcher adds/enables a new watcher. The watcher will be fired up right away if the
// event manager is already running, otherwise it's scheduled to run when Run() is called.
func (mngr *Manager) AddWatcher(ctx context.Context, watcher Watcher) error {
	mngr.watchersMutex.Lock()
	defer mngr.watchersMutex.Unlock()
	id := watcher.ID()
	if _, found := mngr.watchersMap[id]; found {
		return fmt.Errorf("watcher(%s) was previously added", id)
	}

	// Add the watchers and its events to internal mappings.
	evTypes := make(map[string]*WatcherEventType)
	mngr.watchersMap[id] = true

	for _, curr := range watcher.Events() {
		evType := &WatcherEventType{
			watcher: watcher,
			evType:  curr,
			removed: make(chan bool),
		}

		evTypes[curr] = evType
		mngr.watcherEvents = append(mngr.watcherEvents, evType)
	}

	mngr.runningMutex.RLock()
	defer mngr.runningMutex.RUnlock()
	// If we are not running don't bother "running" the watcher, Run() will do it later.
	if !mngr.running {
		return nil
	}

	// If we are already running the "run/launch" the watcher.
	for _, curr := range watcher.Events() {
		logger.Debugf("Adding watcher for event: %s", curr)
		mngr.queue.add(curr)
		go func(watcher Watcher, evType string, removed chan bool) {
			mngr.runWatcher(ctx, watcher, evType, removed)
		}(watcher, curr, evTypes[curr].removed)
	}

	return nil
}

func (mngr *Manager) runWatcher(ctx context.Context, watcher Watcher, evType string, removed chan bool) {
	nCtx, cancel := context.WithCancel(ctx)
	abort := false
	id := watcher.ID()

	go func() {
		abort = <-removed
		logger.Debugf("Got a request to abort watcher(%s) for event: %s", id, evType)
		cancel()
	}()

	for renew := true; renew; {
		var evData interface{}
		var err error

		renew, evData, err = watcher.Run(nCtx, evType)

		logger.Debugf("Watcher(%s) returned event: %q, should renew?: %t", id, evType, renew)

		if abort || mngr.queue.leaving {
			logger.Debugf("Watcher(%s), either are aborting(%t) or leaving(%t), breaking renew cycle",
				id, abort, mngr.queue.leaving)
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
	if !abort {
		removed <- true
	}

	mngr.queue.watcherDone <- evType
}

// Run runs the event manager, it will block until all watchers have given up/failed.
// The event manager is meant to be started right after the early initialization code
// and live until the application ends, the event manager can not be restarted - the Run()
// method will return an error if one tries to run it twice.
func (mngr *Manager) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	mngr.runningMutex.Lock()
	if mngr.running {
		mngr.runningMutex.Unlock()
		return fmt.Errorf("tried calling event manager's Run() twice")
	}
	mngr.running = true
	mngr.runningMutex.Unlock()

	queue := mngr.queue

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
				queue.leaving = true
				finishCallbackHandler <- true
				return
			case <-finishContextHandler:
				logger.Debugf("Got context handler finish signal, leaving.")
				queue.leaving = true
				return
			}
		}
	}(ctx.Done(), queue.finishContextHandler, queue.finishCallbackHandler)

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
				subscribers := mngr.subscribers[busData.evType]
				if subscribers == nil {
					logger.Debugf("No subscriber found for event: %s, returning.", busData.evType)
					continue
				}

				deleteMe := make([]*eventSubscriber, 0)
				for _, curr := range subscribers {
					logger.Debugf("Running registered callback for event: %s", busData.evType)
					renew := (*curr.cb)(ctx, busData.evType, curr.data, busData.data)
					if !renew {
						deleteMe = append(deleteMe, curr)
					}
					logger.Debugf("Returning from event %q subscribed callback, should renew?: %t", busData.evType, renew)
				}

				mngr.subscribersMutex.Lock()
				for _, curr := range deleteMe {
					mngr.unsubscribe(busData.evType, curr.cb)
				}
				leave := mngr.subscribers[busData.evType] == nil
				mngr.subscribersMutex.Unlock()

				// No more subscribers at all, we have nothing more left to do here.
				if leave {
					logger.Debugf("No subscribers left, leaving")
					break
				}
			}
		}
	}(queue.dataBus, queue.finishCallbackHandler)

	// Creates a goroutine for each registered watcher's event and keep handling its
	// execution until they give up/finishes their job by returning renew = false.
	for _, curr := range mngr.watcherEvents {
		queue.add(curr.evType)
		go func(watcher Watcher, evType string, removed chan bool) {
			mngr.runWatcher(ctx, watcher, evType, removed)
		}(curr.watcher, curr.evType, curr.removed)
	}

	// Controls the completion of the watcher go routines, their removal from the queue
	// and signals to context & callback control go routines about watchers completion.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for len := queue.length(); len > 0; {
			doneStr := <-queue.watcherDone
			len = queue.del(doneStr)
			delete(mngr.removingWatcherEvents, doneStr)
			if !queue.leaving && len == 0 {
				logger.Debugf("All watchers are finished, signaling to leave.")
				queue.finishContextHandler <- true
				queue.finishCallbackHandler <- true
			}
		}
	}()

	wg.Wait()
	return nil
}
