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

package sshtrustedca

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

// Create a named pipe if it doesn't exist.
func createNamedPipe(ctx context.Context, pipePath string) error {
	pipeDir := filepath.Dir(pipePath)
	_, err := os.Stat(pipeDir)

	if err != nil && os.IsNotExist(err) {
		// The perm 0755 is compatible with distros /etc/ssh/ directory.
		if err := os.MkdirAll(pipeDir, 0755); err != nil {
			return err
		}
	}

	if _, err := os.Stat(pipePath); err != nil {
		if os.IsNotExist(err) {
			if err := syscall.Mkfifo(pipePath, 0644); err != nil {
				return fmt.Errorf("failed to create named pipe: %+v", err)
			}
		} else {
			return fmt.Errorf("failed to stat file: " + pipePath)
		}
	}

	restorecon, err := exec.LookPath("restorecon")
	if err != nil {
		logger.Infof("No restorecon available, not restoring SELinux context of: %s", pipePath)
		return nil
	}

	return run.Quiet(ctx, restorecon, pipePath)
}

// finishedCb is used by the event handler to communicate the write to the
// pipe is finised, it's exposed via PipeData.Finished pointer.
func (mp *Watcher) finishedCb() {
	mp.setWaitingWrite(false)
}

func (mp *Watcher) isWaitingWrite() bool {
	mp.mutex.Lock()
	defer mp.mutex.Unlock()
	return mp.waitingWrite
}

func (mp *Watcher) setWaitingWrite(val bool) {
	mp.mutex.Lock()
	defer mp.mutex.Unlock()
	mp.waitingWrite = val
}

// Run listens to ssh_trusted_ca's pipe open calls and report back the event.
func (mp *Watcher) Run(ctx context.Context, evType string) (bool, interface{}, error) {
	var canceled bool

	for mp.isWaitingWrite() {
		time.Sleep(10 * time.Millisecond)
	}

	// Channel used to cancel the context cancelation go routine.
	// Used when the Watcher is returning to the event manager.
	cancelContext := make(chan bool)
	defer close(cancelContext)

	// Cancelation handling code.
	go func() {
		select {
		case <-cancelContext:
			break
		case <-ctx.Done():
			canceled = true

			// Open the pipe as O_RDONLY to release the blocking open O_WRONLY.
			pipeFile, err := os.OpenFile(mp.pipePath, os.O_RDONLY, 0644)
			if err != nil {
				logger.Errorf("Failed to open readonly pipe: %+v", err)
				return
			}

			defer func() {
				if err := pipeFile.Close(); err != nil {
					logger.Errorf("Failed to close readonly pipe: %+v", err)
				}
			}()
		}
	}()

	// If the configured named pipe doesn't exists we create it before emitting events
	// from it.
	if err := createNamedPipe(ctx, mp.pipePath); err != nil {
		return true, nil, err
	}

	// Open the pipe as writeonly, it will block until a read is performed from the
	// other end of the pipe.
	pipeFile, err := os.OpenFile(mp.pipePath, os.O_WRONLY, 0644)
	if err != nil {
		return true, nil, err
	}

	// Have we got a ctx.Done()? if so lets just return from here and unregister
	// the watcher.
	if canceled {
		if err := pipeFile.Close(); err != nil {
			logger.Errorf("Failed to close readonly pipe: %+v", err)
		}
		return false, nil, nil
	}

	cancelContext <- true
	mp.setWaitingWrite(true)

	return true, &PipeData{File: pipeFile, Finished: mp.finishedCb}, nil
}
