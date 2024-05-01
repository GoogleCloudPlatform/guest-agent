//  Copyright 2024 Google LLC
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

//go:build windows

package hostname

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/command"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"golang.org/x/sys/windows"
)

const (
	platformHostsFile = `C:\Windows\System32\Drivers\etc\hosts`
	newline           = "\r\n"
)

var (
	ipcallbackHandleMu          = new(sync.Mutex)
	ipcallbackHandle            uintptr
	iphlpapi                    = windows.NewLazySystemDLL("iphlpapi.dll")
	procNotifyIpInterfaceChange = iphlpapi.NewProc("NotifyIpInterfaceChange")
	procCancelMibChangeNotify2  = iphlpapi.NewProc("CancelMibChangeNotify2")
)

var setHostname = func(hostname string) error {
	return fmt.Errorf("setting hostnames in guest-agent is not supported on windows")
}

func notifyIpInterfaceChange(family uint32, callbackPtr uintptr, callerContext unsafe.Pointer, initialNotif bool, handle *uintptr) error {
	notify := 0
	if initialNotif {
		notify = 1
	}

	r, _, e := procNotifyIpInterfaceChange.Call(
		uintptr(family),                 // Address family
		callbackPtr,                     // callback ptr
		uintptr(callerContext),          // caller context
		uintptr(notify),                 // call callback immediately after registration
		uintptr(unsafe.Pointer(handle)), // handle for deregistering callback
	)
	if r != 0 {
		return e
	}
	return nil
}

func cancelMibChangeNotify2(handle uintptr) (err error) {
	r, _, e := procCancelMibChangeNotify2.Call(handle)
	if r != 0 {
		return e
	}
	return nil
}

func initPlatform(ctx context.Context) {
	ipcallbackHandleMu.Lock()
	defer ipcallbackHandleMu.Unlock()
	if ipcallbackHandle != 0 {
		logger.Infof("ip callback is already registered")
		return
	}
	callback := func() uintptr {
		logger.Infof("ip interface changed, reconfiguring fqdn")
		req := []byte(fmt.Sprintf(`{"Command":"%s"}`, ReconfigureHostnameCommand))
		b := command.SendCommand(ctx, req)
		logger.Debugf("got response: %s from reconfigure request", b)
		return 0 // Report success
	}
	err := notifyIpInterfaceChange(
		syscall.AF_UNSPEC, //ipv4+6
		syscall.NewCallback(callback),
		nil, // Don't need to pass any caller context
		false,
		&ipcallbackHandle,
	)
	if err != nil {
		logger.Errorf("unable to register callback for ip interface change: %v", err)
	}
}

func closePlatform() {
	ipcallbackHandleMu.Lock()
	defer ipcallbackHandleMu.Unlock()
	if ipcallbackHandle == 0 {
		logger.Infof("ip callback handle is not registered")
		return
	}
	err := cancelMibChangeNotify2(ipcallbackHandle)
	if err != nil {
		logger.Errorf("unable to unregister callback for ip interface change: %v", err)
	}
}

func overwrite(dst string, contents []byte) error {
	stat, err := os.Stat(dst)
	if err != nil {
		return err
	}
	return utils.SaferWriteFile(contents, dst, stat.Mode(), -1, -1)
}
