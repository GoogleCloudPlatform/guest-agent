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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/google/go-cmp/cmp"
)

func TestVlanParentInterfaceSuccess(t *testing.T) {
	tests := []struct {
		parentInterface string
		expectedResult  string
	}{
		{"/computeMetadata/v1/instance/network-interfaces/0/", "eth0"},
		{"/computeMetadata/v1/instance/network-interfaces/1/", "eth1"},
		{"/computeMetadata/v1/instance/network-interfaces/2/", "eth2"},
	}

	availableNics := []string{
		"eth0",
		"eth1",
		"eth2",
	}

	for i, curr := range tests {
		t.Run(fmt.Sprintf("test-vlan-parent-success-%d", i), func(t *testing.T) {
			vlan := metadata.VlanInterface{ParentInterface: curr.parentInterface}

			parent, err := vlanParentInterface(availableNics, vlan)
			if err != nil {
				t.Fatalf("expected err: nil, got: %+v", err)
			}

			if parent != curr.expectedResult {
				t.Fatalf("got wront parent value, expected: %s, got: %s", curr.expectedResult, parent)
			}
		})
	}
}

func TestVlanParentInterfaceFailure(t *testing.T) {
	tests := []string{
		"/computeMetadata/v1/instance/network-interfaces/x/",
		"/computeMetadata/v1/instance/network-interfaces/0/",                    // Valid format but interfaces slices will have zero elements.
		"/computeMetadata/v1/instance/network-interfaces/18446744073709551616/", // Out of int64 range - strconv.Atoi() should fail.
		"/computeMetadata/v1/instance/0/",
		"/computeMetadata/v1/instance/network-interfaces0/",
		"/computeMetadata/v1/instance/network-interfaces/",
		"/computeMetadata/",
		"",
	}

	for i, curr := range tests {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
			vlan := metadata.VlanInterface{ParentInterface: curr}
			_, err := vlanParentInterface([]string{}, vlan)
			if err == nil {
				t.Fatalf("vlanParentInterface(%s) = nil, want: non-nil", curr)
			}
		})
	}
}

func TestVlanInterfaceListsIpv6(t *testing.T) {
	nics := map[int]VlanInterface{
		0: {VlanInterface: metadata.VlanInterface{Vlan: 4, DHCPv6Refresh: "123456"}},
		1: {VlanInterface: metadata.VlanInterface{Vlan: 5}},
		2: {VlanInterface: metadata.VlanInterface{Vlan: 6, MTU: 1234}},
		3: {VlanInterface: metadata.VlanInterface{Vlan: 7, Mac: "acd", ParentInterface: "/parent/0", DHCPv6Refresh: "7890"}},
	}
	want := []int{4, 7}
	got := vlanInterfaceListsIpv6(nics)
	sort.Ints(got)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("vlanInterfaceListsIpv6(%+v) returned unexpected diff (-want,+got)\n%s", nics, diff)
	}
}

func TestVlanInterfaceParentMap(t *testing.T) {
	tests := []struct {
		name                  string
		nics                  map[int]metadata.VlanInterface
		allEthernetInterfaces []string
		wantErr               bool
		wantMap               map[int]string
	}{
		{
			name:                  "all_valid_nics",
			allEthernetInterfaces: []string{"ens3", "ens4"},
			nics: map[int]metadata.VlanInterface{
				4: {Vlan: 4, ParentInterface: "/computeMetadata/v1/instance/network-interfaces/0/"},
				5: {Vlan: 5, ParentInterface: "/computeMetadata/v1/instance/network-interfaces/1/"},
			},
			wantMap: map[int]string{
				4: "ens3",
				5: "ens4",
			},
		},
		{
			name:                  "invalid_parent",
			allEthernetInterfaces: []string{"ens3"},
			nics: map[int]metadata.VlanInterface{
				5: {Vlan: 5, ParentInterface: "/computeMetadata/v1/instance/network-interfaces/1/"},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := vlanInterfaceParentMap(test.nics, test.allEthernetInterfaces)
			if (err != nil) != test.wantErr {
				t.Fatalf("vlanInterfaceParentMap(%+v, %v) = error [%v], want error: %t", test.nics, test.allEthernetInterfaces, err, test.wantErr)
			}
			if diff := cmp.Diff(test.wantMap, got); diff != "" {
				t.Errorf("vlanInterfaceParentMap(%+v, %v) returned unexpected diff (-want,+got)\n%s", test.nics, test.allEthernetInterfaces, diff)
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "file")
	if err != nil {
		t.Fatalf("os.CreateTemp(%s, file) failed unexpectedly with error: %v", dir, err)
	}
	defer f.Close()

	tests := []struct {
		name string
		want bool
		path string
	}{
		{
			name: "existing_file",
			want: true,
			path: f.Name(),
		},
		{
			name: "existing_dir",
			want: false,
			path: dir,
		},
		{
			name: "non_existing_file",
			want: false,
			path: filepath.Join(t.TempDir(), "random"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := fileExists(test.path); got != test.want {
				t.Errorf("fileExists(%s) = %t, want = %t", test.path, got, test.want)
			}
		})
	}
}
