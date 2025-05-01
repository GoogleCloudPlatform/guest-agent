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
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/go-ini/ini"
	"github.com/google/go-cmp/cmp"
)

var (
	// testNetworkManager is the test NetworkManager implementation to use for the test.
	testNetworkManager = &networkManager{}
)

// networkManagerMockRunner is the mock run client for this test.
type nmMockRunner struct {
	// networkServiceValue is the return value of 'systemctl status network.service'.
	networkServiceValue bool

	// isActiveErr indicates whether 'systemctl is-active ...' returns an error or not.
	isActiveErr bool

	// statusOpts are options to set for the behavior of 'nmcli dev status'
	statusOpts nmStatusOpts
}

func (n nmMockRunner) Quiet(ctx context.Context, name string, args ...string) error {
	return nil
}

func (n nmMockRunner) WithOutput(ctx context.Context, name string, args ...string) *run.Result {
	if name == "systemctl" {
		if slices.Contains(args, "status") && slices.Contains(args, "network.service") {
			if n.networkServiceValue {
				return &run.Result{
					StdOut: "NetworkManager.service",
				}
			}
			return &run.Result{}
		}
		if slices.Contains(args, "is-active") && slices.Contains(args, "NetworkManager.service") {
			if n.isActiveErr {
				return &run.Result{
					ExitCode: 1,
				}
			}
			return &run.Result{}
		}
	}
	if name == "nmcli" && slices.Contains(args, "dev") && slices.Contains(args, "status") {
		if n.statusOpts.returnError {
			return &run.Result{
				ExitCode: 1,
				StdErr:   "mock error status",
			}
		}
		if n.statusOpts.managed {
			return &run.Result{
				StdOut: "iface:connected\nlo:unmanaged",
			}
		}
		return &run.Result{
			StdOut: "iface:unmanaged\nlo:connected(externally)",
		}
	}
	return &run.Result{}
}

func (n nmMockRunner) WithOutputTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) *run.Result {
	return &run.Result{}
}

func (n nmMockRunner) WithCombinedOutput(ctx context.Context, name string, args ...string) *run.Result {
	return &run.Result{}
}

// nmTestOpts are options to set for test environment setup.
type nmTestOpts struct {
	// lookPathOpts contains options to set for the behavior of exec.LookPath.
	lookPathOpts nmLookPathOpts

	// runnerOpts contains options to set for the network manager mock runner.
	runnerOpts nmRunnerOpts
}

// nmLookPathOpts are options to set for the behavior of exec.LookPath.
type nmLookPathOpts struct {
	// returnValue determines the return value of exec.LookPath.
	returnValue bool

	// returnError determines whether exec.LookPath should return an error.
	// This takes precedence over returnValue.
	returnError bool
}

// nmRunnerOpts are options to set for the network manager test's mock runner.
type nmRunnerOpts struct {
	// networkServiceValue is the return value of 'systemctl status network.service'.
	networkServiceValue bool

	// isActiveErr indicates whether 'systemctl is-active ...' returns an error or not.
	isActiveErr bool

	// statusOpts are options to set for the behavior of 'nmcli dev status'
	statusOpts nmStatusOpts
}

// nmStatusOpts are options to set for the behavior of 'nmcli dev status'.
type nmStatusOpts struct {
	// managed indicates whether the interface is managed.
	managed bool

	// returnError indicates whether 'nmcli dev status' should return an error.
	returnError bool
}

// nmTestSetup sets up the environment before each test.
func nmTestSetup(t *testing.T, opts nmTestOpts) {
	t.Helper()

	lookPathOpts := opts.lookPathOpts
	if lookPathOpts.returnError {
		execLookPath = func(name string) (string, error) {
			return "", fmt.Errorf("mock error lookpath")
		}
	} else if lookPathOpts.returnValue {
		execLookPath = func(name string) (string, error) {
			return name, nil
		}
	} else {
		execLookPath = func(name string) (string, error) {
			return name, exec.ErrNotFound
		}
	}

	runnerOpts := opts.runnerOpts
	run.Client = &nmMockRunner{
		networkServiceValue: runnerOpts.networkServiceValue,
		isActiveErr:         runnerOpts.isActiveErr,
		statusOpts:          runnerOpts.statusOpts,
	}
}

// nmTestTearDown cleans up the test environment after each test.
func nmTestTearDown(t *testing.T) {
	t.Helper()

	run.Client = &run.Runner{}
	testNetworkManager.configDir = defaultNetworkManagerConfigDir
}

// TestNetworkManagerIsManaging tests whether networkManager's IsManaging returns the
// correct values provided various mock environment setups.
func TestNetworkManagerIsManaging(t *testing.T) {
	tests := []struct {
		// name is the name of this test.
		name string

		// opts are options to set for the mock runner.
		opts nmTestOpts

		// expectedRes is the expected return value of IsManaging().
		expectedRes bool

		// expectErr indicates whether to expect an error.
		expectErr bool

		// expectedErr is the expected error message if an error is expected.
		expectedErr string
	}{
		// lookpath nmcli does not exist.
		{
			name:        "lookpath-nmcli-not-exist",
			opts:        nmTestOpts{},
			expectedRes: false,
			expectErr:   false,
		},
		// lookpath nmcli error.
		{
			name: "lookpath-nmcli-error",
			opts: nmTestOpts{
				lookPathOpts: nmLookPathOpts{
					returnError: true,
				},
			},
			expectedRes: false,
			expectErr:   true,
			expectedErr: `error looking up path for "nmcli": mock error lookpath`,
		},
		// 'systemctl is-active NetworkManager.service' error.
		{
			name: "is-active-error",
			opts: nmTestOpts{
				lookPathOpts: nmLookPathOpts{
					returnValue: true,
				},
				runnerOpts: nmRunnerOpts{
					isActiveErr: true,
				},
			},
			expectedRes: false,
			expectErr:   false,
		},
		// 'nmcli dev status' error.
		{
			name: "nmcli-error",
			opts: nmTestOpts{
				lookPathOpts: nmLookPathOpts{
					returnValue: true,
				},
				runnerOpts: nmRunnerOpts{
					statusOpts: nmStatusOpts{
						returnError: true,
					},
				},
			},
			expectedRes: false,
			expectErr:   true,
			expectedErr: "error checking status of devices on NetworkManager: mock error status",
		},
		// 'nmcli dev status' unmanaged.
		{
			name: "nmcli-unmanaged",
			opts: nmTestOpts{
				lookPathOpts: nmLookPathOpts{
					returnValue: true,
				},
				runnerOpts: nmRunnerOpts{
					statusOpts: nmStatusOpts{
						managed: false,
					},
				},
			},
			expectedRes: false,
			expectErr:   false,
		},
		// 'nmcli dev status' managed.
		{
			name: "nmcli-managed",
			opts: nmTestOpts{
				lookPathOpts: nmLookPathOpts{
					returnValue: true,
				},
				runnerOpts: nmRunnerOpts{
					statusOpts: nmStatusOpts{
						managed: true,
					},
				},
			},
			expectedRes: true,
			expectErr:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			nmTestSetup(t, test.opts)

			res, err := testNetworkManager.IsManaging(ctx, "iface")
			if test.expectErr {
				if err == nil {
					t.Fatalf("no error returned when error expected")
				}
				if err.Error() != test.expectedErr {
					t.Fatalf("unexpected error message.\nExpected: %s\nActual: %v\n", test.expectedErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res != test.expectedRes {
				t.Fatalf("unexpected response. Expected: %v, Actual: %v", test.expectedRes, res)
			}

			nmTestTearDown(t)
		})
	}
}

// TestWriteNetworkManagerConfigs tests whether writeNetworkManagerConfigs() correclty writes
// the connection files to the correct place and contain the correct contents.
func TestWriteNetworkManagerConfigs(t *testing.T) {
	if err := cfg.Load(nil); err != nil {
		t.Fatalf("cfg.Load(nil) failed unexpectedly with error: %v", err)
	}

	tests := []struct {
		// name is the name of this test.
		name string

		// testInterfaces is the list of test interfaces.
		testInterfaces []string

		// wantInterfaces is the list of test interfaces expected in output.
		wantInterfaces []string

		// expectedIDs is the list of expected IDs.
		expectedIDs []string

		// expectedFiles is the list of expected files.
		expectedFiles []string

		// should manage primary nic interface.
		managePrimary bool
	}{
		// One interface.
		{
			name:           "one-nic",
			testInterfaces: []string{"iface"},
			wantInterfaces: []string{"iface"},
			expectedIDs:    []string{"google-guest-agent-iface"},
			expectedFiles: []string{
				"google-guest-agent-iface.nmconnection",
			},
			managePrimary: true,
		},
		// Multiple interfaces.
		{
			name:           "multinic",
			testInterfaces: []string{"iface0", "iface1", "iface2"},
			wantInterfaces: []string{"iface0", "iface1", "iface2"},
			expectedIDs: []string{
				"google-guest-agent-iface0",
				"google-guest-agent-iface1",
				"google-guest-agent-iface2",
			},
			expectedFiles: []string{
				"google-guest-agent-iface0.nmconnection",
				"google-guest-agent-iface1.nmconnection",
				"google-guest-agent-iface2.nmconnection",
			},
			managePrimary: true,
		},
		{
			name:           "multinic_no_primary",
			testInterfaces: []string{"iface0", "iface1", "iface2"},
			wantInterfaces: []string{"iface1", "iface2"},
			expectedIDs: []string{
				"google-guest-agent-iface1",
				"google-guest-agent-iface2",
			},
			expectedFiles: []string{
				"google-guest-agent-iface1.nmconnection",
				"google-guest-agent-iface2.nmconnection",
			},
			managePrimary: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nmTestSetup(t, nmTestOpts{})
			cfg.Get().NetworkInterfaces.ManagePrimaryNIC = test.managePrimary

			configDir := path.Join(t.TempDir(), "system-connections")
			if err := os.MkdirAll(configDir, 0755); err != nil {
				t.Fatalf("error creating temp dir: %v", err)
			}
			testNetworkManager.configDir = configDir

			conns, err := testNetworkManager.writeNetworkManagerConfigs(test.testInterfaces)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for i, conn := range conns {
				// Load the file and check the sections.
				configFilePath := path.Join(configDir, test.expectedFiles[i])
				opts := ini.LoadOptions{
					Loose:       true,
					Insensitive: true,
				}

				configFile, err := ini.LoadSources(opts, configFilePath)
				if err != nil {
					t.Fatalf("error reading config: %v", err)
				}

				config := new(nmConfig)
				if err = configFile.MapTo(config); err != nil {
					t.Fatalf("error parsing config ini: %v", err)
				}

				if !config.GuestAgent.ManagedByGuestAgent {
					t.Fatalf("guest-agent's managed key is set to false, expected true")
				}

				if config.Connection.ID != conn {
					t.Fatalf("unexpected connection id. Expected: %v, Actual: %v", conn, config.Connection.ID)
				}

				if config.Connection.InterfaceName != test.wantInterfaces[i] {
					t.Fatalf("unexpected interface name. Expected: %v, Actual: %v", test.wantInterfaces[i], config.Connection.InterfaceName)
				}
			}

			nmTestTearDown(t)
		})
	}
}

func TestVlanInterface(t *testing.T) {
	ctx := context.Background()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("could not list local interfaces: %+v", err)
	}

	interfaceName := ifaces[1].Name

	validNics := &Interfaces{
		EthernetInterfaces: []metadata.NetworkInterfaces{{
			Mac: ifaces[1].HardwareAddr.String(),
		}},
		VlanInterfaces: map[string]VlanInterface{
			"0-22": {
				VlanInterface: metadata.VlanInterface{
					Mac:             "foobar",
					ParentInterface: "/computeMetadata/v1/instance/network-interfaces/0/",
					Vlan:            22,
					MTU:             1460,
				},
				ParentInterfaceID: ifaces[1].Name,
			},
		},
	}

	wantCfg := &nmConfig{
		GuestAgent: guestAgentSection{ManagedByGuestAgent: true},
		Connection: nmConnectionSection{
			InterfaceName: fmt.Sprintf("gcp.%s.22", interfaceName),
			ID:            fmt.Sprintf("google-guest-agent-gcp.%s.22", interfaceName),
			ConnType:      "vlan",
		},
		Ipv4:     nmIPv4Section{Method: "auto"},
		Ipv6:     nmIPv6Section{Method: "auto", MTU: 1460},
		Vlan:     &nmVlan{Flags: 1, ID: 22, Parent: interfaceName},
		Ethernet: &nmEthernet{OverrideMacAddress: "foobar", MTU: 1460},
	}

	nmTestSetup(t, nmTestOpts{})
	configDir := filepath.Join(t.TempDir(), "system-connections")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("error creating temp dir: %v", err)
	}

	testNetworkManager.configDir = configDir

	if err := testNetworkManager.SetupVlanInterface(ctx, nil, validNics); err != nil {
		t.Fatalf("testNetworkManager.SetupVlanInterface(ctx, nil, %+v) = err [%v], want error: nil", validNics, err)
	}

	name := testNetworkManager.vlanInterfaceName(interfaceName, 22)
	cfgFile := testNetworkManager.networkManagerConfigFilePath(name)
	nmCfg := new(nmConfig)

	if err := readIniFile(cfgFile, nmCfg); err != nil {
		t.Fatalf("readIniFile(%s, nmConfig) failed unexpectedly with error: %v", cfgFile, err)
	}

	if diff := cmp.Diff(wantCfg, nmCfg); diff != "" {
		t.Errorf("SetupVlanInterface returned unexpected diff (-want,+got)\n%s", diff)
	}

	if err := testNetworkManager.Rollback(ctx, validNics); err != nil {
		t.Fatalf("testNetworkManager.Rollback(ctx, %+v) failed unexpectedly with error: %v", validNics, err)
	}

	if _, err := os.Stat(cfgFile); err == nil {
		t.Errorf("testNetworkManager.Rollback(ctx, %+v) did not remove %q", validNics, cfgFile)
	}

}
