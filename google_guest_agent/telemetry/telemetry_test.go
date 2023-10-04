// Copyright 2023 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package telemetry

import (
	"context"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/fakes"
)

type mdsClient struct {
	getKeyHeaders map[string]string
	fakes.MDSClient
}

func (c *mdsClient) GetKey(ctx context.Context, key string, headers map[string]string) (string, error) {
	c.getKeyHeaders = headers

	return "", nil
}

func TestRecord(t *testing.T) {
	client := &mdsClient{}

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

	if err := Record(context.Background(), client, d); err != nil {
		t.Fatalf("Error running Record: %v", err)
	}

	want := map[string]string{
		"X-Google-Guest-Agent": formatGuestAgent(d),
		"X-Google-Guest-OS":    formatGuestOS(d),
	}

	got := client.getKeyHeaders
	for k, v := range want {
		if got[k] != v {
			t.Errorf("received headers does not contain all expected headers, want: %q, got: %q", want, got)
		}
	}

}
