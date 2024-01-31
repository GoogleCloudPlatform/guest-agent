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
	"slices"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/go-ini/ini"
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
	priority string
}

// init adds this network manager service to the list of known network managers.
func init() {
	registerManager(&systemdNetworkd{
		configDir:      "/usr/lib/systemd/network",
		networkCtlKeys: []string{"AdministrativeState", "SetupState"},
		priority:       "1",
	}, false)
}

// guestAgent contains the guest-agent control flags/data.
type guestAgent struct {
	// ManagedByGuestAgent determines if a given configuration was written and is
	// managed by guest-agent.
	ManagedByGuestAgent bool
}

// systemdMatchConfig contains the systemd-networkd's interface matching criteria.
type systemdMatchConfig struct {
	// Name is the matching criteria based on the interface name.
	Name string
}

// systemdNetworkConfig contains the actual interface rule's configuration.
type systemdNetworkConfig struct {
	// DHCP determines the ipv4/ipv6 protocol version for use with dhcp.
	DHCP string

	// DNSDefaultRoute is used to determine if the link's configured DNS servers are
	// used for resolving domain names that do not match any link's domain.
	DNSDefaultRoute bool
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
	GuestAgent guestAgent

	// Match is the systemd-networkd ini file's [Match] section.
	Match systemdMatchConfig

	// Network is the systemd-networkd ini file's [Network] section.
	Network systemdNetworkConfig

	// DHCPv4 is the systemd-networkd ini file's [DHCPv4] section.
	DHCPv4 *systemdDHCPConfig `ini:",omitempty"`

	// DHCPv6 is the systemd-networkd ini file's [DHCPv4] section.
	DHCPv6 *systemdDHCPConfig `ini:",omitempty"`
}

// Name returns the name of the network manager service.
func (n systemdNetworkd) Name() string {
	return "systemd-networkd"
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

// Setup sets up the non-primary network interfaces for systemd-networkd by writing
// configuration files to the specified configuration directory.
func (n systemdNetworkd) Setup(ctx context.Context, config *cfg.Sections, payload []metadata.NetworkInterfaces) error {
	// Create a network configuration file with default configurations for each network interface.
	googleInterfaces, googleIpv6Interfaces := interfaceListsIpv4Ipv6(payload)

	// Write the config files.
	if err := writeSystemdConfig(googleInterfaces, googleIpv6Interfaces, n.configDir, n.priority); err != nil {
		return fmt.Errorf("error writing network configs: %v", err)
	}

	// Avoid restarting systemd-networkd.
	if err := run.Quiet(ctx, "networkctl", "reload"); err != nil {
		return fmt.Errorf("error reloading systemd-networkd network configs: %v", err)
	}
	return nil
}

// writeSystemdConfig writes the systemd config for all the provided interfaces in the
// provided directory using the given priority.
func writeSystemdConfig(interfaces, ipv6Interfaces []string, configDir, priority string) error {
	for i, iface := range interfaces {
		logger.Debugf("write systemd-networkd network config for %s", iface)

		var dhcp = "ipv4"
		if slices.Contains(ipv6Interfaces, iface) {
			dhcp = "yes"
		}

		// Create and setup ini file.
		data := systemdConfig{
			GuestAgent: guestAgent{
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

		config := ini.Empty()
		if err := ini.ReflectFrom(config, &data); err != nil {
			return fmt.Errorf("error creating config ini: %v", err)
		}

		// Priority is lexicographically sorted in ascending order by file name. So a configuration
		// starting with '1-' takes priority over a configuration file starting with '10-'. Setting
		// a priority of 1 allows the guest-agent to override any existing default configurations
		// while also allowing users the freedom of using priorities of '0...' to override the
		// agent's own configurations.
		configPath := fmt.Sprintf("%s/%s-%s-google-guest-agent.network", configDir, priority, iface)
		if err := config.SaveTo(configPath); err != nil {
			return fmt.Errorf("error saving config for %s: %v", iface, err)
		}
	}
	return nil
}

// Rollback deletes the configuration files created by the agent for systemd-networkd.
func (n systemdNetworkd) Rollback(ctx context.Context, payload []metadata.NetworkInterfaces) error {
	logger.Infof("rolling back changes for %s", n.Name())
	interfaces, err := interfaceNames(payload)
	if err != nil {
		return fmt.Errorf("failed to get list of interface names: %v", err)
	}

	for _, iface := range interfaces {
		// Find expected files.
		configFile := fmt.Sprintf("%s/%s-%s-google-guest-agent.network", n.configDir, n.priority, iface)
		logger.Debugf("checking for %s", configFile)
		opts := ini.LoadOptions{
			Loose:       true,
			Insensitive: true,
		}
		config, err := ini.LoadSources(opts, configFile)
		if err != nil {
			// Could not find the file.
			continue
		}

		// Parse the config ini.
		sections := new(systemdConfig)
		if err = config.MapTo(sections); err != nil {
			return fmt.Errorf("error parsing config ini: %v", err)
		}

		// Check that the guest section exists and the key is set to true.
		if sections.GuestAgent.ManagedByGuestAgent {
			logger.Debugf("removing %s", configFile)
			if err = os.Remove(configFile); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	// Avoid restarting systemd-networkd.
	if err := run.Quiet(ctx, "networkctl", "reload"); err != nil {
		return fmt.Errorf("error reloading systemd-networkd network configs: %v", err)
	}
	return nil
}
