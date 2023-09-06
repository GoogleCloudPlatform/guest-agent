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

// Package metadata implement the metadata events watcher.
package metadata

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	// WatcherID is the metadata watcher's ID.
	WatcherID = "metadata-watcher"
	// ReadyEvent notifies subscribers that metadata is ready and we've successfully
	// got a first metadata descriptor - meaning metadata is ready.
	ReadyEvent = "metadata-watcher,ready"
	// LongpollEvent is the metadata's longpoll event type ID.
	LongpollEvent = "metadata-watcher,longpoll"
)

// Watcher is the metadata event watcher implementation.
type Watcher struct {
	// client is the metadata client interface.
	client metadata.MDSClientInterface
	// failedPrevious determines if we have already logged an error.
	failedPrevious bool
	// ready determines if ReadyEvent has already being emitted.
	ready bool
	// readyChan is the inter event communication mechanism.
	readyChan chan bool
}

// New allocates and initializes a new Watcher.
func New() *Watcher {
	return &Watcher{
		client:    metadata.New(),
		ready:     false,
		readyChan: make(chan bool),
	}
}

// ID returns the metadata event watcher id.
func (mp *Watcher) ID() string {
	return WatcherID
}

// Events returns an slice with all implemented events.
func (mp *Watcher) Events() []string {
	return []string{ReadyEvent, LongpollEvent}
}

func (mp *Watcher) runReadyWatcher(ctx context.Context, evType string) (bool, interface{}, error) {
	descriptor, err := mp.client.Get(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("Failed to get metadata descriptor: %+v", err)
	}

	logger.Debugf("Metadata watcher: we got a first metadata descriptor.")
	// Syn up with longPoll event watcher.
	mp.ready = true

	// Make it doesn't block if runLongpollWatcher is not listening the channel.
	select {
	case mp.readyChan <- true:
		logger.Debugf("Metadata watcher: notified longPoll watcher that metadata is ready.")
	default:
	}

	// This is a single shot event, once ready a ready signal will never be emitted again.
	return false, descriptor, nil
}

func (mp *Watcher) runLongpollWatcher(ctx context.Context, evType string) (bool, interface{}, error) {
	// Wait until ReadyEvent has being emitted.
	if !mp.ready {
		logger.Debugf("Metadata watcher: waiting until we have a first descriptor ready.")
		select {
		case <-mp.readyChan:
			break
		case <-ctx.Done(): // Handle the case of context cancelation while waiting for readyChan.
			break
		}
	}

	descriptor, err := mp.client.Watch(ctx)
	if err != nil {
		// Only log error once to avoid transient errors and not to spam the log on network failures.
		if !mp.failedPrevious {
			if urlErr, ok := err.(*url.Error); ok {
				if _, ok := urlErr.Err.(*net.OpError); ok {
					logger.Errorf("Network error when requesting metadata, make sure your instance has an active network and can reach the metadata server.")
				}
			}
			logger.Errorf("Error watching metadata: %s", err)
			mp.failedPrevious = true
		}
	} else {
		mp.failedPrevious = false
	}

	return true, descriptor, err
}

// Run listens to metadata changes and report back the event.
func (mp *Watcher) Run(ctx context.Context, evType string) (bool, interface{}, error) {
	switch evType {
	case ReadyEvent:
		return mp.runReadyWatcher(ctx, evType)
	case LongpollEvent:
		return mp.runLongpollWatcher(ctx, evType)
	default:
		return false, nil, fmt.Errorf("Metadata watcher: invalid event type: %s", evType)
	}
}
