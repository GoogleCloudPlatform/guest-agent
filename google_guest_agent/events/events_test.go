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

package events

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/metadata"
)

func TestConstructor(t *testing.T) {
	tests := []struct {
		config  *Config
		success bool
	}{
		{config: nil, success: true},
		{config: &Config{Watchers: []string{metadata.WatcherID}}, success: true},
		{config: &Config{Watchers: []string{"foobar"}}, success: false},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
			_, err := New(tt.config)
			if err != nil && tt.success {
				t.Errorf("expected success, got error: %+v", err)
			}
		})
	}
}

func TestInitWatcers(t *testing.T) {
	tests := []struct {
		watchers []Watcher
		success  bool
	}{
		{watchers: []Watcher{metadata.New()}, success: true},
		{watchers: []Watcher{&testWatcher{}}, success: false},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
			err := initWatchers(tt.watchers)
			if err != nil && tt.success {
				t.Errorf("expected success, got error: %+v", err)
			}
		})
	}
}

type testWatcher struct {
	watcherID string
	counter   int
	maxCount  int
}

func (tprod *testWatcher) ID() string {
	return tprod.watcherID
}

func (tprod *testWatcher) Run(ctx context.Context) (bool, string, interface{}, error) {
	if tprod.counter >= tprod.maxCount {
		return false, "", nil, nil
	}
	tprod.counter++
	evData := tprod.counter
	return true, tprod.watcherID + ",test-event", &evData, nil
}

func TestRun(t *testing.T) {
	watcherID := "test-watcher"
	maxCount := 10

	err := initWatchers([]Watcher{
		&testWatcher{
			watcherID: watcherID,
			maxCount:  maxCount,
		},
	})

	if err != nil {
		t.Fatalf("Failed to init/register watcher: %+v", err)
	}

	eventManager, err := New(&Config{Watchers: []string{watcherID}})
	if err != nil {
		t.Fatalf("Failed to init event manager: %+v", err)
	}

	counter := 0
	eventManager.Subscribe("test-watcher,test-event", &counter, func(evType string, data interface{}, evData *EventData) bool {
		dd := data.(*int)
		*dd++
		return true
	})

	eventManager.Run(context.Background())

	if counter != maxCount-1 {
		t.Errorf("Failed to increment callback counter, expected: %d, got: %d", maxCount, counter)
	}
}

func TestUnsubscribe(t *testing.T) {
	watcherID := "test-watcher"
	maxCount := 10
	unsubscribeAt := 2

	err := initWatchers([]Watcher{
		&testWatcher{
			watcherID: watcherID,
			maxCount:  maxCount,
		},
	})

	if err != nil {
		t.Fatalf("Failed to init/register watcher: %+v", err)
	}

	eventManager, err := New(&Config{Watchers: []string{watcherID}})
	if err != nil {
		t.Fatalf("Failed to init event manager: %+v", err)
	}

	counter := 0
	eventManager.Subscribe("test-watcher,test-event", nil, func(evType string, data interface{}, evData *EventData) bool {
		if counter == unsubscribeAt {
			return false
		}
		counter++
		return true
	})

	eventManager.Run(context.Background())

	if counter != unsubscribeAt {
		t.Errorf("Failed to unsubscribe callback, expected: %d, got: %d", unsubscribeAt, counter)
	}
}

func TestCancel(t *testing.T) {
	watcherID := "test-watcher"
	timeout := (1 * time.Second) / 100

	err := initWatchers([]Watcher{
		&testCancel{
			watcherID: watcherID,
			timeout:   timeout,
		},
	})

	if err != nil {
		t.Fatalf("Failed to init/register watcher: %+v", err)
	}

	eventManager, err := New(&Config{Watchers: []string{watcherID}})
	if err != nil {
		t.Fatalf("Failed to init event manager: %+v", err)
	}

	eventManager.Subscribe("test-watcher,test-event", nil, func(evType string, data interface{}, evData *EventData) bool {
		t.Errorf("Expected to have canceled before calling callback")
		return true
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(timeout / 2)
		cancel()
	}()

	eventManager.Run(ctx)
}

type testCancel struct {
	watcherID string
	timeout   time.Duration
}

func (tc *testCancel) ID() string {
	return tc.watcherID
}

func (tc *testCancel) Run(ctx context.Context) (bool, string, interface{}, error) {
	time.Sleep(tc.timeout)
	return true, tc.watcherID + ",test-event", nil, nil
}

func TestComplexEventData(t *testing.T) {
	watcherID := "test-watcher"
	timeout := (1 * time.Second) / 100

	err := initWatchers([]Watcher{
		&testComplexEventData{
			watcherID: watcherID,
			timeout:   timeout,
		},
	})

	if err != nil {
		t.Fatalf("Failed to init/register watcher: %+v", err)
	}

	eventManager, err := New(&Config{Watchers: []string{watcherID}})
	if err != nil {
		t.Fatalf("Failed to init event manager: %+v", err)
	}

	eventManager.Subscribe("test-watcher,test-event", nil, func(evType string, data interface{}, evData *EventData) bool {
		t.Errorf("Expected to have canceled before calling callback")
		cData := evData.Data.(*complexData)
		if cData == nil {
			t.Errorf("Wrong event data, expected a pointer, got nil")
		}

		if cData.name != "complex-data" {
			t.Errorf("Wrong complex data, expected: complex-data, got: %s", cData.name)
		}
		return true
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(timeout / 2)
		cancel()
	}()

	eventManager.Run(ctx)
}

type complexData struct {
	name string
}

type testComplexEventData struct {
	watcherID string
	timeout   time.Duration
}

func (tc *testComplexEventData) ID() string {
	return tc.watcherID
}

func (tc *testComplexEventData) Run(ctx context.Context) (bool, string, interface{}, error) {
	time.Sleep(tc.timeout)
	return true, tc.watcherID + ",test-event", &complexData{name: "complex-data"}, nil
}
