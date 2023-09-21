//  Copyright 2022 Google LLC
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
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	addressKey        = regKeyBase + `\ForwardedIps`
	oldWSFCAddresses  string
	oldWSFCEnable     bool
	interfacesEnabled bool
	interfaces        []net.Interface
)

type addressMgr struct{}

func (a *addressMgr) parseWSFCAddresses(config *cfg.Sections) string {
	if config.WSFC != nil && config.WSFC.Addresses != "" {
		return config.WSFC.Addresses
	}
	if newMetadata.Instance.Attributes.WSFCAddresses != "" {
		return newMetadata.Instance.Attributes.WSFCAddresses
	}
	if newMetadata.Project.Attributes.WSFCAddresses != "" {
		return newMetadata.Project.Attributes.WSFCAddresses
	}

	return ""
}

func (a *addressMgr) parseWSFCEnable(config *cfg.Sections) bool {
	if config.WSFC != nil {
		return config.WSFC.Enable
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

func compareRoutes(configuredRoutes, desiredRoutes []string) (toAdd, toRm []string) {
	for _, desiredRoute := range desiredRoutes {
		if !utils.ContainsString(desiredRoute, configuredRoutes) {
			toAdd = append(toAdd, desiredRoute)
		}
	}

	for _, configuredRoute := range configuredRoutes {
		if !utils.ContainsString(configuredRoute, desiredRoutes) {
			toRm = append(toRm, configuredRoute)
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
func getLocalRoutes(ctx context.Context, config *cfg.Sections, ifname string) ([]string, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("getLocalRoutes unimplemented on Windows")
	}

	protoID := config.IPForwarding.EthernetProtoID
	args := fmt.Sprintf("route list table local type local scope host dev %s proto %s", ifname, protoID)
	out := run.WithOutput(ctx, "ip", strings.Split(args, " ")...)
	if out.ExitCode != 0 {
		return nil, error(out)
	}
	var res []string
	for _, line := range strings.Split(out.StdOut, "\n") {
		line = strings.TrimPrefix(line, "local ")
		line = strings.TrimSpace(line)
		if line != "" {
			res = append(res, line)
		}
	}

	// and again for IPv6 routes, without 'scope host' which is IPv4 only
	args = fmt.Sprintf("-6 route list table local type local dev %s proto %s", ifname, protoID)
	out = run.WithOutput(ctx, "ip", strings.Split(args, " ")...)
	if out.ExitCode != 0 {
		return nil, error(out)
	}
	for _, line := range strings.Split(out.StdOut, "\n") {
		line = strings.TrimPrefix(line, "local ")
		line = strings.Split(line, " ")[0]
		line = strings.TrimSpace(line)
		if line != "" {
			res = append(res, line)
		}
	}
	return res, nil
}

// TODO: addLocalRoute and addRoute should be merged with the addition of ipForwardType to ipForwardEntry.
func addLocalRoute(ctx context.Context, config *cfg.Sections, ip, ifname string) error {
	if runtime.GOOS == "windows" {
		return errors.New("addLocalRoute unimplemented on Windows")
	}

	// TODO: Subnet size should be parsed from alias IP entries.
	if !strings.Contains(ip, "/") {
		ip = ip + "/32"
	}
	protoID := config.IPForwarding.EthernetProtoID
	args := fmt.Sprintf("route add to local %s scope host dev %s proto %s", ip, ifname, protoID)
	return run.Quiet(ctx, "ip", strings.Split(args, " ")...)
}

// TODO: removeLocalRoute should be changed to removeIPForwardEntry and match getIPForwardEntries.
func removeLocalRoute(ctx context.Context, config *cfg.Sections, ip, ifname string) error {
	if runtime.GOOS == "windows" {
		return errors.New("removeLocalRoute unimplemented on Windows")
	}

	// TODO: Subnet size should be parsed from alias IP entries.
	if !strings.Contains(ip, "/") {
		ip = ip + "/32"
	}
	protoID := config.IPForwarding.EthernetProtoID
	args := fmt.Sprintf("route delete to local %s scope host dev %s proto %s", ip, ifname, protoID)
	return run.Quiet(ctx, "ip", strings.Split(args, " ")...)
}

// Filter out forwarded ips based on WSFC (Windows Failover Cluster Settings).
// If only EnableWSFC is set, all ips in the ForwardedIps and TargetInstanceIps will be ignored.
// If WSFCAddresses is set (with or without EnableWSFC), only ips in the list will be filtered out.
// TODO return a filtered list rather than modifying the metadata object. liamh@15-11-19
func (a *addressMgr) applyWSFCFilter(config *cfg.Sections) {
	wsfcAddresses := a.parseWSFCAddresses(config)

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
				if !utils.ContainsString(ip, wsfcAddrs) {
					filteredForwardedIps = append(filteredForwardedIps, ip)
				}
			}
			interfaces[idx].ForwardedIps = filteredForwardedIps

			var filteredTargetInstanceIps []string
			for _, ip := range interfaces[idx].TargetInstanceIps {
				if !utils.ContainsString(ip, wsfcAddrs) {
					filteredTargetInstanceIps = append(filteredTargetInstanceIps, ip)
				}
			}
			interfaces[idx].TargetInstanceIps = filteredTargetInstanceIps
		}
	} else {
		wsfcEnable := a.parseWSFCEnable(config)
		if wsfcEnable {
			for idx := range newMetadata.Instance.NetworkInterfaces {
				newMetadata.Instance.NetworkInterfaces[idx].ForwardedIps = nil
				newMetadata.Instance.NetworkInterfaces[idx].TargetInstanceIps = nil
			}
		}
	}
}

func (a *addressMgr) Diff(ctx context.Context) (bool, error) {
	config := cfg.Get()
	wsfcAddresses := a.parseWSFCAddresses(config)
	wsfcEnable := a.parseWSFCEnable(config)

	diff := !reflect.DeepEqual(newMetadata.Instance.NetworkInterfaces, oldMetadata.Instance.NetworkInterfaces) ||
		wsfcEnable != oldWSFCEnable || wsfcAddresses != oldWSFCAddresses

	oldWSFCAddresses = wsfcAddresses
	oldWSFCEnable = wsfcEnable
	return diff, nil
}

func (a *addressMgr) Timeout(ctx context.Context) (bool, error) {
	return false, nil
}

func (a *addressMgr) Disabled(ctx context.Context) (bool, error) {
	config := cfg.Get()

	// Local configuration takes precedence over metadata's configuration.
	if config.AddressManager != nil {
		return config.AddressManager.Disable, nil
	}

	if newMetadata.Instance.Attributes.DisableAddressManager != nil {
		return *newMetadata.Instance.Attributes.DisableAddressManager, nil
	}
	if newMetadata.Project.Attributes.DisableAddressManager != nil {
		return *newMetadata.Project.Attributes.DisableAddressManager, nil
	}

	// This is the linux config key, defaulting to true. On Linux, the
	// config file has lower priority since we ship a file with defaults.
	return !config.Daemons.NetworkDaemon, nil
}

func (a *addressMgr) Set(ctx context.Context) error {
	config := cfg.Get()

	if runtime.GOOS == "windows" {
		a.applyWSFCFilter(config)
	}

	var err error
	interfaces, err = net.Interfaces()
	if err != nil {
		return fmt.Errorf("error populating interfaces: %v", err)
	}

	if config.NetworkInterfaces.Setup {
		if runtime.GOOS != "windows" {
			logger.Debugf("Configure IPv6")
			if err := configureIPv6(ctx); err != nil {
				// Continue through IPv6 configuration errors.
				logger.Errorf("Error configuring IPv6: %v", err)
			}
		}

		if runtime.GOOS != "windows" && !interfacesEnabled {
			logger.Debugf("Enable network interfaces")
			if err := enableNetworkInterfaces(ctx, config); err != nil {
				return err
			}
			interfacesEnabled = true
		}
	}

	if !config.NetworkInterfaces.IPForwarding {
		return nil
	}

	logger.Debugf("Add routes for aliases, forwarded IP and target-instance IPs")
	// Add routes for IP aliases, forwarded and target-instance IPs.
	for _, ni := range newMetadata.Instance.NetworkInterfaces {
		iface, err := getInterfaceByMAC(ni.Mac)
		if err != nil {
			if !utils.ContainsString(ni.Mac, badMAC) {
				logger.Errorf("Error getting interface: %s", err)
				badMAC = append(badMAC, ni.Mac)
			}
			continue
		}
		wantIPs := ni.ForwardedIps
		wantIPs = append(wantIPs, ni.ForwardedIpv6s...)
		if config.IPForwarding.TargetInstanceIPs {
			wantIPs = append(wantIPs, ni.TargetInstanceIps...)
		}
		// IP Aliases are not supported on windows.
		if runtime.GOOS != "windows" && config.IPForwarding.IPAliases {
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
				// Only add to `forwardedIPs` if it is recorded in the registry.
				if utils.ContainsString(ip, regFwdIPs) {
					forwardedIPs = append(forwardedIPs, ip)
				}
			}
		} else {
			forwardedIPs, err = getLocalRoutes(ctx, config, iface.Name)
			if err != nil {
				logger.Errorf("Error getting routes: %v", err)
				continue
			}
		}

		// Trims any '/32' suffix for consistency.
		trimSuffix := func(entries []string) []string {
			var res []string
			for _, entry := range entries {
				res = append(res, strings.TrimSuffix(entry, "/32"))
			}
			return res
		}
		forwardedIPs = trimSuffix(forwardedIPs)
		wantIPs = trimSuffix(wantIPs)

		toAdd, toRm := compareRoutes(forwardedIPs, wantIPs)

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
			// If the IP is not in toAdd, add to registry list and continue.
			if !utils.ContainsString(ip, toAdd) {
				registryEntries = append(registryEntries, ip)
				continue
			}
			var err error
			if runtime.GOOS == "windows" {
				// Don't addAddress if this is already configured.
				if !utils.ContainsString(ip, configuredIPs) {
					err = addAddress(net.ParseIP(ip), net.IPv4Mask(255, 255, 255, 255), uint32(iface.Index))
				}
			} else {
				err = addLocalRoute(ctx, config, ip, iface.Name)
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
				if !utils.ContainsString(ip, configuredIPs) {
					continue
				}
				err = removeAddress(net.ParseIP(ip), uint32(iface.Index))
			} else {
				err = removeLocalRoute(ctx, config, ip, iface.Name)
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

// Enables or disables IPv6 on network interfaces.
func configureIPv6(ctx context.Context) error {
	var newNi, oldNi metadata.NetworkInterfaces
	if len(newMetadata.Instance.NetworkInterfaces) == 0 {
		return fmt.Errorf("no interfaces found in metadata")
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

		// Before obtaining or releasing an IPv6 lease, we wait for
		// 'tentative' IPs as part of SLAAC. We wait up to 5 seconds
		// for this condition to automatically resolve.
		tentative := []string{"-6", "-o", "a", "s", "dev", iface.Name, "scope", "link", "tentative"}
		for i := 0; i < 5; i++ {
			res := run.WithOutput(ctx, "ip", tentative...)
			if res.ExitCode == 0 && res.StdOut == "" {
				break
			}
			time.Sleep(1 * time.Second)
		}
		if err := run.Quiet(ctx, "dhclient", "-r", "-6", "-1", "-v", iface.Name); err != nil {
			return err
		}
	case oldNi.DHCPv6Refresh == "" && newNi.DHCPv6Refresh != "":
		// enable
		tentative := []string{"-6", "-o", "a", "s", "dev", iface.Name, "scope", "link", "tentative"}
		for i := 0; i < 5; i++ {
			res := run.WithOutput(ctx, "ip", tentative...)
			if res.ExitCode == 0 && res.StdOut == "" {
				break
			}
			time.Sleep(1 * time.Second)
		}
		val := fmt.Sprintf("net.ipv6.conf.%s.accept_ra_rt_info_max_plen=128", iface.Name)
		if err := run.Quiet(ctx, "sysctl", val); err != nil {
			return err
		}
		if err := run.Quiet(ctx, "dhclient", "-1", "-6", "-v", iface.Name); err != nil {
			return err
		}
	}
	return nil
}

// enableNetworkInterfaces runs `dhclient eth1 eth2 ... ethN`
// and `dhclient -6 eth1 eth2 ... ethN`.
// On RHEL7, it also calls disableNM for each interface.
// On SLES, it calls enableSLESInterfaces instead of dhclient.
func enableNetworkInterfaces(ctx context.Context, config *cfg.Sections) error {
	if len(newMetadata.Instance.NetworkInterfaces) < 2 {
		return nil
	}
	var googleInterfaces []string
	// The primary (first) interface is managed by the OS, we only handle
	// secondary interfaces in this code.
	for _, ni := range newMetadata.Instance.NetworkInterfaces[1:] {
		iface, err := getInterfaceByMAC(ni.Mac)
		if err != nil {
			if !utils.ContainsString(ni.Mac, badMAC) {
				logger.Errorf("Error getting interface: %s", err)
				badMAC = append(badMAC, ni.Mac)
			}
			continue
		}
		googleInterfaces = append(googleInterfaces, iface.Name)
	}
	var googleIpv6Interfaces []string
	for _, ni := range newMetadata.Instance.NetworkInterfaces[1:] {
		if ni.DHCPv6Refresh == "" {
			// This interface is not IPv6 enabled
			continue
		}
		iface, err := getInterfaceByMAC(ni.Mac)
		if err != nil {
			if !utils.ContainsString(ni.Mac, badMAC) {
				logger.Errorf("Error getting interface: %s", err)
				badMAC = append(badMAC, ni.Mac)
			}
			continue
		}
		googleIpv6Interfaces = append(googleIpv6Interfaces, iface.Name)
	}

	switch {
	case osInfo.OS == "sles":
		return enableSLESInterfaces(ctx, googleInterfaces)
	case (osInfo.OS == "rhel" || osInfo.OS == "centos") && osInfo.Version.Major >= 7:
		for _, iface := range googleInterfaces {
			err := disableNM(iface)
			if err != nil {
				return err
			}
		}
		fallthrough
	default:
		dhcpCommand := config.NetworkInterfaces.DHCPCommand
		if dhcpCommand != "" {
			tokens := strings.Split(dhcpCommand, " ")
			return run.Quiet(ctx, tokens[0], tokens[1:]...)
		}

		// Try IPv4 first as it's higher priority.
		if err := run.Quiet(ctx, "dhclient", googleInterfaces...); err != nil {
			return err
		}

		if len(googleIpv6Interfaces) == 0 {
			return nil
		}
		for _, iface := range googleIpv6Interfaces {
			// Enable kernel to accept to route advertisements.
			val := fmt.Sprintf("net.ipv6.conf.%s.accept_ra_rt_info_max_plen=128", iface)
			if err := run.Quiet(ctx, "sysctl", val); err != nil {
				return err
			}
		}

		var dhclientArgs6 []string
		dhclientArgs6 = append([]string{"-6"}, googleIpv6Interfaces...)
		return run.Quiet(ctx, "dhclient", dhclientArgs6...)
	}
}

// enableSLESInterfaces writes one ifcfg file for each interface, then
// runs `wicked ifup eth1 eth2 ... ethN`
func enableSLESInterfaces(ctx context.Context, interfaces []string) error {
	var err error
	var priority = 10100
	for _, iface := range interfaces {
		logger.Debugf("write enabling ifcfg-%s config", iface)

		var ifcfg *os.File
		ifcfg, err = os.Create("/etc/sysconfig/network/ifcfg-" + iface)
		if err != nil {
			return err
		}
		defer closer(ifcfg)
		contents := []string{
			googleComment,
			"STARTMODE=hotplug",
			// NOTE: 'dhcp' is the dhcp4+dhcp6 option.
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
	return run.Quiet(ctx, "/usr/sbin/wicked", args...)
}

// disableNM writes an ifcfg file with DHCP and NetworkManager disabled.
func disableNM(iface string) error {
	logger.Debugf("write disabling ifcfg-%s config", iface)
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
