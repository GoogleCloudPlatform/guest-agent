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

// Package sshtrustedca implement the sshd trusted ca cert pipe events watcher.
package sshtrustedca

import (
	"os"
	"sync"
)

const (
	// WatcherID is the sshtrustedca watcher's ID.
	WatcherID = "ssh-trusted-ca-pipe-watcher"
	// ReadEvent is the sshtrustedca's read event type ID.
	ReadEvent = "ssh-trusted-ca-pipe-watcher,read"
	// DefaultPipePath defines the default ssh trusted ca pipe path.
	DefaultPipePath = "/etc/ssh/oslogin_trustedca.pub"
)

// Watcher is the sshtrustedca event watcher implementation.
type Watcher struct {
	// pipePath points to the named pipe it's writing to.
	pipePath string

	// waitingWrite is a flag to inform the Watcher that the Handler has or
	// hasn't finished writing.
	waitingWrite bool

	// mutex protects waitingWrite on concurrent accesses.
	mutex sync.Mutex
}

// PipeData wraps the pipe event data.
type PipeData struct {
	// File is the writeonly pipe's file descriptor. The user/handler must
	// make sure to close it after processing the event.
	File *os.File

	// Finished is a callback used by the event handler to inform the write to
	// the pipe is finished.
	Finished func()
}

// New allocates and initializes a new Watcher.
func New(pipePath string) *Watcher {
	return &Watcher{
		pipePath: pipePath,
	}
}

// ID returns the sshtrustedca event watcher id.
func (mp *Watcher) ID() string {
	return WatcherID
}

// Events returns an slice with all implemented events.
func (mp *Watcher) Events() []string {
	return []string{ReadEvent}
}
