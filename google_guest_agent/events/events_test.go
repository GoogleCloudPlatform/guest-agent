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
	"sync"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/metadata"
)

func TestAddWatcher(t *testing.T) {
	eventManager := newManager()
	metadataWatcher := metadata.New()
	ctx := context.Background()

	err := eventManager.AddWatcher(ctx, metadataWatcher)
	if err != nil {
		t.Errorf("expected success, got error: %+v", err)
	}

	err = eventManager.AddWatcher(ctx, metadataWatcher)
	if err == nil {
		t.Errorf("expected error, had success, event manager shouldn't add same watcher twice")
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

	ctx := context.Background()
	eventManager := newManager()

	err := eventManager.AddWatcher(ctx, &testWatcher{
		watcherID: watcherID,
		maxCount:  maxCount,
	})

	if err != nil {
		t.Fatalf("Failed to add watcher to event manager: %+v", err)
	}

	counter := 0
	eventManager.Subscribe("test-watcher,test-event", &counter, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		dd := data.(*int)
		*dd++
		return true
	})

	if err := eventManager.Run(ctx); err != nil {
		t.Errorf("Failed to run event managed, expected success, got error: %+v", err)
	}

	if counter != maxCount {
		t.Errorf("Failed to increment callback counter, expected: %d, got: %d", maxCount, counter)
	}
}

func TestUnsubscribe(t *testing.T) {
	watcherID := "test-watcher"
	maxCount := 10
	unsubscribeAt := 2

	ctx := context.Background()
	eventManager := newManager()

	err := eventManager.AddWatcher(ctx, &testWatcher{
		watcherID: watcherID,
		maxCount:  maxCount,
	})

	if err != nil {
		t.Fatalf("Failed to add watcher to event manager: %+v", err)
	}

	counter := 0
	eventManager.Subscribe("test-watcher,test-event", nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		if counter == unsubscribeAt {
			return false
		}
		counter++
		return true
	})

	if err := eventManager.Run(ctx); err != nil {
		t.Errorf("Failed to run event managed, expected success, got error: %+v", err)
	}

	if counter != unsubscribeAt {
		t.Errorf("Failed to unsubscribe callback, expected: %d, got: %d", unsubscribeAt, counter)
	}
}

func TestCancelBeforeCallbacks(t *testing.T) {
	watcherID := "test-watcher"
	timeout := (1 * time.Second) / 100

	ctx, cancel := context.WithCancel(context.Background())
	eventManager := newManager()

	err := eventManager.AddWatcher(ctx, &testCancel{
		watcherID: watcherID,
		timeout:   timeout,
	})

	if err != nil {
		t.Fatalf("Failed to add watcher to event manager: %+v", err)
	}

	eventManager.Subscribe("test-watcher,test-event", nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		t.Errorf("Expected to have canceled before calling callback")
		return true
	})

	go func() {
		time.Sleep(timeout / 2)
		cancel()
	}()

	if err := eventManager.Run(ctx); err != nil {
		t.Errorf("Failed to run event managed, expected success, got error: %+v", err)
	}
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

	ctx, cancel := context.WithCancel(context.Background())
	eventManager := newManager()

	err := eventManager.AddWatcher(ctx, &testCancel{
		watcherID: watcherID,
		timeout:   timeout,
	})

	if err != nil {
		t.Fatalf("Failed to add watcher to event manager: %+v", err)
	}

	eventManager.Subscribe("test-watcher,test-event", nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		return true
	})

	go func() {
		time.Sleep(timeout * 10)
		cancel()
	}()

	if err := eventManager.Run(ctx); err != nil {
		t.Errorf("Failed to run event managed, expected success, got error: %+v", err)
	}
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

			ctx := context.Background()
			eventManager := newManager()

			err := eventManager.AddWatcher(ctx, &testCancelWatcher{
				watcherID: watcherID,
				after:     curr.cancelWatcherAfter,
			})

			if err != nil {
				t.Fatalf("Failed to add watcher to event manager: %+v", err)
			}

			eventManager.Subscribe("test-watcher,test-event", nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
				time.Sleep(1 * time.Millisecond)
				if cancelSubscriberAfter == 0 {
					return false
				}
				cancelSubscriberAfter--
				return true
			})

			if err := eventManager.Run(ctx); err != nil {
				t.Errorf("Failed to run event managed, expected success, got error: %+v", err)
			}
		})
	}
}

func TestMultipleEvents(t *testing.T) {
	watcherID := "multiple-events"
	firstEvent := "multiple-events,first-event"
	secondEvent := "multiple-events,second-event"

	ctx := context.Background()
	eventManager := newManager()

	err := eventManager.AddWatcher(ctx, &testMultipleEvents{
		watcherID: watcherID,
		eventIDS:  []string{firstEvent, secondEvent},
	})

	if err != nil {
		t.Fatalf("Failed to add watcher to event manager: %+v", err)
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

	if err := eventManager.Run(ctx); err != nil {
		t.Errorf("Failed to run event managed, expected success, got error: %+v", err)
	}

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

func TestAddWatcherAfterRun(t *testing.T) {
	firstWatcher := &genericWatcher{
		watcherID:   "first-watcher",
		shouldRenew: true,
	}

	secondWatcher := &genericWatcher{
		watcherID: "second-watcher",
	}

	ctx := context.Background()
	eventManager := newManager()

	err := eventManager.AddWatcher(ctx, firstWatcher)

	if err != nil {
		t.Fatalf("Failed to add watcher to event manager: %+v", err)
	}

	eventManager.Subscribe(firstWatcher.eventID(), nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		if err := eventManager.AddWatcher(ctx, secondWatcher); err != nil {
			t.Errorf("Failed to add a second watcher: %+v, expected success", err)
		}
		firstWatcher.shouldRenew = false
		return false
	})

	var hitSecondEvent bool
	eventManager.Subscribe(secondWatcher.eventID(), nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		hitSecondEvent = true
		return false
	})

	if err := eventManager.Run(ctx); err != nil {
		t.Errorf("Failed to run event managed, expected success, got error: %+v", err)
	}

	if !hitSecondEvent {
		t.Errorf("Failed registering second watcher, expected hitSecondEvent: false, got: %t", hitSecondEvent)
	}
}

type genericWatcher struct {
	watcherID   string
	shouldRenew bool
	wait        time.Duration
}

func (gw *genericWatcher) eventID() string {
	return gw.watcherID + ",test-event"
}

func (gw *genericWatcher) ID() string {
	return gw.watcherID
}

func (gw *genericWatcher) Events() []string {
	return []string{gw.eventID()}
}

func (gw *genericWatcher) Run(ctx context.Context, evType string) (bool, interface{}, error) {
	if gw.wait > 0 {
		time.Sleep(gw.wait)
	}
	return gw.shouldRenew, nil, nil
}

func TestAddDefaultWatchers(t *testing.T) {
	firstWatcher := &genericWatcher{
		watcherID:   "first-watcher",
		shouldRenew: false,
	}

	defaultWatchers = []Watcher{
		firstWatcher,
	}

	ctx := context.Background()
	eventManager := newManager()

	err := eventManager.AddDefaultWatchers(ctx)

	if err != nil {
		t.Fatalf("Failed to add watcher to event manager: %+v", err)
	}

	if len(eventManager.watchersMap) == 0 {
		t.Fatalf("Failed to add default watchers, expected: %d, got: %d", len(defaultWatchers),
			len(eventManager.watchersMap))
	}

	if len(eventManager.watcherEvents) == 0 {
		t.Fatalf("Failed to add default watchers, expected: %d, got: %d", len(defaultWatchers),
			len(eventManager.watcherEvents))
	}
}

func TestCallingRunTwice(t *testing.T) {
	firstWatcher := &genericWatcher{
		watcherID:   "first-watcher",
		shouldRenew: false,
	}

	defaultWatchers = []Watcher{
		firstWatcher,
	}

	timeout := (1 * time.Second) / 100
	ctx, cancel := context.WithCancel(context.Background())
	eventManager := newManager()

	err := eventManager.AddDefaultWatchers(ctx)

	if err != nil {
		t.Fatalf("Failed to add watcher to event manager: %+v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(timeout)
		cancel()
	}()

	errors := []error{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := eventManager.Run(ctx); err != nil {
			errors = append(errors, err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := eventManager.Run(ctx); err != nil {
			errors = append(errors, err)
		}
	}()

	wg.Wait()

	if len(errors) == 0 {
		t.Errorf("Executing Run() twice should fail, we got not failure")
	}

	if len(errors) > 1 {
		t.Errorf("Executing Run() twice should produce a single error, got: %+v", errors)
	}
}

type testRemoveWatcher struct {
	watcherID string
	timeout   time.Duration
}

func (tc *testRemoveWatcher) ID() string {
	return tc.watcherID
}

func (tc *testRemoveWatcher) Events() []string {
	return []string{tc.watcherID + ",test-event"}
}

func (tc *testRemoveWatcher) Run(ctx context.Context, evType string) (bool, interface{}, error) {
	select {
	case <-ctx.Done():
		return false, nil, nil
	case <-time.After(tc.timeout):
		return true, nil, nil
	}
}

func TestRemoveWatcherBeforeCallbacks(t *testing.T) {
	watcherID := "test-watcher"
	timeout := (1 * time.Second) / 100

	ctx := context.Background()
	eventManager := newManager()

	watcher := &testRemoveWatcher{
		watcherID: watcherID,
		timeout:   timeout,
	}

	err := eventManager.AddWatcher(ctx, watcher)

	if err != nil {
		t.Fatalf("Failed to add watcher to event manager: %+v", err)
	}

	eventManager.Subscribe("test-watcher,test-event", nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		t.Errorf("Expected to have canceled before calling callback")
		return false
	})

	go func() {
		time.Sleep(timeout / 2)
		if err := eventManager.RemoveWatcher(ctx, watcher); err != nil {
			t.Errorf("Failed to remove watcher: %+v", err)
		}
	}()

	if err := eventManager.Run(ctx); err != nil {
		t.Errorf("Failed running event manager, expected success, got error: %+v", err)
	}
}

func TestRemoveWatcherFromCallback(t *testing.T) {
	watcher := &genericWatcher{
		watcherID:   "first-watcher",
		shouldRenew: true,
	}

	ctx := context.Background()
	eventManager := newManager()

	err := eventManager.AddWatcher(ctx, watcher)

	if err != nil {
		t.Fatalf("Failed to add watcher to event manager: %+v", err)
	}

	eventManager.Subscribe(watcher.eventID(), nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		if err := eventManager.RemoveWatcher(ctx, watcher); err != nil {
			t.Fatalf("Failed to remove watcher, it should have succeeded: %+v", err)
		}
		return true
	})

	if err := eventManager.Run(ctx); err != nil {
		t.Errorf("Failed running event manager, expected success, got error: %+v", err)
	}
}

func TestCrossWatcherRemovalFromCallback(t *testing.T) {
	firstWatcher := &genericWatcher{
		watcherID:   "first-watcher",
		shouldRenew: true,
	}

	secondWatcher := &genericWatcher{
		watcherID:   "second-watcher",
		shouldRenew: true,
	}

	thirdWatcher := &genericWatcher{
		watcherID:   "third-watcher",
		shouldRenew: true,
		wait:        (1 * time.Second) / 3,
	}

	ctx := context.Background()
	eventManager := newManager()

	watchers := []Watcher{
		firstWatcher,
		secondWatcher,
		thirdWatcher,
	}

	for _, curr := range watchers {
		err := eventManager.AddWatcher(ctx, curr)

		if err != nil {
			t.Fatalf("Failed to add watcher to event manager: %+v", err)
		}
	}

	removed := false
	eventManager.Subscribe(thirdWatcher.eventID(), nil, func(ctx context.Context, evType string, data interface{}, evData *EventData) bool {
		if !removed {
			if err := eventManager.RemoveWatcher(ctx, firstWatcher); err != nil {
				t.Errorf("Failed to remove firstWatcher, it should have succeeded: %+v", err)
			}
			if err := eventManager.RemoveWatcher(ctx, secondWatcher); err != nil {
				t.Errorf("Failed to remove secondWatcher, it should have succeeded: %+v", err)
			}
			removed = true
			return true
		}

		queueLen := eventManager.queue.length()
		if queueLen != 1 {
			t.Errorf("Failed to remove watcher, expected remaining watchers: 1, got: %d", queueLen)
		}

		if err := eventManager.RemoveWatcher(ctx, thirdWatcher); err != nil {
			t.Errorf("Failed to remove thirdWatcher, it should have succeeded: %+v", err)
		}

		return false
	})

	if err := eventManager.Run(ctx); err != nil {
		t.Errorf("Failed running event manager, expected success, got error: %+v", err)
	}
}
