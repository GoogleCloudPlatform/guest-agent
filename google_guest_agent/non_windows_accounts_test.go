// Copyright 2026 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !windows

package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// withStubUserExists replaces userExistsFunc for the duration of a test and
// restores it afterwards.
func withStubUserExists(t *testing.T, stub func(string) (bool, error)) {
	t.Helper()
	orig := userExistsFunc
	userExistsFunc = stub
	t.Cleanup(func() { userExistsFunc = orig })
}

// withFastPolling shrinks the poll interval so tests don't have to wait out
// the real production backoff, and restores the original values afterwards.
func withFastPolling(t *testing.T, attempts int) {
	t.Helper()
	origInterval, origAttempts := userVisiblePollInterval, userVisiblePollAttempts
	userVisiblePollInterval = time.Millisecond
	userVisiblePollAttempts = attempts
	t.Cleanup(func() {
		userVisiblePollInterval = origInterval
		userVisiblePollAttempts = origAttempts
	})
}

func TestWaitForUserVisibleImmediateSuccess(t *testing.T) {
	calls := 0
	withStubUserExists(t, func(string) (bool, error) {
		calls++
		return true, nil
	})
	withFastPolling(t, 5)

	if err := waitForUserVisible(context.Background(), "testuser"); err != nil {
		t.Fatalf("waitForUserVisible() = %v, want nil", err)
	}
	if calls != 1 {
		t.Errorf("userExistsFunc called %d times, want 1 (no retry needed)", calls)
	}
}

func TestWaitForUserVisibleEventualSuccess(t *testing.T) {
	calls := 0
	withStubUserExists(t, func(string) (bool, error) {
		calls++
		if calls < 3 {
			// Mirrors the observed nscd race: the account was just
			// created but a lookup transiently reports it missing.
			return false, errors.New("user: unknown user testuser")
		}
		return true, nil
	})
	withFastPolling(t, 5)

	if err := waitForUserVisible(context.Background(), "testuser"); err != nil {
		t.Fatalf("waitForUserVisible() = %v, want nil", err)
	}
	if calls != 3 {
		t.Errorf("userExistsFunc called %d times, want 3", calls)
	}
}

func TestWaitForUserVisibleNeverVisible(t *testing.T) {
	calls := 0
	wantErr := errors.New("user: unknown user testuser")
	withStubUserExists(t, func(string) (bool, error) {
		calls++
		return false, wantErr
	})
	withFastPolling(t, 4)

	err := waitForUserVisible(context.Background(), "testuser")
	if err == nil {
		t.Fatal("waitForUserVisible() = nil, want error")
	}
	if calls != 4 {
		t.Errorf("userExistsFunc called %d times, want 4 (userVisiblePollAttempts)", calls)
	}
}

func TestWaitForUserVisibleContextCancelled(t *testing.T) {
	withStubUserExists(t, func(string) (bool, error) {
		return false, errors.New("not yet")
	})
	withFastPolling(t, 5)
	// Use a real (tiny) interval so the context has something to race
	// against, otherwise the immediate Done() would be indistinguishable
	// from a slow stub.
	userVisiblePollInterval = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := waitForUserVisible(ctx, "testuser")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForUserVisible() = %v, want context.Canceled", err)
	}
}
