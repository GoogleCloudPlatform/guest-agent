// Copyright 2018 Google LLC

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
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
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
		expired, _ := utils.CheckExpired(k.ExpireOn)
		if tt.e != expired {
			t.Errorf("diagnosticsEntry.expired() with ExpiredOn %q should return %t", k.ExpireOn, tt.e)
		}
	}
}

func TestDiagnosticsDisabled(t *testing.T) {
	var tests = []struct {
		name string
		data []byte
		md   *metadata.Descriptor
		want bool
	}{
		{"not explicitly enabled", []byte(""), &metadata.Descriptor{}, false},
		{"enabled in cfg only", []byte("[diagnostics]\nenable=true"), &metadata.Descriptor{}, false},
		{"disabled in cfg only", []byte("[diagnostics]\nenable=false"), &metadata.Descriptor{}, true},
		{"disabled in cfg, enabled in instance metadata", []byte("[diagnostics]\nenable=false"), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{EnableDiagnostics: mkptr(true)}}}, true},
		{"enabled in cfg, disabled in instance metadata", []byte("[diagnostics]\nenable=true"), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{EnableDiagnostics: mkptr(false)}}}, false},
		{"enabled in instance metadata only", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{EnableDiagnostics: mkptr(true)}}}, false},
		{"enabled in project metadata only", []byte(""), &metadata.Descriptor{Project: metadata.Project{Attributes: metadata.Attributes{EnableDiagnostics: mkptr(true)}}}, false},
		{"disabled in instance metadata only", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{EnableDiagnostics: mkptr(false)}}}, true},
		{"enabled in instance metadata, disabled in project metadata", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{EnableDiagnostics: mkptr(true)}}, Project: metadata.Project{Attributes: metadata.Attributes{EnableDiagnostics: mkptr(false)}}}, false},
		{"disabled in project metadata only", []byte(""), &metadata.Descriptor{Project: metadata.Project{Attributes: metadata.Attributes{EnableDiagnostics: mkptr(false)}}}, true},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reloadConfig(t, tt.data)

			newMetadata = tt.md
			mgr := diagnosticsMgr{
				fakeWindows: true,
			}

			got, err := mgr.Disabled(ctx)
			if err != nil {
				t.Errorf("Failed to run diagnosticsMgr's Disable() call: %+v", err)
			}

			if got != tt.want {
				t.Errorf("test case %q, diagnostics.disabled() got: %t, want: %t", tt.name, got, tt.want)
			}
		})
	}
}
