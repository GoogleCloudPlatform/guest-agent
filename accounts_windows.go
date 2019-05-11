//  Copyright 2017 Google Inc. All Rights Reserved.
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

package main

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	netAPI32                    = windows.NewLazySystemDLL("netapi32.dll")
	procNetUserAdd              = netAPI32.NewProc("NetUserAdd")
	procNetUserGetInfo          = netAPI32.NewProc("NetUserGetInfo")
	procNetUserSetInfo          = netAPI32.NewProc("NetUserSetInfo")
	procNetLocalGroupAddMembers = netAPI32.NewProc("NetLocalGroupAddMembers")
)

type (
	DWORD  uint32
	LPWSTR *uint16

	USER_INFO_0 struct {
		Usri0_name LPWSTR
	}

	USER_INFO_1 struct {
		Usri1_name         LPWSTR
		Usri1_password     LPWSTR
		Usri1_password_age DWORD
		Usri1_priv         DWORD
		Usri1_home_dir     LPWSTR
		Usri1_comment      LPWSTR
		Usri1_flags        DWORD
		Usri1_script_path  LPWSTR
	}

	LOCALGROUP_MEMBERS_INFO_0 struct {
		Lgrmi0_sid *syscall.SID
	}

	USER_INFO_1003 struct {
		Usri1003_password LPWSTR
	}
)

const (
	USER_PRIV_GUEST = 0
	USER_PRIV_USER  = 1
	USER_PRIV_ADMIN = 2

	UF_SCRIPT                          = 0x0001
	UF_ACCOUNTDISABLE                  = 0x0002
	UF_HOMEDIR_REQUIRED                = 0x0008
	UF_LOCKOUT                         = 0x0010
	UF_PASSWD_NOTREQD                  = 0x0020
	UF_PASSWD_CANT_CHANGE              = 0x0040
	UF_ENCRYPTED_TEXT_PASSWORD_ALLOWED = 0x0080

	UF_TEMP_DUPLICATE_ACCOUNT    = 0x0100
	UF_NORMAL_ACCOUNT            = 0x0200
	UF_INTERDOMAIN_TRUST_ACCOUNT = 0x0800
	UF_WORKSTATION_TRUST_ACCOUNT = 0x1000
	UF_SERVER_TRUST_ACCOUNT      = 0x2000

	UF_DONT_EXPIRE_PASSWD                     = 0x10000
	UF_MNS_LOGON_ACCOUNT                      = 0x20000
	UF_SMARTCARD_REQUIRED                     = 0x40000
	UF_TRUSTED_FOR_DELEGATION                 = 0x80000
	UF_NOT_DELEGATED                          = 0x100000
	UF_USE_DES_KEY_ONLY                       = 0x200000
	UF_DONT_REQUIRE_PREAUTH                   = 0x400000
	UF_PASSWORD_EXPIRED                       = 0x800000
	UF_TRUSTED_TO_AUTHENTICATE_FOR_DELEGATION = 0x1000000
	UF_NO_AUTH_DATA_REQUIRED                  = 0x2000000
	UF_PARTIAL_SECRETS_ACCOUNT                = 0x4000000
	UF_USE_AES_KEYS                           = 0x8000000
)

func resetPwd(username, pwd string) error {
	uPtr, err := syscall.UTF16PtrFromString(username)
	if err != nil {
		return fmt.Errorf("error encoding username to UTF16: %v", err)
	}
	pPtr, err := syscall.UTF16PtrFromString(pwd)
	if err != nil {
		return fmt.Errorf("error encoding password to UTF16: %v", err)
	}

	ret, _, _ := procNetUserSetInfo.Call(
		uintptr(0),
		uintptr(unsafe.Pointer(uPtr)),
		uintptr(1003),
		uintptr(unsafe.Pointer(&USER_INFO_1003{pPtr})),
		uintptr(0))
	if ret != 0 {
		return fmt.Errorf("nonzero return code from NetUserSetInfo: %d", ret)
	}
	return nil
}

func addToGroup(username, group string) error {
	gPtr, err := syscall.UTF16PtrFromString(group)
	if err != nil {
		return fmt.Errorf("error encoding group to UTF16: %v", err)
	}

	sid, _, _, err := syscall.LookupSID("", username)
	if err != nil {
		return err
	}

	sArray := []LOCALGROUP_MEMBERS_INFO_0{{sid}}
	ret, _, _ := procNetLocalGroupAddMembers.Call(
		uintptr(0),
		uintptr(unsafe.Pointer(gPtr)),
		uintptr(0),
		uintptr(unsafe.Pointer(&sArray[0])),
		uintptr(1),
	)

	if ret != 0 {
		return fmt.Errorf("nonzero return code from NetLocalGroupAddMembers: %d", ret)
	}
	return nil
}

func createAdminUser(username, pwd string) error {
	uPtr, err := syscall.UTF16PtrFromString(username)
	if err != nil {
		return fmt.Errorf("error encoding username to UTF16: %v", err)
	}
	pPtr, err := syscall.UTF16PtrFromString(pwd)
	if err != nil {
		return fmt.Errorf("error encoding password to UTF16: %v", err)
	}

	uInfo1 := USER_INFO_1{
		Usri1_name:     uPtr,
		Usri1_password: pPtr,
		Usri1_priv:     USER_PRIV_USER,
		Usri1_flags:    UF_SCRIPT | UF_NORMAL_ACCOUNT | UF_DONT_EXPIRE_PASSWD,
	}
	ret, _, _ := procNetUserAdd.Call(
		uintptr(0),
		uintptr(1),
		uintptr(unsafe.Pointer(&uInfo1)),
		uintptr(0),
	)
	if ret != 0 {
		return fmt.Errorf("nonzero return code from NetUserAdd: %d", ret)
	}
	return addToGroup(username, "Administrators")
}

func userExists(name string) (bool, error) {
	uPtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return false, fmt.Errorf("error encoding username to UTF16: %v", err)
	}
	ret, _, _ := procNetUserGetInfo.Call(
		uintptr(0),
		uintptr(unsafe.Pointer(uPtr)),
		uintptr(1),
		uintptr(unsafe.Pointer(&USER_INFO_0{})),
	)
	if ret != 0 {
		return false, fmt.Errorf("nonzero return code from NetUserGetInfo: %d", ret)
	}

	return true, nil
}
