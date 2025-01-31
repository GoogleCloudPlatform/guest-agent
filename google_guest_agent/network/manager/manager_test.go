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
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/osinfo"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/google/go-cmp/cmp"
)

const (
	// testOSVersion encapsulates the mock version to use for testing.
	testOSVersion = 2
)

// Create mock service.
type mockService struct {
	// isFallback indicates whether this service is a fallback.
	isFallback bool

	// isManaging indicates whether this service is managing the primary interface.
	isManaging bool

	// managingError indicates whether isManaging() should return an error.
	managingError bool

	// rollbackError indicates whether Rollback() should return an error.
	rollbackError bool

	// rolledBack indicates whether the network config was rolled back
	rolledBack bool
}

// Name implements the Service interface.
func (n *mockService) Name() string {
	if n.isFallback {
		return "fallback"
	}
	return "service"
}

// Configure gives the opportunity for the Service implementation to adjust its configuration
// based on the Guest Agent configuration.
func (n *mockService) Configure(context.Context, *cfg.Sections) {
}

// IsManaging implements the Service interface.
func (n *mockService) IsManaging(context.Context, string) (bool, error) {
	if n.managingError {
		return false, fmt.Errorf("mock error")
	}
	return n.isManaging, nil
}

// SetupEthernetInterface implements the Service interface.
func (n *mockService) SetupEthernetInterface(context.Context, *cfg.Sections, *Interfaces) error {
	return nil
}

// SetupVlanInterface implements the Service interface.
func (n *mockService) SetupVlanInterface(context.Context, *cfg.Sections, *Interfaces) error {
	return nil
}

// Rollback implements the Service interface.
func (n *mockService) Rollback(context.Context, *Interfaces) error {
	n.rolledBack = true
	if n.rollbackError {
		return fmt.Errorf("mock error")
	}
	return nil
}

// RollbackNics implements the Service interface.
func (n *mockService) RollbackNics(ctx context.Context, nics *Interfaces) error {
	return n.Rollback(ctx, nics)
}

// managerTestSetup does pre-test setup steps.
func managerTestSetup() {
	// Clear the known network managers and fallbacks.
	knownNetworkManagers = []Service{}

	// Create our own osinfo function for testing.
	osinfoGet = func() osinfo.OSInfo {
		return osinfo.OSInfo{
			OS:            "test",
			VersionID:     "test",
			PrettyName:    "Test",
			KernelRelease: "test",
			KernelVersion: "Test",
			Version: osinfo.Ver{
				Major:  testOSVersion,
				Minor:  0,
				Patch:  0,
				Length: 0,
			},
		}
	}
}

// TestDetectNetworkManager tests whether DetectNetworkManager()
// returns expected values given certain mock environment setups.
func TestDetectNetworkManager(t *testing.T) {
	tests := []struct {
		// name is the name of the test.
		name string

		// managers are the list of mock services to register.
		services []*mockService

		// expectedManager is the manager expected to be returned.
		expectedManager mockService

		// expectErr dictates whether to expect an error.
		expectErr bool

		// expectedErrorMessage is the expected error message when an error is returned.
		expectedErrorMessage string
	}{
		// Base test case testing if it works.
		{
			name: "no-error",
			services: []*mockService{
				{
					isFallback: false,
					isManaging: true,
				},
				{
					isFallback: true,
					isManaging: false,
				},
			},
			expectedManager: mockService{
				isFallback: false,
				isManaging: true,
			},
			expectErr: false,
		},
		// Test if an error is returned if no network manager is found.
		{
			name: "no-manager-found",
			services: []*mockService{
				{
					isFallback: false,
					isManaging: false,
				},
				{
					isFallback: false,
					isManaging: false,
				},
			},
			expectErr:            true,
			expectedErrorMessage: "no network manager impl found for iface",
		},
		// Test if an error is returned if IsManaging() fails.
		{
			name: "is-managing-fail",
			services: []*mockService{
				{
					isFallback:    false,
					managingError: true,
				},
				{
					isFallback: false,
					isManaging: true,
				},
			},
			expectErr:            true,
			expectedErrorMessage: "mock error",
		},
		// Test if the fallback service is returned if all other services are not detected.
		{
			name: "fallback",
			services: []*mockService{
				{
					isFallback: false,
					isManaging: false,
				},
				{
					isFallback: false,
					isManaging: false,
				},
				{
					isFallback: false,
					isManaging: false,
				},
				{
					isFallback: true,
					isManaging: true,
				},
			},
			expectedManager: mockService{
				isFallback: true,
				isManaging: true,
			},
			expectErr: false,
		},
	}

	prevKnownNetworkManager := knownNetworkManagers
	t.Cleanup(func() {
		knownNetworkManagers = prevKnownNetworkManager
	})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			managerTestSetup()

			knownNetworkManagers = nil
			for _, service := range test.services {
				knownNetworkManagers = append(knownNetworkManagers, service)
			}

			activeService, err := detectNetworkManager(context.Background(), "iface")

			if err != nil {
				if !test.expectErr {
					t.Fatalf("unexpected error: %v", err)
				}
				if err.Error() != test.expectedErrorMessage {
					t.Fatalf("error message does not match: Expected: %s, Actual: %v", test.expectedErrorMessage, err)
				}

				// Avoid checking expectedManager.
				return
			}
			if test.expectErr {
				t.Fatalf("no error returned when error expected, expected error: %s", test.expectedErrorMessage)
			}

			if *activeService.manager.(*mockService) != test.expectedManager {
				t.Fatalf("did not get expected network manager. Expected: %v, Actual: %v", test.expectedManager, activeService)
			}
		})
	}
}

// TestRollbackToDefault ensures that all network managers are rolled back,
// including the active manager.
func TestFallbackToDefault(t *testing.T) {
	managerTestSetup()
	ctx := context.Background()

	prevKnownNetworkManager := knownNetworkManagers
	t.Cleanup(func() {
		knownNetworkManagers = prevKnownNetworkManager
	})
	knownNetworkManagers = []Service{
		&mockService{
			isManaging: true,
		},
		&mockService{
			isManaging:    true,
			rollbackError: true,
		},
		&mockService{
			isManaging: false,
		},
	}

	if err := FallbackToDefault(ctx); err != nil {
		t.Fatalf("FallbackToDefault(ctx) = %v, want nil", err)
	}

	for i, svc := range knownNetworkManagers {
		if !svc.(*mockService).rolledBack {
			t.Errorf("knownNetworkManagers[%d].rolledBack = %t, want true", i, svc.(*mockService).rolledBack)
		}
	}
}

func TestBuildInterfacesFromAllPhysicalNICs(t *testing.T) {
	nics, err := buildInterfacesFromAllPhysicalNICs()
	if err != nil {
		t.Fatalf("buildInterfacesFromAllPhysicalNICs() = %v, want nil", err)
	}

	for _, nic := range nics.EthernetInterfaces {
		if _, err := GetInterfaceByMAC(nic.Mac); err != nil {
			t.Errorf("GetInterfaceByMAC(%q) = %v, want nil)", nic.Mac, err)
		}
	}
}

func TestShouldManageInterface(t *testing.T) {
	if err := cfg.Load(nil); err != nil {
		t.Fatalf("cfg.Load(nil) = %v, want nil", err)
	}
	if shouldManageInterface(true) {
		t.Error("with default config, shouldManageInterface(isPrimary = true) = true, want false")
	}
	if !shouldManageInterface(false) {
		t.Error("with default config, shouldManageInterface(isPrimary = false) = false, want true")
	}
	if err := cfg.Load([]byte("[NetworkInterfaces]\nmanage_primary_nic=true")); err != nil {
		t.Fatalf("cfg.Load(%q) = %v, want nil", "[NetworkInterfaces]\nmanage_primary_nic=true", err)
	}
	if !shouldManageInterface(true) {
		t.Error("with manage_primary_nic=false, shouldManageInterface(isPrimary = true) = false, want true")
	}
	if !shouldManageInterface(false) {
		t.Error("with manage_primary_nic=false, shouldManageInterface(isPrimary = false) = false, want true")
	}
}

func TestReformatVlanNics(t *testing.T) {
	mds := &metadata.Descriptor{Instance: metadata.Instance{
		VlanNetworkInterfaces: map[int]map[int]metadata.VlanInterface{
			0: {
				5: {Mac: "a", ParentInterface: "/computeMetadata/v1/instance/network-interfaces/0/", Vlan: 5},
				6: {Mac: "b", Vlan: 6, IP: "1.2.3.4"},
			},
			1: {
				7: {Mac: "c", Vlan: 7, DHCPv6Refresh: "123456"},
			},
		},
	}}
	nics := &Interfaces{VlanInterfaces: map[string]VlanInterface{}}
	want := &Interfaces{VlanInterfaces: map[string]VlanInterface{
		"0-5": {VlanInterface: metadata.VlanInterface{Mac: "a", ParentInterface: "/computeMetadata/v1/instance/network-interfaces/0/", Vlan: 5}, ParentInterfaceID: "eth0"},
		"0-6": {VlanInterface: metadata.VlanInterface{Mac: "b", Vlan: 6, IP: "1.2.3.4"}, ParentInterfaceID: "eth0"},
		"1-7": {VlanInterface: metadata.VlanInterface{Mac: "c", Vlan: 7, DHCPv6Refresh: "123456"}, ParentInterfaceID: "eth1"},
	}}

	ethernetInterfaces := []string{"eth0", "eth1"}

	if err := reformatVlanNics(mds, nics, ethernetInterfaces); err != nil {
		t.Fatalf("reformatVlanNics(%+v, %+v, %+v) failed unexpectedly with error: %v", mds, nics, ethernetInterfaces, err)
	}

	if diff := cmp.Diff(want.VlanInterfaces, nics.VlanInterfaces); diff != "" {
		t.Errorf("reformatVlanNics(%+v, %+v, %+v) returned unexpected diff (-want,+got):\n %s", mds, nics, ethernetInterfaces, diff)
	}
}

func TestReformatVlanNicsError(t *testing.T) {
	mds := &metadata.Descriptor{Instance: metadata.Instance{
		VlanNetworkInterfaces: map[int]map[int]metadata.VlanInterface{
			0: {
				5: {Mac: "a", ParentInterface: "/computeMetadata/v1/instance/network-interfaces/0/", Vlan: 5},
				6: {Mac: "b", Vlan: 6, IP: "1.2.3.4"},
			},
			1: {
				7: {Mac: "c", Vlan: 7, DHCPv6Refresh: "123456"},
			},
		},
	}}
	nics := &Interfaces{VlanInterfaces: map[string]VlanInterface{}}

	tests := []struct {
		name               string
		mds                *metadata.Descriptor
		ethernetInterfaces []string
	}{
		{
			name:               "invalid_parentId",
			mds:                mds,
			ethernetInterfaces: []string{"eth0"},
		},
		{
			name: "all_invalid_parentIds",
			mds:  mds,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := reformatVlanNics(mds, nics, test.ethernetInterfaces); err == nil {
				t.Fatalf("reformatVlanNics(%+v, %+v, %+v) succeeded, want error", mds, nics, test.ethernetInterfaces)
			}
		})
	}
}
