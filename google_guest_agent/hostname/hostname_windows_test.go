//  Copyright 2024 Google LLC.
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
	"syscall"
	"testing"
)

func TestNotifyIpInterfaceChange(t *testing.T) {
	var handle uintptr
	var callbackExecuted bool
	callback := func() uintptr {
		callbackExecuted = true
		return 0
	}
	if err := notifyIpInterfaceChange(syscall.AF_UNSPEC, syscall.NewCallback(callback), nil, true, &handle); err != nil {
		t.Errorf("failed to register callback: %v", err)
	}
	if handle == 0 {
		t.Error("notification handle is nil after registering callback")
	}
	if !callbackExecuted {
		t.Errorf("callback was not executed, callbackExecuted = %v", callbackExecuted)
	}
	if err := cancelMibChangeNotify2(handle); err != nil {
		t.Errorf("failed to unregister callback: %v", err)
	}
}