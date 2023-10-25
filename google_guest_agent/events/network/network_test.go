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

package network

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	// This is a for loop and not a table-driven test because nothing about the
	// input needs to change to test re-opening a second listener on the same pipe
	// name. Without proper synchronization this causes flaky tests.
	for i := 1; i <= 2; i++ {
		t.Run(fmt.Sprintf("run #%d", i), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(300)*time.Millisecond)
			defer cancel()
			pipe, err := getTestPipePath()
			if err != nil {
				t.Fatal(err)
			}
			netw := NewNetworkWatcher(pipe)
			go func() {
				time.Sleep(time.Duration(150) * time.Millisecond)
				conn, err := dialTestPipe(ctx, pipe)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				_, err = conn.Write([]byte(IfaceUpEvent))
				if err != nil {
					t.Errorf("error sending to pipe: %v", err)
				}
			}()
			ok, _, err := netw.Run(ctx, IfaceUpEvent)
			if err != nil {
				t.Errorf("error watching for event: %v", err)
			}
			if !ok {
				t.Error("event was not triggered")
			}
		})
	}
}

func TestRunTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(2)*time.Millisecond)
	defer cancel()
	pipe, err := getTestPipePath()
	if err != nil {
		t.Fatal(err)
	}
	netw := NewNetworkWatcher(pipe)
	ok, _, err := netw.Run(ctx, IfaceUpEvent)
	if err != context.DeadlineExceeded {
		t.Errorf("unexpected error watching for event, got %v want %v", err, context.DeadlineExceeded)
	}
	if ok {
		t.Error("network watcher resubscribed after event timeout")
	}
}
