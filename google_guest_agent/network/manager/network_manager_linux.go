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
	"os"
	"os/exec"
	"path"
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

// nmIPSection is the ipv4/ipv6 section of NetworkManager's keyfile.
type nmIPSection struct {
	// Method is the IP configuration method. Supports "auto", "manual", and "link-local".
	Method string `ini:"method"`
}

// nmConfig is a wrapper containing all the sections for the NetworkManager keyfile.
type nmConfig struct {
	// GuestAgent is the 'guest-agent' section.
	GuestAgent guestAgentSection `ini:"guest-agent"`

	// Connection is the connection section.
	Connection nmConnectionSection `ini:"connection"`

	// Ipv4 is the ipv4 section.
	Ipv4 nmIPSection `ini:"ipv4"`

	// Ipv6 is the ipv6 section.
	Ipv6 nmIPSection `ini:"ipv6"`
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
func (n networkManager) Name() string {
	return "NetworkManager"
}

// Configure gives the opportunity for the Service implementation to adjust its configuration
// based on the Guest Agent configuration.
func (n *networkManager) Configure(ctx context.Context, config *cfg.Sections) {
}

// IsManaging checks if NetworkManager is managing the provided interface.
func (n networkManager) IsManaging(ctx context.Context, iface string) (bool, error) {
	// Check whether NetworkManager.service is active.
	if err := run.Quiet(ctx, "systemctl", "is-active", "NetworkManager.service"); err != nil {
		return false, nil
	}

	// Check for existence of nmcli. Without nmcli, the agent cannot tell NetworkManager
	// to reload the configs for its connections.
	if _, err := execLookPath("nmcli"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("error checking for nmcli: %v", err)
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
func (n networkManager) SetupEthernetInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
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

	// Enable the new connections. Ignore the primary interface as it will already be up.
	for _, ifname := range interfaces[1:] {
		if err = run.Quiet(ctx, "nmcli", "conn", "up", "ifname", ifname); err != nil {
			return fmt.Errorf("error enabling connection %s: %v", ifname, err)
		}
	}
	return nil
}

// SetupVlanInterface writes the apppropriate vLAN interfaces configuration for the network manager service
// for all configured interfaces.
func (n networkManager) SetupVlanInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error {
	return nil
}

// networkManagerConfigFilePath gets the config file path for the provided interface.
func (n networkManager) networkManagerConfigFilePath(iface string) string {
	return path.Join(n.configDir, fmt.Sprintf("google-guest-agent-%s.nmconnection", iface))
}

func (n networkManager) ifcfgFilePath(iface string) string {
	return path.Join(n.networkScriptsDir, fmt.Sprintf("ifcfg-%s", iface))
}

// writeNetworkManagerConfigs writes the configuration files for NetworkManager.
func (n networkManager) writeNetworkManagerConfigs(ifaces []string) ([]string, error) {
	var result []string

	for _, iface := range ifaces {
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
			Ipv4: nmIPSection{
				Method: "auto",
			},
			Ipv6: nmIPSection{
				Method: "auto",
			},
		}

		// Save the config.
		if err := writeIniFile(configFilePath, &config); err != nil {
			return []string{}, fmt.Errorf("error saving connection config for %s: %v", iface, err)
		}

		// The permissions need to be 600 in order for nmcli to load and use the file correctly.
		if err := os.Chmod(configFilePath, 0600); err != nil {
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

		result = append(result, iface)
	}

	return result, nil
}

// Rollback deletes the configurations created by Setup().
func (n networkManager) Rollback(ctx context.Context, nics *Interfaces) error {
	ifaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("getting interfaces: %v", err)
	}

	for _, iface := range ifaces {
		configFilePath := path.Join(n.configDir, fmt.Sprintf("google-guest-agent-%s.nmconnection", iface))

		_, err := os.Stat(configFilePath)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Debugf("Can't remove NetworkManager configuration(%s), %s", configFilePath, err)
			} else {
				logger.Debugf("NetworkManager's configuration file doesn't exist, ignoring.")
			}
			continue
		}

		config := new(nmConfig)
		if err := readIniFile(configFilePath, config); err != nil {
			return fmt.Errorf("failed to load NetworkManager .nmconnection file: %v", err)
		}

		if config.GuestAgent.ManagedByGuestAgent {
			logger.Debugf("Attempting to remove NetworkManager configuration %s", configFilePath)

			if err = os.Remove(configFilePath); err != nil {
				return fmt.Errorf("error deleting config file for %s: %v", iface, err)
			}
		}
	}

	if err := run.Quiet(ctx, "nmcli", "conn", "reload"); err != nil {
		return fmt.Errorf("error reloading NetworkManager config cache: %v", err)
	}
	return nil
}
