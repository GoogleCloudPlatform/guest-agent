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
