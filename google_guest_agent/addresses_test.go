// Copyright 2017 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
)

func reloadConfig(t *testing.T, extraDefaults []byte) {
	t.Helper()
	if err := cfg.Load(extraDefaults); err != nil {
		t.Fatalf("Error parsing config: %+v", err)
	}
}

func TestCompareRoutes(t *testing.T) {
	var tests = []struct {
		forwarded, metadata, wantAdd, wantRm []string
	}{
		// These should return toAdd:
		// In Md, not present
		{nil, []string{"1.2.3.4"}, []string{"1.2.3.4"}, nil},
		{nil, []string{"1.2.3.4", "5.6.7.8"}, []string{"1.2.3.4", "5.6.7.8"}, nil},

		// These should return toRm:
		// Present, not in Md
		{[]string{"1.2.3.4"}, nil, nil, []string{"1.2.3.4"}},
		{[]string{"1.2.3.4", "5.6.7.8"}, []string{"5.6.7.8"}, nil, []string{"1.2.3.4"}},

		// These should return nil, nil:
		// Present, in Md
		{[]string{"1.2.3.4"}, []string{"1.2.3.4"}, nil, nil},
		{[]string{"1.2.3.4", "5.6.7.8"}, []string{"1.2.3.4", "5.6.7.8"}, nil, nil},
		{[]string{"1.2.3.4", "5.6.7.8"}, []string{"1.2.3.4", "5.6.7.8"}, nil, nil},
	}

	for idx, tt := range tests {
		toAdd, toRm := compareRoutes(tt.forwarded, tt.metadata)
		if !reflect.DeepEqual(tt.wantAdd, toAdd) {
			t.Errorf("case %d: toAdd does not match expected: forwarded: %q, metadata: %q, got: %q, want: %q", idx, tt.forwarded, tt.metadata, toAdd, tt.wantAdd)
		}
		if !reflect.DeepEqual(tt.wantRm, toRm) {
			t.Errorf("case %d: toRm does not match expected: forwarded: %q, metadata: %q, got: %q, want: %q", idx, tt.forwarded, tt.metadata, toRm, tt.wantRm)
		}
	}
}

func TestAddressDisabled(t *testing.T) {
	var tests = []struct {
		name string
		data []byte
		md   *metadata.Descriptor
		want bool
	}{
		{"not explicitly disabled", []byte(""), &metadata.Descriptor{}, false},
		{"enabled in cfg only", []byte("[addressManager]\ndisable=false"), &metadata.Descriptor{}, false},
		{"disabled in cfg only", []byte("[addressManager]\ndisable=true"), &metadata.Descriptor{}, true},
		{"disabled in cfg, enabled in instance metadata", []byte("[addressManager]\ndisable=true"), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{DisableAddressManager: mkptr(false)}}}, true},
		{"enabled in cfg, disabled in instance metadata", []byte("[addressManager]\ndisable=false"), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{DisableAddressManager: mkptr(true)}}}, false},
		{"enabled in instance metadata only", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{DisableAddressManager: mkptr(false)}}}, false},
		{"enabled in project metadata only", []byte(""), &metadata.Descriptor{Project: metadata.Project{Attributes: metadata.Attributes{DisableAddressManager: mkptr(false)}}}, false},
		{"disabled in instance metadata only", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{DisableAddressManager: mkptr(true)}}}, true},
		{"enabled in instance metadata, disabled in project metadata", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{DisableAddressManager: mkptr(false)}}, Project: metadata.Project{Attributes: metadata.Attributes{DisableAddressManager: mkptr(true)}}}, false},
		{"disabled in project metadata only", []byte(""), &metadata.Descriptor{Project: metadata.Project{Attributes: metadata.Attributes{DisableAddressManager: mkptr(true)}}}, true},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reloadConfig(t, tt.data)
			newMetadata = tt.md
			got, err := (&addressMgr{}).Disabled(ctx)
			if err != nil {
				t.Errorf("Failed to run addressMgr's Disabled() call, got error: %+v", err)
			}

			if got != tt.want {
				t.Errorf("addressMgr.Disabled() got: %t, want: %t", got, tt.want)
			}
		})
	}
}

func TestAddressDiff(t *testing.T) {
	var tests = []struct {
		name string
		data []byte
		md   *metadata.Descriptor
		want bool
	}{
		{"not set", []byte(""), &metadata.Descriptor{}, false},
		{"enabled in cfg only", []byte("[wsfc]\nenable=true"), &metadata.Descriptor{}, true},
		{"disabled in cfg only", []byte("[wsfc]\nenable=false"), &metadata.Descriptor{}, false},
		{"disabled in cfg, enabled in instance metadata", []byte("[wsfc]\nenable=false"), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{EnableWSFC: mkptr(true)}}}, false},
		{"enabled in cfg, disabled in instance metadata", []byte("[wsfc]\nenable=true"), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{EnableWSFC: mkptr(false)}}}, true},
		{"enabled in instance metadata only", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{EnableWSFC: mkptr(true)}}}, true},
		{"enabled in project metadata only", []byte(""), &metadata.Descriptor{Project: metadata.Project{Attributes: metadata.Attributes{EnableWSFC: mkptr(true)}}}, true},
		{"disabled in instance metadata only", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{EnableWSFC: mkptr(false)}}}, false},
		{"enabled in instance metadata, disabled in project metadata", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{EnableWSFC: mkptr(true)}}, Project: metadata.Project{Attributes: metadata.Attributes{EnableWSFC: mkptr(false)}}}, true},
		{"disabled in project metadata only", []byte(""), &metadata.Descriptor{Project: metadata.Project{Attributes: metadata.Attributes{EnableWSFC: mkptr(false)}}}, false},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reloadConfig(t, tt.data)

			oldWSFCEnable = false
			oldMetadata = &metadata.Descriptor{}
			newMetadata = tt.md

			got, err := (&addressMgr{}).Diff(ctx)
			if err != nil {
				t.Errorf("Failed to run addressMgr's Diff() call, got error: %+v", err)
			}

			if got != tt.want {
				t.Errorf("addresses.diff() got: %t, want: %t", got, tt.want)
			}
		})
	}
}

func TestWsfcFilter(t *testing.T) {
	var tests = []struct {
		metaDataJSON []byte
		expectedIps  []string
	}{
		// signle nic with enable-wsfc set to true
		{[]byte(`{"instance":{"attributes":{"enable-wsfc":"true"}, "networkInterfaces":[{"forwardedIps":["192.168.0.0", "192.168.0.1"]}]}}`), []string{}},
		// multi nic with enable-wsfc set to true
		{[]byte(`{"instance":{"attributes":{"enable-wsfc":"true"}, "networkInterfaces":[{"forwardedIps":["192.168.0.0", "192.168.0.1"]},{"forwardedIps":["192.168.0.2"]}]}}`), []string{}},
		// filter with wsfc-addrs
		{[]byte(`{"instance":{"attributes":{"wsfc-addrs":"192.168.0.1"}, "networkInterfaces":[{"forwardedIps":["192.168.0.0", "192.168.0.1"]}]}}`), []string{"192.168.0.0"}},
		// filter with both wsfc-addrs and enable-wsfc flag
		{[]byte(`{"instance":{"attributes":{"wsfc-addrs":"192.168.0.1", "enable-wsfc":"true"}, "networkInterfaces":[{"forwardedIps":["192.168.0.0", "192.168.0.1"]}]}}`), []string{"192.168.0.0"}},
		// filter with invalid wsfc-addrs
		{[]byte(`{"instance":{"attributes":{"wsfc-addrs":"192.168.0"}, "networkInterfaces":[{"forwardedIps":["192.168.0.0", "192.168.0.1"]}]}}`), []string{"192.168.0.0", "192.168.0.1"}},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
			var md metadata.Descriptor

			reloadConfig(t, nil)

			if err := json.Unmarshal(tt.metaDataJSON, &md); err != nil {
				t.Error("failed to unmarshal test JSON:", tt, err)
			}

			newMetadata = &md
			testAddress := addressMgr{}
			testAddress.applyWSFCFilter(cfg.Get())

			forwardedIps := []string{}
			for _, ni := range newMetadata.Instance.NetworkInterfaces {
				forwardedIps = append(forwardedIps, ni.ForwardedIps...)
			}

			if !reflect.DeepEqual(forwardedIps, tt.expectedIps) {
				t.Errorf("wsfc filter failed: expect - %q, actual - %q", tt.expectedIps, forwardedIps)
			}
		})
	}
}

func TestWsfcFlagTriggerAddressDiff(t *testing.T) {
	var tests = []struct {
		newMetadata, oldMetadata *metadata.Descriptor
	}{
		// trigger diff on wsfc-addrs
		{&metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{WSFCAddresses: "192.168.0.1"}}}, &metadata.Descriptor{}},
		{&metadata.Descriptor{Project: metadata.Project{Attributes: metadata.Attributes{WSFCAddresses: "192.168.0.1"}}}, &metadata.Descriptor{}},
		{&metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{WSFCAddresses: "192.168.0.1"}}}, &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{WSFCAddresses: "192.168.0.2"}}}},
		{&metadata.Descriptor{Project: metadata.Project{Attributes: metadata.Attributes{WSFCAddresses: "192.168.0.1"}}}, &metadata.Descriptor{Project: metadata.Project{Attributes: metadata.Attributes{WSFCAddresses: "192.168.0.2"}}}},
	}

	ctx := context.Background()
	for i, tt := range tests {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
			reloadConfig(t, nil)

			oldWSFCAddresses = tt.oldMetadata.Instance.Attributes.WSFCAddresses
			newMetadata = tt.newMetadata
			oldMetadata = tt.oldMetadata
			testAddress := addressMgr{}

			diff, err := testAddress.Diff(ctx)
			if err != nil {
				t.Errorf("Failed to run addressMgr's Diff() call, got error: %+v", err)
			}

			if !diff {
				t.Errorf("old: %v new: %v doesn't trigger diff.", tt.oldMetadata, tt.newMetadata)
			}
		})
	}
}
