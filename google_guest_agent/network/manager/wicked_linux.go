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
	"path"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

type wicked struct {
	// configDir is the directory to which to write configuration files.
	configDir string
}

const (
	// defaultWickedConfigDir is the default location for wicked configuration files.
	defaultWickedConfigDir = "/etc/sysconfig/network"

	// wickedCommand is the expected path to the wicked CLI.
	wickedCommand = "/usr/sbin/wicked"
)

// init adds this network manager service to the list of known managers.
func init() {
	registerManager(&wicked{
		configDir: defaultWickedConfigDir,
	}, false)
}

// Name returns the name of this network manager service.
func (n wicked) Name() string {
	return "wicked"
}

// IsManaging checks whether wicked is managing the provided interface.
func (n wicked) IsManaging(ctx context.Context, iface string) (bool, error) {
	// Check the current main network service. Primarily applicable to SUSE images.
	res := run.WithOutput(ctx, "systemctl", "status", "network.service")
	if strings.Contains(res.StdOut, "wicked.service") {
		return true, nil
	}

	// Check if the wicked service is running.
	res = run.WithOutput(ctx, "systemctl", "is-active", "wicked.service")
	if res.ExitCode != 0 {
		return false, nil
	}

	// Check the status of configured interfaces.
	res = run.WithOutput(ctx, wickedCommand, "ifstatus", iface, "--brief")
	if res.ExitCode != 0 {
		return false, fmt.Errorf("failed to check status of wicked configuration: %s", res.StdErr)
	}
	fields := strings.Fields(res.StdOut)
	if fields[1] == "up" || fields[1] == "setup-in-progress" {
		return true, nil
	}
	return false, nil
}

// SetupEthernetInterface writes the necessary configuration files for each interface and enables them.
func (n wicked) SetupEthernetInterface(ctx context.Context, cfg *cfg.Sections, nics *Interfaces) error {
	if len(nics.EthernetInterfaces) < 2 {
		return nil
	}
	ifaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("failed to get network interfaces: %v", err)
	}

	if err = writeWickedConfigs(n.configDir, ifaces[1:]); err != nil {
		return fmt.Errorf("error writing wicked configurations: %v", err)
	}

	args := append([]string{"ifup"}, ifaces[1:]...)
	if err = run.Quiet(ctx, wickedCommand, args...); err != nil {
		return fmt.Errorf("error enabling interfaces: %v", err)
	}
	return nil
}

// SetupVlanInterface writes the apppropriate vLAN interfaces configuration for the network manager service
// for all configured interfaces.
func (n wicked) SetupVlanInterface(ctx context.Context, cfg *cfg.Sections, nics *Interfaces) error {
	return nil
}

// writeWickedConfigs writes config files for the given ifaces in the given configuration
// directory.
func writeWickedConfigs(configDir string, ifaces []string) error {
	var priority = 10100

	// Write the config for all the non-primary network interfaces.
	for _, iface := range ifaces {
		logger.Debugf("write enabling ifcfg-%s config", iface)

		var ifcfg *os.File

		ifcfg, err := os.Create(ifcfgFilePath(configDir, iface))
		if err != nil {
			return err
		}
		defer ifcfg.Close()
		contents := []string{
			googleComment,
			"STARTMODE=hotplug",
			// NOTE: 'dhcp' is the dhcp4+dhcp6 option.
			"BOOTPROTO=dhcp",
			fmt.Sprintf("DHCLIENT_ROUTE_PRIORITY=%d", priority),
		}
		_, err = ifcfg.WriteString(strings.Join(contents, "\n"))
		if err != nil {
			return fmt.Errorf("error writing config file for %s: %v", iface, err)
		}
		priority += 100
	}
	return nil
}

// ifcfgFilePath gets the file path for the configuration file for the given interface.
func ifcfgFilePath(configDir, iface string) string {
	return path.Join(configDir, fmt.Sprintf("ifcfg-%s", iface))
}

// Rollback deletes all the ifcfg files written by Setup, then reloads wicked.service.
func (n wicked) Rollback(ctx context.Context, nics *Interfaces) error {
	ifaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("failed to get network interfaces: %v", err)
	}

	// Since configuration files are only written for non-primary, only check
	// for non-primary configuration files.
	for _, iface := range ifaces[1:] {
		configFile := ifcfgFilePath(n.configDir, iface)

		// Check if the file exists.
		if _, err = os.Stat(configFile); err != nil {
			continue
		}

		// Check that it contains the google comment.
		contents, err := os.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("error reading config file for %s: %v", iface, err)
		}

		lines := strings.Split(string(contents), "\n")
		for _, line := range lines {
			if line == googleComment {
				// Delete the file.
				if err = os.Remove(configFile); err != nil {
					return fmt.Errorf("error deleting config file for %s: %v", iface, err)
				}

				// Reload for this interface.
				if err = run.Quiet(ctx, wickedCommand, "ifreload", iface); err != nil {
					return fmt.Errorf("error reloading config for %s: %v", iface, err)
				}
				break
			}
		}
	}
	return nil
}
