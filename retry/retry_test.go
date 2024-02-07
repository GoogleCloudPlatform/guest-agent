// Copyright 2024 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package retry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRetry(t *testing.T) {
	ctx := context.Background()
	ctr := 0

	fn := func() error {
		ctr++
		if ctr == 2 {
			return nil
		}
		return fmt.Errorf("fake error")
	}

	policy := Policy{MaxAttempts: 5, BackoffFactor: 2, Jitter: time.Millisecond}

	if err := Run(ctx, policy, fn); err != nil {
		t.Errorf("Retry(ctx, %+v, fn) failed unexpectedly, err: %+v", policy, err)
	}

	want := 2
	if ctr != want {
		t.Errorf("Retry(ctx, %+v, fn) retried %d times, should've returned after %d retries", policy, ctr, want)
	}
}

func TestRetryError(t *testing.T) {
	ctx := context.Background()
	ctr := 0

	fn := func() error {
		ctr++
		return fmt.Errorf("fake error")
	}

	policy := Policy{MaxAttempts: 4, BackoffFactor: 1, Jitter: time.Millisecond * 2}

	if err := Run(ctx, policy, fn); err == nil {
		t.Errorf("Retry(ctx, %+v, fn) succeded, want error", policy)
	}

	// Max retry attempts error.
	if ctr != policy.MaxAttempts {
		t.Errorf("Retry(ctx, %+v, fn) retried %d times, should've returned after %d retries", policy, ctr, policy.MaxAttempts)
	}

	// Zero attempts error.
	zeroPolicy := Policy{MaxAttempts: 0, BackoffFactor: 1, Jitter: time.Millisecond * 2}

	if err := Run(ctx, zeroPolicy, fn); err == nil {
		t.Errorf("Retry(ctx, %+v, fn) succeded, want zero attempts error", zeroPolicy)
	}

	// Emtpy function error.
	if err := Run(ctx, policy, nil); err == nil {
		t.Errorf("Retry(ctx, %+v, nil) succeded, want nil function error", policy)
	}

	// Context cancelled error.
	c, cancel := context.WithTimeout(ctx, time.Microsecond)
	cancel()
	if err := Run(c, policy, fn); err == nil {
		t.Errorf("Retry(ctx, %+v, fn) succeded, want context error", policy)
	}
}

func TestRetryWithResponse(t *testing.T) {
	ctx := context.Background()
	ctr := 0

	fn := func() (int, error) {
		ctr++
		if ctr == 2 {
			return ctr, nil
		}
		return -1, fmt.Errorf("fake error")
	}

	policy := Policy{MaxAttempts: 5, BackoffFactor: 1, Jitter: time.Millisecond}
	want := 2
	got, err := RunWithResponse(ctx, policy, fn)
	if err != nil {
		t.Errorf("RetryWithResponse(ctx, %+v, fn) failed unexpectedly, err: %+v", policy, err)
	}
	if got != want {
		t.Errorf("RetryWithResponse(ctx, %+v, fn) = %d, want %d", policy, got, want)
	}
	if ctr != want {
		t.Errorf("RetryWithResponse(ctx, %+v, fn) retried %d times, should've returned after %d retries", policy, ctr, want)
	}
}

func TestBackoff(t *testing.T) {
	tests := []struct {
		name     string
		factor   float64
		attempts int
		jitter   time.Duration
		want     []time.Duration
	}{
		{
			name:     "constant_backoff",
			factor:   1,
			attempts: 5,
			jitter:   time.Duration(10),
			want:     []time.Duration{10, 10, 10, 10, 10},
		},
		{
			name:     "exponential_backoff_2",
			factor:   2,
			attempts: 4,
			jitter:   time.Duration(10),
			want:     []time.Duration{10, 20, 40, 80},
		},
		{
			name:     "exponential_backoff_3",
			factor:   3,
			attempts: 4,
			jitter:   time.Duration(10),
			want:     []time.Duration{10, 30, 90, 270},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := Policy{MaxAttempts: tt.attempts, BackoffFactor: tt.factor, Jitter: tt.jitter}
			for i := 0; i < tt.attempts; i++ {
				if got := backoff(i, policy); got != tt.want[i] {
					t.Errorf("backoff(%d, %+v) = %d, want %d", i, policy, got, tt.want[i])
				}
			}
		})
	}
}

func TestIsRetriable(t *testing.T) {
	// Fake ShouldRetry() override.
	f := func(err error) bool {
		return !errors.Is(err, context.DeadlineExceeded)
	}

	tests := []struct {
		name   string
		err    error
		policy Policy
		want   bool
	}{
		{
			name: "no_override",
			want: true,
		},
		{
			name:   "override_no_retry",
			err:    context.DeadlineExceeded,
			policy: Policy{ShouldRetry: f},
			want:   false,
		},
		{
			name:   "override_retry",
			err:    fmt.Errorf("fake retriable error"),
			policy: Policy{ShouldRetry: f},
			want:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetriable(tt.policy, tt.err); got != tt.want {
				t.Errorf("isRetriable(%+v, %+v) = %t, want %t", tt.policy, tt.err, got, tt.want)
			}
		})
	}
}
