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
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/osinfo"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/ps"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
)

// The test DHClient to use in the test.
var testDHClient = dhclient{}

// The mock Runner client to use for this test.
type dhclientMockRunner struct {
	// quietErr indicates whether Quiet() should return an error.
	quietErr bool
}

func (d dhclientMockRunner) Quiet(ctx context.Context, name string, args ...string) error {
	if d.quietErr {
		// Error every time to see the command being run.
		var msg = name
		for _, arg := range args {
			msg += fmt.Sprintf(" %v", arg)
		}
		return fmt.Errorf(msg)
	}
	return nil
}

func (d dhclientMockRunner) WithOutput(ctx context.Context, name string, args ...string) *run.Result {
	return &run.Result{
		StdOut: fmt.Sprintf("%v %v", name, args),
	}
}

func (d dhclientMockRunner) WithOutputTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) *run.Result {
	return &run.Result{}
}

func (d dhclientMockRunner) WithCombinedOutput(ctx context.Context, name string, args ...string) *run.Result {
	return &run.Result{}
}

// The mock Ps client to use for this test.
type dhclientMockPs struct {
	// ifaces is the list of mock interfaces.
	ifaces []string

	// existFlags dictates whether Find should return a process or empty.
	existFlags []bool

	// returnError indicates whether Find should return an error. If set to true,
	// this takes priority over whatever is set in existFlags.
	returnError bool

	// ipVersions are the ipVersions to "look for".
	ipVersions []ipVersion
}

func (d dhclientMockPs) Find(exematch string) ([]ps.Process, error) {
	var result []ps.Process

	if d.returnError {
		return result, fmt.Errorf("mock error")
	}

	for i, existFlag := range d.existFlags {
		if existFlag {
			result = append(result, ps.Process{
				Pid: 2,
				Exe: "/random/path",
				CommandLine: []string{
					"dhclient",
					d.ipVersions[i].dhclientArg,
					d.ifaces[i],
				},
			})
		}
	}

	return result, nil
}

// Options for setting up test environment.
type dhclientTestOpts struct {
	// runErr indicates whether to error when running run.Quiet()
	// The error returned contains the arguments used when calling the function.
	runErr bool

	// processOpts contains options for dhclientProcessExists().
	processOpts dhclientProcessOpts
}

// Options for setting up dhclientProcessExists mocks.
type dhclientProcessOpts struct {
	// ifaces is the list of interfaces.
	ifaces []string

	// existFlag indicates whether ps.Find() should return a process, no processes, or an error
	// for the corresponding interface.
	existFlags []bool

	// returnError indicates whether to return an error. This takes precedence over
	// any value set in existFlags if set to true.
	returnError bool

	// ipVersion is the ipVersion for the corresponding interface.
	ipVersions []ipVersion
}

// dhclientTestSetup sets up the test.
func dhclientTestSetup(t *testing.T, opts dhclientTestOpts) {
	t.Helper()

	// We have to mock dhclientProcessExists as we cannot mock where the ps
	// package checks for processes here.
	processOpts := opts.processOpts
	ps.Client = &dhclientMockPs{
		ifaces:      processOpts.ifaces,
		existFlags:  processOpts.existFlags,
		returnError: processOpts.returnError,
		ipVersions:  processOpts.ipVersions,
	}

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

	run.Client = &dhclientMockRunner{
		quietErr: opts.runErr,
	}
}

// dhclientTestTearDown cleans up after each test.
func dhclientTestTearDown(t *testing.T) {
	t.Helper()

	run.Client = &run.Runner{}
	osinfoGet = osinfo.Get
	execLookPath = exec.LookPath
	ps.Client = &ps.LinuxClient{}
}

// TestDhclientIsManaging checks if IsManaging returns the correct values given certain
// situations or environment setups.
func TestDhclientIsManaging(t *testing.T) {
	tests := []struct {
		// name indicates what this test is testing.
		name string

		// findPath is the override for exec.LookPath for testing purposes.
		findPath func(path string) (string, error)

		// expectedBool is the expected boolean output of IsManaging.
		expectedBool bool

		// expectErr dictates whether to expect an error from IsManaging.
		expectErr bool
	}{
		// DHClient CLI exists.
		{
			name: "dhclient-exists",
			findPath: func(path string) (string, error) {
				return "", nil
			},
			expectedBool: true,
			expectErr:    false,
		},
		// DHClient does not exist.
		{
			name: "dhclient-not-exist",
			findPath: func(path string) (string, error) {
				return "", exec.ErrNotFound
			},
			expectedBool: false,
			expectErr:    false,
		},
		// Error finding path.
		{
			name: "dhclient-error",
			findPath: func(path string) (string, error) {
				return "", fmt.Errorf("mock error")
			},
			expectedBool: false,
			expectErr:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			execLookPath = test.findPath

			out, err := testDHClient.IsManaging(ctx, "test")

			if out != test.expectedBool {
				t.Fatalf("error checking dhclient management: expected %v, got %v", test.expectedBool, out)
			}

			if !test.expectErr && err != nil {
				t.Fatalf("unexpected error checking dhclient management: %v", err)
			}

			if test.expectErr && err == nil {
				t.Fatalf("no error when error expected")
			}
		})
	}
}

// TestPartitionInterfaces tests that partitionInterfaces behaves as expected given
// a mock set of inputs.
func TestPartitionInterfaces(t *testing.T) {
	tests := []struct {
		// name is the name of the test.
		name string

		// testInterfaces is a list of test interfaces.
		testInterfaces []string

		// testIpv6Interfaces is a list of test IPv6 interfaces.
		testIpv6Interfaces []string

		// existFlags is a list of flags to indicate if the process for the
		// corresponding interface exists.
		existFlags []bool

		// ipVersions is a list of flags to indicate the ipversion of each
		// corresponding interface.
		ipVersions []ipVersion

		// expectedObtainIpv4 is the expected obtainIPv4Interfaces output.
		expectedObtainIpv4 []string

		// expectedObtainIpv6 is the expected obtainIpv6Interfaces output.
		expectedObtainIpv6 []string

		// expectedReleaseIpv6 is the expected releaseIpv6Interfaces output.
		expectedReleaseIpv6 []string
	}{
		{
			name:                "all-ipv4",
			testInterfaces:      []string{"obtain1", "obtain2"},
			testIpv6Interfaces:  []string{},
			existFlags:          []bool{false, false},
			ipVersions:          []ipVersion{ipv4, ipv4},
			expectedObtainIpv4:  []string{"obtain1", "obtain2"},
			expectedObtainIpv6:  []string{},
			expectedReleaseIpv6: []string{},
		},
		{
			name:                "all-ipv6",
			testInterfaces:      []string{"obtain1", "obtain2"},
			testIpv6Interfaces:  []string{"obtain1", "obtain2"},
			existFlags:          []bool{false, false},
			ipVersions:          []ipVersion{ipv6, ipv6},
			expectedObtainIpv4:  []string{"obtain1", "obtain2"},
			expectedObtainIpv6:  []string{"obtain1", "obtain2"},
			expectedReleaseIpv6: []string{},
		},
		{
			name:                "ipv4-ipv6",
			testInterfaces:      []string{"obtain1", "obtain2"},
			testIpv6Interfaces:  []string{"obtain2"},
			existFlags:          []bool{false, false},
			ipVersions:          []ipVersion{ipv4, ipv6},
			expectedObtainIpv4:  []string{"obtain1", "obtain2"},
			expectedObtainIpv6:  []string{"obtain2"},
			expectedReleaseIpv6: []string{},
		},
		{
			name:                "release-ipv6",
			testInterfaces:      []string{"obtain1", "release1"},
			testIpv6Interfaces:  []string{"obtain1"},
			existFlags:          []bool{false, true},
			ipVersions:          []ipVersion{ipv4, ipv6},
			expectedObtainIpv4:  []string{"obtain1", "release1"},
			expectedObtainIpv6:  []string{"obtain1"},
			expectedReleaseIpv6: []string{"release1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()

			opts := dhclientTestOpts{
				processOpts: dhclientProcessOpts{
					ifaces:     test.testInterfaces,
					existFlags: test.existFlags,
					ipVersions: test.ipVersions,
				},
			}
			dhclientTestSetup(t, opts)

			obtainIpv4, obtainIpv6, releaseIpv6, err := partitionInterfaces(ctx, test.testInterfaces, test.testIpv6Interfaces)
			if err != nil {
				t.Fatalf("partitionInterfaces return error when none expected: %v", err)
			}
			if !slices.Equal(obtainIpv4, test.expectedObtainIpv4) {
				t.Errorf("partitionInterfaces(ctx, %v, %v) = obtainIpv4 %v, wanted %v", test.testInterfaces, test.testIpv6Interfaces, obtainIpv4, test.expectedObtainIpv4)
			}
			if !slices.Equal(obtainIpv6, test.expectedObtainIpv6) {
				t.Errorf("partitionInterfaces(ctx, %v, %v) = obtainIpv6 %v, wanted %v", test.testInterfaces, test.testIpv6Interfaces, obtainIpv6, test.expectedObtainIpv6)
			}
			if !slices.Equal(releaseIpv6, test.expectedReleaseIpv6) {
				t.Errorf("partitionInterfaces(ctx, %v, %v) = releaseIpv6 %v, wanted %v", test.testInterfaces, test.testIpv6Interfaces, releaseIpv6, test.expectedReleaseIpv6)
			}

			dhclientTestTearDown(t)
		})
	}
}

// TestRunDhclient tests whether runDhclient calls dhclient with the correct args.
func TestRunDhclient(t *testing.T) {
	tests := []struct {
		// name is the name of the test.
		name string

		// ipVersion is the ipVersion to use.
		ipVersion ipVersion

		// release dictates whether to release the interface.
		release bool

		// expectedFields is the expected output of runDhclient.
		expectedFields []string
	}{
		{
			name:      "ipv4",
			ipVersion: ipv4,
			expectedFields: []string{
				"dhclient",
				"-4",
				"-pf",
				pidFilePath("iface", ipv4),
				"-lf",
				leaseFilePath("iface", ipv4),
				"iface",
			},
		},
		{
			name:      "ipv6",
			ipVersion: ipv6,
			expectedFields: []string{
				"dhclient",
				"-6",
				"-pf",
				pidFilePath("iface", ipv6),
				"-lf",
				leaseFilePath("iface", ipv6),
				"iface",
			},
		},
		{
			name:      "release-ipv6",
			ipVersion: ipv6,
			release:   true,
			expectedFields: []string{
				"dhclient",
				"-6",
				"-pf",
				pidFilePath("iface", ipv6),
				"-lf",
				leaseFilePath("iface", ipv6),
				"-r",
				"iface",
			},
		},
	}

	opts := dhclientTestOpts{
		runErr: true,
	}
	dhclientTestSetup(t, opts)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()

			err := runDhclient(ctx, test.ipVersion, "iface", test.release)
			expectedCommand := strings.Join(test.expectedFields, " ")
			if !strings.Contains(err.Error(), expectedCommand) {
				t.Fatalf("Run error did not contain expected command line.\nError: %v\nExpected: %v\n", err, expectedCommand)
			}
		})
	}
	dhclientTestTearDown(t)
}

// TestDhclientProcessExists tests whether dhclientProcessExists behaves
// correctly given a mock environment setup.
func TestDhclientProcessExists(t *testing.T) {
	tests := []struct {
		// name is the name of the test.
		name string

		// ipVersion is the ipVersion to use in this test.
		ipVersion ipVersion

		// existFlag determines what the findProcess mock should return.
		existFlag bool

		// returnError determines if findProcess should return an error.
		returnError bool

		// expectBool is the expected return value of dhclientProcessExists()
		expectBool bool

		// expectErr dictates whether an error is expected.
		expectErr bool
	}{
		// Process exists ipv4.
		{
			name:       "ipv4",
			ipVersion:  ipv4,
			existFlag:  true,
			expectBool: true,
		},
		// Process exists ipv6.
		{
			name:       "ipv6",
			ipVersion:  ipv6,
			existFlag:  true,
			expectBool: true,
		},
		// Process not exist.
		{
			name:       "not-exist",
			ipVersion:  ipv4,
			existFlag:  false,
			expectBool: false,
		},
		// Error finding process.
		{
			name:        "error",
			ipVersion:   ipv6,
			returnError: true,
			expectBool:  false,
			expectErr:   true,
		},
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("test-dhclient-process-exists-%s", test.name), func(t *testing.T) {
			ctx := context.Background()
			opts := dhclientTestOpts{
				processOpts: dhclientProcessOpts{
					ifaces:      []string{"iface"},
					existFlags:  []bool{test.existFlag},
					returnError: test.returnError,
					ipVersions:  []ipVersion{test.ipVersion},
				},
			}
			dhclientTestSetup(t, opts)

			res, err := dhclientProcessExists(ctx, "iface", test.ipVersion)
			if err != nil {
				if !test.expectErr {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil && test.expectErr {
				t.Fatalf("no error returned when error expected")
			}

			if res != test.expectBool {
				t.Fatalf("incorrect return value. Expected: %v, Actual: %v", test.expectBool, res)
			}

			dhclientTestTearDown(t)
		})
	}
}
