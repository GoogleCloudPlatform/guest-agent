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

package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	// netplanEthernetSuffix is the ethernet drop-in's file suffix.
	netplanEthernetSuffix = "ethernet"

	// netplanVlanSuffix is the vlan drop-in's file suffix.
	netplanVlanSuffix = "vlan"

	// netplanConfigVersion defines the version we are using for netplan's drop-in
	// files.
	netplanConfigVersion = 2
)

// netplan is the netplan's Service interface implementation. From the guest agent's
// network manager perspective the current form only supports the
// systemd-networkd + netplan combination. Since netplan also supports NetworkManager
// as a backend we'll eventually support such a combination in the future.
type netplan struct {
	// netplanConfigDir determines where the agent writes netplan configuration files.
	netplanConfigDir string

	// networkdDropinDir determines where the agent writes the networkd configuration files.
	networkdDropinDir string

	// priority dictates the priority with which guest-agent should write
	// the configuration files.
	priority int
}

// netplanDropin maps the netplan dropin configuration yaml entries/data
// structure.
type netplanDropin struct {
	Network netplanNetwork `yaml:"network"`
}

// netplanNetwork is the netplan's drop-in network section.
type netplanNetwork struct {
	// Version is the netplan's drop-in format version.
	Version int `yaml:"version"`

	// Ethernets are the ethernet configuration entries map.
	Ethernets map[string]netplanEthernet `yaml:"ethernets,omitempty"`

	// Vlans are the vlan interface's configuration entries map.
	Vlans map[string]netplanVlan `yaml:"vlans,omitempty"`
}

// netplanEthernet describes the actual ethernet configuration.
type netplanEthernet struct {
	// Match is the interface's matching rule.
	Match netplanMatch `yaml:"match"`

	// MTU defines the interface's MTU configuration.
	MTU *int

	// DHCPv4 determines if DHCPv4 support must be enabled to such an interface.
	DHCPv4 *bool `yaml:"dhcp4,omitempty"`

	DHCP4Overrides *netplanDHCPOverrides `yaml:"dhcp4-overrides,omitempty"`

	// DHCPv6 determines if DHCPv6 support must be enabled to such an interface.
	DHCPv6 *bool `yaml:"dhcp6,omitempty"`

	DHCP6Overrides *netplanDHCPOverrides `yaml:"dhcp6-overrides,omitempty"`
}

// netplanDHCPOverrides sets the netplan dhcp-overrides configuration.
type netplanDHCPOverrides struct {
	// When true, the domain name received from the DHCP server will be used as DNS
	// search domain over this link.
	UseDomains bool `yaml:"use-domains,omitempty"`
}

// netplanMatch contains the keys uses to match an interface.
type netplanMatch struct {
	// Name is the key used to match an interface by its name.
	Name string `yaml:"name"`
}

// netplanVlan describes the netplan's vlan interface configuration.
type netplanVlan struct {
	// ID is the the VLAN ID.
	ID int `yaml:"id,omitempty"`

	// Link is the vlan's parent interface.
	Link string `yaml:"id,link"`

	// DHCPv4 determines if DHCPv4 support must be enabled to such an interface.
	DHCPv4 *bool `yaml:"dhcp4,omitempty"`

	// DHCPv6 determines if DHCPv6 support must be enabled to such an interface.
	DHCPv6 *bool `yaml:"dhcp6,omitempty"`
}

// networkdNetplanDropin maps systemd-networkd's overriding drop-in if networkd
// is present.
type networkdNetplanDropin struct {
	// Match is the systemd-networkd ini file's [Match] section.
	Match systemdMatchConfig

	// Network is the systemd-networkd ini file's [Network] section.
	Network systemdNetworkConfig `ini:"Network"`

	// DHCPv4 is the systemd-networkd ini file's [DHCPv4] section.
	DHCPv4 *systemdDHCPConfig `ini:",omitempty"`

	// DHCPv6 is the systemd-networkd ini file's [DHCPv4] section.
	DHCPv6 *systemdDHCPConfig `ini:",omitempty"`
}

// Name returns the name of the network manager service.
func (n netplan) Name() string {
	return "netplan"
}

// Configure gives the opportunity for the Service implementation to adjust its configuration
// based on the Guest Agent configuration.
func (n netplan) Configure(ctx context.Context, config *cfg.Sections) {
}

// IsManaging checks whether netplan is present in the system.
func (n netplan) IsManaging(ctx context.Context, iface string) (bool, error) {
	// Check if the netplan CLI exists.
	if _, err := execLookPath("netplan"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("error looking up dhclient path: %v", err)
	}
	return true, nil
}

// SetupEthernetInterface sets the network interfaces for netplan by writing drop-in files to the specified
// configuration directory.
func (n netplan) SetupEthernetInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	// Create a network configuration file with default configurations for each network interface.
	googleInterfaces, googleIpv6Interfaces := interfaceListsIpv4Ipv6(nics.EthernetInterfaces)

	mtuMap, err := interfacesMTUMap(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("error listing interface's MTU configuration: %w", err)
	}

	// Write the config files.
	if err := n.writeNetplanEthernetDropin(mtuMap, googleInterfaces, googleIpv6Interfaces); err != nil {
		return fmt.Errorf("error writing network configs: %v", err)
	}

	// If we are running netplan+systemd-networkd we try to write networkd's drop-in for configs
	// not mapped/supported by netplan.
	if err := n.writeNetworkdDropin(googleInterfaces, googleIpv6Interfaces); err != nil {
		return fmt.Errorf("error writing systemd-networkd's drop-in: %v", err)
	}

	osInfo := osinfoGet()
	// Debian 12 has a pretty generic matching netplan configuration for gce, until we have that
	// changed we are only removing.
	if osInfo.OS == "debian" && osInfo.Version.Major == 12 {
		if err := os.Remove("/etc/netplan/90-default.yaml"); err != nil {
			logger.Debugf("Failed to remove default netplan config: %s", err)
		}
	}

	// Avoid restarting systemd-networkd.
	if err := run.Quiet(ctx, "networkctl", "reload"); err != nil {
		return fmt.Errorf("error reloading systemd-networkd network configs: %v", err)
	}

	// Avoid restarting systemd-networkd.
	if err := run.Quiet(ctx, "netplan", "apply"); err != nil {
		return fmt.Errorf("error applying netplan changes: %w", err)
	}

	return nil
}

// writeNetworkdDropin writes the overloading network-manager's drop-in file for the configurations
// not supported by netplan.
func (n netplan) writeNetworkdDropin(interfaces, ipv6Interfaces []string) error {
	stat, err := os.Stat(n.networkdDropinDir)
	if err != nil {
		return fmt.Errorf("failed to stat systemd-networkd's drop-in root dir: %w", err)
	}

	if !stat.IsDir() {
		return fmt.Errorf("systemd-networkd drop-in dir(%s) is not a dir", n.networkdDropinDir)
	}

	for i, iface := range interfaces {
		logger.Debugf("writing systemd-networkd drop-in config for %s", iface)

		var dhcp = "ipv4"
		if slices.Contains(ipv6Interfaces, iface) {
			dhcp = "yes"
		}

		// Create and setup ini file.
		data := networkdNetplanDropin{
			Match: systemdMatchConfig{
				Name: iface,
			},
			Network: systemdNetworkConfig{
				DNSDefaultRoute: true,
				DHCP:            dhcp,
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
			return fmt.Errorf("failed to write systemd drop-in config: %w", err)
		}
	}

	return nil
}

// networkdDropinFile returns an interface's netplan drop-in file path.
func (n netplan) networkdDropinFile(iface string) string {
	// We are hardcoding the netplan priority to 10 since we are deriving the netplan
	// networkd configuration name based on the interface name only - aligning with
	// the commonly used value for netplan.
	return filepath.Join(n.networkdDropinDir, fmt.Sprintf("10-netplan-%s.network.d", iface), "override.conf")
}

// write writes systemd's drop-in config file.
func (nd networkdNetplanDropin) write(n netplan, iface string) error {
	dropinFile := n.networkdDropinFile(iface)

	logger.Infof("writing systemd drop in to: %s", dropinFile)

	dropinDir := filepath.Dir(dropinFile)
	err := os.MkdirAll(dropinDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create networkd dropin dir: %w", err)
	}

	if err := writeIniFile(dropinFile, &nd); err != nil {
		return fmt.Errorf("error saving netword drop-in file for %s: %v", iface, err)
	}

	return nil
}

// writeNetplanEthernetDropin selects the ethernet configuration, transforms it
// into a netplan dropin format and writes it down to the netplan's drop-in directory.
func (n netplan) writeNetplanEthernetDropin(mtuMap map[string]int, interfaces, ipv6Interfaces []string) error {
	dropin := netplanDropin{
		Network: netplanNetwork{
			Version:   netplanConfigVersion,
			Ethernets: make(map[string]netplanEthernet),
		},
	}

	for i, iface := range interfaces {
		logger.Debugf("Adding %s(%d) to drop-in configuration.", iface, i)

		trueVal := true
		ne := netplanEthernet{
			Match:  netplanMatch{Name: iface},
			DHCPv4: &trueVal,
			DHCP4Overrides: &netplanDHCPOverrides{
				UseDomains: true,
			},
		}

		if mtu, found := mtuMap[iface]; found {
			ne.MTU = &mtu
		}

		if slices.Contains(ipv6Interfaces, iface) {
			ne.DHCPv6 = &trueVal
			ne.DHCP6Overrides = &netplanDHCPOverrides{
				UseDomains: true,
			}
		}

		dropin.Network.Ethernets[iface] = ne
	}

	if err := n.write(dropin, netplanEthernetSuffix); err != nil {
		return fmt.Errorf("failed to write netplan ethernet drop-in config: %+v", err)
	}

	return nil
}

// write writes the netplan dropin file.
func (n netplan) write(nd netplanDropin, suffix string) error {
	dropinFile := n.dropinFile(suffix)
	if err := writeYamlFile(dropinFile, &nd); err != nil {
		return fmt.Errorf("error saving netplan drop-in file %s: %w", dropinFile, err)
	}
	return nil
}

// dropinFile returns the netplan drop-in file.
// Priority is lexicographically sorted in ascending order by file name. So a configuration
// starting with '1-' takes priority over a configuration file starting with '10-'. Setting
// a priority of 1 allows the guest-agent to override any existing default configurations
// while also allowing users the freedom of using priorities of '0...' to override the
// agent's own configurations.
func (n netplan) dropinFile(suffix string) string {
	return filepath.Join(n.netplanConfigDir, fmt.Sprintf("%d-google-guest-agent-%s.yaml", n.priority, suffix))
}

// SetupVlanInterface writes the apppropriate vLAN interfaces netplan configuration.
func (n netplan) SetupVlanInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	// Retrieves the ethernet nics so we can detect the parent one.
	googleInterfaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("could not list interfaces names: %+v", err)
	}

	dropin := netplanDropin{
		Network: netplanNetwork{
			Version: netplanConfigVersion,
		},
	}

	for i, curr := range nics.VlanInterfaces {
		parentInterface, err := vlanParentInterface(googleInterfaces, curr)
		if err != nil {
			return fmt.Errorf("failed to determine vlan's parent interface: %+v", err)
		}

		iface := fmt.Sprintf("gcp.%s.%d", parentInterface, curr.Vlan)
		logger.Debugf("Adding %s(%d) to drop-in configuration.", iface, i)

		trueVal := true
		nv := netplanVlan{
			ID:     curr.Vlan,
			Link:   parentInterface,
			DHCPv4: &trueVal,
		}

		if len(curr.IPv6) > 0 {
			nv.DHCPv6 = &trueVal
		}

		dropin.Network.Vlans[iface] = nv
	}

	if err := n.write(dropin, netplanVlanSuffix); err != nil {
		return fmt.Errorf("failed to write netplan vlan drop-in config: %+v", err)
	}

	return nil
}

// Rollback deletes the ethernet and VLAN interfaces netplan drop-in files.
func (n netplan) Rollback(ctx context.Context, nics *Interfaces) error {
	logger.Infof("rolling back changes for %s", n.Name())

	interfaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("failed to get list of interface names: %v", err)
	}

	var deleteMe []string
	for _, iface := range interfaces {
		// Set networkd drop-in override file for removal.
		networkdDropinFile := n.networkdDropinFile(iface)
		deleteMe = append(deleteMe, networkdDropinFile)

		// Set netplan ethernet drop-in file for removal.
		netplanEthernetDropinFile := n.dropinFile(netplanEthernetSuffix)
		deleteMe = append(deleteMe, netplanEthernetDropinFile)

		// Set netplan vlan drop-in file for removal.
		netplanVlanDropinFile := n.dropinFile(netplanVlanSuffix)
		deleteMe = append(deleteMe, netplanVlanDropinFile)
	}

	for _, configFile := range deleteMe {
		if err := os.Remove(configFile); err != nil {
			if !os.IsNotExist(err) {
				logger.Debugf("Failed to remove drop-in file(%s): %s", configFile, err)
			} else {
				logger.Debugf("No such drop-in file(%s), ignoring.", configFile)
			}
		}
	}

	return nil
}
