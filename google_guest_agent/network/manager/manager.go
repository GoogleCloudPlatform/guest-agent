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
	"net"
	"os"
	"reflect"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/osinfo"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
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

	// RollbackNics rolls back only changes to regular nics (vlan nics are not handled).
	RollbackNics(ctx context.Context, nics *Interfaces) error
}

// serviceStatus is an internal wrapper of a service implementation and its status.
type serviceStatus struct {
	// manager is the network manager implementation.
	manager Service
	// active indicates this service is active/present in the system.
	active bool
}

// VlanInterface are [metadata.VlanInterface] offered by MDS with derived Parent Interface
// name added to it for convenience.
type VlanInterface struct {
	metadata.VlanInterface
	// ParentInterfaceID is the interface name on the host. All network managers should refer
	// this interface name instead of one present in [metadata.VlanInterface] which is just an
	// index to interface in [EthernetInterfaces]
	ParentInterfaceID string
}

// Interfaces wraps both ethernet and vlan interfaces.
type Interfaces struct {
	// EthernetInterfaces are the regular ethernet interfaces descriptors offered by metadata.
	EthernetInterfaces []metadata.NetworkInterfaces

	// VlanInterfaces are the vLAN interfaces descriptors offered by metadata.
	VlanInterfaces map[string]VlanInterface
}

// guestAgentSection is the section added to guest-agent-written ini files to indicate
// that the ini file is managed by the agent.
type guestAgentSection struct {
	// ManagedByGuestAgent indicates whether this ini file is managed by the agent.
	ManagedByGuestAgent bool
}

const (
	googleComment         = "# Added by Google Compute Engine Guest Agent."
	debian12NetplanFile   = "/etc/netplan/90-default.yaml"
	debian12NetplanConfig = `network:
    version: 2
    ethernets:
        all-en:
            match:
                name: en*
            dhcp4: true
            dhcp4-overrides:
                use-domains: true
            dhcp6: true
            dhcp6-overrides:
                use-domains: true
        all-eth:
            match:
                name: eth*
            dhcp4: true
            dhcp4-overrides:
                use-domains: true
            dhcp6: true
            dhcp6-overrides:
                use-domains: true
`
)

var (
	// knownNetworkManagers contains the list of known network managers. This is
	// used to determine the network manager service that is managing the primary
	// network interface.
	knownNetworkManagers []Service

	// osinfoGet points to the function to use for getting osInfo.
	// Primarily used for testing.
	osinfoGet = osinfo.Get

	// seenMetadata keeps a copy of MDS descriptor that was already seen and applied
	// in terms of VLAN/Ethernet NIC configuration by the manager.
	seenMetadata *metadata.Descriptor
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

// reformatVlanNics reads VLAN NIC information from metadata descriptor and formats
// it into [Interfaces.VlanInterfaces] that every network manager understands.
func reformatVlanNics(mds *metadata.Descriptor, nics *Interfaces, ethernetInterfaces []string) error {
	for parentID, vlans := range mds.Instance.VlanNetworkInterfaces {
		if parentID >= len(ethernetInterfaces) {
			return fmt.Errorf("invalid parent index(%d), known interfaces count: %d", parentID, len(ethernetInterfaces))
		}

		for vlanID, vlan := range vlans {
			mapID := fmt.Sprintf("%d-%d", parentID, vlanID)
			nics.VlanInterfaces[mapID] = VlanInterface{VlanInterface: vlan, ParentInterfaceID: ethernetInterfaces[parentID]}
		}
	}
	return nil
}

// SetupInterfaces sets up all secondary network interfaces on the system, and primary network
// interface if enabled in the configuration using the native network manager service detected
// to be managing the primary network interface.
func SetupInterfaces(ctx context.Context, config *cfg.Sections, mds *metadata.Descriptor) error {
	if seenMetadata != nil {
		diff := reflect.DeepEqual(mds.Instance.NetworkInterfaces, seenMetadata.Instance.NetworkInterfaces) &&
			reflect.DeepEqual(mds.Instance.VlanNetworkInterfaces, seenMetadata.Instance.VlanNetworkInterfaces)

		if diff {
			logger.Debugf("MDS returned Ethernet NICs [%+v] and VLAN NICs [%+v] are already seen and applied, skipping", seenMetadata.Instance.NetworkInterfaces, seenMetadata.Instance.VlanNetworkInterfaces)
			return nil
		}
	}

	// User may have disabled network interface setup entirely.
	if !config.NetworkInterfaces.Setup {
		logger.Infof("Network interface setup disabled, skipping...")
		return nil
	}

	nics := &Interfaces{
		EthernetInterfaces: mds.Instance.NetworkInterfaces,
		VlanInterfaces:     map[string]VlanInterface{},
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

	if err := rollbackLeftoverConfigs(ctx, config, mds); err != nil {
		logger.Errorf("Failed to rollback left over configs: %v", err)
	}

	// Attempt to rollback any left over configuration of non active network managers.
	for _, svc := range knownNetworkManagers {
		if svc == activeService.manager {
			continue
		}

		logger.Infof("Rolling back %s", svc.Name())
		if err = svc.Rollback(ctx, nics); err != nil {
			logger.Warningf("Unable to roll back config for %s: %v", svc.Name(), err)
		}
	}

	// Attempt to configure all present/active network managers.

	activeService.manager.Configure(ctx, config)

	logger.Infof("Setting up %s", activeService.manager.Name())
	if err = activeService.manager.SetupEthernetInterface(ctx, config, nics); err != nil {
		return fmt.Errorf("manager(%s): error setting up ethernet interfaces: %v", activeService.manager.Name(), err)
	}

	if config.NetworkInterfaces.VlanSetupEnabled {
		logger.Infof("VLAN setup is enabled via config file, setting up interfaces")
		if err := reformatVlanNics(mds, nics, interfaces); err != nil {
			return fmt.Errorf("unable to read vlans, invalid format: %w", err)
		}
		if err = activeService.manager.SetupVlanInterface(ctx, config, nics); err != nil {
			return fmt.Errorf("manager(%s): error setting up vlan interfaces: %v", activeService.manager.Name(), err)
		}
	}

	logger.Infof("Finished setting up %s", activeService.manager.Name())

	go func() {
		// Setup might not have finished when we log and collect this information. Adding this
		// temporary sleep for debugging purposes to make sure we have up-to-date information.
		time.Sleep(2 * time.Second)
		logInterfaceState(ctx)
	}()

	seenMetadata = mds
	return nil
}

// Remove only primary nics left over configs.
func rollbackLeftoverConfigs(ctx context.Context, config *cfg.Sections, mds *metadata.Descriptor) error {
	// If we are running debian 12 and failed to restore default netplan config
	// we should not rollback dangling/left over configs.
	if err := restoreDebian12NetplanConfig(config); err != nil {
		return fmt.Errorf("Failed to restore debian-12 netplan configuration: %v", err)
	}

	// If we are managing primary nics we don't want to rollback "dangling/left over" configs
	// since we are actually managing them.
	if config.NetworkInterfaces.ManagePrimaryNIC {
		return nil
	}

	primaryInterface := mds.Instance.NetworkInterfaces[0]
	nic := &Interfaces{
		EthernetInterfaces: []metadata.NetworkInterfaces{primaryInterface},
	}

	for _, svc := range knownNetworkManagers {
		if err := svc.RollbackNics(ctx, nic); err != nil {
			logger.Warningf("Failed to rollback primary nic (left over) config for %s: %v", svc.Name(), err)
		}
	}

	return nil
}

// restoreDebian12NetplanConfig recreates the default netplan configuration
// for debian-12 in case user hasn't disabled it and the running system is
// indeed a debian-12 system.
func restoreDebian12NetplanConfig(config *cfg.Sections) error {
	if !config.NetworkInterfaces.RestoreDebian12NetplanConfig {
		logger.Debugf("User provided configuration requested to skip debian-12 netplan configuration")
		return nil
	}

	osDesc := osinfo.Get()
	if osDesc.OS != "debian" || osDesc.Version.Major != 12 {
		logger.Debugf("Not running a debian-12 system, skipping netplan configuration restore")
		return nil
	}

	if _, err := os.Stat(debian12NetplanFile); err != nil {
		if !os.IsNotExist(err) {
			return err
		}

		if err := utils.WriteFile([]byte(debian12NetplanConfig), debian12NetplanFile, 0644); err != nil {
			return fmt.Errorf("Failed to recreate default netplan config: %w", err)
		}

		logger.Debugf("Recreated default netplan config...")
	}

	return nil
}

// FallbackToDefault will attempt to rescue broken networking by rolling back
// all guest-agent modifications to the network configuration.
func FallbackToDefault(ctx context.Context) error {
	nics, err := buildInterfacesFromAllPhysicalNICs()
	if err != nil {
		return fmt.Errorf("could not build list of NICs for fallback: %v", err)
	}

	// Rollback every NIC with every known network manager.
	for _, svc := range knownNetworkManagers {
		logger.Infof("Rolling back %s", svc.Name())
		if err := svc.Rollback(ctx, nics); err != nil {
			logger.Warningf("Failed to roll back config for %s: %v", svc.Name(), err)
		}
	}

	return nil
}

// Build a *Interfaces from all physical interfaces rather than the MDS.
func buildInterfacesFromAllPhysicalNICs() (*Interfaces, error) {
	nics := &Interfaces{
		EthernetInterfaces: nil,
		VlanInterfaces:     map[string]VlanInterface{},
	}

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get interfaces: %v", err)
	}

	for _, iface := range interfaces {
		mac := iface.HardwareAddr.String()
		if mac == "" {
			continue
		}
		nics.EthernetInterfaces = append(nics.EthernetInterfaces, metadata.NetworkInterfaces{
			Mac: mac,
		})
	}

	return nics, nil
}

// shouldManageInterface returns whether the guest agent should manage an interface
// provided whether the interface of interest is the primary interface or not.
func shouldManageInterface(isPrimary bool) bool {
	if isPrimary {
		return cfg.Get().NetworkInterfaces.ManagePrimaryNIC
	}
	return true
}
