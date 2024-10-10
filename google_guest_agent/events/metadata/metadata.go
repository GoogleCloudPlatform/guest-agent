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
	"net"
	"net/url"
	"strings"
	"time"

	network "github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/network/manager"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	// WatcherID is the metadata watcher's ID.
	WatcherID = "metadata-watcher"
	// LongpollEvent is the metadata's longpoll event type ID.
	LongpollEvent = "metadata-watcher,longpoll"
)

// Watcher is the metadata event watcher implementation.
type Watcher struct {
	client         metadata.MDSClientInterface
	failedPrevious bool
}

// New allocates and initializes a new Watcher.
func New() *Watcher {
	return &Watcher{
		client: metadata.New(),
	}
}

// ID returns the metadata event watcher id.
func (mp *Watcher) ID() string {
	return WatcherID
}

// Events returns an slice with all implemented events.
func (mp *Watcher) Events() []string {
	return []string{LongpollEvent}
}

// Run listens to metadata changes and report back the event.
func (mp *Watcher) Run(ctx context.Context, evType string) (bool, interface{}, error) {
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
		if strings.Contains(err.Error(), "network is unreachable") {
			// At this point agent has already retries 100 times so most likely Network is broken. Try
			// falling back to default configs.
			mp.tryFallback(ctx)
		}
	} else {
		mp.failedPrevious = false
	}

	return true, descriptor, err
}

func (mp *Watcher) tryFallback(ctx context.Context) {
	logger.Infof("Falling to OS default network configuration to attempt to recover.")
	if err := network.FallbackToDefault(ctx); err != nil {
		// Just log error and attempt to continue anyway, if we can't reach MDS
		// we can't do anything.
		logger.Errorf("Failed to rollback guest-agent network configuration: %v", err)
	}
	// Give it some time to make sure configurations are applied.
	time.Sleep(time.Second * 2)
	_, err := mp.client.Get(ctx)
	if err != nil {
		logger.Fatalf("Failed to reach MDS after falling back to default config: %v", err)
	}
	// Reset if GET succeeds. If it fails it wouldn't matter as agent would exit and restart.
	mp.failedPrevious = false
}
