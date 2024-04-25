//go:build windows

package hostname

import (
	"syscall"
	"testing"
	"time"
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
	time.Sleep(1 * time.Second)
	if !callbackExecuted {
		t.Errorf("callback was not executed, callbackExecuted = %v", callbackExecuted)
	}
	if err := cancelMibChangeNotify2(handle); err != nil {
		t.Errorf("failed to unregister callback: %v", err)
	}
}
