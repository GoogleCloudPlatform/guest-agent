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

// Package uefi provides utility functions to read UEFI variables.
package uefi

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	// https://en.wikipedia.org/wiki/Microsoft_Windows_library_files#KERNEL32.DLL
	kernelDLL = windows.NewLazySystemDLL("kernel32.dll")
	// https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-getcurrentprocess
	// procGetCurrentProcess retrieves a pseudo handle for the current process.
	procGetCurrentProcess = kernelDLL.NewProc("GetCurrentProcess")
	// https://learn.microsoft.com/en-us/windows/win32/api/handleapi/nf-handleapi-closehandle
	// procCloseHandle closes an open process object handle.
	procCloseHandle = kernelDLL.NewProc("CloseHandle")
	// https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-getfirmwareenvironmentvariablew
	// procGetFirmwareEnvironmentVariableW retrieves the value of the specified UEFI.
	procGetFirmwareEnvironmentVariableW = kernelDLL.NewProc("GetFirmwareEnvironmentVariableW")

	// https://en.wikipedia.org/wiki/Microsoft_Windows_library_files#ADVAPI32.DLL
	advapiDLL = windows.NewLazySystemDLL("advapi32.dll")
	// https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-openprocesstoken
	// procOpenProcessToken opens the access token (contains the security information for a logon session) associated for a process.
	// Token identifies the user, the user's groups, and the user's privileges.
	procOpenProcessToken = advapiDLL.NewProc("OpenProcessToken")
	// https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-lookupprivilegevaluew
	// procLookupPrivilegeValueW is used to retrieve the locally unique identifier (LUID)
	procLookupPrivilegeValueW = advapiDLL.NewProc("LookupPrivilegeValueW")
	// https://learn.microsoft.com/en-us/windows/win32/api/securitybaseapi/nf-securitybaseapi-adjusttokenprivileges
	// procAdjustTokenPrivileges is used for enabling the privileges on the access token.
	procAdjustTokenPrivileges = advapiDLL.NewProc("AdjustTokenPrivileges")
)

const (
	// SE_SYSTEM_ENVIRONMENT_NAME is the privilege required to read a firmware environment variable.
	SE_SYSTEM_ENVIRONMENT_NAME = "SeSystemEnvironmentPrivilege"
	// PROC_TOKEN_ADJUST_PRIVILEGES is access required to change the specified privileges.
	PROC_TOKEN_ADJUST_PRIVILEGES = 0x0020
	// PROC_SE_PRIVILEGE_ENABLED is privilege attribute used with LUID_AND_ATTRIBUTES stating to
	// enable the specified privilege.
	PROC_SE_PRIVILEGE_ENABLED = 0x00000002
)

// https://learn.microsoft.com/en-us/windows/win32/api/ntdef/ns-ntdef-luid
// LUID is the opaque identifier structure that is guaranteed to be unique on the local machine.
// It is used to locally represent the privilege name (e.g. SeSystemEnvironmentPrivilege in this case).
type LUID struct {
	LowPart  uint32
	HighPart int32
}

// https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-luid_and_attributes
// LUID_AND_ATTRIBUTES structure represents a locally unique identifier (LUID) and its attributes.
type LUID_AND_ATTRIBUTES struct {
	LUID       LUID
	Attributes uint32
}

// https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-token_privileges
// TOKEN_PRIVILEGES is the structure that contains information about a set of privileges for an access token.
type TOKEN_PRIVILEGES struct {
	PrivilegeCount uint32
	Privileges     [1]LUID_AND_ATTRIBUTES
}

// ReadVariable reads UEFI variable and returns as byte array.
// Throws an error if variable is invalid or empty.
func ReadVariable(v VariableName) (*Variable, error) {
	logger.Debugf("Enabling required %s priviliges for agent process", SE_SYSTEM_ENVIRONMENT_NAME)
	if err := enablePrivilege(SE_SYSTEM_ENVIRONMENT_NAME); err != nil {
		return nil, err
	}

	name := unsafe.Pointer(syscall.StringToUTF16Ptr(v.Name))
	guid := unsafe.Pointer(syscall.StringToUTF16Ptr("{" + v.GUID + "}"))

	buffer := make([]byte, 1024)

	// This call returns number of bytes written to the output buffer, unused, error
	size, _, err := procGetFirmwareEnvironmentVariableW.Call(
		uintptr(name),
		uintptr(guid),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(uint32(len(buffer))),
	)

	if size == uintptr(0) {
		return nil, fmt.Errorf("unable to read UEFI variable %+v: %w", v, err)
	}

	return &Variable{
		Name:       v,
		Attributes: []byte{},
		Content:    buffer[:size],
	}, nil
}

// enablePrivilege enables the specified privilege for current process.
func enablePrivilege(name string) error {
	// Get current process handle.
	handle, _, err := procGetCurrentProcess.Call()
	if handle == uintptr(0) {
		return fmt.Errorf("unable to get current process handle: %w", err)
	}
	defer procCloseHandle.Call(handle)

	// Get access token that contains the privileges to be modified for the current process.
	var tHandle uintptr
	opRes, _, err := procOpenProcessToken.Call(
		uintptr(handle),
		uintptr(uint32(PROC_TOKEN_ADJUST_PRIVILEGES)),
		uintptr(unsafe.Pointer(&tHandle)),
	)
	if opRes == uintptr(0) {
		return fmt.Errorf("unable to open current process token: %w", err)
	}
	defer procCloseHandle.Call(tHandle)

	// Generate a pointer to a null-terminated string that specifies the name of the privilege.
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return fmt.Errorf("unable to encode privilege name(%s) to UTF16: %w", name, err)
	}

	// Retrieve the LUID for the required privilege.
	var luid LUID
	lpRes, _, err := procLookupPrivilegeValueW.Call(
		uintptr(0),
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(&luid)),
	)
	if lpRes == uintptr(0) {
		return fmt.Errorf("unable to lookup LUID for privilege %q: %w", name, err)
	}

	newState := TOKEN_PRIVILEGES{PrivilegeCount: 1}

	newState.Privileges[0] = LUID_AND_ATTRIBUTES{
		LUID:       luid,
		Attributes: PROC_SE_PRIVILEGE_ENABLED,
	}

	// Enable specified privilege on the current process.
	ajRes, _, err := procAdjustTokenPrivileges.Call(
		uintptr(tHandle),
		uintptr(uint32(0)),
		uintptr(unsafe.Pointer(&newState)),
		uintptr(uint32(0)),
		uintptr(0),
		uintptr(0),
	)
	if ajRes == uintptr(0) {
		return fmt.Errorf("unable to set privilege %q: %w", name, err)
	}

	return nil
}
