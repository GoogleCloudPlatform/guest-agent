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
	"sort"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/google/go-cmp/cmp"
)

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
