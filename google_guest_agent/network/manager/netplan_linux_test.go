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

package manager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/google/go-cmp/cmp"
)

type mockNetplanRunner struct {
	executedCommands []string
}

func (m *mockNetplanRunner) Quiet(ctx context.Context, name string, args ...string) error {
	cmd := strings.Join(args, " ")
	m.executedCommands = append(m.executedCommands, name+" "+cmd)
	return nil
}

func (m *mockNetplanRunner) WithCombinedOutput(ctx context.Context, name string, args ...string) *run.Result {
	return &run.Result{StdErr: "unimplemented"}
}

func (m *mockNetplanRunner) WithOutput(ctx context.Context, name string, args ...string) *run.Result {
	return &run.Result{StdErr: "unimplemented"}
}

func (m *mockNetplanRunner) WithOutputTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) *run.Result {
	return &run.Result{StdErr: "unimplemented"}
}

func setupNetplanRunner(t *testing.T) *mockNetplanRunner {
	t.Helper()

	orig := run.Client
	t.Cleanup(func() { run.Client = orig })

	runner := &mockNetplanRunner{}
	run.Client = runner
	return runner
}

func verifyNetplanDropin(t *testing.T, ifaceName, netplanCfg string, nics *Interfaces) {
	t.Helper()

	wantNetplanDropin := &netplanDropin{
		Network: netplanNetwork{
			Version: 2,
			Vlans: map[string]netplanVlan{
				fmt.Sprintf("gcp.%s.5", ifaceName): {
					ID:                 5,
					Link:               ifaceName,
					DHCPv4:             makebool(true),
					OverrideMacAddress: "mac-address",
					MTU:                1460,
					DHCP4Overrides:     &netplanDHCPOverrides{UseDomains: makebool(false)},
					DHCP6Overrides:     &netplanDHCPOverrides{UseDomains: makebool(false)},
				},
				fmt.Sprintf("gcp.%s.6", ifaceName): {
					ID:                 6,
					Link:               ifaceName,
					DHCPv4:             makebool(true),
					DHCPv6:             makebool(true),
					OverrideMacAddress: "mac-address2",
					MTU:                1500,
					DHCP4Overrides:     &netplanDHCPOverrides{UseDomains: makebool(false)},
					DHCP6Overrides:     &netplanDHCPOverrides{UseDomains: makebool(false)},
				},
			},
		},
	}
	gotNetplanDropin := &netplanDropin{}
	netplanFile := filepath.Join(netplanCfg, "20-google-guest-agent-vlan.yaml")
	if err := readYamlFile(netplanFile, gotNetplanDropin); err != nil {
		t.Errorf("SetupVlanInterface(ctx, nil, %+v) did not write netplan dropin file %q correctly, failed to read: %v", nics, netplanFile, err)
	}
	if diff := cmp.Diff(wantNetplanDropin, gotNetplanDropin); diff != "" {
		t.Errorf("SetupVlanInterface(ctx, nil, %+v) returned diff on written netplan dropin (-want,+got)\n%s", nics, diff)
	}
}

func verifyNetworkdDropin(t *testing.T, ifaceName, networdCfg string, nics *Interfaces) {
	t.Helper()

	nic1Override := filepath.Join(networdCfg, fmt.Sprintf("10-netplan-gcp.%s.5.network.d", ifaceName), "override.conf")
	nic2Override := filepath.Join(networdCfg, fmt.Sprintf("10-netplan-gcp.%s.6.network.d", ifaceName), "override.conf")

	gotNic1Dropin := &networkdNetplanDropin{}
	gotNic2Dropin := &networkdNetplanDropin{}

	if err := readIniFile(nic1Override, gotNic1Dropin); err != nil {
		t.Errorf("SetupVlanInterface(ctx, nil, %+v) did not write networkd dropin file %q correctly, failed to read: %v", nics, nic1Override, err)
	}
	if err := readIniFile(nic2Override, gotNic2Dropin); err != nil {
		t.Errorf("SetupVlanInterface(ctx, nil, %+v) did not write networkd dropin file %q correctly, failed to read: %v", nics, nic2Override, err)
	}

	wantNic1Dropin := &networkdNetplanDropin{
		Match: systemdMatchConfig{
			Name: fmt.Sprintf("gcp.%s.5", ifaceName),
		},
		Network: systemdNetworkConfig{
			DHCP:            "ipv4",
			DNSDefaultRoute: false,
		},
		DHCPv4: &systemdDHCPConfig{
			RoutesToDNS: false,
			RoutesToNTP: false,
		},
	}

	if diff := cmp.Diff(wantNic1Dropin, gotNic1Dropin); diff != "" {
		t.Errorf("SetupVlanInterface(ctx, nil, %+v) returned diff on written networkd dropin (-want,+got)\n%s", nics, diff)
	}

	wantNic2Dropin := &networkdNetplanDropin{
		Match: systemdMatchConfig{
			Name: fmt.Sprintf("gcp.%s.6", ifaceName),
		},
		Network: systemdNetworkConfig{
			DHCP:            "yes",
			DNSDefaultRoute: false,
		},
		DHCPv4: &systemdDHCPConfig{
			RoutesToDNS: false,
			RoutesToNTP: false,
		},
	}

	if diff := cmp.Diff(wantNic2Dropin, gotNic2Dropin); diff != "" {
		t.Errorf("SetupVlanInterface(ctx, nil, %+v) returned diff on written networkd dropin (-want,+got)\n%s", nics, diff)
	}
}

func verifyRollback(t *testing.T, nics *Interfaces, netplanCfg, networdCfg, ifaceName string) {
	t.Helper()

	allNicsNetplan := filepath.Join(netplanCfg, "20-google-guest-agent-vlan.yaml")
	nic1Override := filepath.Join(networdCfg, fmt.Sprintf("10-netplan-gcp.%s.5.network.d", ifaceName), "override.conf")
	nic2Override := filepath.Join(networdCfg, fmt.Sprintf("10-netplan-gcp.%s.6.network.d", ifaceName), "override.conf")

	for _, f := range []string{allNicsNetplan, nic1Override, nic2Override} {
		if _, err := os.Stat(f); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("netplan.Rollback(ctx, %+v) did not remove config file %q", nics, f)
		}
	}
}

func makebool(b bool) *bool {
	return &b
}

func TestSetupVlanInterface(t *testing.T) {
	netplanCfg := t.TempDir()
	networkdCfg := t.TempDir()
	ctx := context.Background()

	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("could not list local interfaces: %+v", err)
	}

	mgr := &netplan{netplanConfigDir: netplanCfg, networkdDropinDir: networkdCfg, priority: 20}
	nics := &Interfaces{
		EthernetInterfaces: []metadata.NetworkInterfaces{
			{
				Mac: ifaces[1].HardwareAddr.String(),
			},
		},
		VlanInterfaces: map[int]metadata.VlanInterface{
			5: {
				ParentInterface: "/computeMetadata/v1/instance/network-interfaces/0/",
				Mac:             "mac-address",
				Vlan:            5,
				MTU:             1460,
			},
			6: {
				ParentInterface: "/computeMetadata/v1/instance/network-interfaces/0/",
				Mac:             "mac-address2",
				Vlan:            6,
				MTU:             1500,
				IPv6:            []string{"::0"},
				DHCPv6Refresh:   "123456",
			},
		},
	}

	runner := setupNetplanRunner(t)

	if err := mgr.SetupVlanInterface(ctx, nil, nics); err != nil {
		t.Errorf("SetupVlanInterface(ctx, nil, %+v) failed unexpectedly with error: %v", nics, err)
	}

	wantCmds := []string{"netplan apply", "networkctl reload"}
	gotCmds := runner.executedCommands
	sort.Strings(gotCmds)

	if diff := cmp.Diff(wantCmds, gotCmds); diff != "" {
		t.Errorf("SetupVlanInterface(ctx, nil, %+v) returned diff on command executed (-want,+got)\n%s", nics, diff)
	}

	verifyNetplanDropin(t, ifaces[1].Name, netplanCfg, nics)

	verifyNetworkdDropin(t, ifaces[1].Name, networkdCfg, nics)

	if err := mgr.Rollback(ctx, nics); err != nil {
		t.Errorf("netplan.Rollback(ctx, %+v) failed unexpectedly with error: %v", nics, err)
	}

	verifyRollback(t, nics, netplanCfg, networkdCfg, ifaces[1].Name)
}
