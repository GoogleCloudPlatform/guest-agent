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
	"slices"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/osinfo"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

// Service is an interface for setting up network configurations
// using different network managing services, such as systemd-networkd and wicked.
type Service interface {
	// IsManaging checks whether this network manager service is managing the provided interface.
	IsManaging(ctx context.Context, iface string) (bool, error)

	// Name is the name of the network manager service.
	Name() string

	// Setup writes the appropriate configurations for the network manager service for all
	// non-primary network interfaces.
	Setup(ctx context.Context, config *cfg.Sections, payload []metadata.NetworkInterfaces) error

	// Rollback rolls back the changes created in Setup.
	Rollback(ctx context.Context, payload []metadata.NetworkInterfaces) error
}

// osConfigRule describes matching rules for OS's, used for specifying either
// network interfaces to ignore during setup or native config hookups.
type osConfigRule struct {
	// osNames is a list of OS's names matching the rule (as described by osInfo)
	osNames []string

	// majorVersions is a map of this OS's versions to ignore.
	majorVersions map[int]bool

	// action defines what action or exception to perform for this rule.
	action osConfigAction
}

// osConfigAction defines the action to be taken in an osConfigRule.
type osConfigAction struct {
	// ignorePrimary determines whether to ignore the primary network interface.
	ignorePrimary bool

	// ignoreSecondary determines whether to ignore all non-primary network interfaces.
	ignoreSecondary bool

	// nativeOSConfig is a function pointer to the specific implementation that
	// enables/disables OS management of the provided nics.
	// NOTE: This will eventually be moved to the specific network manager implementation.
	nativeOSConfig func(ctx context.Context, nic []string) error

	// ignoreSetup indicates whether to ignore the rest of the network management setup.
	// This is used with nativeOSConfig to determine if, after running the function, the
	// agent should continue with the rest of the setup.
	ignoreSetup bool
}

const (
	googleComment = "# Added by Google Compute Engine Guest Agent."

	// osConfigRuleAnyVersion applies a rule for any version of an OS.
	osConfigRuleAnyVersion = -1
)

var (
	// knownNetworkManagers contains the list of known network managers. This is
	// used to determine the network manager service that is managing the primary
	// network interface.
	knownNetworkManagers []Service

	// fallbackNetworkManager is the network manager service to be assumed if
	// none of the known network managers returned true from IsManaging()
	fallbackNetworkManager Service

	// currManager is the Service implementation currently managing the interfaces.
	currManager Service

	// defaultOSRules lists the rules for applying interface configurations for primary
	// and secondary interfaces.
	defaultOSRules = []osConfigRule{
		// Debian rules.
		{
			osNames: []string{"debian"},
			majorVersions: map[int]bool{
				10: true,
				11: true,
				12: true,
			},
			action: osConfigAction{
				ignorePrimary:   true,
				ignoreSecondary: true,
			},
		},
		// RHEL rules.
		{
			osNames: []string{"rhel", "centos", "rocky"},
			majorVersions: map[int]bool{
				7: true,
				8: true,
				9: true,
			},
			action: osConfigAction{
				ignorePrimary: true,
			},
		},
		{
			osNames: []string{"rhel", "centos", "rocky"},
			majorVersions: map[int]bool{
				osConfigRuleAnyVersion: true,
			},
			action: osConfigAction{
				nativeOSConfig: rhelNativeOSConfig,
			},
		},
		// SUSE rules
		{
			osNames: []string{"sles"},
			majorVersions: map[int]bool{
				osConfigRuleAnyVersion: true,
			},
			action: osConfigAction{
				nativeOSConfig: slesNativeOSConfig,
				ignoreSetup:    true,
			},
		},
		{
			osNames: []string{"ubuntu"},
			majorVersions: map[int]bool{
				18: true,
				20: true,
				22: true,
				23: true,
			},
			action: osConfigAction{
				ignorePrimary: true,
			},
		},
	}

	// osinfoGet points to the function to use for getting osInfo.
	// Primarily used for testing.
	osinfoGet = osinfo.Get

	// osRules points to the rules to use for finding relevant ignore rules.
	// Primarily used for testing.
	osRules = defaultOSRules
)

// registerManager registers the provided network manager service to the list of known
// network manager services. Fallback specifies whether the provided service should be
// marked as a fallback service.
func registerManager(s Service, fallback bool) {
	if !fallback {
		knownNetworkManagers = append(knownNetworkManagers, s)
	} else {
		if fallbackNetworkManager != nil {
			panic("trying to register second fallback network manager")
		} else {
			fallbackNetworkManager = s
		}
	}
}

// detectNetworkManager detects the network manager managing the primary network interface.
// This network manager will be used to set up primary and secondary network interfaces.
func detectNetworkManager(ctx context.Context, iface string) (Service, error) {
	logger.Infof("Detecting network manager...")

	networkManagers := knownNetworkManagers
	if fallbackNetworkManager != nil {
		networkManagers = append(networkManagers, fallbackNetworkManager)
	}

	for _, curr := range networkManagers {
		ok, err := curr.IsManaging(ctx, iface)
		if err != nil {
			return nil, err
		}
		if ok {
			logger.Infof("Network manager detected: %s", curr.Name())
			return curr, nil
		}
	}
	return nil, fmt.Errorf("no network manager impl found for %s", iface)
}

// findOSRule finds the osConfigRule that applies to the current system.
func findOSRule(broadVersion bool) *osConfigRule {
	osInfo := osinfoGet()
	for _, curr := range osRules {
		if !slices.Contains(curr.osNames, osInfo.OS) {
			continue
		}

		if broadVersion && curr.majorVersions[osConfigRuleAnyVersion] {
			return &curr
		}

		if !broadVersion && curr.majorVersions[osInfo.Version.Major] {
			return &curr
		}
	}
	return nil
}

// shouldManageInterface returns whether the guest agent should manage an interface
// provided whether the interface of interest is the primary interface or not.
func shouldManageInterface(isPrimary bool) bool {
	rule := findOSRule(false)
	if rule != nil {
		if isPrimary {
			return !rule.action.ignorePrimary
		}
		return !rule.action.ignoreSecondary
	}
	// Assume setup for anything not specified.
	return true
}

// SetupInterfaces sets up all the network interfaces on the system, applying rules described
// by osRules and using the native network manager service detected to be managing the primary
// network interface.
func SetupInterfaces(ctx context.Context, config *cfg.Sections, nics []metadata.NetworkInterfaces) error {
	// User may have disabled network interface setup entirely.
	if !config.NetworkInterfaces.Setup {
		logger.Infof("network interface setup disabled, skipping...")
		return nil
	}

	interfaces, err := interfaceNames(nics)
	if err != nil {
		return fmt.Errorf("error getting interface names: %v", err)
	}
	primaryInterface := interfaces[0]

	// Apply the OS-specific rules.
	osRule := findOSRule(true)
	if osRule != nil && osRule.action.nativeOSConfig != nil {
		logger.Infof("Found OS config rule. Running...")
		if err = osRule.action.nativeOSConfig(ctx, interfaces); err != nil {
			return fmt.Errorf("failed to disable OS nic management: %v", err)
		}
		// Don't run setup.
		if osRule.action.ignoreSetup {
			return nil
		}
	}

	// Get the network manager.
	nm, err := detectNetworkManager(ctx, primaryInterface)
	if err != nil {
		return fmt.Errorf("error detecting network manager service: %v", err)
	}

	// Since the manager is different, undo all the changes of the old manager.
	if currManager != nil && nm != currManager {
		if err = currManager.Rollback(ctx, nics); err != nil {
			return fmt.Errorf("error rolling back config for %s: %v", currManager.Name(), err)
		}
	}

	currManager = nm
	if err = nm.Setup(ctx, config, nics); err != nil {
		return fmt.Errorf("error setting up %s: %v", nm.Name(), err)
	}
	return nil
}

// slesNativeOSConfig writes on ifcfg file for each interface, then runs
// `wicked ifup eth1 ... ethN`
// NOTE: May remove entirely later on due to default configurations.
func slesNativeOSConfig(ctx context.Context, interfaces []string) error {
	var err error
	var priority = 10100
	for _, iface := range interfaces {
		logger.Debugf("write enabling ifcfg-%s config", iface)

		var ifcfg *os.File
		ifcfg, err = os.Create("/etc/sysconfig/network/ifcfg-" + iface)
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
			return err
		}

		priority += 100
	}
	args := append([]string{"ifup", "--timeout", "1"}, interfaces...)
	return run.Quiet(ctx, "/usr/sbin/wicked", args...)
}

// rhelNativeOSConfig writes an ifcfg file with DHCP and NetworkManager disabled
// to all secondary nics.
func rhelNativeOSConfig(ctx context.Context, interfaces []string) error {
	for _, curr := range interfaces[1:] {
		if err := writeRHELIfcfg(curr); err != nil {
			return err
		}
	}
	return nil
}

// writeRHELIfcfg writes the ifcfg file for the specified interface.
func writeRHELIfcfg(iface string) error {
	logger.Debugf("write disabling ifcfg-%s config", iface)
	filename := "/etc/sysconfig/network-scripts/ifcfg-" + iface
	ifcfg, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err == nil {
		defer ifcfg.Close()
		contents := []string{
			googleComment,
			fmt.Sprintf("DEVICE=%s", iface),
			"BOOTPROTO=none",
			"DEFROUTE=no",
			"IPV6INIT=no",
			"NM_CONTROLLED=no",
			"NOZEROCONF=yes",
		}
		_, err = ifcfg.WriteString(strings.Join(contents, "\n"))
		return err
	}
	if os.IsExist(err) {
		return nil
	}
	return err
}
