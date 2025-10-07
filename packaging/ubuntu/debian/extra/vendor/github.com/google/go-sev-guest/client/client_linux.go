// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build linux || freebsd || openbsd || netbsd

// Package client provides an interface to the AMD SEV-SNP guest device commands.
package client

import (
	"flag"
	"fmt"
	"time"

	labi "github.com/google/go-sev-guest/client/linuxabi"
	"golang.org/x/sys/unix"
)

const (
	// defaultSevGuestDevicePath is the platform's usual device path to the SEV guest.
	defaultSevGuestDevicePath = "/dev/sev-guest"
	installURL                = "https://github.com/google/go-sev-guest/blob/main/INSTALL.md"
)

// These flags should not be needed for long term health of the project as the Linux kernel
// catches up with throttling-awareness.
var (
	throttleDuration = flag.Duration("self_throttle_duration", 2*time.Second, "Rate-limit library-initiated device commands to this duration")
	burstMax         = flag.Int("self_throttle_burst", 1, "Rate-limit library-initiated device commands to this many commands per duration")
)

// LinuxDevice implements the Device interface with Linux ioctls.
type LinuxDevice struct {
	fd      int
	lastCmd time.Time
	burst   int
}

// Open opens the SEV-SNP guest device from a given path
func (d *LinuxDevice) Open(path string) error {
	fd, err := unix.Open(path, unix.O_RDWR, 0)
	if err != nil {
		d.fd = -1
		return fmt.Errorf("could not open AMD SEV guest device at %s (see %s): %v", path, installURL, err)
	}
	d.fd = fd
	return nil
}

// OpenDevice opens the SEV-SNP guest device.
func OpenDevice() (*LinuxDevice, error) {
	result := &LinuxDevice{}
	path := *sevGuestPath
	if UseDefaultSevGuest() {
		path = defaultSevGuestDevicePath
	}
	if err := result.Open(path); err != nil {
		return nil, err
	}
	return result, nil
}

// Close closes the SEV-SNP guest device.
func (d *LinuxDevice) Close() error {
	if d.fd == -1 { // Not open
		return nil
	}
	if err := unix.Close(d.fd); err != nil {
		return err
	}
	// Prevent double-close.
	d.fd = -1
	return nil
}

// Ioctl sends a command with its wrapped request and response values to the Linux device.
func (d *LinuxDevice) Ioctl(command uintptr, req any) (uintptr, error) {
	// TODO(Issue #40): Remove the workaround to the ENOTTY lockout when throttled
	// in Linux 6.1 by throttling ourselves first.
	if d.burst == 0 {
		sinceLast := time.Since(d.lastCmd)
		// Self-throttle for tests without guest OS throttle detection
		if sinceLast < *throttleDuration {
			time.Sleep(*throttleDuration - sinceLast)
		}
	}
	switch sreq := req.(type) {
	case *labi.SnpUserGuestRequest:
		abi := sreq.ABI()
		result, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd), command, uintptr(abi.Pointer()))
		abi.Finish(sreq)
		d.burst = (d.burst + 1) % *burstMax
		if d.burst == 0 {
			d.lastCmd = time.Now()
		}

		// TODO(Issue #5): remove the work around for the kernel bug that writes
		// uninitialized memory back on non-EIO.
		if errno != unix.EIO {
			sreq.FwErr = 0
		}
		if errno != 0 {
			return 0, errno
		}
		return result, nil
	}
	return 0, fmt.Errorf("unexpected request value: %v", req)
}
