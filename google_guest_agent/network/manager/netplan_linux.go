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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/osinfo"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
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

	// interfacePrefix is prefix used to override default netplan config. This prefix is
	// used with netplan interface config keys in /run/netplan/20-google-guest-agent-ethernet.yaml
	// and systemd drop-in directory name like /etc/systemd/network/10-netplan-a-ens4.network.d/
	interfacePrefix string
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

// netplanEthernet describes the actual ethernet configuration. Refer
// https://netplan.readthedocs.io/en/stable/netplan-yaml/#properties-for-device-type-ethernets
// for more details.
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
	UseDomains *bool `yaml:"use-domains,omitempty"`
}

// netplanMatch contains the keys uses to match an interface.
type netplanMatch struct {
	// Name is the key used to match an interface by its name.
	Name string `yaml:"name"`
}

// netplanVlan describes the netplan's vlan interface configuration.
// Refer https://netplan.readthedocs.io/en/stable/netplan-yaml/#properties-for-device-type-vlans
// for more details.
type netplanVlan struct {
	// ID is the the VLAN ID.
	ID int `yaml:"id,omitempty"`

	// Link is the vlan's parent interface.
	Link string `yaml:"link,omitempty"`

	// DHCPv4 determines if DHCPv4 support must be enabled to such an interface.
	DHCPv4 *bool `yaml:"dhcp4,omitempty"`

	// DHCPv6 determines if DHCPv6 support must be enabled to such an interface.
	DHCPv6 *bool `yaml:"dhcp6,omitempty"`

	// OverrideMacAddress sets the device’s MAC address. By default it will use same as
	// parent NIC.
	OverrideMacAddress string `yaml:"macaddress,omitempty"`

	// MTU sets the MTU for interface. The default is 1500.
	MTU int `yaml:"mtu,omitempty"`

	// DHCP4Overrides sets DHCP4 overrides for the vlan.
	DHCP4Overrides *netplanDHCPOverrides `yaml:"dhcp4-overrides,omitempty"`

	// DHCP6Overrides sets DHCP6 overrides for the vlan.
	DHCP6Overrides *netplanDHCPOverrides `yaml:"dhcp6-overrides,omitempty"`
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
}

// Name returns the name of the network manager service.
func (n *netplan) Name() string {
	return "netplan"
}

// Configure gives the opportunity for the Service implementation to adjust its configuration
// based on the Guest Agent configuration.
func (n *netplan) Configure(ctx context.Context, config *cfg.Sections) {
	os := osinfo.Get()
	// Debian 12 has a pretty generic matching netplan configuration for gce,
	// regex in /etc/netplan/90-default.yaml matches all en* and eth* nics.
	// Until we have that changed we are adjusting the configuration so we can
	// override the defaults.
	if os.OS == "debian" && os.Version.Major == 12 {
		n.interfacePrefix = "a"
		logger.Infof("Setting up Debian 12, overriding interface prefix with: %q", n.interfacePrefix)
	}
}

// IsManaging checks whether netplan is present in the system.
func (n *netplan) IsManaging(ctx context.Context, iface string) (bool, error) {
	if isUbuntu1804() {
		logger.Infof("Running on Ubuntu 18.04, skipping use of netplan, falling back to dhclient")
		return false, nil
	}

	// Check if the netplan CLI exists.
	return cliExists("netplan")
}

// SetupEthernetInterface sets the network interfaces for netplan by writing drop-in files to the specified
// configuration directory.
func (n *netplan) SetupEthernetInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	// Create a network configuration file with default configurations for each network interface.
	googleInterfaces, googleIpv6Interfaces := interfaceListsIpv4Ipv6(nics.EthernetInterfaces)

	mtuMap, err := interfacesMTUMap(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("error listing interface's MTU configuration: %w", err)
	}

	// Write the config files.
	reload1, err := n.writeNetplanEthernetDropin(mtuMap, googleInterfaces, googleIpv6Interfaces)
	if err != nil {
		return fmt.Errorf("error writing network configs: %v", err)
	}

	// If we are running netplan+systemd-networkd we try to write networkd's drop-in for configs
	// not mapped/supported by netplan.
	reload2, err := n.writeNetworkdDropin(googleInterfaces, googleIpv6Interfaces)
	if err != nil {
		return fmt.Errorf("error writing systemd-networkd's drop-in: %v", err)
	}

	// Avoid unnecessary reloads, if we've really updated some config then only do a reload.
	if reload1 || reload2 {
		if err := n.reloadConfigs(ctx); err != nil {
			return fmt.Errorf("error applying ethernet interface configs: %w", err)
		}
	}

	return nil
}

// reloadConfigs triggers config reload to make sure ethernet/vlan configs are written
// on disk are applied by netplan.
func (n *netplan) reloadConfigs(ctx context.Context) error {
	logger.Infof("Reloading netplan configs...")

	// Avoid restarting netplan.
	if err := run.Quiet(ctx, "netplan", "generate"); err != nil {
		return fmt.Errorf("error generating netplan based config: %w", err)
	}

	// Avoid restarting systemd-networkd.
	if err := run.Quiet(ctx, "networkctl", "reload"); err != nil {
		return fmt.Errorf("error reloading systemd-networkd network configs: %v", err)
	}

	return nil
}

// writeNetworkdDropin writes the overloading network-manager's drop-in file for the configurations
// not supported by netplan.
func (n *netplan) writeNetworkdDropin(interfaces, ipv6Interfaces []string) (bool, error) {
	var requiresReload bool
	stat, err := os.Stat(n.networkdDropinDir)
	if err != nil {
		return false, fmt.Errorf("failed to stat systemd-networkd's drop-in root dir: %w", err)
	}

	if !stat.IsDir() {
		return false, fmt.Errorf("systemd-networkd drop-in dir(%s) is not a dir", n.networkdDropinDir)
	}

	for i, iface := range interfaces {
		if !shouldManageInterface(i == 0) {
			logger.Debugf("ManagePrimaryNIC is disabled, skipping writeNetworkdDropin for %s", iface)
			continue
		}
		if isInvalid(iface) {
			continue
		}
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
		}

		wrote, err := data.write(n, iface)
		if err != nil {
			return false, fmt.Errorf("failed to write systemd drop-in config: %w", err)
		}

		if wrote {
			requiresReload = true
		}
	}

	return requiresReload, nil
}

// networkdDropinFile returns an interface's netplan drop-in file path.
func (n *netplan) networkdDropinFile(iface string) string {
	// We are hardcoding the netplan priority to 10 since we are deriving the netplan
	// networkd configuration name based on the interface name only - aligning with
	// the commonly used value for netplan.
	if n.interfacePrefix != "" {
		return filepath.Join(n.networkdDropinDir, fmt.Sprintf("10-netplan-%s-%s.network.d", n.interfacePrefix, iface), "override.conf")
	}

	return filepath.Join(n.networkdDropinDir, fmt.Sprintf("10-netplan-%s.network.d", iface), "override.conf")
}

// isSame unmarshals netplan networkd dropin config from cfgFile and compares it with
// own instance. If it fails to read it returns false to allow caller to try
// overwriting and fix any issues if file already exists.
func (nd networkdNetplanDropin) isSame(cfgFile string) bool {
	existingCfgs := networkdNetplanDropin{}
	if err := readIniFile(cfgFile, &existingCfgs); err != nil {
		logger.Debugf("Failed to read %q while comparing netplan networkd dropins with error: %v", cfgFile, err)
		return false
	}

	return reflect.DeepEqual(nd, existingCfgs)
}

// write writes systemd's drop-in config file.
func (nd networkdNetplanDropin) write(n *netplan, iface string) (bool, error) {
	dropinFile := n.networkdDropinFile(iface)

	logger.Infof("writing systemd drop in to: %s", dropinFile)

	dropinDir := filepath.Dir(dropinFile)
	if err := os.MkdirAll(dropinDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create networkd dropin dir: %w", err)
	}

	if nd.isSame(dropinFile) {
		logger.Infof("Exact same config already exists at location %q, skipping overwriting to avoid network reload", dropinFile)
		return false, nil
	}

	if err := writeIniFile(dropinFile, &nd); err != nil {
		return false, fmt.Errorf("error saving netword drop-in file for %s: %v", iface, err)
	}

	return true, nil
}

// shouldUseDomains returns true if interface index is 0.
func shouldUseDomains(idx int) *bool {
	res := idx == 0
	return &res
}

// writeNetplanEthernetDropin selects the ethernet configuration, transforms it
// into a netplan dropin format and writes it down to the netplan's drop-in directory.
func (n *netplan) writeNetplanEthernetDropin(mtuMap map[string]int, interfaces, ipv6Interfaces []string) (bool, error) {
	dropin := netplanDropin{
		Network: netplanNetwork{
			Version:   netplanConfigVersion,
			Ethernets: make(map[string]netplanEthernet),
		},
	}

	for i, iface := range interfaces {
		if !shouldManageInterface(i == 0) {
			logger.Debugf("ManagePrimaryNIC is disabled, skipping writeNetplanEthernetDropin for %s", iface)
			continue
		}
		if isInvalid(iface) {
			continue
		}
		logger.Debugf("Adding %s(%d) to drop-in configuration.", iface, i)

		trueVal := true
		ne := netplanEthernet{
			Match:  netplanMatch{Name: iface},
			DHCPv4: &trueVal,
			DHCP4Overrides: &netplanDHCPOverrides{
				UseDomains: shouldUseDomains(i),
			},
		}

		if mtu, found := mtuMap[iface]; found {
			ne.MTU = &mtu
		}

		if slices.Contains(ipv6Interfaces, iface) {
			ne.DHCPv6 = &trueVal
			ne.DHCP6Overrides = &netplanDHCPOverrides{
				UseDomains: shouldUseDomains(i),
			}
		}

		key := n.ID(iface)
		dropin.Network.Ethernets[key] = ne
	}

	// This can happen if its a single NIC VM and primary NIC is not managed
	// by Guest Agent. No need to write a file with just version in [dropin].
	if len(dropin.Network.Ethernets) == 0 {
		logger.Infof("No NICs to configure, skipping writeNetplanEthernetDropin")
		return false, nil
	}

	wrote, err := n.write(dropin, netplanEthernetSuffix)
	if err != nil {
		return false, fmt.Errorf("failed to write netplan ethernet drop-in config: %+v", err)
	}

	return wrote, nil
}

// ID returns the Netplan ID used for referencing parent NIC in VLAN NIC
// configuration and the key in ethernet based NIC configuration.
func (n *netplan) ID(iface string) string {
	key := iface
	if n.interfacePrefix != "" {
		key = fmt.Sprintf("%s-%s", n.interfacePrefix, iface)
	}
	return key
}

// isSame unmarshals netplan dropin config from cfgFile and compares it with
// own instance. If it fails to read it returns false to allow caller to try
// overwriting and fix any issues if file already exists.
func (nd netplanDropin) isSame(cfgFile string) bool {
	existingDropin := netplanDropin{}
	if err := readYamlFile(cfgFile, &existingDropin); err != nil {
		logger.Debugf("Failed to read %q while comparing netplan dropins with error: %v", cfgFile, err)
		return false
	}

	return reflect.DeepEqual(nd, existingDropin)
}

// write writes the netplan dropin file.
func (n *netplan) write(nd netplanDropin, suffix string) (bool, error) {
	dropinFile := n.dropinFile(suffix)
	dropinDir := filepath.Dir(dropinFile)
	err := os.MkdirAll(dropinDir, 0755)
	if err != nil {
		return false, fmt.Errorf("failed to create networkd dropin dir: %w", err)
	}

	if nd.isSame(dropinFile) {
		logger.Infof("Exact same config already exists at location %q, skipping overwriting to avoid network reload", dropinFile)
		return false, nil
	}

	if err := writeYamlFile(dropinFile, &nd); err != nil {
		return false, fmt.Errorf("error saving netplan drop-in file %s: %w", dropinFile, err)
	}
	return true, nil
}

// dropinFile returns the netplan drop-in file.
// Priority is lexicographically sorted in ascending order by file name. So a configuration
// starting with '1-' takes priority over a configuration file starting with '10-'. Setting
// a priority of 1 allows the guest-agent to override any existing default configurations
// while also allowing users the freedom of using priorities of '0...' to override the
// agent's own configurations.
func (n *netplan) dropinFile(suffix string) string {
	return filepath.Join(n.netplanConfigDir, fmt.Sprintf("%d-google-guest-agent-%s.yaml", n.priority, suffix))
}

func (n *netplan) vlanInterfaceName(parentInterface string, vlanID int) string {
	return fmt.Sprintf("gcp.%s.%d", parentInterface, vlanID)
}

// SetupVlanInterface writes the apppropriate vLAN interfaces netplan configuration.
func (n *netplan) SetupVlanInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	var reload1, reload2, reload3 bool
	var err error

	toRemove, err := n.findVlanDiff(nics)
	if err != nil {
		return fmt.Errorf("unable to detect vlan nics to delete: %w", err)
	}

	if toRemove != nil {
		reload1, err = n.rollbackVlanNics(ctx, toRemove)
		if err != nil {
			return fmt.Errorf("unable to remove vlan interfaces (%+v): %w", toRemove, err)
		}
	}

	reload2, err = n.writeNetplanVLANDropin(nics)
	if err != nil {
		return fmt.Errorf("unable to write netplan VLAN dropin: %w", err)
	}

	reload3, err = n.writeNetworkdVLANDropin(nics)
	if err != nil {
		return fmt.Errorf("unable to write netplan networkd VLAN dropin: %w", err)
	}

	if reload1 || reload2 || reload3 {
		if err = n.reloadConfigs(ctx); err != nil {
			return fmt.Errorf("error applying vlan interface configs: %w", err)
		}
	}

	return nil
}

// interfaceFromLink gets the interface name from link name in netplan config.
// Link name in some cases might not be same as interface name as we prefix with "a"
// for precedence.
func (n *netplan) interfaceFromLink(link string) string {
	iface := link
	if n.interfacePrefix != "" {
		iface = strings.TrimPrefix(iface, fmt.Sprintf("%s-", n.interfacePrefix))
	}
	return iface
}

// findVlanDiff compares expectedNics with one configured with netplan config on disk
// and returns only the vlan interfaces to delete.
func (n *netplan) findVlanDiff(expectedNics *Interfaces) (*Interfaces, error) {
	keepInterfaces := make(map[string]string)
	toRemove := Interfaces{VlanInterfaces: make(map[string]VlanInterface)}

	existingVlanCfgs := netplanDropin{}
	netplanVlanDropinFile := n.dropinFile(netplanVlanSuffix)
	// There's no config file per interface, single netplan config file lists all the interfaces.
	if !utils.FileExists(netplanVlanDropinFile, utils.TypeFile) {
		logger.Infof("File %q does not exist, nothing to rollback", netplanVlanDropinFile)
		return nil, nil
	}

	if err := readYamlFile(netplanVlanDropinFile, &existingVlanCfgs); err != nil {
		return nil, fmt.Errorf("unable to read %q trying rollback configs: %w", netplanVlanDropinFile, err)
	}

	if len(existingVlanCfgs.Network.Vlans) == 0 {
		logger.Debugf("No existing VLAN configs found at %q, skipping rollback", netplanVlanDropinFile)
		return nil, nil
	}

	for _, iface := range expectedNics.VlanInterfaces {
		// Set netplan vlan drop-in file for removal.
		ifaceName := n.vlanInterfaceName(iface.ParentInterfaceID, iface.Vlan)
		key := n.ID(ifaceName)
		keepInterfaces[key] = ifaceName
	}

	for vlanKey, vlan := range existingVlanCfgs.Network.Vlans {
		_, ok := keepInterfaces[vlanKey]
		if !ok {
			parentID := n.interfaceFromLink(vlan.Link)
			vlanID := fmt.Sprintf("%s-%d", parentID, vlan.ID)
			toRemove.VlanInterfaces[vlanID] = VlanInterface{
				ParentInterfaceID: parentID,
				VlanInterface: metadata.VlanInterface{
					Vlan: vlan.ID,
				},
			}
		}
	}

	return &toRemove, nil
}

// rollbackVlanNics removes the [nics] and its config (netplan and networkd dropin both) on disk.
func (n *netplan) rollbackVlanNics(ctx context.Context, nics *Interfaces) (bool, error) {
	var deleteNics []string
	var deleteDirs []string

	if len(nics.VlanInterfaces) == 0 {
		logger.Debugf("No VLAN interfaces in args, skipping rollback")
		return false, nil
	}

	existingVlanCfgs := netplanDropin{}
	netplanVlanDropinFile := n.dropinFile(netplanVlanSuffix)
	// There's no config file per interface, single netplan config file lists all the interfaces.
	if utils.FileExists(netplanVlanDropinFile, utils.TypeFile) {
		if err := readYamlFile(netplanVlanDropinFile, &existingVlanCfgs); err != nil {
			return false, fmt.Errorf("unable to read %q trying rollback configs: %w", netplanVlanDropinFile, err)
		}
	}

	for _, iface := range nics.VlanInterfaces {
		ifaceName := n.vlanInterfaceName(iface.ParentInterfaceID, iface.VlanInterface.Vlan)
		key := n.ID(ifaceName)

		deleteNics = append(deleteNics, key)
		delete(existingVlanCfgs.Network.Vlans, key)

		dropin := filepath.Join(n.networkdDropinDir, fmt.Sprintf("10-netplan-%s.network.d", key))
		if utils.FileExists(dropin, utils.TypeDir) {
			// Networkd dropin is a directory for each interface with override.conf file.
			// Remove should delete the complete directory instead of that one file.
			deleteDirs = append(deleteDirs, dropin)
		}
	}

	logger.Infof("Deleting VLAN NICs: %v", deleteNics)
	// Simply removing configs on disk and reloading netplan/networkctl doesn't remove
	// existing vlan nics, it requires instance reboot or systemd-networkd restart. Instead,
	// make sure its removed by [networkctl delete <interfaces>] command.
	args := []string{"delete"}
	args = append(args, deleteNics...)
	if err := run.Quiet(ctx, "networkctl", args...); err != nil {
		return false, fmt.Errorf("networkctl %v failed with error: %w", args, err)
	}

	// If no more VLANs exist simply remove the file.
	if len(existingVlanCfgs.Network.Vlans) == 0 {
		logger.Infof("Removing %s dropin file for vlan rollback", netplanVlanDropinFile)
		if err := os.Remove(netplanVlanDropinFile); err != nil {
			return false, fmt.Errorf("unable to remove netplan vlan dropin (%s): %w", netplanVlanDropinFile, err)
		}
	} else {
		logger.Infof("Updating %s dropin file for vlan rollback", netplanVlanDropinFile)
		// Otherwise, overwrite configs to reflect expected interfaces.
		if _, err := n.write(existingVlanCfgs, netplanVlanSuffix); err != nil {
			return false, fmt.Errorf("unable to update vlan config at (%s): %w", netplanVlanDropinFile, err)
		}
	}

	logger.Infof("Removing directories %v as part of vlan rollback", deleteDirs)
	for _, dir := range deleteDirs {
		if err := os.RemoveAll(dir); err != nil {
			return false, fmt.Errorf("unable to remove directory %q: %w", dir, err)
		}
	}

	return true, nil
}

func (n *netplan) writeNetplanVLANDropin(nics *Interfaces) (bool, error) {
	dropin := netplanDropin{
		Network: netplanNetwork{
			Version: netplanConfigVersion,
			Vlans:   make(map[string]netplanVlan),
		},
	}

	for _, curr := range nics.VlanInterfaces {
		iface := n.vlanInterfaceName(curr.ParentInterfaceID, curr.Vlan)
		logger.Debugf("Adding %s(%d) to drop-in configuration.", iface, curr.Vlan)

		trueVal := true
		falseVal := false
		nv := netplanVlan{
			ID:                 curr.Vlan,
			Link:               n.ID(curr.ParentInterfaceID),
			DHCPv4:             &trueVal,
			OverrideMacAddress: curr.Mac,
			MTU:                curr.MTU,
			DHCP4Overrides:     &netplanDHCPOverrides{UseDomains: &falseVal},
			DHCP6Overrides:     &netplanDHCPOverrides{UseDomains: &falseVal},
		}

		if len(curr.IPv6) > 0 {
			nv.DHCPv6 = &trueVal
		}

		key := n.ID(iface)
		dropin.Network.Vlans[key] = nv
	}

	if len(nics.VlanInterfaces) == 0 {
		return false, nil
	}

	wrote, err := n.write(dropin, netplanVlanSuffix)
	if err != nil {
		return false, fmt.Errorf("failed to write netplan vlan drop-in config: %+v", err)
	}

	return wrote, nil
}

func (n *netplan) writeNetworkdVLANDropin(nics *Interfaces) (bool, error) {
	var reload bool

	stat, err := os.Stat(n.networkdDropinDir)
	if err != nil {
		return false, fmt.Errorf("failed to stat systemd-networkd's drop-in root dir: %w", err)
	}

	if !stat.IsDir() {
		return false, fmt.Errorf("systemd-networkd drop-in dir(%s) is not a dir", n.networkdDropinDir)
	}

	for _, iface := range nics.VlanInterfaces {
		logger.Debugf("writing systemd-networkd drop-in config for VLAN ID: %d", iface.Vlan)

		var dhcp = "ipv4"
		if iface.DHCPv6Refresh != "" {
			dhcp = "yes"
		}

		ifaceName := n.vlanInterfaceName(iface.ParentInterfaceID, iface.Vlan)
		matchID := n.ID(ifaceName)

		// Create and setup ini file.
		data := networkdNetplanDropin{
			Match: systemdMatchConfig{
				Name: matchID,
			},
			Network: systemdNetworkConfig{
				DNSDefaultRoute: false,
				DHCP:            dhcp,
			},
			DHCPv4: &systemdDHCPConfig{
				RoutesToDNS: false,
				RoutesToNTP: false,
			},
		}

		wrote, err := data.write(n, ifaceName)
		if err != nil {
			return false, fmt.Errorf("failed to write systemd drop-in config for VLAN ID(%s): %w", ifaceName, err)
		}

		if wrote {
			reload = true
		}
	}

	return reload, nil
}

// rollbackConfigs is the low level implementation for Rollback and RollbackNics interface.
// If removeVlan is true both regular nics and vlan nics are rolled back.
func (n *netplan) rollbackConfigs(ctx context.Context, nics *Interfaces, removeVlan bool) error {
	var reload bool
	interfaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("failed to get list of interface names: %v", err)
	}

	netplanEthernetDropinFile := n.dropinFile(netplanEthernetSuffix)
	existingEthernetCfgs := netplanDropin{}
	if utils.FileExists(netplanEthernetDropinFile, utils.TypeFile) {
		if err := readYamlFile(netplanEthernetDropinFile, &existingEthernetCfgs); err != nil {
			return fmt.Errorf("unable to read %q trying rollback configs: %w", netplanEthernetDropinFile, err)
		}
	}

	var deleteMe []string
	for _, iface := range interfaces {
		// Set networkd drop-in override file for removal.
		networkdDropinFile := n.networkdDropinFile(iface)
		deleteMe = append(deleteMe, networkdDropinFile)

		// Set netplan ethernet drop-in file for removal.
		if _, ok := existingEthernetCfgs.Network.Ethernets[iface]; ok {
			deleteMe = append(deleteMe, netplanEthernetDropinFile)
		}
	}

	if removeVlan {
		if done, err := n.rollbackVlanNics(ctx, nics); err != nil {
			logger.Debugf("Failed to remove vlan interfaces: %v", err)
		} else {
			if done {
				reload = true
			}
		}
	}

	for _, configFile := range deleteMe {
		logger.Debugf("Removing config file: %q", configFile)
		if err := os.Remove(configFile); err != nil {
			if !os.IsNotExist(err) {
				logger.Debugf("Failed to remove drop-in file(%s): %s", configFile, err)
			} else {
				logger.Debugf("No such drop-in file(%s), ignoring.", configFile)
			}
			continue
		}
		reload = true
	}

	if !reload {
		return nil
	}

	if err := n.reloadConfigs(ctx); err != nil {
		return fmt.Errorf("error reloading configs: %v", err)
	}

	return nil
}

// Rollback deletes the ethernet and VLAN interfaces netplan drop-in files.
func (n *netplan) Rollback(ctx context.Context, nics *Interfaces) error {
	logger.Infof("rolling back changes for %s", n.Name())
	return n.rollbackConfigs(ctx, nics, true)
}

// Rollback deletes the ethernet interfaces netplan drop-in files - only
// regular nics are handled.
func (n *netplan) RollbackNics(ctx context.Context, nics *Interfaces) error {
	logger.Infof("rolling back regular ethernet changes for %s", n.Name())
	return n.rollbackConfigs(ctx, nics, false)
}
