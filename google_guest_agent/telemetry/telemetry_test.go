//  Copyright 2023 Google Inc. All Rights Reserved.
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

package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecordTelemetry(t *testing.T) {
	var got http.Header
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header
		w.WriteHeader(http.StatusOK)
	})

	testsrv := httptest.NewServer(handler)
	defer testsrv.Close()

	old := metadataURL
	metadataURL = testsrv.URL
	defer func() { metadataURL = old }()

	d := Data{
		AgentVersion:  "AgentVersion",
		AgentArch:     "AgentArch",
		OS:            "OS",
		LongName:      "LongName",
		ShortName:     "ShortName",
		Version:       "Version",
		KernelRelease: "KernelRelease",
		KernelVersion: "KernelVersion",
	}

	if err := Record(context.Background(), d); err != nil {
		t.Fatalf("Error running Record: %v", err)
	}

	want := map[string]string{
		"Metadata-Flavor":      "Google",
		"X-Google-Guest-Agent": formatGuestAgent(d),
		"X-Google-Guest-OS":    formatGuestOS(d),
	}

	for k, v := range want {
		if got.Get(k) != v {
			t.Fatalf("received headers does not contain all expected headers, want: %q, got: %q", want, got)
		}
	}

}
