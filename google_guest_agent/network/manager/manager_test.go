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
}

// Name implements the Service interface.
func (n mockService) Name() string {
	if n.isFallback {
		return "fallback"
	}
	return "service"
}

// IsManaging implements the Service interface.
func (n mockService) IsManaging(ctx context.Context, iface string) (bool, error) {
	if n.managingError {
		return false, fmt.Errorf("mock error")
	}
	return n.isManaging, nil
}

// Setup implements the Service interface.
func (n mockService) Setup(ctx context.Context, config *cfg.Sections, payload []metadata.NetworkInterfaces) error {
	return nil
}

// Rollback implements the Service interface.
func (n mockService) Rollback(ctx context.Context, payload []metadata.NetworkInterfaces) error {
	return nil
}

// managerTestSetup does pre-test setup steps.
func managerTestSetup() {
	// Clear the known network managers and fallbacks.
	knownNetworkManagers = []Service{}
	fallbackNetworkManager = nil

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
		services []mockService

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
			services: []mockService{
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
			services: []mockService{
				{
					isFallback: false,
					isManaging: false,
				},
				{
					isFallback: true,
					isManaging: false,
				},
			},
			expectErr:            true,
			expectedErrorMessage: "no network manager impl found for iface",
		},
		// Test if an error is returned if IsManaging() fails.
		{
			name: "is-managing-fail",
			services: []mockService{
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
			services: []mockService{
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

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			managerTestSetup()
			for _, service := range test.services {
				registerManager(service, service.isFallback)
			}

			s, err := detectNetworkManager(context.Background(), "iface")
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
			if err == nil && test.expectErr {
				t.Fatalf("no error returned when error expected")
			}

			if s != test.expectedManager {
				t.Fatalf("did not get expected network manager. Expected: %v, Actual: %v", test.expectedManager, s)
			}
		})
	}
}

// TestFindOSRule tests whether findOSRule() correctly returns the expected values
// depending on whether a matching rule exists or not.
func TestFindOSRule(t *testing.T) {
	managerTestSetup()

	tests := []struct {
		// name is the name of the test.
		name string

		// rules are mock OSConfig rules.
		rules []osConfigRule

		// broadVersion indicates whether to call findOSRule() using broad versions.
		broadVersion bool

		// expectedNil indicates to expect a nil return when set to true.
		expectedNil bool
	}{
		// ignoreRule exists.
		{
			name: "ignore-exist",
			rules: []osConfigRule{
				{
					osNames: []string{"test"},
					majorVersions: map[int]bool{
						testOSVersion: true,
					},
					action: osConfigAction{},
				},
			},
			broadVersion: false,
			expectedNil:  false,
		},
		// ignoreRule broad version exists.
		{
			name: "ignore-exist-broad",
			rules: []osConfigRule{
				{
					osNames: []string{"test"},
					majorVersions: map[int]bool{
						osConfigRuleAnyVersion: true,
					},
					action: osConfigAction{},
				},
			},
			broadVersion: true,
			expectedNil:  false,
		},
		// ignoreRule does not exist.
		{
			name: "ignore-no-exist",
			rules: []osConfigRule{
				{
					osNames: []string{"non-test"},
					majorVersions: map[int]bool{
						0: true,
					},
					action: osConfigAction{},
				},
			},
			broadVersion: false,
			expectedNil:  true,
		},
		// ignoreRule broadVersion does not exist.
		{
			name: "ignore-no-exist-broad",
			rules: []osConfigRule{
				{
					osNames: []string{"non-test"},
					majorVersions: map[int]bool{
						osConfigRuleAnyVersion: true,
					},
					action: osConfigAction{},
				},
			},
			broadVersion: true,
			expectedNil:  true,
		},
		// ignoreRule non-broadVersion exists, but we want broad version.
		{
			name: "ignore-no-exist-broad-nonbroad-exist",
			rules: []osConfigRule{
				{
					osNames: []string{"test"},
					majorVersions: map[int]bool{
						testOSVersion: true,
					},
					action: osConfigAction{},
				},
			},
			broadVersion: true,
			expectedNil:  true,
		},
		// ignoreRule broadVersion exists, but we want non-broad version.
		{
			name: "ignore-no-exist-broad-exist",
			rules: []osConfigRule{
				{
					osNames: []string{"test"},
					majorVersions: map[int]bool{
						osConfigRuleAnyVersion: true,
					},
					action: osConfigAction{},
				},
			},
			broadVersion: false,
			expectedNil:  true,
		},
	}

	// Run the tests.
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			managerTestSetup()

			osRules = test.rules
			osRule := findOSRule(test.broadVersion)

			if osRule == nil && !test.expectedNil {
				t.Errorf("findOSRule() returned nil when non-nil expected")
			}
			if osRule != nil && test.expectedNil {
				t.Errorf("findOSRule() returned non-nil when nil expected: %+v", osRule)
			}

			osRules = defaultOSRules
		})
	}
}
