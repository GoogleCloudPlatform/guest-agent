//  Copyright 2018 Google Inc. All Rights Reserved.
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
	"testing"
	"time"

	"github.com/go-ini/ini"
)

func TestDiagnosticsEntryExpired(t *testing.T) {
	var tests = []struct {
		sTime string
		e     bool
	}{
		{time.Now().Add(5 * time.Minute).Format(time.RFC3339), false},
		{time.Now().Add(-5 * time.Minute).Format(time.RFC3339), true},
		{"some bad time", true},
	}

	for _, tt := range tests {
		k := diagnosticsEntry{ExpireOn: tt.sTime}
		if tt.e != k.expired() {
			t.Errorf("diagnosticsEntry.expired() with ExpiredOn %q should return %t", k.ExpireOn, tt.e)
		}
	}
}

func TestDiagnosticsDisabled(t *testing.T) {
	var tests = []struct {
		name string
		data []byte
		md   *metadata
		want bool
	}{
		{"not explicitly enabled", []byte(""), &metadata{}, true},
		{"enabled in cfg only", []byte("[diagnostics]\nenable=true"), &metadata{}, false},
		{"disabled in cfg only", []byte("[diagnostics]\nenable=false"), &metadata{}, true},
		{"disabled in cfg, enabled in instance metadata", []byte("[diagnostics]\nenable=false"), &metadata{Instance: instance{Attributes: attributes{EnableDiagnostics: mkptr(true)}}}, true},
		{"enabled in cfg, disabled in instance metadata", []byte("[diagnostics]\nenable=true"), &metadata{Instance: instance{Attributes: attributes{EnableDiagnostics: mkptr(false)}}}, false},
		{"enabled in instance metadata only", []byte(""), &metadata{Instance: instance{Attributes: attributes{EnableDiagnostics: mkptr(true)}}}, false},
		{"enabled in project metadata only", []byte(""), &metadata{Project: project{Attributes: attributes{EnableDiagnostics: mkptr(true)}}}, false},
		{"disabled in instance metadata only", []byte(""), &metadata{Instance: instance{Attributes: attributes{EnableDiagnostics: mkptr(false)}}}, true},
		{"enabled in instance metadata, disabled in project metadata", []byte(""), &metadata{Instance: instance{Attributes: attributes{EnableDiagnostics: mkptr(true)}}, Project: project{Attributes: attributes{EnableDiagnostics: mkptr(false)}}}, false},
		{"disabled in project metadata only", []byte(""), &metadata{Project: project{Attributes: attributes{EnableDiagnostics: mkptr(false)}}}, true},
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
		got := (&diagnosticsMgr{}).disabled("windows")
		if got != tt.want {
			t.Errorf("test case %q, diagnostics.disabled() got: %t, want: %t", tt.name, got, tt.want)
		}
	}
}
