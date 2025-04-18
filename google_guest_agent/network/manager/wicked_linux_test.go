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
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
)

var (
	// mockWicked is the test wicked implementation for testing.
	mockWicked = &wicked{wickedCommand: defaultWickedCommand}
)

// wickedTestOpts are options to set for test environment setup.
type wickedTestOpts struct {
	// networkStatus indicates the mock return value from calling 'networkctl status network.service'.
	networkStatus bool

	// isActiveErr indicates the mock return value from calling 'systemctl is-active wicked.service'.
	isActiveErr bool

	// statusOpts are options to set when calling 'wicked ifstatus iface'.
	statusOpts wickedStatusOpts
}

// wickedStatusOpts are options to set when calling 'wicked ifstatus iface'.
type wickedStatusOpts struct {
	// returnValue indicates the return value of calling 'wicked ifstatus iface'.
	returnValue bool

	// returnError indicates whether calling 'wicked ifstatus iface' should return an error.
	// If set to true, this takes precedence over returnValue.
	returnError bool
}

// wickedMockRunner is the mock runner client for this test.
type wickedMockRunner struct {
	// networkStatus indicates the mock return value from calling 'networkctl status network.service'.
	networkStatus bool

	// isActiveErr indicates the mock return value from calling 'systemctl is-active wicked.service'.
	isActiveErr bool

	// statusOpts are options to set when calling 'wicked ifstatus iface'.
	statusOpts wickedStatusOpts
}

func (w wickedMockRunner) Quiet(ctx context.Context, name string, args ...string) error {
	return nil
}

func (w wickedMockRunner) WithOutput(ctx context.Context, name string, args ...string) *run.Result {
	if name == "systemctl" && slices.Contains(args, "status") && slices.Contains(args, "network.service") {
		if w.networkStatus {
			return &run.Result{
				StdOut: "wicked.service",
			}
		}
		return &run.Result{}
	}
	if name == "systemctl" && slices.Contains(args, "is-active") && slices.Contains(args, "wicked.service") {
		if w.isActiveErr {
			return &run.Result{
				ExitCode: 1,
			}
		}
		return &run.Result{}
	}
	if name == mockWicked.wickedCommand && slices.Contains(args, "ifstatus") && slices.Contains(args, "iface") && slices.Contains(args, "--brief") {
		statusOpts := w.statusOpts
		if statusOpts.returnError {
			return &run.Result{
				ExitCode: 1,
				StdErr:   "mock error ifstatus",
			}
		}
		if statusOpts.returnValue {
			return &run.Result{
				StdOut: "iface up",
			}
		}
		return &run.Result{
			StdOut: "iface unmanaged",
		}
	}
	return &run.Result{
		ExitCode: 1,
		StdOut:   "unexpected command",
	}
}

func (w wickedMockRunner) WithOutputTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) *run.Result {
	return &run.Result{}
}

func (w wickedMockRunner) WithCombinedOutput(ctx context.Context, name string, args ...string) *run.Result {
	return &run.Result{}
}

// wickedTestSetup sets up the environment for each test using the provided options.
func wickedTestSetup(t *testing.T, opts wickedTestOpts) {
	t.Helper()

	// Change the configuration directory of the mock wicked service.
	tempDir := path.Join(t.TempDir(), "sysconfig", "network")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("error creating temp directory: %v", err)
	}
	mockWicked.configDir = tempDir

	run.Client = &wickedMockRunner{
		networkStatus: opts.networkStatus,
		isActiveErr:   opts.isActiveErr,
		statusOpts:    opts.statusOpts,
	}
}

func wickedTestTearDown(t *testing.T) {
	t.Helper()

	run.Client = &run.Runner{}
}

// TestIsManaging tests whether IsManaging returns expected values provided
// various mock environment setups.
func TestIsManaging(t *testing.T) {
	tests := []struct {
		// name is the name of the test.
		name string

		// opts are options to set for the test environment.
		opts wickedTestOpts

		// expectedRes is the expected boolean output of IsManaging()
		expectedRes bool

		// expectErr indicates whether to expect an error.
		expectErr bool

		// expectedErr is the expected error message when an error is expected.
		expectedErr string
	}{
		{
			name: "network-service-set",
			opts: wickedTestOpts{
				networkStatus: true,
			},
			expectedRes: true,
		},
		{
			name: "wicked-not-active",
			opts: wickedTestOpts{
				isActiveErr: true,
			},
			expectedRes: false,
		},
		{
			name: "wicked-status-error",
			opts: wickedTestOpts{
				statusOpts: wickedStatusOpts{
					returnError: true,
				},
			},
			expectErr:   true,
			expectedErr: "failed to check status of wicked configuration: mock error ifstatus",
		},
		{
			name: "wicked-status-unmanaged",
			opts: wickedTestOpts{
				statusOpts: wickedStatusOpts{
					returnValue: false,
				},
			},
			expectedRes: false,
			expectErr:   false,
		},
		{
			name: "wicked-managed",
			opts: wickedTestOpts{
				statusOpts: wickedStatusOpts{
					returnValue: true,
				},
			},
			expectedRes: true,
			expectErr:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			wickedTestSetup(t, test.opts)

			res, err := mockWicked.IsManaging(ctx, "iface")
			if test.expectErr {
				if err == nil {
					t.Fatalf("no error returned when error expected")
				}
				if err.Error() != test.expectedErr {
					t.Fatalf("unexpected error message.\nExpected: %v\nActual: %v\n", test.expectedErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res != test.expectedRes {
				t.Fatalf("unexpected response. Expected: %v, Actual: %v", test.expectedRes, res)
			}

			wickedTestTearDown(t)
		})
	}
}

// TestWriteEthernetConfigs tests whether the wicked configuration files are
// written correctly and to the right location.
func TestWriteEthernetConfigs(t *testing.T) {
	if err := cfg.Load(nil); err != nil {
		t.Errorf("cfg.Load(nil) failed unexpectedly with error: %v", err)
	}

	tests := []struct {
		// name is the name of the test.
		name string

		// testInterfaces is the list of mock interfaces for which to write
		// a configuration file.
		testInterfaces []string

		// expectedReloads is the list of interfaces to reload.
		expectedReloads []string

		// expectedFiles is the list of expected file names.
		expectedFiles []string

		// expectedPriority is the list of expected priorities corresponding to
		// a file in expectedFiles.
		expectedPriority []string
	}{
		{
			name:             "one-nic",
			testInterfaces:   []string{"iface"},
			expectedReloads:  []string{},
			expectedFiles:    []string{"ifcfg-iface"},
			expectedPriority: []string{"10100"},
		},
		{
			name:             "multinic",
			testInterfaces:   []string{"iface0", "iface1", "iface2"},
			expectedReloads:  []string{"iface1", "iface2"},
			expectedFiles:    []string{"ifcfg-iface0", "ifcfg-iface1", "ifcfg-iface2"},
			expectedPriority: []string{"10100", "10200", "10300", "10400"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wickedTestSetup(t, wickedTestOpts{})

			written, err := mockWicked.writeEthernetConfigs(test.testInterfaces)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Since everything is written, everything should also be reloaded.
			if !slices.Equal(written, test.expectedReloads) {
				t.Fatalf("writeEthernetConfigs(%v) returned %v, expected %v", test.testInterfaces, written, test.expectedReloads)
			}

			// Check file contents.
			files, err := os.ReadDir(mockWicked.configDir)
			if err != nil {
				t.Fatalf("error reading configuration directory: %v", err)
			}

			for i, file := range files {
				// Check if the file is supposed to be there.
				if !slices.Contains(test.expectedFiles, file.Name()) {
					t.Fatalf("unexpected file in configuration directory: %v", file.Name())
				}

				// Check for the priority under the DHCLIENT_ROUTE_PRIORITY field.
				filePath := path.Join(mockWicked.configDir, file.Name())
				contents, err := os.ReadFile(filePath)
				if err != nil {
					t.Fatalf("error reading config file %s: %v", file.Name(), err)
				}
				lines := strings.Split(string(contents), "\n")

				for _, line := range lines {
					if strings.HasPrefix(line, "DHCLIENT_ROUTE_PRIORITY") {
						fields := strings.Split(line, "=")
						if fields[1] != test.expectedPriority[i] {
							t.Fatalf("unexpected priority. Expected: %v, Actual: %v", test.expectedPriority, fields[1])
						}
					}
				}
			}
			wickedTestTearDown(t)
		})
	}
}
