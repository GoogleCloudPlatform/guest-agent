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
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
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
		VlanInterfaces: map[string]VlanInterface{
			"0-5": {
				VlanInterface: metadata.VlanInterface{
					Mac:  "mac-address",
					Vlan: 5,
					MTU:  1460,
				},
				ParentInterfaceID: ifaces[1].Name,
			},
			"0-6": {
				VlanInterface: metadata.VlanInterface{
					Mac:           "mac-address2",
					Vlan:          6,
					MTU:           1500,
					IPv6:          []string{"::0"},
					DHCPv6Refresh: "123456",
				},
				ParentInterfaceID: ifaces[1].Name,
			},
		},
	}

	runner := setupNetplanRunner(t)

	if err := mgr.SetupVlanInterface(ctx, nil, nics); err != nil {
		t.Errorf("SetupVlanInterface(ctx, nil, %+v) failed unexpectedly with error: %v", nics, err)
	}

	wantCmds := []string{"netplan generate", "networkctl reload"}
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

	want := fmt.Sprintf("networkctl delete gcp.%s.5 gcp.%s.6", ifaces[1].Name, ifaces[1].Name)
	if !slices.Contains(runner.executedCommands, want) {
		t.Errorf("mgr.Rollback did not run %q, found commands: %v", want, runner.executedCommands)
	}
	verifyRollback(t, nics, netplanCfg, networkdCfg, ifaces[1].Name)
}

func TestIsNetworkdNetplanConfigSame(t *testing.T) {
	path1 := filepath.Join(t.TempDir(), "cfg1.yaml")
	path2 := filepath.Join(t.TempDir(), "cfg2.yaml")

	data := networkdNetplanDropin{
		Match: systemdMatchConfig{
			Name: "ens4",
		},
		Network: systemdNetworkConfig{
			DNSDefaultRoute: false,
			DHCP:            "yes",
		},
		DHCPv4: &systemdDHCPConfig{
			RoutesToDNS: false,
			RoutesToNTP: false,
		},
	}

	if err := writeIniFile(path1, &data); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	data2 := data
	data2.Network.DHCP = "ipv4"
	if err := writeIniFile(path2, &data2); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "same_file",
			path: path1,
			want: true,
		},
		{
			name: "modified_file",
			path: path2,
			want: false,
		},
		{
			name: "non_existent_file",
			path: filepath.Join(t.TempDir(), "cfg3.yaml"),
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := data.isSame(test.path); got != test.want {
				t.Errorf("isSame(%s) = %t, want = %t", test.path, got, test.want)
			}
		})
	}
}

func TestIsNetplanConfigSame(t *testing.T) {
	path1 := filepath.Join(t.TempDir(), "cfg1.yaml")
	path2 := filepath.Join(t.TempDir(), "cfg2.yaml")
	mtu := 1234

	dropin := netplanDropin{
		Network: netplanNetwork{
			Version: netplanConfigVersion,
			Ethernets: map[string]netplanEthernet{
				"eth0": {
					Match:          netplanMatch{Name: "eth0"},
					MTU:            &mtu,
					DHCPv4:         makebool(true),
					DHCP4Overrides: &netplanDHCPOverrides{UseDomains: makebool(false)},
				},
			},
		},
	}

	if err := writeYamlFile(path1, &dropin); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	dropin2 := dropin
	dropin2.Network.Vlans = map[string]netplanVlan{
		"1234.eth0": {
			ID:   1234,
			Link: "eth0",
		},
	}
	if err := writeYamlFile(path2, &dropin2); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "same_file",
			path: path1,
			want: true,
		},
		{
			name: "modified_file",
			path: path2,
			want: false,
		},
		{
			name: "non_existent_file",
			path: filepath.Join(t.TempDir(), "cfg3.yaml"),
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := dropin.isSame(test.path); got != test.want {
				t.Errorf("isSame(%s) = %t, want = %t", test.path, got, test.want)
			}
		})
	}
}

func TestPartialVlanRemoval(t *testing.T) {
	netplan := &netplan{
		netplanConfigDir:  t.TempDir(),
		networkdDropinDir: t.TempDir(),
	}

	netplandropin := netplanDropin{
		Network: netplanNetwork{
			Version: 2,
			Vlans: map[string]netplanVlan{
				"gcp.ens4.5": {
					ID:                 5,
					Link:               "ens4",
					DHCPv4:             makebool(true),
					OverrideMacAddress: "mac-address",
					MTU:                1460,
					DHCP4Overrides:     &netplanDHCPOverrides{UseDomains: makebool(false)},
					DHCP6Overrides:     &netplanDHCPOverrides{UseDomains: makebool(false)},
				},
				"gcp.ens4.6": {
					ID:                 6,
					Link:               "ens4",
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

	if ok, err := netplan.write(netplandropin, netplanVlanSuffix); err != nil || !ok {
		t.Fatalf("netplan write for test dropin file returned - err: %v, wrote: %t", err, ok)
	}

	// 	nic1Override := filepath.Join(networdCfg, fmt.Sprintf("10-netplan-gcp.%s.5.network.d", ifaceName), "override.conf")
	// Test networkd dropin configs.
	ens45DropinDir := filepath.Join(netplan.networkdDropinDir, "10-netplan-gcp.ens4.5.network.d")
	ens46DropinDir := filepath.Join(netplan.networkdDropinDir, "10-netplan-gcp.ens4.6.network.d")
	if err := os.MkdirAll(ens45DropinDir, 0755); err != nil {
		t.Fatalf("os.MkdirAll(%s, 0755) failed unexpectedly with error: %v", ens45DropinDir, err)
	}
	if err := os.MkdirAll(ens46DropinDir, 0755); err != nil {
		t.Fatalf("os.MkdirAll(%s, 0755) failed unexpectedly with error: %v", ens46DropinDir, err)
	}

	ens45Override := filepath.Join(ens45DropinDir, "override.conf")
	if _, err := os.Create(ens45Override); err != nil {
		t.Fatalf("os.Create(%s) failed unexpectedly with error: %v", ens45Override, err)
	}
	ens46Override := filepath.Join(ens46DropinDir, "override.conf")
	if _, err := os.Create(ens46Override); err != nil {
		t.Fatalf("os.Create(%s) failed unexpectedly with error: %v", ens46Override, err)
	}

	nics := &Interfaces{
		VlanInterfaces: map[string]VlanInterface{
			"ens4-5": {
				VlanInterface: metadata.VlanInterface{
					Mac:  "mac-address",
					Vlan: 5,
					MTU:  1460,
				},
				ParentInterfaceID: "ens4",
			},
		},
	}

	wantNics := &Interfaces{
		VlanInterfaces: map[string]VlanInterface{
			"ens4-6": {
				VlanInterface: metadata.VlanInterface{
					Vlan: 6,
				},
				ParentInterfaceID: "ens4",
			},
		},
	}

	got, err := netplan.findVlanDiff(nics)
	if err != nil {
		t.Fatalf("netplan.findVlanDiff(%+v) failed unexpectedly with error: %v", nics, err)
	}

	if got == nil {
		t.Fatalf("netplan.findVlanDiff(%+v) return nil, expected non nil", nics)
	}

	if diff := cmp.Diff(wantNics, got); diff != "" {
		t.Errorf("netplan.findVlanDiff(%+v) returned diff on vlans to remove (-want,+got)\n%s", nics, diff)
	}

	runner := setupNetplanRunner(t)

	reload, err := netplan.rollbackVlanNics(context.Background(), got)
	if err != nil {
		t.Fatalf("netplan.rollbackVlanNics(ctx, %+v) failed unexpectedly with error: %v", got, err)
	}
	if !reload {
		t.Fatalf("netplan.rollbackVlanNics(ctx, %+v) returned false for reload want true ", err)
	}

	// Verify we did networctl delete.
	wantCmd := "networkctl delete gcp.ens4.6"
	if !slices.Contains(runner.executedCommands, wantCmd) {
		t.Errorf("netplan.rollbackVlanNics did not run %s, executed %+v", wantCmd, runner.executedCommands)
	}

	// Netplan dropin should still exist for gcp.ens4.5 but not for gcp.ens4.6.
	content, err := os.ReadFile(netplan.dropinFile(netplanVlanSuffix))
	if err != nil {
		t.Fatalf("os.ReadFile(%s) failed unexpectedly with error: %v", netplan.dropinFile(netplanVlanSuffix), err)
	}
	gotContent := string(content)
	if strings.Contains(gotContent, "gcp.ens4.6") || !strings.Contains(gotContent, "gcp.ens4.5") {
		t.Errorf("%s after netplan.rollbackVlanNics =\n %s, gcp.ens4.5 should exist but not gcp.ens4.6.", netplan.dropinFile(netplanVlanSuffix), gotContent)
	}
	if !utils.FileExists(ens45Override, utils.TypeFile) {
		t.Errorf("netplan.rollbackVlanNics unexpectedly removed %s", ens45Override)
	}
	if utils.FileExists(ens46DropinDir, utils.TypeDir) {
		t.Errorf("netplan.rollbackVlanNics did not remove %s", ens46DropinDir)
	}
}
