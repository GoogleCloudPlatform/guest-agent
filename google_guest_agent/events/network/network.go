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

// Package network implements network event watchers.
package network

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	// NetworkWatcherID is the ID of the watcher listening for network events.
	NetworkWatcherID = "network-watcher"
	// IfaceUpEvent is emitted when a network interface is configured and set up. This is signaled by kernel callbacks on windows, and network hook scripts writing to named pipes on linux.
	IfaceUpEvent = NetworkWatcherID + ",iface-up"
	// HostnameReconfigureEvent is emitted when the hostname is reconfigured. Emitting this event is the trigger for reconfiguration, so emitting is the same as requesting.
	HostnameReconfigureEvent = NetworkWatcherID + ",hostname-reconfigure"
)

var (
	pipeLock      = new(sync.Mutex)
	eventChannels = make(map[string]chan (any))
	eventList     = []string{IfaceUpEvent, HostnameReconfigureEvent}
)

// NewNetworkWatcher is a NetworkWatcher constructor. The pipe path can be set
// for convenience during testing, but creating watchers with different pipe
// paths is not supported and will not work.
func NewNetworkWatcher(pipePath string) Watcher {
	for _, evType := range eventList {
		if eventChannels[evType] == nil {
			eventChannels[evType] = make(chan any, 1)
		}
	}
	return Watcher{pipePath: pipePath}
}

// Watcher is the structure which listens for network events.
type Watcher struct {
	pipePath string
}

// ID returns the watcher ID
func (w Watcher) ID() string {
	return NetworkWatcherID
}

// Events returns the list of events handled by this watcher
func (w Watcher) Events() []string {
	return eventList
}

// Run starts a listener goroutine and waits for events of the given type to come through the channel with the same name.
func (w Watcher) Run(ctx context.Context, evType string) (bool, interface{}, error) {
	errs := make(chan error, 1)
	listenctx, listencancel := context.WithCancel(ctx)
	defer listencancel() // Create a child context and cancel it when it when we got the event we're looking for.
	go w.listenForEvents(listenctx, errs)
	evChan, ok := eventChannels[evType]
	if !ok {
		return false, nil, fmt.Errorf("unknown event type %s", evType)
	}
	for {
		select {
		case <-ctx.Done():
			return false, nil, ctx.Err() // Unregister on context cancellation
		case err := <-errs:
			return true, nil, err // Retry on failure
		case <-evChan:
			// Notify only that any event happened, not currently reporting any data about the event.
			return true, nil, nil
		}
	}
}

// Listen on the network watcher pipe for events and direct them to the correct channels until the context is cancelled. Sending an error over the channel indicates an inability to listen to events, not a problem with an individual connection.
func (w Watcher) listenForEvents(ctx context.Context, errs chan (error)) {
	pipeLock.Lock()
	defer pipeLock.Unlock()
	if ctx.Err() != nil {
		// No longer need events.
		return
	}
	srv, err := w.listen(ctx, w.pipePath)
	if err != nil {
		errs <- err
		logger.Errorf("could not listen for network events on %s: %v", w.pipePath, err)
		return
	}
	defer srv.Close()
	for {
		if ctx.Err() != nil {
			return
		}
		conn, err := srv.Accept()
		if err != nil {
			logger.Infof("error on connection to pipe %s: %v", w.pipePath, err)
			continue
		}
		go func(conn net.Conn) {
			defer conn.Close()
			data, err := io.ReadAll(conn)
			if err != nil {
				logger.Debugf("error reading data from connection to pipe %s: %v", w.pipePath, err)
				return
			}
			for _, event := range w.Events() {
				if strings.HasSuffix(event, string(data)) {
					eventChannels[event] <- struct{}{}
					return
				}
			}
		}(conn)
	}
}
