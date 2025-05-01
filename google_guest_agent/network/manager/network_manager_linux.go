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
	"os"
	"path/filepath"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	// defaultNetworkManagerConfigDir is the directory where the network manager nmconnection files are stored.
	defaultNetworkManagerConfigDir = "/etc/NetworkManager/system-connections"

	// defaultNetworkScriptsDir is the directory where the old (no longer managed) ifcfg files are stored.
	defaultNetworkScriptsDir = "/etc/sysconfig/network-scripts"

	// nmConfigFileMode is the file mode for the NetworkManager config files.
	// The permissions need to be 600 in order for nmcli to load and use the file correctly.
	nmConfigFileMode = 0600
)

// nmConnectionSection is the connection section of NetworkManager's keyfile.
type nmConnectionSection struct {
	// InterfaceName is the name of the interface to configure.
	InterfaceName string `ini:"interface-name"`

	// ID is the unique ID for this connection.
	ID string `ini:"id"`

	// ConnType is the type of connection (i.e. ethernet).
	ConnType string `ini:"type"`
}

// nmIPv4Section is the ipv4 section of NetworkManager's keyfile.
type nmIPv4Section struct {
	// Method is the IP configuration method. Supports "auto", "manual", and "link-local".
	Method string `ini:"method"`
}

// nmIPSection is the ipv6 section of NetworkManager's keyfile.
type nmIPv6Section struct {
	// Method is the IP configuration method. Supports "auto", "manual", and "link-local".
	Method string `ini:"method"`

	// MTU is MTU configuration for the interface. Default is auto, we set it explicitly
	// for VLAN based interfaces.
	MTU int `ini:"mtu"`
}

// nmConfig is a wrapper containing all the sections for the NetworkManager keyfile.
type nmConfig struct {
	// GuestAgent is the 'guest-agent' section.
	GuestAgent guestAgentSection `ini:"guest-agent"`

	// Connection is the connection section.
	Connection nmConnectionSection `ini:"connection"`

	// Ipv4 is the ipv4 section.
	Ipv4 nmIPv4Section `ini:"ipv4"`

	// Ipv6 is the ipv6 section.
	Ipv6 nmIPv6Section `ini:"ipv6"`

	// Vlan is the vlan section.
	Vlan *nmVlan `ini:"vlan,omitempty"`

	// Ethernet is 802-3-ethernet section.
	Ethernet *nmEthernet `ini:"ethernet,omitempty"`
}

// nmEthernet is the [802-3-ethernet setting] section of nm-settings.
// See https://networkmanager.dev/docs/api/latest/nm-settings-nmcli.html for more details.
type nmEthernet struct {
	// OverrideMacAddress requests that the device use this MAC address instead. This is
	// required in case of VLAN NICs which otherwise by default ends up using parent NICs address.
	OverrideMacAddress string `ini:"cloned-mac-address"`

	// MTU is MTU configuration for the interface. Default is auto, we set it explicitly
	// for VLAN based interfaces.
	MTU int `ini:"mtu"`
}

// nmVlan is the [vlan setting] section of nm-settings.
type nmVlan struct {
	// Flags are one or more flags which control the behavior and features of the VLAN interface.
	// See vlan.flags at https://networkmanager.dev/docs/api/latest/nm-settings-nmcli.html for details.
	Flags int `ini:"flags"`

	// ID is the actual Vlan ID.
	ID int `ini:"id"`

	// Parent is the name of the parent interface.
	Parent string `ini:"parent"`
}

// networkManager implements the manager.Service interface for NetworkManager.
type networkManager struct {
	// configDir is the directory to which to write the configuration files.
	configDir string

	// networkScriptsDir is the directory containing no longer supported ifcfg files, this files
	// need to be migrated case they are found.
	networkScriptsDir string
}

// Name is the name of this network manager service.
func (n *networkManager) Name() string {
	return "NetworkManager"
}

// Configure gives the opportunity for the Service implementation to adjust its configuration
// based on the Guest Agent configuration.
func (n *networkManager) Configure(ctx context.Context, config *cfg.Sections) {
}

// IsManaging checks if NetworkManager is managing the provided interface.
func (n *networkManager) IsManaging(ctx context.Context, iface string) (bool, error) {
	// Check whether NetworkManager.service is active.
	if err := run.Quiet(ctx, "systemctl", "is-active", "NetworkManager.service"); err != nil {
		return false, nil
	}

	// Check for existence of nmcli. Without nmcli, the agent cannot tell NetworkManager
	// to reload the configs for its connections.
	exists, err := cliExists("nmcli")
	if !exists {
		return false, err
	}

	// Use nmcli to check status of provided  interface.
	res := run.WithOutput(ctx, "nmcli", "-t", "-f", "DEVICE,STATE", "dev", "status")
	if res.ExitCode != 0 {
		return false, fmt.Errorf("error checking status of devices on NetworkManager: %v", res.StdErr)
	}

	output := res.StdOut
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, iface) {
			fields := strings.Split(line, ":")
			return fields[1] == "connected", nil
		}
	}
	return false, nil
}

// Setup sets up the necessary configurations for NetworkManager.
func (n *networkManager) SetupEthernetInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	ifaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("error getting interfaces: %v", err)
	}

	interfaces, err := n.writeNetworkManagerConfigs(ifaces)
	if err != nil {
		return fmt.Errorf("error writing NetworkManager connection configs: %v", err)
	}

	// This is primarily for RHEL-7 compatibility. Without reloading, attempting to
	// enable the connections in the next step returns a "mismatched interface" error.
	if err := run.Quiet(ctx, "nmcli", "conn", "reload"); err != nil {
		return fmt.Errorf("error reloading NetworkManager config cache: %v", err)
	}

	logger.Infof("Bringing up %v connection profiles", interfaces)

	// Enable the new connections. List will contain only those IDs which are added by
	// agent and needs to be refreshed.
	for _, ifname := range interfaces {
		// https://networkmanager.dev/docs/api/latest/nmcli.html
		if err = run.Quiet(ctx, "nmcli", "conn", "up", "id", ifname); err != nil {
			return fmt.Errorf("error enabling connection %s: %v", ifname, err)
		}
	}
	return nil
}

// vlanInterfaceName generates vlan interface name based on parent interface
// name and VLAN ID.
func (n *networkManager) vlanInterfaceName(parentInterface string, vlanID int) string {
	return fmt.Sprintf("gcp.%s.%d", parentInterface, vlanID)
}

// SetupVlanInterface writes the apppropriate vLAN interfaces configuration for the network manager service
// for all configured interfaces.
func (n *networkManager) SetupVlanInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	if len(nics.VlanInterfaces) == 0 {
		logger.Debugf("No VLAN interfaces found, skipping setup")
		return nil
	}

	if err := n.writeVLANConfigs(nics); err != nil {
		return fmt.Errorf("error writing NetworkManager VLAN configs: %w", err)
	}

	if err := run.Quiet(ctx, "nmcli", "conn", "reload"); err != nil {
		return fmt.Errorf("error reloading NetworkManager config cache for VLAN interfaces: %v", err)
	}

	return nil
}

// writeVLANConfigs writes NetworkManager configs for VLAN interfaces.
func (n *networkManager) writeVLANConfigs(nics *Interfaces) error {
	for _, curr := range nics.VlanInterfaces {
		iface := n.vlanInterfaceName(curr.ParentInterfaceID, curr.Vlan)
		cfgFile := n.networkManagerConfigFilePath(iface)
		connID := fmt.Sprintf("google-guest-agent-%s", iface)

		nmCfg := nmConfig{
			GuestAgent: guestAgentSection{
				ManagedByGuestAgent: true,
			},
			Connection: nmConnectionSection{
				InterfaceName: iface,
				ID:            connID,
				ConnType:      "vlan",
			},
			Vlan: &nmVlan{
				// 1 is NM_VLAN_FLAG_REORDER_HEADERS.
				Flags:  1,
				ID:     curr.Vlan,
				Parent: curr.ParentInterfaceID,
			},
			Ipv4: nmIPv4Section{
				Method: "auto",
			},
			Ipv6: nmIPv6Section{
				Method: "auto",
				MTU:    curr.MTU,
			},
			Ethernet: &nmEthernet{
				OverrideMacAddress: curr.Mac,
				MTU:                curr.MTU,
			},
		}

		if err := writeIniFile(cfgFile, &nmCfg); err != nil {
			return fmt.Errorf("error writing vlan config for %q: %v", iface, err)
		}

		// If the permission is not properly set nmcli will fail to load the file correctly.
		if err := os.Chmod(cfgFile, nmConfigFileMode); err != nil {
			return fmt.Errorf("error updating permissions for %s: %w", cfgFile, err)
		}
	}

	return nil
}

// networkManagerConfigFilePath gets the config file path for the provided interface.
func (n *networkManager) networkManagerConfigFilePath(iface string) string {
	return filepath.Join(n.configDir, fmt.Sprintf("google-guest-agent-%s.nmconnection", iface))
}

func (n *networkManager) ifcfgFilePath(iface string) string {
	return filepath.Join(n.networkScriptsDir, fmt.Sprintf("ifcfg-%s", iface))
}

// writeNetworkManagerConfigs writes the configuration files for NetworkManager.
func (n *networkManager) writeNetworkManagerConfigs(ifaces []string) ([]string, error) {
	var result []string

	for i, iface := range ifaces {
		if !shouldManageInterface(i == 0) {
			logger.Debugf("ManagePrimaryNIC is disabled, skipping writeNetworkManagerConfigs for %s", iface)
			continue
		}
		if isInvalid(iface) {
			continue
		}
		logger.Debugf("writing nmconnection file for %s", iface)

		configFilePath := n.networkManagerConfigFilePath(iface)
		connID := fmt.Sprintf("google-guest-agent-%s", iface)

		// Create the ini file.
		config := nmConfig{
			GuestAgent: guestAgentSection{
				ManagedByGuestAgent: true,
			},
			Connection: nmConnectionSection{
				InterfaceName: iface,
				ID:            connID,
				ConnType:      "ethernet",
			},
			Ipv4: nmIPv4Section{
				Method: "auto",
			},
			Ipv6: nmIPv6Section{
				Method: "auto",
			},
		}

		// Save the config.
		if err := writeIniFile(configFilePath, &config); err != nil {
			return []string{}, fmt.Errorf("error saving connection config for %s: %v", iface, err)
		}

		// The permissions need to be 600 in order for nmcli to load and use the file correctly.
		if err := os.Chmod(configFilePath, nmConfigFileMode); err != nil {
			return []string{}, fmt.Errorf("error updating permissions for %s connection config: %v", iface, err)
		}

		// Clean up the files written by the old agent. Make sure they're managed
		// by the agent before deleting them.
		ifcfgFilePath := n.ifcfgFilePath(iface)
		contents, err := os.ReadFile(ifcfgFilePath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read ifcfg file(%s): %v", ifcfgFilePath, err)
		}

		// Check for the google comment.
		if strings.Contains(string(contents), "# Added by Google Compute Engine OS Login.") {
			if err = os.Remove(ifcfgFilePath); err != nil {
				return nil, fmt.Errorf("failed to remove previously managed ifcfg file(%s): %v", ifcfgFilePath, err)
			}
		}

		result = append(result, connID)
	}

	return result, nil
}

func (n *networkManager) rollbackConfigs(ctx context.Context, nics *Interfaces, removeVlan bool) error {
	ifaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("getting interfaces: %v", err)
	}

	reconnectPrimaryNic := false

	for i, iface := range ifaces {
		removed, err := n.removeInterface(iface)
		if err != nil {
			logger.Errorf("Failed to remove %q interface with error: %v", iface, err)
		}
		if i == 0 && removed {
			reconnectPrimaryNic = true
		}
	}

	if removeVlan {
		for _, vnic := range nics.VlanInterfaces {
			iface := n.vlanInterfaceName(vnic.ParentInterfaceID, vnic.Vlan)
			if _, err := n.removeInterface(iface); err != nil {
				logger.Errorf("Failed to remove %q interface with error: %v", iface, err)
			}
		}
	}

	if err := run.Quiet(ctx, "nmcli", "conn", "reload"); err != nil {
		return fmt.Errorf("error reloading NetworkManager config cache: %v", err)
	}

	// NetworkManager will not create a default connection if we are removing the one
	// we manage, in that case we need to force it to connect and then with that create
	// a default connection.
	if reconnectPrimaryNic {
		if err := run.Quiet(ctx, "nmcli", "device", "connect", ifaces[0]); err != nil {
			return fmt.Errorf("error connecting NetworkManager's managed interface: %s, %v", ifaces[0], err)
		}
	}

	return nil
}

// Rollback deletes the configurations created by Setup() - both regular and vlan nics
// are handed.
func (n *networkManager) Rollback(ctx context.Context, nics *Interfaces) error {
	return n.rollbackConfigs(ctx, nics, true)
}

// Rollback deletes the configurations created by Setup() - only regular nics
// are handled.
func (n *networkManager) RollbackNics(ctx context.Context, nics *Interfaces) error {
	return n.rollbackConfigs(ctx, nics, false)
}

// removeInterface verifies .nmconnection is managed by Guest Agent and removes it.
// It returns true if the configuration removal succeeds and false otherwise.
func (n *networkManager) removeInterface(iface string) (bool, error) {
	configFilePath := n.networkManagerConfigFilePath(iface)

	_, err := os.Stat(configFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("unable to remove %q interface, stat failed on %q: %w", iface, configFilePath, err)
		}
		logger.Debugf("NetworkManager's configuration file %q doesn't exist, ignoring.", configFilePath)
		return false, nil
	}

	config := new(nmConfig)
	if err := readIniFile(configFilePath, config); err != nil {
		return false, fmt.Errorf("failed to load NetworkManager %q file: %v", configFilePath, err)
	}

	if config.GuestAgent.ManagedByGuestAgent {
		logger.Debugf("Attempting to remove NetworkManager configuration %s", configFilePath)

		if err = os.Remove(configFilePath); err != nil {
			return false, fmt.Errorf("error deleting config file for %s: %v", iface, err)
		}
	}
	return true, nil
}
