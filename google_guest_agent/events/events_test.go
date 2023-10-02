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

func (tprod *testWatcher) Events() []string {
	return []string{tprod.watcherID + ",test-event"}
}

func (tprod *testWatcher) Run(ctx context.Context, evType string) (bool, interface{}, error) {
	tprod.counter++
	evData := tprod.counter

	if tprod.counter >= tprod.maxCount {
		return false, nil, nil
	}

	return true, &evData, nil
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
	eventManager.Subscribe("test-watcher,test-event", &counter, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		dd := data.(*int)
		*dd++
		return true
	})

	eventManager.Run(context.Background())

	if counter != maxCount {
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
	eventManager.Subscribe("test-watcher,test-event", nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
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

func TestCancelBeforeCallbacks(t *testing.T) {
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

	eventManager.Subscribe("test-watcher,test-event", nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
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

func (tc *testCancel) Events() []string {
	return []string{tc.watcherID + ",test-event"}
}

func (tc *testCancel) Run(ctx context.Context, evType string) (bool, interface{}, error) {
	time.Sleep(tc.timeout)
	return true, nil, nil
}

func TestCancelAfterCallbacks(t *testing.T) {
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

	eventManager.Subscribe("test-watcher,test-event", nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		return true
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(timeout * 10)
		cancel()
	}()

	eventManager.Run(ctx)
}

type testCancelWatcher struct {
	watcherID string
	after     int
}

func (tc *testCancelWatcher) ID() string {
	return tc.watcherID
}

func (tc *testCancelWatcher) Events() []string {
	return []string{tc.watcherID + ",test-event"}
}

func (tc *testCancelWatcher) Run(ctx context.Context, evType string) (bool, interface{}, error) {
	time.Sleep(10 * time.Millisecond)
	if tc.after == 0 {
		return false, nil, nil
	}
	tc.after--
	return true, nil, nil
}

func TestCancelCallbacksAndWatchers(t *testing.T) {
	watcherID := "test-watcher"

	tests := []struct {
		cancelWatcherAfter    int
		cancelSubscriberAfter int
	}{
		{10, 20},
		{20, 10},
		{10, 10},
		{0, 0},
		{100, 200},
		{200, 100},
		{100, 100},
	}

	for i, curr := range tests {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
			cancelSubscriberAfter := curr.cancelSubscriberAfter

			err := initWatchers([]Watcher{
				&testCancelWatcher{
					watcherID: watcherID,
					after:     curr.cancelWatcherAfter,
				},
			})

			if err != nil {
				t.Fatalf("Failed to init/register watcher: %+v", err)
			}

			eventManager, err := New(&Config{Watchers: []string{watcherID}})
			if err != nil {
				t.Fatalf("Failed to init event manager: %+v", err)
			}

			eventManager.Subscribe("test-watcher,test-event", nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
				time.Sleep(1 * time.Millisecond)
				if cancelSubscriberAfter == 0 {
					return false
				}
				cancelSubscriberAfter--
				return true
			})

			eventManager.Run(context.Background())
		})
	}
}

func TestMultipleEvents(t *testing.T) {
	watcherID := "multiple-events"
	firstEvent := "multiple-events,first-event"
	secondEvent := "multiple-events,second-event"

	err := initWatchers([]Watcher{
		&testMultipleEvents{
			watcherID: watcherID,
			eventIDS:  []string{firstEvent, secondEvent},
		},
	})

	if err != nil {
		t.Fatalf("Failed to init/register watcher: %+v", err)
	}

	eventManager, err := New(&Config{Watchers: []string{watcherID}})
	if err != nil {
		t.Fatalf("Failed to init event manager: %+v", err)
	}

	var hitFirstEvent bool
	eventManager.Subscribe(firstEvent, nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		hitFirstEvent = true
		return false
	})

	var hitSecondEvent bool
	eventManager.Subscribe(secondEvent, nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		hitSecondEvent = true
		return false
	})

	eventManager.Run(context.Background())

	if !hitFirstEvent || !hitSecondEvent {
		t.Errorf("Failed to call back events, first event hit? (%t), second event hit? (%t)", hitFirstEvent, hitSecondEvent)
	}
}

type testMultipleEvents struct {
	watcherID string
	eventIDS  []string
}

func (tt *testMultipleEvents) ID() string {
	return tt.watcherID
}

func (tt *testMultipleEvents) Events() []string {
	return tt.eventIDS
}

func (tt *testMultipleEvents) Run(ctx context.Context, evType string) (bool, interface{}, error) {
	return false, nil, nil
}
