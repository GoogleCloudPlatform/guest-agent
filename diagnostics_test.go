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
		k := diagnosticsEntryJSON{ExpireOn: tt.sTime}
		if tt.e != k.expired() {
			t.Errorf("diagnosticsEntryJSON.expired() with ExpiredOn %q should return %t", k.ExpireOn, tt.e)
		}
	}
}

func TestDiagnosticsDisabled(t *testing.T) {
	var tests = []struct {
		name string
		data []byte
		md   *metadataJSON
		want bool
	}{
		{"not explicitly enabled", []byte(""), &metadataJSON{}, true},
		{"enabled in cfg only", []byte("[diagnostics]\nenable=true"), &metadataJSON{}, false},
		{"disabled in cfg only", []byte("[diagnostics]\nenable=false"), &metadataJSON{}, true},
		{"disabled in cfg, enabled in instance metadata", []byte("[diagnostics]\nenable=false"), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{EnableDiagnostics: "true"}}}, true},
		{"enabled in cfg, disabled in instance metadata", []byte("[diagnostics]\nenable=true"), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{EnableDiagnostics: "false"}}}, false},
		{"enabled in instance metadata only", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{EnableDiagnostics: "true"}}}, false},
		{"enabled in project metadata only", []byte(""), &metadataJSON{Project: projectJSON{Attributes: attributesJSON{EnableDiagnostics: "true"}}}, false},
		{"disabled in instance metadata only", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{EnableDiagnostics: "false"}}}, true},
		{"enabled in instance metadata, disabled in project metadata", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{EnableDiagnostics: "true"}}, Project: projectJSON{Attributes: attributesJSON{EnableDiagnostics: "false"}}}, false},
		{"disabled in project metadata only", []byte(""), &metadataJSON{Project: projectJSON{Attributes: attributesJSON{EnableDiagnostics: "false"}}}, true},
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
		got := (&diagnosticsMgr{}).disabled()
		if got != tt.want {
			t.Errorf("test case %q, diagnostics.disabled() got: %t, want: %t", tt.name, got, tt.want)
		}
	}
}
