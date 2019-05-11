//  Copyright 2017 Google Inc. All Rights Reserved.
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

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/compute-image-windows/logger"
	"github.com/go-ini/ini"
)

func TestCompareIPs(t *testing.T) {
	var tests = []struct {
		regFwdIPs, mdFwdIPs, cfgIPs, wantAdd, wantRm []string
	}{
		// These should return toAdd:
		// In MD, not Reg or config
		{nil, []string{"1.2.3.4"}, nil, []string{"1.2.3.4"}, nil},
		// In MD and in Reg, not config
		{[]string{"1.2.3.4"}, []string{"1.2.3.4"}, nil, []string{"1.2.3.4"}, nil},

		// These should return toRm:
		// In Reg and config, not Md
		{[]string{"1.2.3.4"}, nil, []string{"1.2.3.4"}, nil, []string{"1.2.3.4"}},

		// These should return nil, nil:
		// In Reg, Md and config
		{[]string{"1.2.3.4"}, []string{"1.2.3.4"}, []string{"1.2.3.4"}, nil, nil},
		// In Md and config, not Reg
		{nil, []string{"1.2.3.4"}, []string{"1.2.3.4"}, nil, nil},
		// Only in Reg
		{[]string{"1.2.3.4"}, nil, nil, nil, nil},
		// Only in config
		{nil, nil, []string{"1.2.3.4"}, nil, nil},
	}

	for _, tt := range tests {
		toAdd, toRm := compareIPs(tt.regFwdIPs, tt.mdFwdIPs, tt.cfgIPs)
		if !reflect.DeepEqual(tt.wantAdd, toAdd) {
			t.Errorf("toAdd does not match expected: regFwdIPs: %q, mdFwdIPs: %q, cfgIPs: %q, got: %q, want: %q", tt.regFwdIPs, tt.mdFwdIPs, tt.cfgIPs, toAdd, tt.wantAdd)
		}
		if !reflect.DeepEqual(tt.wantRm, toRm) {
			t.Errorf("toRm does not match expected: regFwdIPs: %q, mdFwdIPs: %q, cfgIPs: %q, got: %q, want: %q", tt.regFwdIPs, tt.mdFwdIPs, tt.cfgIPs, toRm, tt.wantRm)
		}
	}

}

func TestAddressDisabled(t *testing.T) {
	var tests = []struct {
		name string
		data []byte
		md   *metadataJSON
		want bool
	}{
		{"not explicitly disabled", []byte(""), &metadataJSON{}, false},
		{"enabled in cfg only", []byte("[addressManager]\ndisable=false"), &metadataJSON{}, false},
		{"disabled in cfg only", []byte("[addressManager]\ndisable=true"), &metadataJSON{}, true},
		{"disabled in cfg, enabled in instance metadata", []byte("[addressManager]\ndisable=true"), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAddressManager: "false"}}}, true},
		{"enabled in cfg, disabled in instance metadata", []byte("[addressManager]\ndisable=false"), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAddressManager: "true"}}}, false},
		{"enabled in instance metadata only", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAddressManager: "false"}}}, false},
		{"enabled in project metadata only", []byte(""), &metadataJSON{Project: projectJSON{Attributes: attributesJSON{DisableAddressManager: "false"}}}, false},
		{"disabled in instance metadata only", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAddressManager: "true"}}}, true},
		{"enabled in instance metadata, disabled in project metadata", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAddressManager: "false"}}, Project: projectJSON{Attributes: attributesJSON{DisableAddressManager: "true"}}}, false},
		{"disabled in project metadata only", []byte(""), &metadataJSON{Project: projectJSON{Attributes: attributesJSON{DisableAddressManager: "true"}}}, true},
	}

	for _, tt := range tests {
		cfg, err := ini.InsensitiveLoad(tt.data)
		if err != nil {
			t.Errorf("test case %q: error parsing config: %v", tt.name, err)
			continue
		}
		if cfg == nil {
			cfg = &ini.File{}
		}
		newMetadata = tt.md
		config = cfg
		got := (&addressMgr{}).disabled()
		if got != tt.want {
			t.Errorf("test case %q, disabled? got: %t, want: %t", tt.name, got, tt.want)
		}
	}
}

func TestAddressDiff(t *testing.T) {
	var tests = []struct {
		name string
		data []byte
		md   *metadataJSON
		want bool
	}{
		{"not set", []byte(""), &metadataJSON{}, false},
		{"enabled in cfg only", []byte("[wsfc]\nenable=true"), &metadataJSON{}, true},
		{"disabled in cfg only", []byte("[wsfc]\nenable=false"), &metadataJSON{}, false},
		{"disabled in cfg, enabled in instance metadata", []byte("[wsfc]\nenable=false"), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{EnableWSFC: "true"}}}, false},
		{"enabled in cfg, disabled in instance metadata", []byte("[wsfc]\nenable=true"), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{EnableWSFC: "false"}}}, true},
		{"enabled in instance metadata only", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{EnableWSFC: "true"}}}, true},
		{"enabled in project metadata only", []byte(""), &metadataJSON{Project: projectJSON{Attributes: attributesJSON{EnableWSFC: "true"}}}, true},
		{"disabled in instance metadata only", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{EnableWSFC: "false"}}}, false},
		{"enabled in instance metadata, disabled in project metadata", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{EnableWSFC: "true"}}, Project: projectJSON{Attributes: attributesJSON{EnableWSFC: "false"}}}, true},
		{"disabled in project metadata only", []byte(""), &metadataJSON{Project: projectJSON{Attributes: attributesJSON{EnableWSFC: "false"}}}, false},
	}

	for _, tt := range tests {
		cfg, err := ini.InsensitiveLoad(tt.data)
		if err != nil {
			t.Errorf("test case %q: error parsing config: %v", tt.name, err)
			continue
		}
		if cfg == nil {
			cfg = &ini.File{}
		}
		oldWSFCEnable = false
		oldMetadata = &metadataJSON{}
		newMetadata = tt.md
		config = cfg
		got := (&addressMgr{}).diff()
		if got != tt.want {
			t.Errorf("test case %q, addresses.diff() got: %t, want: %t", tt.name, got, tt.want)
		}
	}
}

func TestWsfcFilter(t *testing.T) {
	var tests = []struct {
		metaData    []byte
		expectedIps []string
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

	config = ini.Empty()
	for _, tt := range tests {
		var metadata metadataJSON
		if err := json.Unmarshal(tt.metaData, &metadata); err != nil {
			t.Error("invalid test case:", tt, err)
		}

		newMetadata = &metadata
		testAddress := addressMgr{}
		testAddress.applyWSFCFilter()

		forwardedIps := []string{}
		for _, ni := range newMetadata.Instance.NetworkInterfaces {
			forwardedIps = append(forwardedIps, ni.ForwardedIps...)
		}

		if !reflect.DeepEqual(forwardedIps, tt.expectedIps) {
			t.Errorf("wsfc filter failed: expect - %q, actual - %q", tt.expectedIps, forwardedIps)
		}
	}
}

func TestWsfcFlagTriggerAddressDiff(t *testing.T) {
	var tests = []struct {
		newMetadata, oldMetadata *metadataJSON
	}{
		// trigger diff on wsfc-addrs
		{&metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{WSFCAddresses: "192.168.0.1"}}}, &metadataJSON{}},
		{&metadataJSON{Project: projectJSON{Attributes: attributesJSON{WSFCAddresses: "192.168.0.1"}}}, &metadataJSON{}},
		{&metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{WSFCAddresses: "192.168.0.1"}}}, &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{WSFCAddresses: "192.168.0.2"}}}},
		{&metadataJSON{Project: projectJSON{Attributes: attributesJSON{WSFCAddresses: "192.168.0.1"}}}, &metadataJSON{Project: projectJSON{Attributes: attributesJSON{WSFCAddresses: "192.168.0.2"}}}},
	}

	config = ini.Empty()
	for _, tt := range tests {
		oldWSFCAddresses = tt.oldMetadata.Instance.Attributes.WSFCAddresses
		newMetadata = tt.newMetadata
		oldMetadata = tt.oldMetadata
		testAddress := addressMgr{}
		if !testAddress.diff() {
			t.Errorf("old: %q new: %q doesn't tirgger diff.", tt.oldMetadata, tt.newMetadata)
		}
	}
}

func TestAddressLogStatus(t *testing.T) {
	var buf bytes.Buffer
	logger.Init("test", "")
	logger.Log = log.New(&buf, "", 0)

	// Disable it.
	addressDisabled = false
	newMetadata = &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAddressManager: "true"}}}
	config = ini.Empty()
	disabled := (&addressMgr{}).disabled()
	if !disabled {
		t.Fatal("expected true but got", disabled)
	}
	want := fmt.Sprintln("test: GCE address manager status: disabled")
	if buf.String() != want {
		t.Errorf("got: %q, want: %q", buf.String(), want)
	}
	buf.Reset()

	// Enable it.
	newMetadata = &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAddressManager: "false"}}}
	disabled = (&addressMgr{}).disabled()
	if disabled {
		t.Fatal("expected false but got", disabled)
	}
	want = fmt.Sprintln("test: GCE address manager status: enabled")
	if buf.String() != want {
		t.Errorf("got: %q, want: %q", buf.String(), want)
	}
}
