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

// Package manager is responsible for detecting the current network manager service, and
// writing and rolling back appropriate configurations for each network manager service.
package manager

import (
	"fmt"
	"net"
	"os/exec"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	badMAC = make(map[string]net.Interface)

	// execLookPath points to the function to check if a path exists.
	execLookPath = exec.LookPath
)

// interfaceNames extracts the names of the network interfaces from the provided list
// of network interfaces.
func interfaceNames(nics []metadata.NetworkInterfaces) ([]string, error) {
	var ifaces []string
	for _, ni := range nics {
		iface, err := GetInterfaceByMAC(ni.Mac)
		if err != nil {
			return nil, err
		}
		ifaces = append(ifaces, iface.Name)
	}
	return ifaces, nil
}

// interfaceListsIpv4Ipv6 gets a list of interface names. The first list is a list of all
// interfaces, and the second list consists of only interfaces that support IPv6.
func interfaceListsIpv4Ipv6(nics []metadata.NetworkInterfaces) ([]string, []string) {
	var googleInterfaces []string
	var googleIpv6Interfaces []string

	for _, ni := range nics {
		iface, err := GetInterfaceByMAC(ni.Mac)
		if err != nil {
			if _, found := badMAC[ni.Mac]; !found {
				logger.Errorf("error getting interface: %s", err)
				badMAC[ni.Mac] = iface
			}
			continue
		}
		if ni.DHCPv6Refresh != "" {
			googleIpv6Interfaces = append(googleIpv6Interfaces, iface.Name)
		}
		googleInterfaces = append(googleInterfaces, iface.Name)
	}
	return googleInterfaces, googleIpv6Interfaces
}

// GetInterfaceByMAC gets the interface given the mac string.
func GetInterfaceByMAC(mac string) (net.Interface, error) {
	hwaddr, err := net.ParseMAC(mac)
	if err != nil {
		return net.Interface{}, err
	}

	interfaces, err := net.Interfaces()
	if err != nil {
		return net.Interface{}, fmt.Errorf("failed to get interfaces: %v", err)
	}

	for _, iface := range interfaces {
		if iface.HardwareAddr.String() == hwaddr.String() {
			return iface, nil
		}
	}
	return net.Interface{}, fmt.Errorf("no interface found with MAC %s", mac)
}
