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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	// minSupportedVersion is the version from which we start supporting
	// systemd-networkd.
	minSupportedVersion = 252

	// defaultSystemdNetworkdPriority is a value adjusted to be above netplan
	// (usually set to 10) and low enough to be under the generic configurations.
	defaultSystemdNetworkdPriority = 20

	// deprecatedPriority is the priority previously supported by us and
	// requires us to roll it back.
	deprecatedPriority = 1
)

type systemdNetworkd struct {
	// configDir determines where the agent writes its configuration files.
	configDir string

	// networkCtlKeys helps with compatibility with different versions of
	// systemd, where the desired status key can be different.
	networkCtlKeys []string

	// priority dictates the priority with which guest-agent should write
	// the configuration files.
	priority int

	// deprecatedPriority is the priority previously supported by us and
	// requires us to roll it back.
	deprecatedPriority int
}

// guestAgentManaged define an interface for configurations to identify if
// they are managed by Guest Agent.
type guestAgentManaged interface {
	// isGuestAgentManaged returns true if the implementing configuration handler
	// has the keys and sections identifying it as managed by Guest Agent.
	isGuestAgentManaged() bool
}

// systemdMatchConfig contains the systemd-networkd's interface matching criteria.
type systemdMatchConfig struct {
	// Name is the matching criteria based on the interface name.
	Name string

	// Type is the matching type i.e. vlan.
	Type string `ini:",omitempty"`
}

// systemdLinkConfig contains the systemd-networkd's link configuration section.
type systemdLinkConfig struct {
	// MACAddress is the address to be set to the link.
	MACAddress string

	// MTUBytes is the systemd-networkd's Link's MTU configuration in bytes.
	MTUBytes int
}

// systemdNetworkConfig contains the actual interface rule's configuration.
type systemdNetworkConfig struct {
	// DHCP determines the ipv4/ipv6 protocol version for use with dhcp.
	DHCP string `ini:"DHCP,omitempty"`

	// DNSDefaultRoute is used to determine if the link's configured DNS servers are
	// used for resolving domain names that do not match any link's domain.
	DNSDefaultRoute bool

	// VLAN specifies the VLANs this network should be member of.
	VLANS []string `ini:"VLAN,omitempty,allowshadow"`
}

// systemdDHCPConfig contains the dhcp specific configurations for a
// systemd network configuration. RouteToDNS and RouteToNTP are present
// only in context of [DHCPv4]
// https://www.freedesktop.org/software/systemd/man/latest/systemd.network.html#RoutesToDNS=
// https://www.freedesktop.org/software/systemd/man/latest/systemd.network.html#RoutesToNTP=
type systemdDHCPConfig struct {
	// RoutesToDNS defines if routes to the DNS servers received from the DHCP
	// shoud be configured/installed.
	RoutesToDNS bool

	// RoutesToNTP defines if routes to the NTP servers received from the DHCP
	// shoud be configured/installed.
	RoutesToNTP bool
}

// systemdConfig wraps the interface configuration for systemd-networkd.
// Ultimately the structure will be unmarshalled into a .ini file.
type systemdConfig struct {
	// GuestAgent is a section containing guest-agent control flags/data.
	GuestAgent guestAgentSection

	// Match is the systemd-networkd ini file's [Match] section.
	Match systemdMatchConfig

	// Network is the systemd-networkd ini file's [Network] section.
	Network systemdNetworkConfig

	// DHCPv4 is the systemd-networkd ini file's [DHCPv4] section.
	DHCPv4 *systemdDHCPConfig `ini:",omitempty"`

	// DHCPv6 is the systemd-networkd ini file's [DHCPv4] section.
	DHCPv6 *systemdDHCPConfig `ini:",omitempty"`

	// Link is the systemd-networkd init file's [Link] section.
	Link *systemdLinkConfig `ini:",omitempty"`
}

// systemdVlan is the systemd's netdev [VLAN] section.
type systemdVlan struct {
	// Id is the vlan's id.
	ID int `ini:"Id,omitempty"`

	// ReorderHeader determines if the vlan reorder header must be used.
	ReorderHeader bool
}

// systemdNetdev is the systemd's netdev [NetDev] section.
type systemdNetdev struct {
	// Name is the vlan interface name.
	Name string

	// Kind is the vlan interface's Kind: "vlan".
	Kind string
}

// systemdNetdevConfig is the systemd's netdev configuration file.
type systemdNetdevConfig struct {
	// GuestAgent is a section containing guest-agent control flags/data.
	GuestAgent guestAgentSection

	//NetDev is the systemd-networkd netdev file's [NetDev] section.
	NetDev systemdNetdev

	//NetDev is the systemd-networkd netdev file's [VLAN] section.
	VLAN systemdVlan
}

// Name returns the name of the network manager service.
func (n *systemdNetworkd) Name() string {
	return "systemd-networkd"
}

// Configure gives the opportunity for the Service implementation to adjust its configuration
// based on the Guest Agent configuration.
func (n *systemdNetworkd) Configure(ctx context.Context, config *cfg.Sections) {
	n.configDir = config.Unstable.SystemdConfigDir
}

// IsManaging checks whether systemd-networkd is managing the provided interface.
// This first checks if the systemd-networkd service is running, then uses networkctl
// to check if systemd-networkd is managing or has configured the provided interface.
func (n *systemdNetworkd) IsManaging(ctx context.Context, iface string) (bool, error) {
	// Check the version.
	exists, err := cliExists("networkctl")
	if !exists {
		return false, err
	}

	res := run.WithOutput(ctx, "networkctl", "--version")
	if res.ExitCode != 0 {
		return false, fmt.Errorf("error checking networkctl version: %v", res.StdErr)
	}
	// The version is the second field of the first line.
	versionString := strings.Split(strings.Split(res.StdOut, "\n")[0], " ")[1]
	version, err := strconv.Atoi(versionString)
	if err != nil {
		return false, fmt.Errorf("error parsing systemd version: %v", err)
	}
	if version < minSupportedVersion {
		logger.Infof("systemd-networkd version %v not supported: minimum %v required", version, minSupportedVersion)
		return false, nil
	}

	// First check if the service is running.
	res = run.WithOutput(ctx, "systemctl", "is-active", "systemd-networkd.service")
	if res.ExitCode != 0 {
		return false, nil
	}

	// Check systemd network configuration.
	res = run.WithOutput(ctx, "/bin/sh", "-c", fmt.Sprintf("networkctl status %s --json=short", iface))
	if res.ExitCode != 0 {
		return false, fmt.Errorf("failed to check systemd-networkd network status: %v", res.StdErr)
	}

	interfaceStatus := make(map[string]any)

	if err = json.Unmarshal([]byte(res.StdOut), &interfaceStatus); err != nil {
		return false, fmt.Errorf("failed to unmarshal interface status: %v", err)
	}

	for _, statusKey := range n.networkCtlKeys {
		state, found := interfaceStatus[statusKey]
		if !found {
			continue
		}

		return state == "configured", nil
	}
	return false, fmt.Errorf("could not determine interface state, one of %v was not present", n.networkCtlKeys)
}

// SetupEthernetInterface sets up the non-primary network interfaces for systemd-networkd by writing
// configuration files to the specified configuration directory.
func (n *systemdNetworkd) SetupEthernetInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	// Create a network configuration file with default configurations for each network interface.
	googleInterfaces, googleIpv6Interfaces := interfaceListsIpv4Ipv6(nics.EthernetInterfaces)

	// Write the config files.
	if err := n.writeEthernetConfig(googleInterfaces, googleIpv6Interfaces); err != nil {
		return fmt.Errorf("error writing network configs: %v", err)
	}

	// Make sure to rollback previously supported and now deprecated .network and .netdev
	// config files.
	for _, iface := range googleInterfaces {
		if _, err := n.rollbackNetwork(n.deprecatedNetworkFile(iface)); err != nil {
			logger.Infof("Failed to rollback .network file: %v", err)
		}

		if _, err := n.rollbackNetwork(n.deprecatedNetdevFile(iface)); err != nil {
			logger.Infof("Failed to rollback .netdev file: %v", err)
		}
	}

	// Avoid restarting systemd-networkd.
	if err := run.Quiet(ctx, "networkctl", "reload"); err != nil {
		return fmt.Errorf("error reloading systemd-networkd network configs: %v", err)
	}

	return nil
}

// SetupVlanInterface writes the apppropriate vLAN interfaces configuration for the network manager service
// for all configured interfaces.
func (n *systemdNetworkd) SetupVlanInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	var keepMe []string

	for _, curr := range nics.VlanInterfaces {
		iface := fmt.Sprintf("gcp.%s.%d", curr.ParentInterfaceID, curr.Vlan)

		// Create and setup .network file.
		networkConfig := systemdConfig{
			GuestAgent: guestAgentSection{
				ManagedByGuestAgent: true,
			},
			Match: systemdMatchConfig{
				Name: iface,
				Type: "vlan",
			},
			Network: systemdNetworkConfig{
				DHCP: "yes", // enables ipv4 and ipv6
			},
			Link: &systemdLinkConfig{
				MACAddress: curr.Mac,
				MTUBytes:   curr.MTU,
			},
		}

		if err := networkConfig.write(n, iface); err != nil {
			return fmt.Errorf("failed to write systemd's vlan .network config: %+v", err)
		}

		// Create and setup .netdev file.
		netdevConfig := systemdNetdevConfig{
			GuestAgent: guestAgentSection{
				ManagedByGuestAgent: true,
			},
			NetDev: systemdNetdev{
				Name: iface,
				Kind: "vlan",
			},
			VLAN: systemdVlan{
				ID:            curr.Vlan,
				ReorderHeader: false,
			},
		}

		if err := netdevConfig.write(n, iface); err != nil {
			return fmt.Errorf("failed to write systemd's vlan .netdev config: %+v", err)
		}

		// Add VLAN keys to the VLAN's parent .network config file.
		parentFile := n.networkFile(curr.ParentInterfaceID)
		parentConfig := new(systemdConfig)

		if err := readIniFile(parentFile, parentConfig); err != nil {
			return fmt.Errorf("failed to read vlan's parent interface .network config: %+v", err)
		}

		// Add the vlan interface to parents VLAN key if not there already.
		if !slices.Contains(parentConfig.Network.VLANS, iface) {
			parentConfig.Network.VLANS = append(parentConfig.Network.VLANS, iface)

			if err := parentConfig.write(n, curr.ParentInterfaceID); err != nil {
				return fmt.Errorf("error writing vlan parent's .network config: %+v", err)
			}
		}

		keepMe = append(keepMe, iface)
	}

	// Attempt to remove vlan interface configurations that are not known - i.e. they were previously
	// added by users but are no longer present on their mds configuration.
	requiresRestart, err := n.removeVlanInterfaces(ctx, keepMe)
	if err != nil {
		return fmt.Errorf("failed to remove vlan interface configuration: %+v", err)
	}

	if !requiresRestart {
		logger.Debugf("No changes applied to systemd-network's vlan config, skipping restart.")
		return nil
	}

	// Apply network changes avoiding to restart systemd-networkd.
	if err := run.Quiet(ctx, "networkctl", "reload"); err != nil {
		return fmt.Errorf("error reloading systemd-networkd network configs: %v", err)
	}

	return nil
}

// removeVlanInterfaces removes vlan interfaces that are not present in keepMe slice.
func (n *systemdNetworkd) removeVlanInterfaces(ctx context.Context, keepMe []string) (bool, error) {
	files, err := os.ReadDir(n.configDir)
	if err != nil {
		return false, fmt.Errorf("failed to read content from %s: %+v", n.configDir, err)
	}

	configExp := `(?P<priority>[0-9]+)-(?P<interface>.*\.[0-9]+)-(?P<suffix>.*)\.(?P<extension>network|netdev)`
	configRegex := regexp.MustCompile(configExp)
	requiresRestart := false

	var ifacesDeleteMe []string
	var filesDeleteMe []string

	for _, file := range files {
		var (
			currIface, extension string
			found                bool
		)

		if file.IsDir() {
			continue
		}

		groups := utils.RegexGroupsMap(configRegex, file.Name())

		// If we don't have a matching interface skip it.
		if currIface, found = groups["interface"]; !found {
			continue
		}

		// If we don't have a matching extension skip it.
		if extension, found = groups["extension"]; !found {
			continue
		}

		// If this is an interface still present skip it.
		if slices.Contains(keepMe, currIface) {
			continue
		}

		ptrMap := map[string]guestAgentManaged{
			"network": new(systemdConfig),
			"netdev":  new(systemdNetdevConfig),
		}

		ptr, foundPtr := ptrMap[extension]
		if !foundPtr {
			return requiresRestart, fmt.Errorf("regex matching failed, invalid extension: %s", extension)
		}

		filePath := filepath.Join(n.configDir, file.Name())
		if err := readIniFile(filePath, ptr); err != nil {
			return requiresRestart, fmt.Errorf("failed to read .network file before removal: %+v", err)
		}

		// Although the file name is following the same pattern we are assuming this is not
		// managed by us - skip it.
		if !ptr.isGuestAgentManaged() {
			continue
		}

		if extension == "network" {
			network := ptr.(*systemdConfig)
			ifacesDeleteMe = append(ifacesDeleteMe, network.Match.Name)
		}

		filesDeleteMe = append(filesDeleteMe, filePath)
		requiresRestart = true
	}

	if len(ifacesDeleteMe) > 0 {
		args := []string{"delete"}
		args = append(args, ifacesDeleteMe...)
		if err := run.Quiet(ctx, "networkctl", args...); err != nil {
			return false, fmt.Errorf("networkctl %v failed with error: %w", args, err)
		}
	}

	for _, filePath := range filesDeleteMe {
		if err := os.Remove(filePath); err != nil {
			return requiresRestart, fmt.Errorf("failed to remove vlan interface config(%s): %+v", filePath, err)
		}
	}

	return requiresRestart, nil
}

// netdevFile returns the systemd's .netdev file path.
// Priority is lexicographically sorted in ascending order by file name. So a configuration
// starting with '1-' takes priority over a configuration file starting with '10-'. Setting
// a priority of 1 allows the guest-agent to override any existing default configurations
// while also allowing users the freedom of using priorities of '0...' to override the
// agent's own configurations.
func (n *systemdNetworkd) netdevFile(iface string) string {
	return filepath.Join(n.configDir, fmt.Sprintf("%d-%s-google-guest-agent.netdev", n.priority, iface))
}

// deprecatedNetdevFile returns the older and deprecated networkd's netdev file. It's
// present mainly to allow us to roll it back.
func (n *systemdNetworkd) deprecatedNetdevFile(iface string) string {
	return filepath.Join(n.configDir, fmt.Sprintf("%d-%s-google-guest-agent.netdev", n.deprecatedPriority, iface))
}

// networkFile returns the systemd's .network file path.
// Priority is lexicographically sorted in ascending order by file name. So a configuration
// starting with '1-' takes priority over a configuration file starting with '10-'. Setting
// a priority of 1 allows the guest-agent to override any existing default configurations
// while also allowing users the freedom of using priorities of '0...' to override the
// agent's own configurations.
func (n *systemdNetworkd) networkFile(iface string) string {
	return filepath.Join(n.configDir, fmt.Sprintf("%d-%s-google-guest-agent.network", n.priority, iface))
}

// deprecatedNetworkFile returns the older and deprecated networkd's network file. It's
// present mainly to allow us to roll it back.
func (n *systemdNetworkd) deprecatedNetworkFile(iface string) string {
	return filepath.Join(n.configDir, fmt.Sprintf("%d-%s-google-guest-agent.network", n.deprecatedPriority, iface))
}

// write writes systemd's .netdev config file.
func (nd systemdNetdevConfig) write(n *systemdNetworkd, iface string) error {
	if err := writeIniFile(n.netdevFile(iface), &nd); err != nil {
		return fmt.Errorf("error saving .netdev config for %s: %v", iface, err)
	}
	return nil
}

// isGuestAgentManaged returns true if the netdev config file contains the
// GuestAgent section and key.
func (nd systemdNetdevConfig) isGuestAgentManaged() bool {
	return nd.GuestAgent.ManagedByGuestAgent
}

// write writes the systemd's configuration file to its destination.
func (sc systemdConfig) write(n *systemdNetworkd, iface string) error {
	if err := writeIniFile(n.networkFile(iface), &sc); err != nil {
		return fmt.Errorf("error saving .network config for %s: %v", iface, err)
	}
	return nil
}

// isGuestAgentManaged returns true if the network config file contains the
// GuestAgent section and key.
func (sc systemdConfig) isGuestAgentManaged() bool {
	return sc.GuestAgent.ManagedByGuestAgent
}

// writeEthernetConfig writes the systemd config for all the provided interfaces in the
// provided directory using the given priority.
func (n *systemdNetworkd) writeEthernetConfig(interfaces, ipv6Interfaces []string) error {
	for i, iface := range interfaces {
		if !shouldManageInterface(i == 0) {
			logger.Debugf("ManagePrimaryNIC is disabled, skipping systemdNetworkd writeEthernetConfig for %s", iface)
			continue
		}
		if isInvalid(iface) {
			continue
		}
		logger.Debugf("write systemd-networkd network config for %s", iface)

		var dhcp = "ipv4"
		if slices.Contains(ipv6Interfaces, iface) {
			dhcp = "yes"
		}

		// Create and setup ini file.
		data := systemdConfig{
			GuestAgent: guestAgentSection{
				ManagedByGuestAgent: true,
			},
			Match: systemdMatchConfig{
				Name: iface,
			},
			Network: systemdNetworkConfig{
				DHCP:            dhcp,
				DNSDefaultRoute: true,
			},
		}

		// We are only interested on DHCP offered routes on the primary nic,
		// ignore it for the secondary ones.
		if i != 0 {
			data.Network.DNSDefaultRoute = false
			data.DHCPv4 = &systemdDHCPConfig{
				RoutesToDNS: false,
				RoutesToNTP: false,
			}
			data.DHCPv6 = &systemdDHCPConfig{
				RoutesToDNS: false,
				RoutesToNTP: false,
			}
		}

		if err := data.write(n, iface); err != nil {
			return fmt.Errorf("failed to write systemd's ethernet interface config: %+v", err)
		}
	}

	return nil
}

// Rollback deletes the configuration files created by the agent for
// systemd-networkd - both regular and vlan nics are handled.
func (n *systemdNetworkd) Rollback(ctx context.Context, nics *Interfaces) error {
	return n.rollbackConfigs(ctx, nics, true)
}

// Rollback deletes the configuration files created by the agent for
// systemd-networkd - only regular nics are handled.
func (n *systemdNetworkd) RollbackNics(ctx context.Context, nics *Interfaces) error {
	return n.rollbackConfigs(ctx, nics, false)
}

// rollbackConfigs is the low level implementation of Rollback and RollbackNics
// interface. If removeVlan is true both regular nics and vlan nics are removed
// otherwise only regular nics are removed.
func (n *systemdNetworkd) rollbackConfigs(ctx context.Context, nics *Interfaces, removeVlan bool) error {
	logger.Infof("rolling back changes for %s", n.Name())
	interfaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("failed to get list of interface names: %v", err)
	}

	ethernetRequiresRestart := false

	// Rollback ethernet interfaces.
	for _, iface := range interfaces {
		reqRestart1, err := n.rollbackNetwork(n.networkFile(iface))
		if err != nil {
			logger.Infof("Failed to rollback .network file: %v", err)
		}

		reqRestart2, err := n.rollbackNetdev(n.networkFile(iface))
		if err != nil {
			logger.Warningf("Failed to rollback .network file: %v", err)
		}

		reqRestart3, err := n.rollbackNetwork(n.deprecatedNetworkFile(iface))
		if err != nil {
			logger.Warningf("Failed to rollback .network file: %v", err)
		}

		reqRestart4, err := n.rollbackNetdev(n.deprecatedNetdevFile(iface))
		if err != nil {
			logger.Warningf("Failed to rollback .network file: %v", err)
		}

		if reqRestart1 || reqRestart2 || reqRestart3 || reqRestart4 {
			ethernetRequiresRestart = true
		}
	}

	vlanRequiresRestart := false
	// Rollback vlan interfaces.
	if removeVlan {
		vlanRequiresRestart, err = n.removeVlanInterfaces(ctx, nil)
		if err != nil {
			logger.Warningf("Failed to rollback vlan interfaces: %v", err)
		}
	}

	if !ethernetRequiresRestart && !vlanRequiresRestart {
		logger.Debugf("No systemd-networkd's configuration rolled back, skipping restart.")
		return nil
	}

	// Avoid restarting systemd-networkd.
	if err := run.Quiet(ctx, "networkctl", "reload"); err != nil {
		return fmt.Errorf("error reloading systemd-networkd network configs: %v", err)
	}

	return nil
}

func (n *systemdNetworkd) rollbackNetwork(configFile string) (bool, error) {
	logger.Debugf("Checking for %s", configFile)

	_, err := os.Stat(configFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("Failed to stat systemd-networkd configuration(%s): %w", configFile, err)
		}
		logger.Debugf("No systemd-networkd configuration found: %s", configFile)
		return false, nil
	}

	sections := new(systemdConfig)
	if err := readIniFile(configFile, sections); err != nil {
		return false, fmt.Errorf("failed to read systemd's .network file: %+v", err)
	}

	// Check that the guest section exists and the key is set to true.
	if sections.isGuestAgentManaged() {
		logger.Debugf("removing %s", configFile)
		if err = os.Remove(configFile); err != nil {
			return false, fmt.Errorf("removing systemd-networkd config(%s): %w", configFile, err)
		}
		return true, nil
	}

	return false, nil
}

func (n *systemdNetworkd) rollbackNetdev(configFile string) (bool, error) {
	logger.Debugf("Checking for %s", configFile)

	_, err := os.Stat(configFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("Failed to stat systemd-networkd configuration(%s): %w", configFile, err)
		}
		logger.Debugf("No systemd-networkd configuration found: %s", configFile)
		return false, nil
	}

	sections := new(systemdNetdevConfig)
	if err := readIniFile(configFile, sections); err != nil {
		return false, fmt.Errorf("failed to read systemd's .netdev file: %+v", err)
	}

	// Check that the guest section exists and the key is set to true.
	if sections.isGuestAgentManaged() {
		logger.Debugf("removing %s", configFile)
		if err = os.Remove(configFile); err != nil {
			return false, fmt.Errorf("removing systemd-networkd config(%s): %w", configFile, err)
		}
		return true, nil
	}

	return false, nil
}
