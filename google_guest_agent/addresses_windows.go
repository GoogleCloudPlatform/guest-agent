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

//go:build windows
// +build windows

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
	ipHlpAPI = windows.NewLazySystemDLL("iphlpapi.dll")

	procAddIPAddress                    = ipHlpAPI.NewProc("AddIPAddress")
	procDeleteIPAddress                 = ipHlpAPI.NewProc("DeleteIPAddress")
	procCreateIpForwardEntry            = ipHlpAPI.NewProc("CreateIpForwardEntry")
	procDeleteIpForwardEntry            = ipHlpAPI.NewProc("DeleteIpForwardEntry")
	procGetIpForwardTable               = ipHlpAPI.NewProc("GetIpForwardTable")
	procGetIpInterfaceEntry             = ipHlpAPI.NewProc("GetIpInterfaceEntry")
	procSetIpInterfaceEntry             = ipHlpAPI.NewProc("SetIpInterfaceEntry")
	procCreateUnicastIpAddressEntry     = ipHlpAPI.NewProc("CreateUnicastIpAddressEntry")
	procInitializeUnicastIpAddressEntry = ipHlpAPI.NewProc("InitializeUnicastIpAddressEntry")
	procGetUnicastIpAddressEntry        = ipHlpAPI.NewProc("GetUnicastIpAddressEntry")
	procDeleteUnicastIpAddressEntry     = ipHlpAPI.NewProc("DeleteUnicastIpAddressEntry")
)

const (
	AF_NET   = 2
	AF_INET6 = 23
)

type MIB_IPFORWARD_TYPE DWORD

const (
	MIB_IPROUTE_TYPE_OTHER    MIB_IPFORWARD_TYPE = 1
	MIB_IPROUTE_TYPE_INVALID                     = 2
	MIB_IPROUTE_TYPE_DIRECT                      = 3
	MIB_IPROUTE_TYPE_INDIRECT                    = 4
)

type MIB_IPFORWARD_PROTO DWORD

const MIB_IPPROTO_NETMGMT MIB_IPFORWARD_PROTO = 3

type (
	in_addr struct {
		S_un struct {
			S_addr uint32
		}
	}

	SOCKADDR_IN struct {
		sin_family int16
		sin_addr   in_addr
	}

	SOCKADDR_INET struct {
		Ipv4      SOCKADDR_IN
		si_family int16
	}

	NET_LUID struct {
		Value uint64
		Info  struct {
			NetLuidIndex uint64
			IfType       uint64
		}
	}

	MIB_UNICASTIPADDRESS_ROW struct {
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

	IF_INDEX DWORD

	MIB_IPFORWARDROW struct {
		dwForwardDest      uint32
		dwForwardMask      uint32
		dwForwardPolicy    uint32
		dwForwardNextHop   uint32
		dwForwardIfIndex   IF_INDEX
		dwForwardType      MIB_IPFORWARD_TYPE
		dwForwardProto     MIB_IPFORWARD_PROTO
		dwForwardAge       int32
		dwForwardNextHopAS int32
		dwForwardMetric1   int32
		dwForwardMetric2   int32
		dwForwardMetric3   int32
		dwForwardMetric4   int32
		dwForwardMetric5   int32
	}
)

func addAddress(ip net.IP, mask net.IPMask, index uint32) error {
	// CreateUnicastIpAddressEntry only available Vista onwards.
	if err := procCreateUnicastIpAddressEntry.Find(); err != nil {
		return addIPAddress(ip, mask, index)
	}
	subnet, _ := mask.Size()
	return createUnicastIpAddressEntry(ip, uint8(subnet), index)
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
		return fmt.Errorf("nonzero return code from CreateUnicastIpAddressEntry: %s", syscall.Errno(ret))
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
		return fmt.Errorf("nonzero return code from GetUnicastIpAddressEntry: %s", syscall.Errno(ret))
	}

	if ret, _, _ := procDeleteUnicastIpAddressEntry.Call(uintptr(unsafe.Pointer(ipRow))); ret != 0 {
		return fmt.Errorf("nonzero return code from DeleteUnicastIpAddressEntry: %s", syscall.Errno(ret))
	}
	return nil
}

func addIPAddress(ip net.IP, mask net.IPMask, index uint32) error {
	var nteC int
	var nteI int

	ret, _, _ := procAddIPAddress.Call(
		uintptr(binary.LittleEndian.Uint32(ip.To4())),
		uintptr(binary.LittleEndian.Uint32(mask)),
		uintptr(index),
		uintptr(unsafe.Pointer(&nteC)),
		uintptr(unsafe.Pointer(&nteI)))
	if ret != 0 {
		return fmt.Errorf("nonzero return code from AddIPAddress: %s", syscall.Errno(ret))
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
				return fmt.Errorf("nonzero return code from DeleteIPAddress: %s", syscall.Errno(ret))
			}
			return nil
		}
	}
	return fmt.Errorf("did not find address %s on system", ip)
}

func getIPForwardEntries() ([]ipForwardEntry, error) {
	buf := make([]byte, 1)
	size := uint32(len(buf))
	// First call gets the size of MIB_IPFORWARDTABLE.
	procGetIpForwardTable.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
	)

	buf = make([]byte, size)
	if ret, _, _ := procGetIpForwardTable.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		0,
	); ret != 0 {
		return nil, fmt.Errorf("nonzero return code from GetIpForwardTable: %s", syscall.Errno(ret))
	}

	/*
	  struct MIB_IPFORWARDTABLE {
	    DWORD            dwNumEntries;
	    MIB_IPFORWARDROW table[ANY_SIZE];
	  }
	*/
	numEntries := *(*uint32)(unsafe.Pointer(&buf[0]))
	// Walk through the returned table for each entry.
	var fes []ipForwardEntry
	for i := uint32(0); i < numEntries; i++ {
		// Extract each MIB_IPFORWARDROW from MIB_IPFORWARDTABLE
		fr := *((*MIB_IPFORWARDROW)(unsafe.Pointer(
			(uintptr(unsafe.Pointer(&buf[0])) + unsafe.Sizeof(numEntries)) + (unsafe.Sizeof(MIB_IPFORWARDROW{}) * uintptr(i)),
		)))
		fd := make([]byte, 4)
		binary.LittleEndian.PutUint32(fd, uint32(fr.dwForwardDest))
		fm := make([]byte, 4)
		binary.LittleEndian.PutUint32(fm, uint32(fr.dwForwardMask))
		nh := make([]byte, 4)
		binary.LittleEndian.PutUint32(nh, uint32(fr.dwForwardNextHop))
		fe := ipForwardEntry{
			ipForwardDest:    net.IP(fd),
			ipForwardMask:    net.IPMask(fm),
			ipForwardNextHop: net.IP(nh),
			ipForwardIfIndex: int32(fr.dwForwardIfIndex),
			ipForwardMetric1: fr.dwForwardMetric1,
		}
		fes = append(fes, fe)
	}

	return fes, nil
}

func addIPForwardEntry(fe ipForwardEntry) error {
	// https://docs.microsoft.com/en-us/windows/win32/api/iphlpapi/nf-iphlpapi-createipforwardentry

	fr := &MIB_IPFORWARDROW{
		dwForwardDest:      binary.LittleEndian.Uint32(fe.ipForwardDest.To4()),
		dwForwardMask:      binary.LittleEndian.Uint32(fe.ipForwardMask),
		dwForwardPolicy:    0, // unused
		dwForwardNextHop:   binary.LittleEndian.Uint32(fe.ipForwardNextHop.To4()),
		dwForwardIfIndex:   IF_INDEX(fe.ipForwardIfIndex),
		dwForwardType:      MIB_IPROUTE_TYPE_INDIRECT, // unused
		dwForwardProto:     MIB_IPPROTO_NETMGMT,
		dwForwardAge:       0, // unused
		dwForwardNextHopAS: 0, // unused
		dwForwardMetric1:   fe.ipForwardMetric1,
		dwForwardMetric2:   -1, // unused
		dwForwardMetric3:   -1, // unused
		dwForwardMetric4:   -1, // unused
		dwForwardMetric5:   -1, // unused
	}

	if ret, _, _ := procCreateIpForwardEntry.Call(uintptr(unsafe.Pointer(fr))); ret != 0 {
		return fmt.Errorf("nonzero return code from CreateIpForwardEntry: %s", syscall.Errno(ret))
	}
	return nil
}
