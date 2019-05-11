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
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ipHlpAPI            = windows.NewLazySystemDLL("iphlpapi.dll")
	procAddIPAddress    = ipHlpAPI.NewProc("AddIPAddress")
	procDeleteIPAddress = ipHlpAPI.NewProc("DeleteIPAddress")

	procCreateUnicastIpAddressEntry     = ipHlpAPI.NewProc("CreateUnicastIpAddressEntry")
	procInitializeUnicastIpAddressEntry = ipHlpAPI.NewProc("InitializeUnicastIpAddressEntry")
	procGetUnicastIpAddressEntry        = ipHlpAPI.NewProc("GetUnicastIpAddressEntry")
	procDeleteUnicastIpAddressEntry     = ipHlpAPI.NewProc("DeleteUnicastIpAddressEntry")
)

const (
	AF_NET   = 2
	AF_INET6 = 23
)

type in_addr struct {
	S_un struct {
		S_addr uint32
	}
}

type SOCKADDR_IN struct {
	sin_family int16
	sin_addr   in_addr
}

type SOCKADDR_INET struct {
	Ipv4      SOCKADDR_IN
	si_family int16
}

type NET_LUID struct {
	Value uint64
	Info  struct {
		NetLuidIndex uint64
		IfType       uint64
	}
}

type MIB_UNICASTIPADDRESS_ROW struct {
	Address            SOCKADDR_INET
	InterfaceLuid      NET_LUID
	InterfaceIndex     uint32
	PrefixOrigin       uint32
	SuffixOrigin       uint32
	ValidLifetime      uint32
	PreferredLifetime  uint32
	OnLinkPrefixLength uint8
	SkipAsSource       bool
}

func addAddress(ip, mask net.IP, index uint32) error {
	// CreateUnicastIpAddressEntry only available Vista onwards.
	if err := procCreateUnicastIpAddressEntry.Find(); err != nil {
		return addIPAddress(ip, mask, index)
	}
	return createUnicastIpAddressEntry(ip, 32, index)
}

func removeAddress(ip net.IP, index uint32) error {
	// DeleteUnicastIpAddressEntry only available Vista onwards.
	if err := procDeleteUnicastIpAddressEntry.Find(); err != nil {
		return deleteIPAddress(ip)
	}
	return deleteUnicastIpAddressEntry(ip, index)
}

func createUnicastIpAddressEntry(ip net.IP, prefix uint8, index uint32) error {
	ipRow := new(MIB_UNICASTIPADDRESS_ROW)
	// No return value.
	procInitializeUnicastIpAddressEntry.Call(uintptr(unsafe.Pointer(ipRow)))

	ipRow.InterfaceIndex = index
	ipRow.OnLinkPrefixLength = prefix
	// https://blogs.technet.microsoft.com/rmilne/2012/02/08/fine-grained-control-when-registering-multiple-ip-addresses-on-a-network-card/
	ipRow.SkipAsSource = true
	ipRow.Address.si_family = AF_NET
	ipRow.Address.Ipv4.sin_family = AF_NET
	ipRow.Address.Ipv4.sin_addr.S_un.S_addr = binary.LittleEndian.Uint32(ip.To4())

	if ret, _, _ := procCreateUnicastIpAddressEntry.Call(uintptr(unsafe.Pointer(ipRow))); ret != 0 {
		return fmt.Errorf("nonzero return code from CreateUnicastIpAddressEntry: %d", ret)
	}
	return nil
}

func deleteUnicastIpAddressEntry(ip net.IP, index uint32) error {
	ipRow := new(MIB_UNICASTIPADDRESS_ROW)

	ipRow.InterfaceIndex = index
	ipRow.Address.si_family = AF_NET
	ipRow.Address.Ipv4.sin_family = AF_NET
	ipRow.Address.Ipv4.sin_addr.S_un.S_addr = binary.LittleEndian.Uint32(ip.To4())

	ret, _, _ := procGetUnicastIpAddressEntry.Call(uintptr(unsafe.Pointer(ipRow)))

	// ERROR_NOT_FOUND
	if ret == 1168 {
		// This address was added by addIPAddress(), need to remove with deleteIPAddress()
		return deleteIPAddress(ip)
	}

	if ret != 0 {
		return fmt.Errorf("nonzero return code from GetUnicastIpAddressEntry: %d", ret)
	}

	if ret, _, _ := procDeleteUnicastIpAddressEntry.Call(uintptr(unsafe.Pointer(ipRow))); ret != 0 {
		return fmt.Errorf("nonzero return code from DeleteUnicastIpAddressEntry: %d", ret)
	}
	return nil
}

func addIPAddress(ip, mask net.IP, index uint32) error {
	ip = ip.To4()
	mask = mask.To4()
	var nteC int
	var nteI int

	ret, _, _ := procAddIPAddress.Call(
		uintptr(binary.LittleEndian.Uint32(ip)),
		uintptr(binary.LittleEndian.Uint32(mask)),
		uintptr(index),
		uintptr(unsafe.Pointer(&nteC)),
		uintptr(unsafe.Pointer(&nteI)))
	if ret != 0 {
		return fmt.Errorf("nonzero return code from AddIPAddress: %d", ret)
	}
	return nil
}

func deleteIPAddress(ip net.IP) error {
	ip = ip.To4()
	b := make([]byte, 1)
	ai := (*syscall.IpAdapterInfo)(unsafe.Pointer(&b[0]))
	l := uint32(0)
	syscall.GetAdaptersInfo(ai, &l)

	b = make([]byte, int32(l))
	ai = (*syscall.IpAdapterInfo)(unsafe.Pointer(&b[0]))
	if err := syscall.GetAdaptersInfo(ai, &l); err != nil {
		return err
	}

	for ; ai != nil; ai = ai.Next {
		for ipl := &ai.IpAddressList; ipl != nil; ipl = ipl.Next {
			ipb := bytes.Trim(ipl.IpAddress.String[:], "\x00")
			if string(ipb) != ip.String() {
				continue
			}
			nteC := ipl.Context
			ret, _, _ := procDeleteIPAddress.Call(uintptr(nteC))
			if ret != 0 {
				return fmt.Errorf("nonzero return code from DeleteIPAddress: %d", ret)
			}
			return nil
		}
	}
	return fmt.Errorf("did not find address %s on system", ip)
}
