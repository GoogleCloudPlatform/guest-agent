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
	// ManagedByGuestAgent indicates whether this ini file is managed by the agent.
	ManagedByGuestAgent bool
}

const (
	googleComment = "# Added by Google Compute Engine Guest Agent."
)

var (
	// knownNetworkManagers contains the list of known network managers. This is
	// used to determine the network manager service that is managing the primary
	// network interface.
	knownNetworkManagers []Service

	// osinfoGet points to the function to use for getting osInfo.
	// Primarily used for testing.
	osinfoGet = osinfo.Get
)

// detectNetworkManager detects the network manager managing the primary network interface.
// This network manager will be used to set up primary and secondary network interfaces.
func detectNetworkManager(ctx context.Context, iface string) (*serviceStatus, error) {
	logger.Infof("Detecting network manager...")

	for _, curr := range knownNetworkManagers {
		active, err := curr.IsManaging(ctx, iface)
		if err != nil {
			return nil, err
		}

		if active {
			return &serviceStatus{manager: curr, active: active}, nil
		}
	}

	return nil, fmt.Errorf("no network manager impl found for %s", iface)
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
	activeService, err := detectNetworkManager(ctx, primaryInterface)
	if err != nil {
		return fmt.Errorf("error detecting network manager service: %v", err)
	}

	// Attempt to rollback any left over configuration of non active network managers.
	for _, svc := range knownNetworkManagers {
		if svc == activeService.manager {
			continue
		}

		logger.Infof("Rolling back %s", svc.Name())
		if err = svc.Rollback(ctx, nics); err != nil {
			logger.Errorf("Failed to roll back config for %s: %v", svc.Name(), err)
		}
	}

	// Attempt to configure all present/active network managers.

	activeService.manager.Configure(ctx, config)

	logger.Infof("Setting up %s", activeService.manager.Name())
	if err = activeService.manager.SetupEthernetInterface(ctx, config, nics); err != nil {
		return fmt.Errorf("manager(%s): error setting up ethernet interfaces: %v", activeService.manager.Name(), err)
	}

	if config.Unstable.VlanSetupEnabled {
		if err = activeService.manager.SetupVlanInterface(ctx, config, nics); err != nil {
			return fmt.Errorf("manager(%s): error setting up vlan interfaces: %v", activeService.manager.Name(), err)
		}
	}

	logger.Infof("Finished setting up %s", activeService.manager.Name())

	return nil
}
