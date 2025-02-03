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
	"fmt"
	"net"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/ps"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	// The base directory for dhclient files managed by guest agent.
	// For finer control of the execution, dhclient is invoked for
	// each interface individually such that each call will have its
	// own PID file. This is where those PID and lease files are
	// expected to be written.
	defaultBaseDhclientDir = "/run"
)

var (
	// ipv4 is a wrapper containing the protocol version and its respective
	// dhclient argument.
	ipv4 = ipVersion{"ipv4", "-4"}

	// ipv6 is a wrapper containing the protocol version and its respective
	// dhclient argument.
	ipv6 = ipVersion{"ipv6", "-6"}

	// baseDhclientDir points to the base directory for DHClient leases and PIDs.
	baseDhclientDir = defaultBaseDhclientDir

	// vlanIfaceCommonSet is a set of commands to setup common elements of a vlan interface
	// it sets link and dev level configurations.
	vlanIfaceCommonSet = run.CommandSet{
		{
			Command: "ip link add link {{.ParentInterface}} name {{.Iface}} type vlan id {{.Vlan}} reorder_hdr off",
			Error:   "vlan({{.Iface}}): failed to add link",
		},
		{
			Command: "ip link set dev {{.Iface}} address {{.MacAddress}}",
			Error:   "vlan({{.Iface}}): failed to set itnerface's mac address",
		},
		{
			Command: "ip link set dev {{.Iface}} mtu {{.MTU}}",
			Error:   "vlan({{.Iface}}): failed to set interface's MTU",
		},
		{
			Command: "ip link set up {{.Iface}}",
			Error:   "vlan({{.Iface}}): failed to bring interface up",
		},
	}

	// ipAddressSet is a set of commands used to setup the ip address both in the ipv4 and
	// ipv6 cases.
	ipAddressSet = run.CommandSet{
		{
			Command: "ip {{.IPVersion.Flag}} addr add dev {{.Iface}} {{.Address}}",
			Error:   "vlan({{.Iface}}): failed to set ip address {{.Address}}",
		},
	}

	// commonRouteSet is a set of commands used to setup routes both in the ipv4 and ipv6 cases.
	commonRouteSet = run.CommandSet{
		{
			Command: "ip {{.IPVersion.Flag}} route add {{.Gateway}} dev {{.Iface}}",
			Error:   "vlan({{.Iface}}): failed to add {{.IPVersion.Desc}} route to gateway {{.Gateway}}",
		},
	}

	// ipv4RouteCommand is a set of commands relevant only for setting routes for ipv4 networks.
	ipv4RouteCommand = run.CommandSet{
		{
			Command: "ip route add {{.Address}} via {{.Gateway}}",
			Error:   "vlan({{.Iface}}): failed to set gateway route",
		},
	}

	// deleteLinkCmd is a command spec dedicated to deleting ethernet links.
	deleteLinkCmd = run.CommandSpec{
		Command: "ip link delete {{.Iface}}",
		Error:   "vlan({{.Iface}}): failed to delete link",
	}
)

// InterfaceConfig wraps the vlan's link and interface's configuration.
type InterfaceConfig struct {
	// Iface is the interface name.
	Iface string

	// ParentInterface is the name of the vlan's parent interface.
	ParentInterface string

	// MTU is the vlan's MTU value.
	MTU int

	// MacAddress is the vlan's Mac Address.
	MacAddress string

	// Vlan is the vlan's id.
	Vlan int
}

// IPConfig wraps the interface's configuration as well as the IP configuration.
type IPConfig struct {
	// InterfaceConfig contains the interface's config.
	InterfaceConfig

	// IPVersion is either ipv4 or ipv6.
	IPVersion ipVersion

	// Address is the IP address.
	Address string

	// Gateway is the gateway address (for ipv4 it will be md's gateway entry and for
	// ipv6 it will be populated with GatewayIpv6).
	Gateway string
}

// ipVersion is a wrapper containing the human-readable version string and
// the respective dhclient argument.
type ipVersion struct {
	// Desc is the human-readable IP protocol version.
	Desc string

	// Flag is the respective argument for DHClient invocation.
	Flag string
}

// dhclient implements the manager.Service interface for dhclient use cases.
type dhclient struct{}

// Name returns the name of the network manager service.
func (n *dhclient) Name() string {
	return "dhclient"
}

// Configure gives the opportunity for the Service implementation to adjust its configuration
// based on the Guest Agent configuration.
func (n *dhclient) Configure(ctx context.Context, config *cfg.Sections) {
}

// isDhclientInstalled returns true if the dhclient binary/executable is
// installed in the running system.
func (n *dhclient) isDhclientInstalled() (bool, error) {
	return cliExists("dhclient")
}

// IsManaging checks if the dhclient CLI is available.
func (n *dhclient) IsManaging(ctx context.Context, iface string) (bool, error) {
	return n.isDhclientInstalled()
}

// SetupEthernetInterface sets up the non-primary interfaces with dhclient, having different setup procedures
// for IPv6 network interfaces and IPv4 network interfaces.
func (n *dhclient) SetupEthernetInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	dhcpCommand := config.NetworkInterfaces.DHCPCommand
	if dhcpCommand != "" {
		tokens := strings.Split(dhcpCommand, " ")
		return run.Quiet(ctx, tokens[0], tokens[1:]...)
	}

	// Get all interfaces separated by ipv4 and ipv6.
	googleInterfaces, googleIpv6Interfaces := interfaceListsIpv4Ipv6(nics.EthernetInterfaces)
	obtainIpv4Interfaces, obtainIpv6Interfaces, releaseIpv6Interfaces, err := partitionInterfaces(ctx, googleInterfaces, googleIpv6Interfaces)
	if err != nil {
		return fmt.Errorf("error partitioning interfaces: %v", err)
	}

	// Release IPv6 leases.
	for _, iface := range releaseIpv6Interfaces {
		if err := runDhclient(ctx, ipv6, iface, true); err != nil {
			logger.Errorf("failed to run dhclient: %+x", err)
		}
	}

	// Setup IPV4.
	for _, iface := range obtainIpv4Interfaces {
		if err := runDhclient(ctx, ipv4, iface, false); err != nil {
			logger.Errorf("failed to run dhclient: %+x", err)
		}
	}

	if len(obtainIpv6Interfaces) == 0 {
		return nil
	}

	// Wait for tentative IPs to resolve as part of SLAAC for primary network interface.
	tentative := []string{"-6", "-o", "a", "s", "dev", googleInterfaces[0], "scope", "link", "tentative"}
	for i := 0; i < 5; i++ {
		res := run.WithOutput(ctx, "ip", tentative...)
		if res.ExitCode == 0 && res.StdOut == "" {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Setup IPv6.
	for _, iface := range obtainIpv6Interfaces {
		// Set appropriate system values.
		val := fmt.Sprintf("net.ipv6.conf.%s.accept_ra_rt_info_max_plen=128", iface)
		if err := run.Quiet(ctx, "sysctl", val); err != nil {
			return err
		}

		if err := runDhclient(ctx, ipv6, iface, false); err != nil {
			logger.Errorf("failed to run dhclient: %+x", err)
		}
	}
	return nil
}

// SetupVlanInterface calls the appropriate native commands to configure a vlan interface.
func (n *dhclient) SetupVlanInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	logger.Debugf("vlans: %+v", nics.VlanInterfaces)

	sysInterfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("failed to list systems interfaces: %+v", err)
	}

	interfaceMap := map[string]net.Interface{}
	for _, curr := range sysInterfaces {
		interfaceMap[curr.Name] = curr
	}

	var keepMe []string

	for _, curr := range nics.VlanInterfaces {
		logger.Debugf("vlan(%d) parent interface: %s", curr.Vlan, curr.ParentInterfaceID)

		// For dhclient/native implementation we use a "gcp." prefix to the interface name
		// so we can determine it is a guest agent managed vlan interface.
		iface := fmt.Sprintf("gcp.%s.%d", curr.ParentInterfaceID, curr.Vlan)
		existingIface, found := interfaceMap[iface]

		// If the interface already exists and has the same configuration just keep it.
		if found && existingIface.HardwareAddr.String() == curr.Mac &&
			existingIface.MTU == curr.MTU {
			keepMe = append(keepMe, iface)
			continue
		}

		// Generic description of the interface.
		ifaceDesc := InterfaceConfig{iface, curr.ParentInterfaceID, curr.MTU, curr.Mac, curr.Vlan}

		// If the vlan interface exists but the configuration has changed we recreate it.
		if found {
			if err := deleteLinkCmd.RunQuiet(ctx, ifaceDesc); err != nil {
				return err
			}
		}

		if err := vlanIfaceCommonSet.RunQuiet(ctx, ifaceDesc); err != nil {
			return err
		}

		batches := make(map[any][]run.CommandSet)

		if curr.IP != "" {
			// ipv4 specific configurations.
			ipv4Config := IPConfig{
				InterfaceConfig: ifaceDesc,
				IPVersion:       ipv4,
				Address:         curr.IP,
				Gateway:         curr.Gateway,
			}
			batches[ipv4Config] = []run.CommandSet{ipAddressSet, commonRouteSet, ipv4RouteCommand}
		}

		for i, ipv6Address := range curr.IPv6 {
			// ipv6 specific configurations.
			ipv6Config := IPConfig{
				InterfaceConfig: ifaceDesc,
				IPVersion:       ipv6,
				Address:         ipv6Address,
				Gateway:         curr.GatewayIPv6,
			}
			batches[ipv6Config] = []run.CommandSet{ipAddressSet}
			if i == 0 {
				batches[ipv6Config] = append(batches[ipv6Config], commonRouteSet)
			}
		}

		for data, batch := range batches {
			for _, curr := range batch {
				if err := curr.RunQuiet(ctx, data); err != nil {
					return err
				}
			}
		}

		keepMe = append(keepMe, iface)
	}

	if err := n.removeVlanInterfaces(ctx, keepMe); err != nil {
		return fmt.Errorf("failed to remove uninstalled vlan interfaces: %+v", err)
	}

	return nil
}

func (n *dhclient) removeVlanInterfaces(ctx context.Context, keepMe []string) error {
	sysInterfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("failed to list systems interfaces: %+v", err)
	}

	vlanExpStr := `(?P<prefix>gcp).(?P<parent>.*)\.(?P<vlan>[0-9]+)`
	vlanExp := regexp.MustCompile(vlanExpStr)

	// Remove vlan interfaces that are no longer present/configured.
	for _, curr := range sysInterfaces {
		iface := curr.Name

		// If this is an interface to keep skip it.
		if slices.Contains(keepMe, iface) {
			continue
		}

		groups := utils.RegexGroupsMap(vlanExp, iface)

		// If it's not a vlan interface skip it.
		if _, found := groups["vlan"]; !found {
			continue
		}

		ifaceConfig := InterfaceConfig{
			Iface: iface,
		}

		if err := deleteLinkCmd.RunQuiet(ctx, ifaceConfig); err != nil {
			return err
		}
	}

	return nil
}

// pidFilePath gets the expected file path for the PID pertaining to the provided
// interface and IP version.
func pidFilePath(iface string, ipVersion ipVersion) string {
	return path.Join(baseDhclientDir, fmt.Sprintf("dhclient.google-guest-agent.%s.%s.pid", iface, ipVersion.Flag))
}

// leaseFilePath gets the expected file path for the leases pertaining to the provided
// interface and IP version.
func leaseFilePath(iface string, ipVersion ipVersion) string {
	return path.Join(baseDhclientDir, fmt.Sprintf("dhclient.google-guest-agent.%s.%s.lease", iface, ipVersion.Flag))
}

// runDhclient obtains a lease with the provided IP version for the given
// network interface. If release is set, this will release leases instead.
func runDhclient(ctx context.Context, ipVersion ipVersion, nic string, release bool) error {
	pidFile := pidFilePath(nic, ipVersion)
	leaseFile := leaseFilePath(nic, ipVersion)

	dhclientArgs := []string{ipVersion.Flag, "-pf", pidFile, "-lf", leaseFile}

	if release {
		// Only release if the flag is set.
		releaseArgs := append(dhclientArgs, "-r", nic)
		if err := run.Quiet(ctx, "dhclient", releaseArgs...); err != nil {
			return fmt.Errorf("error releasing lease for %s: %v", nic, err)
		}
	} else {
		// Now obtain a lease if release is not set.
		dhclientArgs = append(dhclientArgs, nic)
		if err := run.Quiet(ctx, "dhclient", dhclientArgs...); err != nil {
			return fmt.Errorf("error running dhclient for %s: %v", nic, err)
		}
	}
	return nil
}

// paritionInterfaces creates 3 lists of interfaces
// The first list contains interfaces for which to obtain an IPv4 lease.
// The second list contains interfaces for which to obtain an IPv6 lease.
// The third list contains interfaces for which to release their IPv6 lease.
// It will skip primary NIC for IPv4 if process is already running or disabled via config.
// Secondary NICs will be configured as long as there's no already existing dhclient
// process managing it.
func partitionInterfaces(ctx context.Context, interfaces, ipv6Interfaces []string) ([]string, []string, []string, error) {
	var obtainIpv4Interfaces []string
	var obtainIpv6Interfaces []string
	var releaseIpv6Interfaces []string

	for i, iface := range interfaces {
		if !shouldManageInterface(i == 0) {
			// Do not setup anything for this interface to avoid duplicate processes.
			logger.Debugf("ManagePrimaryNIC is disabled, skipping dhclient launch for %s", iface)
			continue
		}
		if isInvalid(iface) {
			continue
		}
		// On 18.04 we fallback to dhclient as networkctl is very old and has not reload support for example.
		// Default netplan config will take care of it, do not launch dhclient for primary NIC on 18.04.
		if (i == 0) && isUbuntu1804() {
			logger.Debugf("ManagePrimaryNIC is enabled, but skipping primary nic as its managed by default OS config")
			continue
		}

		// Check for IPv4 interfaces for which to obtain a lease.
		processExists, err := dhclientProcessExists(ctx, iface, ipv4)
		if err != nil {
			return nil, nil, nil, err
		}
		if !processExists {
			obtainIpv4Interfaces = append(obtainIpv4Interfaces, iface)
		}

		// Check for IPv6 interfaces for which to obtain a lease.
		processExists, err = dhclientProcessExists(ctx, iface, ipv6)
		if err != nil {
			return nil, nil, nil, err
		}

		if slices.Contains(ipv6Interfaces, iface) && !processExists {
			// Obtain a lease and spin up the DHClient process.
			obtainIpv6Interfaces = append(obtainIpv6Interfaces, iface)
		} else if !slices.Contains(ipv6Interfaces, iface) && processExists {
			// Release the lease since the DHClient IPv6 process is running,
			// but the interface is no longer IPv6.
			releaseIpv6Interfaces = append(releaseIpv6Interfaces, iface)
		}
	}

	return obtainIpv4Interfaces, obtainIpv6Interfaces, releaseIpv6Interfaces, nil
}

// dhclientProcessExists checks if a dhclient process for the provided
// interface and IP version exists.
func dhclientProcessExists(_ context.Context, iface string, ipVersion ipVersion) (bool, error) {
	processes, err := ps.Find(".*dhclient.*")
	if err != nil {
		return false, fmt.Errorf("error finding dhclient process: %v", err)
	}

	// Check for any dhclient process that contains the iface and IP version provided.
	for _, process := range processes {
		commandLine := process.CommandLine

		containsInterface := slices.Contains(commandLine, iface)
		containsProtocolArg := slices.Contains(commandLine, ipVersion.Flag)

		if containsInterface {
			// IPv4 DHClient calls don't necessarily have the '-4' flag set.
			if ipVersion == ipv6 {
				return containsProtocolArg, nil
			}
			if ipVersion == ipv4 && !slices.Contains(commandLine, ipv6.Flag) {
				return true, nil
			}
		}
	}
	return false, nil
}

// anyDhclientProcessExists returns true if there's at-least one dhclient process
// running for any of the known ethernet interfaces, regarless of Ipv4/Ipv6 stack.
func anyDhclientProcessExists(nics *Interfaces) (bool, error) {
	processes, err := ps.Find(".*dhclient.*")
	if err != nil {
		return false, fmt.Errorf("error finding dhclient process: %v", err)
	}

	if len(processes) == 0 {
		return false, nil
	}

	interfaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return false, fmt.Errorf("error getting interface names: %v", err)
	}

	for _, process := range processes {
		commandLine := process.CommandLine
		for _, iface := range interfaces {
			if slices.Contains(commandLine, iface) {
				return true, nil
			}
		}
	}
	return false, nil
}

// Rollback releases all leases from DHClient, effectively undoing the dhclient configurations.
func (n *dhclient) Rollback(ctx context.Context, nics *Interfaces) error {
	if err := n.RollbackNics(ctx, nics); err != nil {
		return fmt.Errorf("failed to rollback ethernet interfaces: %w", err)
	}

	// This prevents incorrect rollback by dhclient where NICs are managed by netplan.
	// VLAN interfaces does not have dhclient process running and IPs are assigned
	// directly by running [ipAddressSet] command. Attempt to rollback any VLAN interfaces
	// only if network stack is managed by dhclient (at-least one dhclient process for
	// known ethernet interfaces). Simple dhclient existence does not prove this its managed by
	// dhcliet as in case of Debian-12 we have dhclient but NICs are managed by netplan/networkd.

	managed, err := anyDhclientProcessExists(nics)
	if err != nil {
		return fmt.Errorf("unable to detect if nics are dhclient managed: %w", err)
	}

	if !managed {
		logger.Infof("No dhclient process found for any of the known ethernet interfaces, skipping vlan rollback.")
		return nil
	}

	if err := n.removeVlanInterfaces(ctx, nil); err != nil {
		return fmt.Errorf("failed to rollback vlan interfaces: %+v", err)
	}

	return nil
}

// Rollback releases all leases from DHClient, effectively undoing the dhclient
// configurations - only regular nics are handled.
func (n *dhclient) RollbackNics(ctx context.Context, nics *Interfaces) error {
	// Determine if we can even rollback dhclient processes.
	if isInstalled, err := n.isDhclientInstalled(); !isInstalled || err != nil {
		logger.Debugf("No preconditions met for dhclient roll back, skipping.")
		return nil
	}

	googleInterfaces, googleIpv6Interfaces := interfaceListsIpv4Ipv6(nics.EthernetInterfaces)

	// Release all the interface leases from dhclient.
	for _, iface := range googleInterfaces {
		ipv4Exists, err := dhclientProcessExists(ctx, iface, ipv4)
		if err != nil {
			return fmt.Errorf("error checking if ipv4 dhclient process for %s exists: %v", iface, err)
		}
		if ipv4Exists {
			if err = runDhclient(ctx, ipv4, iface, true); err != nil {
				return err
			}
		}
	}

	for _, iface := range googleIpv6Interfaces {
		ipv6Exists, err := dhclientProcessExists(ctx, iface, ipv6)
		if err != nil {
			return fmt.Errorf("error checking if ipv6 dhclient process for %s exists: %v", iface, err)
		}
		if ipv6Exists {
			if err = runDhclient(ctx, ipv6, iface, true); err != nil {
				return err
			}
		}
	}

	return nil
}
