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
	"reflect"
	"runtime"
	"slices"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	network "github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/network/manager"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	addressKey       = regKeyBase + `\ForwardedIps`
	oldWSFCAddresses string
	oldWSFCEnable    bool
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
			if err = deleteRegKey(addressKey, oldName); err != nil {
				logger.Warningf("Failed to delete key: %q, name: %q from registry", addressKey, oldName)
			}
		}
	} else if err != nil {
		return nil, err
	}
	return regFwdIPs, nil
}

func compareRoutes(configuredRoutes, desiredRoutes []string) (toAdd, toRm []string) {
	for _, desiredRoute := range desiredRoutes {
		if !slices.Contains(configuredRoutes, desiredRoute) {
			toAdd = append(toAdd, desiredRoute)
		}
	}

	for _, configuredRoute := range configuredRoutes {
		if !slices.Contains(desiredRoutes, configuredRoute) {
			toRm = append(toRm, configuredRoute)
		}
	}
	return toAdd, toRm
}

var badMAC []string

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
				if !slices.Contains(wsfcAddrs, ip) {
					filteredForwardedIps = append(filteredForwardedIps, ip)
				}
			}
			interfaces[idx].ForwardedIps = filteredForwardedIps

			var filteredTargetInstanceIps []string
			for _, ip := range interfaces[idx].TargetInstanceIps {
				if !slices.Contains(wsfcAddrs, ip) {
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

	// Setup network interfaces.
	err := network.SetupInterfaces(ctx, config, newMetadata.Instance.NetworkInterfaces)
	if err != nil {
		return fmt.Errorf("failed to setup network interfaces: %v", err)
	}

	if !config.NetworkInterfaces.IPForwarding {
		return nil
	}

	logger.Debugf("Add routes for aliases, forwarded IP and target-instance IPs")
	// Add routes for IP aliases, forwarded and target-instance IPs.
	for _, ni := range newMetadata.Instance.NetworkInterfaces {
		iface, err := network.GetInterfaceByMAC(ni.Mac)
		if err != nil {
			if !slices.Contains(badMAC, ni.Mac) {
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
				if slices.Contains(regFwdIPs, ip) {
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
			if !slices.Contains(toAdd, ip) {
				registryEntries = append(registryEntries, ip)
				continue
			}
			var err error
			if runtime.GOOS == "windows" {
				// Don't addAddress if this is already configured.
				if !slices.Contains(configuredIPs, ip) {
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
				if !slices.Contains(configuredIPs, ip) {
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
