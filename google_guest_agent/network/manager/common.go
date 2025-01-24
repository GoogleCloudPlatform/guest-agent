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
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/osinfo"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/go-ini/ini"

	"gopkg.in/yaml.v3"
)

var (
	badMAC = make(map[string]net.Interface)

	// execLookPath points to the function to check if a path exists.
	execLookPath = exec.LookPath
)

func cliExists(name string) (bool, error) {
	_, err := execLookPath(name)
	if err == nil {
		return true, nil
	}

	if errors.Is(err, exec.ErrNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("error looking up path for %q: %v", name, err)
}

// logInterfaceState logs all network interface state present on the machine.
func logInterfaceState(ctx context.Context) {
	logger.Infof("Getting current interface state and routes")
	ifaces, err := net.Interfaces()
	if err != nil {
		logger.Warningf("Unable to get all interface: %v, will skip logging state", err)
	}

	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			logger.Warningf("Unable to get interface (%s) addresses: %v", iface.Name, err)
		}
		logger.Infof("Interface(%s), State: %+v, Addresses: %+v", iface.Name, iface, addrs)
	}

	res := run.WithOutput(ctx, "ip", "route")
	if res.ExitCode != 0 {
		logger.Warningf("Unable to get ip routes: %s", res.StdErr)
		return
	}
	logger.Infof("Currently present IP routes:\n %s", res.StdOut)
}

// interfaceNames extracts the names of the network interfaces from the provided list
// of network interfaces.
func interfaceNames(nics []metadata.NetworkInterfaces) ([]string, error) {
	var ifaces []string
	for _, ni := range nics {
		iface, err := GetInterfaceByMAC(ni.Mac)
		if err != nil {
			if _, found := badMAC[ni.Mac]; !found {
				logger.Errorf("error getting interface: %v", err)
				badMAC[ni.Mac] = iface
				continue
			}
		}
		ifaces = append(ifaces, iface.Name)
	}
	if len(ifaces) == 0 {
		return nil, fmt.Errorf("no valid interfaces found")
	}
	return ifaces, nil
}

// vlanInterfaceParentMap gets a map of VLAN IDs and its parent NIC.
func vlanInterfaceParentMap(nics map[int]metadata.VlanInterface, allEthernetInterfaces []string) (map[int]string, error) {
	vlans := make(map[int]string)

	for _, ni := range nics {
		parentInterface, err := vlanParentInterface(allEthernetInterfaces, ni)
		if err != nil {
			return nil, fmt.Errorf("failed to determine vlan's parent interface: %+v", err)
		}
		vlans[ni.Vlan] = parentInterface
	}

	return vlans, nil
}

// vlanInterfaceListsIpv6 gets a list of VLAN IDs that support IPv6.
func vlanInterfaceListsIpv6(nics map[int]VlanInterface) []int {
	var googleIpv6Interfaces []int

	for _, ni := range nics {
		if ni.DHCPv6Refresh != "" {
			googleIpv6Interfaces = append(googleIpv6Interfaces, ni.Vlan)
		}
	}
	return googleIpv6Interfaces
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

// interfacesMTUMap returns a map indexes by the interface's name with the MTU value
// provided by the metadata descriptor.
func interfacesMTUMap(nics []metadata.NetworkInterfaces) (map[string]int, error) {
	res := make(map[string]int)

	for _, ni := range nics {
		iface, err := GetInterfaceByMAC(ni.Mac)
		if err != nil {
			if _, found := badMAC[ni.Mac]; !found {
				logger.Errorf("error getting interface: %s", err)
				badMAC[ni.Mac] = iface
			}
			continue
		}
		res[iface.Name] = ni.MTU
	}

	return res, nil
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

// vlanParentInterface returns the interface name of the parent interface of a vlan interface.
func vlanParentInterface(ethernetInterfaces []string, vlan metadata.VlanInterface) (string, error) {
	regexStr := "(?P<prefix>.*network-interfaces)/(?P<interface>[0-9]+)/"
	parentRegex := regexp.MustCompile(regexStr)

	groups := utils.RegexGroupsMap(parentRegex, vlan.ParentInterface)

	ifaceIndex, found := groups["interface"]
	if !found {
		return "", fmt.Errorf("invalid vlan's ParentInterface reference, no interface index found")
	}

	index, err := strconv.Atoi(ifaceIndex)
	if err != nil {
		return "", fmt.Errorf("failed to parse parent index(%s): %+v", ifaceIndex, err)
	}

	if index >= len(ethernetInterfaces) {
		return "", fmt.Errorf("invalid parent index(%d), known interfaces count: %d", index, len(ethernetInterfaces))
	}

	return ethernetInterfaces[index], nil
}

// readIniFile reads and parses the content of filePath and loads it into ptr.
func readIniFile(filePath string, ptr any) error {
	opts := ini.LoadOptions{
		Loose:        true,
		Insensitive:  true,
		AllowShadows: true,
	}

	config, err := ini.LoadSources(opts, filePath)
	if err != nil {
		return fmt.Errorf("failed to load ini file file: %+v", err)
	}

	// Parse the config ini.
	if err = config.MapTo(ptr); err != nil {
		return fmt.Errorf("error parsing ini: %v", err)
	}

	return nil
}

// writeIniFile writes ptr data into filePath file marshalled in a ini file format.
func writeIniFile(filePath string, ptr any) error {
	config := ini.Empty()
	if err := ini.ReflectFrom(config, ptr); err != nil {
		return fmt.Errorf("error creating .netdev config ini: %v", err)
	}

	if err := config.SaveTo(filePath); err != nil {
		return fmt.Errorf("error saving config: %v", err)
	}

	return nil
}

// writeYamlFile writes ptr data into filePath file marshalled as a yaml file format.
func writeYamlFile(filePath string, ptr any) error {
	data, err := yaml.Marshal(ptr)
	if err != nil {
		return fmt.Errorf("error marshalling yaml file: %w", err)
	}
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("error writing yaml file: %w", err)
	}
	return nil
}

// readYamlFile reads and parses the content of filePath and loads it into ptr.
func readYamlFile(filepath string, ptr any) error {
	bytes, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("unable to read %q: %w", filepath, err)
	}

	return yaml.Unmarshal(bytes, ptr)
}

// isUbuntu1804 checks if agent is running on Ubuntu 18.04. This is a helper
// method to support some exceptions we have for 18.04.
func isUbuntu1804() bool {
	info := osinfo.Get()
	if info.OS == "ubuntu" && info.VersionID == "18.04" {
		return true
	}
	return false
}
