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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
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
	minSupportedVersion = 253
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
}

// init adds this network manager service to the list of known network managers.
func init() {
	registerManager(&systemdNetworkd{
		configDir:      "/usr/lib/systemd/network",
		networkCtlKeys: []string{"AdministrativeState", "SetupState"},
		priority:       1,
	}, false)
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
	DHCP string

	// DNSDefaultRoute is used to determine if the link's configured DNS servers are
	// used for resolving domain names that do not match any link's domain.
	DNSDefaultRoute bool

	// VLAN specifies the VLANs this network should be member of.
	VLANS []string `ini:"VLAN,omitempty,allowshadow"`
}

// systemdDHCPConfig contains the dhcp specific configurations for a
// systemd network configuration.
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
func (n systemdNetworkd) Name() string {
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
func (n systemdNetworkd) IsManaging(ctx context.Context, iface string) (bool, error) {
	// Check the version.
	if _, err := execLookPath("networkctl"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("error looking up networkctl path: %v", err)
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
func (n systemdNetworkd) SetupEthernetInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	// Create a network configuration file with default configurations for each network interface.
	googleInterfaces, googleIpv6Interfaces := interfaceListsIpv4Ipv6(nics.EthernetInterfaces)

	// Write the config files.
	if err := n.writeEthernetConfig(googleInterfaces, googleIpv6Interfaces); err != nil {
		return fmt.Errorf("error writing network configs: %v", err)
	}

	// Avoid restarting systemd-networkd.
	if err := run.Quiet(ctx, "networkctl", "reload"); err != nil {
		return fmt.Errorf("error reloading systemd-networkd network configs: %v", err)
	}

	return nil
}

// SetupVlanInterface writes the apppropriate vLAN interfaces configuration for the network manager service
// for all configured interfaces.
func (n systemdNetworkd) SetupVlanInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	// Retrieves the ethernet nics so we can detect the parent one.
	googleInterfaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("could not list interfaces names: %+v", err)
	}

	var keepMe []string

	for _, curr := range nics.VlanInterfaces {
		parentInterface, err := vlanParentInterface(googleInterfaces, curr)
		if err != nil {
			return fmt.Errorf("failed to determine vlan's parent interface: %+v", err)
		}

		iface := fmt.Sprintf("gcp.%s.%d", parentInterface, curr.Vlan)

		// Create and setup .network file.
		networkConfig := systemdConfig{
			GuestAgent: guestAgentSection{
				Managed: true,
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
				Managed: true,
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
		parentFile := n.networkFile(parentInterface)
		parentConfig := new(systemdConfig)

		if err := readIniFile(parentFile, parentConfig); err != nil {
			return fmt.Errorf("failed to read vlan's parent interface .network config: %+v", err)
		}

		// Add the vlan interface to parents VLAN key if not there already.
		if !slices.Contains(parentConfig.Network.VLANS, iface) {
			parentConfig.Network.VLANS = append(parentConfig.Network.VLANS, iface)

			if err := parentConfig.write(n, parentInterface); err != nil {
				return fmt.Errorf("error writing vlan parent's .network config: %+v", err)
			}
		}

		keepMe = append(keepMe, iface)
	}

	// Attempt to remove vlan interface configurations that are not known - i.e. they were previously
	// added by users but are no longer present on their mds configuration.
	if err := n.removeVlanInterfaces(keepMe); err != nil {
		return fmt.Errorf("failed to remove vlan interface configuration: %+v", err)
	}

	// Apply network changes avoiding to restart systemd-networkd.
	if err := run.Quiet(ctx, "networkctl", "reload"); err != nil {
		return fmt.Errorf("error reloading systemd-networkd network configs: %v", err)
	}

	return nil
}

// removeVlanInterfaces removes vlan interfaces that are not present in keepMe slice.
func (n systemdNetworkd) removeVlanInterfaces(keepMe []string) error {
	files, err := os.ReadDir(n.configDir)
	if err != nil {
		return fmt.Errorf("failed to read content from %s: %+v", n.configDir, err)
	}

	configExp := `(?P<priority>[0-9]+)-(?P<interface>.*\.[0-9]+)-(?P<suffix>.*)\.(?P<extension>network|netdev)`
	configRegex := regexp.MustCompile(configExp)

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
			return fmt.Errorf("regex matching failed, invalid etension: %s", extension)
		}

		filePath := path.Join(n.configDir, file.Name())
		if err := readIniFile(filePath, ptr); err != nil {
			return fmt.Errorf("failed to read .network file before removal: %+v", err)
		}

		// Although the file name is following the same pattern we are assuming this is not
		// managed by us - skip it.
		if !ptr.isGuestAgentManaged() {
			continue
		}

		if err := os.Remove(path.Join(n.configDir, file.Name())); err != nil {
			return fmt.Errorf("failed to remove vlan interface config(%s): %+v", file.Name(), err)
		}
	}

	return nil
}

// netdevFile returns the systemd's .netdev file path.
// Priority is lexicographically sorted in ascending order by file name. So a configuration
// starting with '1-' takes priority over a configuration file starting with '10-'. Setting
// a priority of 1 allows the guest-agent to override any existing default configurations
// while also allowing users the freedom of using priorities of '0...' to override the
// agent's own configurations.
func (n systemdNetworkd) netdevFile(iface string) string {
	return path.Join(n.configDir, fmt.Sprintf("%d-%s-google-guest-agent.netdev", n.priority, iface))
}

// networkFile returns the systemd's .network file path.
// Priority is lexicographically sorted in ascending order by file name. So a configuration
// starting with '1-' takes priority over a configuration file starting with '10-'. Setting
// a priority of 1 allows the guest-agent to override any existing default configurations
// while also allowing users the freedom of using priorities of '0...' to override the
// agent's own configurations.
func (n systemdNetworkd) networkFile(iface string) string {
	return path.Join(n.configDir, fmt.Sprintf("%d-%s-google-guest-agent.network", n.priority, iface))
}

// write writes systemd's .netdev config file.
func (nd systemdNetdevConfig) write(n systemdNetworkd, iface string) error {
	if err := writeIniFile(n.netdevFile(iface), &nd); err != nil {
		return fmt.Errorf("error saving .netdev config for %s: %v", iface, err)
	}
	return nil
}

// isGuestAgentManaged returns true if the netdev config file contains the
// GuestAgent section and key.
func (nd systemdNetdevConfig) isGuestAgentManaged() bool {
	return nd.GuestAgent.Managed
}

// write writes the systemd's configuration file to its destination.
func (sc systemdConfig) write(n systemdNetworkd, iface string) error {
	if err := writeIniFile(n.networkFile(iface), &sc); err != nil {
		return fmt.Errorf("error saving .network config for %s: %v", iface, err)
	}
	return nil
}

// isGuestAgentManaged returns true if the network config file contains the
// GuestAgent section and key.
func (sc systemdConfig) isGuestAgentManaged() bool {
	return sc.GuestAgent.Managed
}

// writeEthernetConfig writes the systemd config for all the provided interfaces in the
// provided directory using the given priority.
func (n systemdNetworkd) writeEthernetConfig(interfaces, ipv6Interfaces []string) error {
	for i, iface := range interfaces {
		logger.Debugf("write systemd-networkd network config for %s", iface)

		var dhcp = "ipv4"
		if slices.Contains(ipv6Interfaces, iface) {
			dhcp = "yes"
		}

		// Create and setup ini file.
		data := systemdConfig{
			GuestAgent: guestAgentSection{
				Managed: true,
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

// Rollback deletes the configuration files created by the agent for systemd-networkd.
func (n systemdNetworkd) Rollback(ctx context.Context, nics *Interfaces) error {
	logger.Infof("rolling back changes for %s", n.Name())
	interfaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("failed to get list of interface names: %v", err)
	}

	// Rollback ethernet interfaces.
	for _, iface := range interfaces {
		// Find expected files.
		configFile := n.networkFile(iface)
		sections := new(systemdConfig)

		logger.Debugf("checking for %s", configFile)
		if err := readIniFile(configFile, sections); err != nil {
			return fmt.Errorf("failed to read systemd's .network file: %+v", err)
		}

		// Check that the guest section exists and the key is set to true.
		if sections.isGuestAgentManaged() {
			logger.Debugf("removing %s", configFile)
			if err = os.Remove(configFile); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	// Rollback vlan interfaces.
	if err := n.removeVlanInterfaces(nil); err != nil {
		return fmt.Errorf("failed to rollback vlan interfaces: %+v", err)
	}

	// Avoid restarting systemd-networkd.
	if err := run.Quiet(ctx, "networkctl", "reload"); err != nil {
		return fmt.Errorf("error reloading systemd-networkd network configs: %v", err)
	}
	return nil
}
