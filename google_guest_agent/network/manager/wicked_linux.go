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
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

type wicked struct {
	// configDir is the directory to which to write configuration files.
	configDir string

	// wickedCommand contains the fully qualified path of wicked cli.
	wickedCommand string
}

const (
	// defaultWickedConfigDir is the default location for wicked configuration files.
	defaultWickedConfigDir = "/etc/sysconfig/network"

	// defaultWickedCommand is the expected path to the wicked CLI.
	defaultWickedCommand = "/usr/sbin/wicked"
)

// init adds this network manager service to the list of known managers.
func init() {
	wickedCommand, err := exec.LookPath("wicked")
	if err != nil {
		logger.Infof("failed to find wicked path, falling back to default: %+v", err)
		wickedCommand = defaultWickedCommand
	}

	registerManager(&wicked{
		configDir:     defaultWickedConfigDir,
		wickedCommand: wickedCommand,
	}, false)
}

// Name returns the name of this network manager service.
func (n wicked) Name() string {
	return "wicked"
}

// Configure gives the opportunity for the Service implementation to adjust its configuration
// based on the Guest Agent configuration.
func (n wicked) Configure(ctx context.Context, config *cfg.Sections) {
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
	res = run.WithOutput(ctx, n.wickedCommand, "ifstatus", iface, "--brief")
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

	if err = n.writeEthernetConfigs(ifaces[1:]); err != nil {
		return fmt.Errorf("error writing wicked configurations: %v", err)
	}

	args := append([]string{"ifup"}, ifaces[1:]...)
	if err = run.Quiet(ctx, n.wickedCommand, args...); err != nil {
		return fmt.Errorf("error enabling interfaces: %v", err)
	}
	return nil
}

// SetupVlanInterface writes the apppropriate vLAN interfaces configuration for the network manager service
// for all configured interfaces.
func (n wicked) SetupVlanInterface(ctx context.Context, cfg *cfg.Sections, nics *Interfaces) error {
	// Retrieves the ethernet nics so we can detect the parent one.
	googleInterfaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("could not list interfaces names: %+v", err)
	}

	var keepMe []string

	// Make sure the dhclient route priority is in a different range of ethernet nics.
	priority := 20200

	for _, curr := range nics.VlanInterfaces {
		parentInterface, err := vlanParentInterface(googleInterfaces, curr)
		if err != nil {
			return fmt.Errorf("failed to determine vlan's parent interface: %+v", err)
		}

		iface := fmt.Sprintf("gcp.%s.%d", parentInterface, curr.Vlan)

		configLines := []string{
			googleComment,
			"BOOTPROTO=dhcp", // NOTE: 'dhcp' is the dhcp4+dhcp6 option.
			"VLAN=yes",
			"ETHTOOL_OPTIONS=reorder_hdr off",
			fmt.Sprintf("DEVICE=%s", iface),
			fmt.Sprintf("MTU=%d", curr.MTU),
			fmt.Sprintf("LLADDR=%s", curr.Mac),
			fmt.Sprintf("ETHERDEVICE=%s", parentInterface),
			fmt.Sprintf("VLAN_ID=%d", curr.Vlan),
			fmt.Sprintf("DHCLIENT_ROUTE_PRIORITY=%d", priority),
		}

		ifcfg, err := os.Create(n.ifcfgFilePath(iface))
		if err != nil {
			return fmt.Errorf("failed to create vlan's ifcfg file: %+v", err)
		}

		content := strings.Join(configLines, "\n")
		writeLen, err := ifcfg.WriteString(content)
		if err != nil {
			return fmt.Errorf("error writing vlan's icfg file for %s: %v", iface, err)
		}

		contentLen := len(content)
		if writeLen != contentLen {
			return fmt.Errorf("error writing vlan's ifcfg file, wrote %d bytes, config content size is %d bytes",
				writeLen, contentLen)
		}

		if err = run.Quiet(ctx, n.wickedCommand, "ifup", iface); err != nil {
			return fmt.Errorf("error enabling vlan's interfaces: %v", err)
		}

		priority += 100
		keepMe = append(keepMe, iface)
	}

	if err := n.cleanupVlanInterfaces(ctx, keepMe); err != nil {
		return fmt.Errorf("failed to cleanup vlan interfaces: %+v", err)
	}

	return nil
}

// cleanupVlanInterfaces removes vlan interfaces no longer configured, i.e. they were previously
// added by the user but removed later.
func (n wicked) cleanupVlanInterfaces(ctx context.Context, keepMe []string) error {
	files, err := os.ReadDir(n.configDir)
	if err != nil {
		return fmt.Errorf("failed to read content from %s: %+v", n.configDir, err)
	}

	configExp := `(?P<prefix>ifcfg)-(?P<interface>gcp\..*\..*)`
	configRegex := regexp.MustCompile(configExp)

	for _, file := range files {
		var (
			iface string
			found bool
		)

		if file.IsDir() {
			continue
		}

		groups := utils.RegexGroupsMap(configRegex, file.Name())

		// If we don't have a matching interface skip it.
		if iface, found = groups["interface"]; !found {
			continue
		}

		if slices.Contains(keepMe, iface) {
			continue
		}

		if err := n.removeInterface(ctx, iface); err != nil {
			return fmt.Errorf("failed to remove vlan interface: %+v", err)
		}
	}

	return nil
}

// writeEthernetConfigs writes config files for the given ifaces in the given configuration
// directory.
func (n wicked) writeEthernetConfigs(ifaces []string) error {
	var priority = 10100

	// Write the config for all the non-primary network interfaces.
	for _, iface := range ifaces {
		logger.Debugf("write enabling ifcfg-%s config", iface)

		var ifcfg *os.File

		ifcfg, err := os.Create(n.ifcfgFilePath(iface))
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
func (n wicked) ifcfgFilePath(iface string) string {
	return path.Join(n.configDir, fmt.Sprintf("ifcfg-%s", iface))
}

func (n wicked) removeInterface(ctx context.Context, iface string) error {
	configFilePath := n.ifcfgFilePath(iface)

	// Check if the file exists.
	info, err := os.Stat(configFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat wicked ifcfg file: %+v", err)
	}

	commentLen := len(googleComment)

	// We definetly don't manage this file, skip it.
	if info.Size() < int64(commentLen) {
		return nil
	}

	configFile, err := os.Open(configFilePath)
	if err != nil {
		return fmt.Errorf("failed to open wicked ifcfg file: %+v", err)
	}
	defer configFile.Close()

	buffer := make([]byte, commentLen)
	readSize, err := configFile.Read(buffer)
	if err != nil {
		return fmt.Errorf("failed to read google comment from wicked ifcfg file: %+v", err)
	}

	if readSize != commentLen {
		return fmt.Errorf("failed to read comment section, read %d bytes, expected to read %d",
			readSize, commentLen)
	}

	// This file is clearly not managed by us.
	if string(buffer) != googleComment {
		return nil
	}

	// Delete the ifcfg file.
	if err = os.Remove(configFilePath); err != nil {
		return fmt.Errorf("error deleting config file for %s: %v", iface, err)
	}

	// Reload for this interface.
	if err = run.Quiet(ctx, n.wickedCommand, "ifreload", iface); err != nil {
		return fmt.Errorf("error reloading config for %s: %v", iface, err)
	}

	return nil
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
		if err := n.removeInterface(ctx, iface); err != nil {
			return fmt.Errorf("failed to rollback wicked ethernet interface: %+v", err)
		}
	}

	for _, curr := range nics.VlanInterfaces {
		parentInterface, err := vlanParentInterface(ifaces, curr)
		if err != nil {
			return fmt.Errorf("failed to determine vlan's parent interface: %+v", err)
		}

		iface := fmt.Sprintf("%s.%d", parentInterface, curr.Vlan)
		if err := n.removeInterface(ctx, iface); err != nil {
			return fmt.Errorf("failed to rollback wicked ethernet interface: %+v", err)
		}
	}

	return nil
}
