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
	"net"
	"sync"
	"syscall"
	"unsafe"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/Microsoft/go-winio"
)

// DefaultPipePath is the default named pipe path for windows.
const DefaultPipePath = `\\.\pipe\google-guest-agent-network-events`

var (
	addrChangeLock = new(sync.Mutex)
	ipdll          *syscall.DLL
)

func (w Watcher) listen(ctx context.Context, path string) (net.Listener, error) {
	// Winio library does not provide any method to listen on context. Failing to
	// specify a pipeconfig (or using the zero value) results in flaky ACCESS_DENIED
	// errors when re-opening the same pipe (~1/10).
	// https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-createnamedpipea#remarks
	// Even with a pipeconfig, this flakes ~1/200 runs, hence the retry until the
	// context is expired or listen is successful.
	var l net.Listener
	var lastError error
	for {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("context expired: %v before successful listen (last error: %v)", ctx.Err(), lastError)
		}
		l, lastError = winio.ListenPipe(path, &winio.PipeConfig{MessageMode: false, InputBufferSize: 1024, OutputBufferSize: 1024})
		if lastError == nil {
			go func() {
				err := notifyAddrChange(ctx, path)
				if err != nil {
					logger.Errorf("error waiting for address change: %v", err)
				}
			}()
			return l, lastError
		}
	}
}

func notifyAddrChange(ctx context.Context, path string) error {
	addrChangeLock.Lock()
	defer addrChangeLock.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// This function will hang indefinitely until an ip address changes on the
	// system. Calling this repeatedly without the above lock mechanism would
	// likely end up causing a memory leak.
	logger.Debugf("Listening for address change")
	var err error
	if ipdll == nil {
		ipdll, err = syscall.LoadDLL("iphlpapi.dll")
		if err != nil {
			return err
		}
	}
	NotifyAddrChange, err := ipdll.FindProc("NotifyAddrChange")
	if err != nil {
		return err
	}
	// https://learn.microsoft.com/en-us/windows/win32/api/iphlpapi/nf-iphlpapi-notifyaddrchange#remarks
	NotifyAddrChange.Call(uintptr(unsafe.Pointer(nil)), uintptr(unsafe.Pointer(nil)))
	conn, err := winio.DialPipeContext(ctx, path)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(HostnameReconfigureEvent))
	return err
}
