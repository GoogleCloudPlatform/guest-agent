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

// Package retry implements retry logic helpers to execute arbitrary functions with defined policy.
package retry

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

// IsRetriable is method signature for implementing to override default logic of retrying each error.
type IsRetriable func(error) bool

// Policy represents the struct to configure the retry behavior.
type Policy struct {
	// MaxAttempts represents the maximum number of retry attempts.
	MaxAttempts int
	// BackoffFactor is the multiplier by which retry interval (Jitter) increases after each retry.
	// For constant backoff set Backoff factor to 1.
	BackoffFactor float64
	// Jitter is the interval before the first retry.
	Jitter time.Duration
	// ShouldRetry is optional and the way to override default retry logic of retry every error.
	// If ShouldRetry is not provided/implemented every error will be retried until all attempts are exhausted.
	ShouldRetry IsRetriable
}

// backoff computes interval between retries. Interval is jitter*(backoffFactor^attempt).
// For e.g. if jitter was set to 10 and factor was 3, backoff between attempts would be [10, 30, 90, 270...].
func backoff(attempt int, policy Policy) time.Duration {
	b := float64(policy.Jitter) * math.Pow(policy.BackoffFactor, float64(attempt))
	return time.Duration(b)
}

// isRetriable checks if error is retriable. If ShouldRetry is unimplemented it always returns
// true, otherwise overriden method's logic determines the retry behavior.
func isRetriable(policy Policy, err error) bool {
	if policy.ShouldRetry == nil {
		return true
	}
	return policy.ShouldRetry(err)
}

// RunWithResponse executes and retries the function on failure based on policy defined and returns response on success.
func RunWithResponse[T any](ctx context.Context, policy Policy, f func() (T, error)) (T, error) {
	var (
		res T
		err error
	)

	if f == nil {
		return res, fmt.Errorf("retry function cannot be nil")
	}

	for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
		if res, err = f(); err == nil {
			return res, nil
		}

		if err != nil && !isRetriable(policy, err) {
			return res, fmt.Errorf("giving up, retry policy returned false on error: %+v", err)
		}

		logger.Debugf("Attempt %d failed with error %+v", attempt, err)

		// Return early, no need to wait if all retries have exhausted.
		if attempt+1 >= policy.MaxAttempts {
			return res, fmt.Errorf("exhausted all (%d) retries, last error: %+v", policy.MaxAttempts, err)
		}

		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(backoff(attempt, policy)):
		}
	}
	return res, fmt.Errorf("num of retries set to 0, made no attempts to run")
}

// Run executes and retries the function on failure based on policy defined and returns nil-error on success.
func Run(ctx context.Context, policy Policy, f func() error) error {
	if f == nil {
		return fmt.Errorf("retry function cannot be nil")
	}

	fn := func() (any, error) {
		return nil, f()
	}
	_, err := RunWithResponse(ctx, policy, fn)
	return err
}
