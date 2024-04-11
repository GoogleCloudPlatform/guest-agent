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
	"slices"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/osinfo"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

// Service is an interface for setting up network configurations
// using different network managing services, such as systemd-networkd and wicked.
type Service interface {
	// Configure gives the opportunity for the Service implementation to adjust its configuration
	// based on the Guest Agent configuration.
	Configure(ctx context.Context, config *cfg.Sections)

	// IsManaging checks whether this network manager service is managing the provided interface.
	IsManaging(ctx context.Context, iface string) (bool, error)

	// Name is the name of the network manager service.
	Name() string

	// SetupEthernetInterface writes the appropriate configurations for the network manager service for all
	// non-primary network interfaces.
	SetupEthernetInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error

	// SetupVlanInterface writes the apppropriate vLAN interfaces configuration for the network manager service
	// for all configured interfaces.
	SetupVlanInterface(ctx context.Context, config *cfg.Sections, nics *Interfaces) error

	// Rollback rolls back the changes created in Setup.
	Rollback(ctx context.Context, nics *Interfaces) error
}

// serviceStatus is an internal wrapper of a service implementation and its status.
type serviceStatus struct {
	// manager is the network manager implementation.
	manager Service
	// active indicates this service is active/present in the system.
	active bool
}

// Interfaces wraps both ethernet and vlan interfaces.
type Interfaces struct {
	// EthernetInterfaces are the regular ethernet interfaces descriptors offered by metadata.
	EthernetInterfaces []metadata.NetworkInterfaces

	// VlanInterfaces are the vLAN interfaces descriptors offered by metadata.
	VlanInterfaces map[int]metadata.VlanInterface
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
}

// guestAgentSection is the section added to guest-agent-written ini files to indicate
// that the ini file is managed by the agent.
type guestAgentSection struct {
	// Managed indicates whether this ini file is managed by the agent.
	Managed bool
}

const (
	googleComment = "# Added by Google Compute Engine Guest Agent."
)

var (
	// knownNetworkManagers contains the list of known network managers. This is
	// used to determine the network manager service that is managing the primary
	// network interface.
	knownNetworkManagers []Service

	// fallbackNetworkManager is the network manager service to be assumed if
	// none of the known network managers returned true from IsManaging()
	fallbackNetworkManager Service

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
		// Ubuntu rules
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
func detectNetworkManager(ctx context.Context, iface string) ([]serviceStatus, error) {
	logger.Infof("Detecting network manager...")

	networkManagers := knownNetworkManagers
	if fallbackNetworkManager != nil {
		networkManagers = append(networkManagers, fallbackNetworkManager)
	}

	var res []serviceStatus
	for _, curr := range networkManagers {
		active, err := curr.IsManaging(ctx, iface)
		if err != nil {
			return nil, err
		}

		if active {
			res = append(res, serviceStatus{manager: curr, active: active})
		}
	}

	if len(res) == 0 {
		if fallbackNetworkManager != nil {
			return append(res, serviceStatus{manager: fallbackNetworkManager, active: true}), nil
		}
		return nil, fmt.Errorf("no network manager impl found for %s", iface)
	}

	return res, nil
}

// findOSRule finds the osConfigRule that applies to the current system.
func findOSRule() *osConfigRule {
	osInfo := osinfoGet()
	for _, curr := range osRules {
		if !slices.Contains(curr.osNames, osInfo.OS) {
			continue
		}

		if curr.majorVersions[osInfo.Version.Major] {
			return &curr
		}
	}
	return nil
}

// shouldManageInterface returns whether the guest agent should manage an interface
// provided whether the interface of interest is the primary interface or not.
func shouldManageInterface(isPrimary bool) bool {
	rule := findOSRule()
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
func SetupInterfaces(ctx context.Context, config *cfg.Sections, mds *metadata.Descriptor) error {
	// User may have disabled network interface setup entirely.
	if !config.NetworkInterfaces.Setup {
		logger.Infof("Network interface setup disabled, skipping...")
		return nil
	}

	nics := &Interfaces{
		EthernetInterfaces: mds.Instance.NetworkInterfaces,
		VlanInterfaces:     map[int]metadata.VlanInterface{},
	}

	for _, curr := range mds.Instance.VlanNetworkInterfaces {
		for key, val := range curr {
			nics.VlanInterfaces[key] = val
		}
	}

	interfaces, err := interfaceNames(nics.EthernetInterfaces)
	if err != nil {
		return fmt.Errorf("error getting interface names: %v", err)
	}
	primaryInterface := interfaces[0]

	// Get the network manager.
	services, err := detectNetworkManager(ctx, primaryInterface)
	if err != nil {
		return fmt.Errorf("error detecting network manager service: %v", err)
	}

	// Attempt to rollback any left over configuration of non active network managers.
	for _, svc := range services {
		logger.Infof("Rolling back %s", svc.manager.Name())
		if err = svc.manager.Rollback(ctx, nics); err != nil {
			logger.Errorf("Failed to roll back config for %s: %v", svc.manager.Name(), err)
		}
	}

	// Attempt to configure all present/active network managers.
	for _, svc := range services {
		if !svc.active {
			continue
		}

		svc.manager.Configure(ctx, config)

		logger.Infof("Setting up %s", svc.manager.Name())
		if err = svc.manager.SetupEthernetInterface(ctx, config, nics); err != nil {
			return fmt.Errorf("manager(%s): error setting up ethernet interfaces: %v", svc.manager.Name(), err)
		}

		if config.Unstable.VlanSetupEnabled {
			if err = svc.manager.SetupVlanInterface(ctx, config, nics); err != nil {
				return fmt.Errorf("manager(%s): error setting up vlan interfaces: %v", svc.manager.Name(), err)
			}
		}

		logger.Infof("Finished setting up %s", svc.manager.Name())
	}

	return nil
}
