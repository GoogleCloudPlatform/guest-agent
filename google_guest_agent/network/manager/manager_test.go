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
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
)

const (
	// Values for determining behavior of mockService's IsManaging()
	valueFalse = 0
	valueTrue  = 1
	valueErr   = 2
)

// Create mock service.
type mockService struct {
	isFallback bool
	isManaging int
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
	if n.isManaging == valueErr {
		return false, fmt.Errorf("mock error")
	}
	return n.isManaging == valueTrue, nil
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
}

// TestDetectNetworkManager tests whether DetectNetworkManager()
// correctly returns a network manager that's not the fallback.
func TestDetectNetworkManager(t *testing.T) {
	managerTestSetup()
	registerManager(mockService{
		isFallback: false,
		isManaging: valueTrue,
	}, false)
	registerManager(mockService{
		isFallback: true,
		isManaging: valueFalse,
	}, false)
	var expectedManager = knownNetworkManagers[0]

	s, err := detectNetworkManager(context.Background(), "iface")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.Name() != expectedManager.Name() {
		t.Fatalf("did not get expected network manager: got %s, wanted %s", s.Name(), expectedManager.Name())
	}
}

// TestDetectNetworkManagerNoManager tests whether DetectNetworkManager
// correctly errors if no suitable network managers are found.
func TestDetectNetworkManagerNoManager(t *testing.T) {
	managerTestSetup()
	registerManager(mockService{
		isFallback: false,
		isManaging: valueFalse,
	}, false)
	registerManager(mockService{
		isFallback: true,
		isManaging: valueFalse,
	}, true)

	_, err := detectNetworkManager(context.Background(), "iface")
	if err == nil {
		t.Fatalf("DetectNetworkManager() did not return an error when it should have")
	}

	var expectedError = "no network manager impl found for iface"
	if err.Error() != expectedError {
		t.Fatalf("error did not match expected error message: \nExpected: %v,\nActual: %s", expectedError, err.Error())
	}
}

// TestDetectNetworkManagerError tests whether DetectNetworkManager()
// correctly throws an error if IsManaging() errors.
func TestDetectNetworkManagerError(t *testing.T) {
	managerTestSetup()
	registerManager(mockService{
		isFallback: false,
		isManaging: valueErr,
	}, false)
	registerManager(mockService{
		isFallback: true,
		isManaging: valueTrue,
	}, true)

	_, err := detectNetworkManager(context.Background(), "iface")
	if err == nil {
		t.Fatalf("DetectNetworkManager() did not return an error when it should have")
	}

	var expectedError = "mock error"
	if err.Error() != expectedError {
		t.Fatalf("error did not match expected error message: \nExpected: %v,\nActual: %s", expectedError, err.Error())
	}
}

// TestDetectNetworkManagerFallback tests whether DetectNetworkManager
// correctly returns the fallback if all other network manager services
// are not detected.
func TestDetectNetworkManagerFallback(t *testing.T) {
	managerTestSetup()
	registerManager(mockService{
		isFallback: false,
		isManaging: valueFalse,
	}, false)
	registerManager(mockService{
		isFallback: false,
		isManaging: valueFalse,
	}, false)
	registerManager(mockService{
		isFallback: false,
		isManaging: valueFalse,
	}, false)
	registerManager(mockService{
		isFallback: true,
		isManaging: valueTrue,
	}, true)

	s, err := detectNetworkManager(context.Background(), "iface")
	if err != nil {
		t.Fatalf("DetectNetworkManager() incorrectly returned an error: %v", err)
	}

	var expectedName = "fallback"
	if s.Name() != expectedName {
		t.Fatalf("DetectNetworkManager() did not return correct manager: Expected: %v, Actual: %v", expectedName, s.Name())
	}
}
