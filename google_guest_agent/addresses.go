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
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	addressKey        = regKeyBase + `\ForwardedIps`
	oldWSFCAddresses  string
	oldWSFCEnable     bool
	interfacesEnabled bool
	interfaces        []net.Interface

	protoID = 66
)

type addressMgr struct{}

func (a *addressMgr) parseWSFCAddresses() string {
	wsfcAddresses := config.Section("wsfc").Key("addresses").String()
	if wsfcAddresses != "" {
		return wsfcAddresses
	}
	if newMetadata.Instance.Attributes.WSFCAddresses != "" {
		return newMetadata.Instance.Attributes.WSFCAddresses
	}
	if newMetadata.Project.Attributes.WSFCAddresses != "" {
		return newMetadata.Project.Attributes.WSFCAddresses
	}

	return ""
}

func (a *addressMgr) parseWSFCEnable() bool {
	wsfcEnable, err := config.Section("wsfc").Key("enable").Bool()
	if err == nil {
		return wsfcEnable
	}
	if newMetadata.Instance.Attributes.EnableWSFC != nil {
		return *newMetadata.Instance.Attributes.EnableWSFC
	}
	if newMetadata.Project.Attributes.EnableWSFC != nil {
		return *newMetadata.Project.Attributes.EnableWSFC
	}
	return false
}

func getForwardsFromRegistry(mac string) ([]string, error) {
	regFwdIPs, err := readRegMultiString(addressKey, mac)
	if err == errRegNotExist {
		// The old agent stored MAC addresses without the ':',
		// check for those and clean them up.
		oldName := strings.Replace(mac, ":", "", -1)
		regFwdIPs, err = readRegMultiString(addressKey, oldName)
		if err == nil {
			deleteRegKey(addressKey, oldName)
		}
	} else if err != nil {
		return nil, err
	}
	return regFwdIPs, nil
}

func compareIPs(configuredIPs, desiredIPs []string) (toAdd, toRm []string) {
	for _, desiredIP := range desiredIPs {
		if !containsString(desiredIP, configuredIPs) {
			toAdd = append(toAdd, desiredIP)
		}
	}

	for _, configuredIP := range configuredIPs {
		if !containsString(configuredIP, desiredIPs) {
			toRm = append(toRm, configuredIP)
		}
	}
	return toAdd, toRm
}

var badMAC []string

func getInterfaceByMAC(mac string) (net.Interface, error) {
	hwaddr, err := net.ParseMAC(mac)
	if err != nil {
		return net.Interface{}, err
	}

	for _, iface := range interfaces {
		if iface.HardwareAddr.String() == hwaddr.String() {
			return iface, nil
		}
	}
	return net.Interface{}, fmt.Errorf("no interface found with MAC %s", mac)
}

// https://www.ietf.org/rfc/rfc1354.txt
// Only fields that we currently care about.
type ipForwardEntry struct {
	ipForwardDest    net.IP
	ipForwardMask    net.IPMask
	ipForwardNextHop net.IP
	ipForwardIfIndex int32
	ipForwardMetric1 int32
}

// TODO: getLocalRoutes and getIPForwardEntries should be merged.
func getLocalRoutes(ifname string) ([]string, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("getLocalRoutes unimplemented on Windows")
	}

	args := fmt.Sprintf("route list table local type local scope host dev %s proto %d", ifname, protoID)
	out, err := exec.Command("ip", strings.Split(args, " ")...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf(string(ee.Stderr))
		}
		return nil, err
	}
	var res []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimPrefix(line, "local ")
		line = strings.TrimSpace(line)
		if line != "" {
			res = append(res, line)
		}
	}
	return res, nil
}

// TODO: addLocalRoute and addRoute should be merged with the addition of ipForwardType to ipForwardEntry.
func addLocalRoute(ip, ifname string) error {
	if runtime.GOOS == "windows" {
		return errors.New("addLocalRoute unimplemented on Windows")
	}

	// TODO: Subnet size should be parsed from alias IP entries.
	if !strings.Contains(ip, "/") {
		ip = ip + "/32"
	}
	args := fmt.Sprintf("route add to local %s scope host dev %s proto %d", ip, ifname, protoID)
	return runCmd(exec.Command("ip", strings.Split(args, " ")...))
}

// TODO: removeLocalRoute should be changed to removeIPForwardEntry and match getIPForwardEntries.
func removeLocalRoute(ip, ifname string) error {
	if runtime.GOOS == "windows" {
		return errors.New("removeLocalRoute unimplemented on Windows")
	}

	// TODO: Subnet size should be parsed from alias IP entries.
	if !strings.Contains(ip, "/") {
		ip = ip + "/32"
	}
	args := fmt.Sprintf("route delete to local %s scope host dev %s proto %d", ip, ifname, protoID)
	return runCmd(exec.Command("ip", strings.Split(args, " ")...))
}

// Filter out forwarded ips based on WSFC (Windows Failover Cluster Settings).
// If only EnableWSFC is set, all ips in the ForwardedIps and TargetInstanceIps will be ignored.
// If WSFCAddresses is set (with or without EnableWSFC), only ips in the list will be filtered out.
// TODO return a filtered list rather than modifying the metadata object. liamh@15-11-19
func (a *addressMgr) applyWSFCFilter() {
	wsfcAddresses := a.parseWSFCAddresses()

	var wsfcAddrs []string
	for _, wsfcAddr := range strings.Split(wsfcAddresses, ",") {
		if wsfcAddr == "" {
			continue
		}

		if net.ParseIP(wsfcAddr) == nil {
			logger.Errorf("Address for WSFC is not in valid form %s", wsfcAddr)
			continue
		}

		wsfcAddrs = append(wsfcAddrs, wsfcAddr)
	}

	if len(wsfcAddrs) != 0 {
		interfaces := newMetadata.Instance.NetworkInterfaces
		for idx := range interfaces {
			var filteredForwardedIps []string
			for _, ip := range interfaces[idx].ForwardedIps {
				if !containsString(ip, wsfcAddrs) {
					filteredForwardedIps = append(filteredForwardedIps, ip)
				}
			}
			interfaces[idx].ForwardedIps = filteredForwardedIps

			var filteredTargetInstanceIps []string
			for _, ip := range interfaces[idx].TargetInstanceIps {
				if !containsString(ip, wsfcAddrs) {
					filteredTargetInstanceIps = append(filteredTargetInstanceIps, ip)
				}
			}
			interfaces[idx].TargetInstanceIps = filteredTargetInstanceIps
		}
	} else {
		wsfcEnable := a.parseWSFCEnable()
		if wsfcEnable {
			for idx := range newMetadata.Instance.NetworkInterfaces {
				newMetadata.Instance.NetworkInterfaces[idx].ForwardedIps = nil
				newMetadata.Instance.NetworkInterfaces[idx].TargetInstanceIps = nil
			}
		}
	}
}

func (a *addressMgr) diff() bool {
	wsfcAddresses := a.parseWSFCAddresses()
	wsfcEnable := a.parseWSFCEnable()

	diff := !reflect.DeepEqual(newMetadata.Instance.NetworkInterfaces, oldMetadata.Instance.NetworkInterfaces) ||
		wsfcEnable != oldWSFCEnable || wsfcAddresses != oldWSFCAddresses

	oldWSFCAddresses = wsfcAddresses
	oldWSFCEnable = wsfcEnable
	return diff
}

func (a *addressMgr) timeout() bool {
	select {
	case <-ticker:
		return true
	default:
		return false
	}
}

func (a *addressMgr) disabled(os string) (disabled bool) {
	disabled, err := config.Section("addressManager").Key("disable").Bool()
	if err == nil {
		return disabled
	}
	if newMetadata.Instance.Attributes.DisableAddressManager != nil {
		return *newMetadata.Instance.Attributes.DisableAddressManager
	}
	if newMetadata.Project.Attributes.DisableAddressManager != nil {
		return *newMetadata.Project.Attributes.DisableAddressManager
	}
	return false
}

func (a *addressMgr) set() error {
	if runtime.GOOS == "windows" {
		a.applyWSFCFilter()
	}

	var err error
	interfaces, err = net.Interfaces()
	if err != nil {
		return err
	}

	if runtime.GOOS != "windows" {
		if err := configureIPv6(); err != nil {
			return err
		}
	}

	if runtime.GOOS != "windows" && !interfacesEnabled {
		if err := enableNetworkInterfaces(); err != nil {
			return err
		}
		interfacesEnabled = true
	}

	// Add routes for forwarded and target-instance IPs.
	for _, ni := range newMetadata.Instance.NetworkInterfaces {
		iface, err := getInterfaceByMAC(ni.Mac)
		if err != nil {
			if !containsString(ni.Mac, badMAC) {
				logger.Errorf("Error getting interface: %s", err)
				badMAC = append(badMAC, ni.Mac)
			}
			continue
		}
		wantIPs := append(ni.ForwardedIps, ni.TargetInstanceIps...)
		if runtime.GOOS != "windows" {
			// IP Aliases are not supported on windows.
			wantIPs = append(wantIPs, ni.IPAliases...)
		}

		var forwardedIPs []string
		var configuredIPs []string
		if runtime.GOOS == "windows" {
			addrs, err := iface.Addrs()
			if err != nil {
				logger.Errorf("Error getting addresses for interface %s: %s", iface.Name, err)
			}
			for _, addr := range addrs {
				configuredIPs = append(configuredIPs, strings.TrimSuffix(addr.String(), "/32"))
			}
			regFwdIPs, err := getForwardsFromRegistry(ni.Mac)
			if err != nil {
				logger.Errorf("Error getting forwards from registry: %s", err)
				continue
			}
			for _, ip := range configuredIPs {
				// Only add to forwardedIPs if the interface is setup and is in the registry.
				if containsString(ip, regFwdIPs) {
					forwardedIPs = append(forwardedIPs, ip)
				}
			}
		} else {
			forwardedIPs, err = getLocalRoutes(iface.Name)
			if err != nil {
				logger.Errorf("Error getting routes: %v", err)
				continue
			}
		}

		toAdd, toRm := compareIPs(forwardedIPs, wantIPs)

		if len(toAdd) != 0 || len(toRm) != 0 {
			var msg string
			msg = fmt.Sprintf("Changing forwarded IPs for %s from %q to %q by", ni.Mac, forwardedIPs, wantIPs)
			if len(toAdd) != 0 {
				msg += fmt.Sprintf(" adding %q", toAdd)
			}
			if len(toRm) != 0 {
				if len(toAdd) != 0 {
					msg += " and"
				}
				msg += fmt.Sprintf(" removing %q", toRm)
			}
			logger.Infof(msg)
		}

		var registryEntries []string
		for _, ip := range wantIPs {
			// If the IP is not in the list of ones to add, add to registry list and continue.
			if !containsString(ip, toAdd) {
				registryEntries = append(registryEntries, ip)
				continue
			}
			var err error
			if runtime.GOOS == "windows" {
				// Don't addAddress if this is already configured.
				if !containsString(ip, configuredIPs) {
					err = addAddress(net.ParseIP(ip), net.IPv4Mask(255, 255, 255, 255), uint32(iface.Index))
				}
			} else {
				err = addLocalRoute(ip, iface.Name)
			}
			if err == nil {
				registryEntries = append(registryEntries, ip)
			} else {
				logger.Errorf("error adding route: %v", err)
			}
		}

		for _, ip := range toRm {
			var err error
			if runtime.GOOS == "windows" {
				if !containsString(ip, configuredIPs) {
					continue
				}
				err = removeAddress(net.ParseIP(ip), uint32(iface.Index))
			} else {
				err = removeLocalRoute(ip, iface.Name)
			}
			if err != nil {
				logger.Errorf("error removing route: %v", err)
				// Add IPs we fail to remove to registry to maintain accurate record.
				registryEntries = append(registryEntries, ip)
			}
		}

		if runtime.GOOS == "windows" {
			if err := writeRegMultiString(addressKey, ni.Mac, registryEntries); err != nil {
				logger.Errorf("error writing registry: %s", err)
			}
		}
	}

	return nil
}

// Enables or disables IPv6 on the primary interface.
func configureIPv6() error {
	var newNi, oldNi networkInterfaces
	if len(newMetadata.Instance.NetworkInterfaces) == 0 {
		return fmt.Errorf("No interfaces found in metadata")
	}
	newNi = newMetadata.Instance.NetworkInterfaces[0]
	if len(oldMetadata.Instance.NetworkInterfaces) > 0 {
		oldNi = oldMetadata.Instance.NetworkInterfaces[0]
	}
	iface, err := getInterfaceByMAC(newNi.Mac)
	if err != nil {
		return err
	}
	switch {
	case oldNi.DHCPv6Refresh != "" && newNi.DHCPv6Refresh == "",
		newNi.DHCPv6Refresh == "" && len(oldMetadata.Instance.NetworkInterfaces) == 0:
		// disable
		// uses empty old interface slice to indicate this is first-run.
		if err := runCmd(exec.Command("dhclient", "-r", "-6", "-v", iface.Name)); err != nil {
			return err
		}
	case oldNi.DHCPv6Refresh == "" && newNi.DHCPv6Refresh != "":
		// enable
		tentative := exec.Command("ip", "-6", "-o", "a", "s", "dev", iface.Name, "scope", "link", "tentative")
		for i := 0; i < 5; i++ {
			res, err := runCmdOutput(tentative)
			if err == nil && res == "" {
				break
			}
			time.Sleep(1 * time.Second)
		}
		val := fmt.Sprintf("net.ipv6.conf.%s.accept_ra_rt_info_max_plen=128", iface.Name)
		if err := runCmd(exec.Command("sysctl", val)); err != nil {
			return err
		}
		if err := runCmd(exec.Command("dhclient", "-1", "-6", "-v", iface.Name)); err != nil {
			return err
		}
	}
	return nil
}

var osrelease release

// enableNetworkInterfaces runs `dhclient -x; dhclient eth1 eth2 ... ethN`.
// On RHEL7, it also calls disableNM for each interface.
// On SLES, it calls enableSLESInterfaces instead of dhclient.
func enableNetworkInterfaces() error {
	var googleInterfaces []string
	for _, ni := range newMetadata.Instance.NetworkInterfaces {
		iface, err := getInterfaceByMAC(ni.Mac)
		if err != nil {
			if !containsString(ni.Mac, badMAC) {
				logger.Errorf("Error getting interface: %s", err)
				badMAC = append(badMAC, ni.Mac)
			}
			continue
		}
		googleInterfaces = append(googleInterfaces, iface.Name)
	}

	switch {
	case osrelease.os == "sles":
		return enableSLESInterfaces(googleInterfaces)
	case osrelease.os == "rhel" && osrelease.version.major == 7:
		for _, iface := range googleInterfaces {
			err := disableNM(iface)
			if err != nil {
				return err
			}
		}
		fallthrough
	default:
		err := runCmd(exec.Command("dhclient", "-x"))
		if err != nil {
			logger.Warningf("Error running 'dhclient -x': %v.", err)
		}
		return runCmd(exec.Command("dhclient", googleInterfaces...))
	}
}

// enableSLESInterfaces writes one ifcfg file for each interface, then
// runs `wicked ifup eth1 eth2 ... ethN`
func enableSLESInterfaces(interfaces []string) error {
	var err error
	var priority = 10100
	for _, iface := range interfaces {
		var ifcfg *os.File
		ifcfg, err = os.Create("/etc/sysconfig/network/ifcfg-" + iface)
		if err != nil {
			return err
		}
		defer closer(ifcfg)
		contents := []string{
			googleComment,
			"STARTMODE=hotplug",
			"BOOTPROTO=dhcp",
			fmt.Sprintf("DHCLIENT_ROUTE_PRIORITY=%d", priority),
		}
		_, err = ifcfg.WriteString(strings.Join(contents, "\n"))
		if err != nil {
			return err
		}
		priority += 100
	}
	args := append([]string{"ifup", "--timeout", "1"}, interfaces...)
	return runCmd(exec.Command("/usr/sbin/wicked", args...))
}

// disableNM writes an ifcfg file with DHCP and NetworkManager disabled.
func disableNM(iface string) error {
	filename := "/etc/sysconfig/network-scripts/ifcfg-" + iface
	ifcfg, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err == nil {
		defer closer(ifcfg)
		contents := []string{
			googleComment,
			fmt.Sprintf("DEVICE=%s", iface),
			"BOOTPROTO=none",
			"DEFROUTE=no",
			"IPV6INIT=no",
			"NM_CONTROLLED=no",
			"NOZEROCONF=yes",
		}
		_, err = ifcfg.WriteString(strings.Join(contents, "\n"))
		return err
	}
	if os.IsExist(err) {
		return nil
	}
	return err
}
