// Copyright 2024 Google LLC

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
	"reflect"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
)

// captureRunner records the argv of the last command it was asked to run.
type captureRunner struct {
	name string
	args []string
}

func (c *captureRunner) Quiet(ctx context.Context, name string, args ...string) error {
	c.name = name
	c.args = args
	return nil
}

func (c *captureRunner) WithOutput(ctx context.Context, name string, args ...string) *run.Result {
	c.name = name
	c.args = args
	return &run.Result{}
}

func (c *captureRunner) WithOutputTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) *run.Result {
	c.name = name
	c.args = args
	return &run.Result{}
}

func (c *captureRunner) WithCombinedOutput(ctx context.Context, name string, args ...string) *run.Result {
	c.name = name
	c.args = args
	return &run.Result{}
}

// A forwarded/alias IP value from metadata that smuggles additional
// whitespace-separated tokens.
const injectingIP = "169.254.1.1/32 via 10.0.0.1 table 255"

func TestLocalRouteArgsAreNotSplit(t *testing.T) {
	reloadConfig(t, nil)
	config := cfg.Get()
	proto := config.IPForwarding.EthernetProtoID

	orig := run.Client
	capture := &captureRunner{}
	run.Client = capture
	defer func() { run.Client = orig }()

	tests := []struct {
		name string
		call func() error
		want []string
	}{
		{
			name: "add",
			call: func() error { return addLocalRoute(context.Background(), config, injectingIP, "eth0") },
			want: []string{"route", "add", "to", "local", injectingIP, "scope", "host", "dev", "eth0", "proto", proto},
		},
		{
			name: "remove",
			call: func() error { return removeLocalRoute(context.Background(), config, injectingIP, "eth0") },
			want: []string{"route", "delete", "to", "local", injectingIP, "scope", "host", "dev", "eth0", "proto", proto},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capture.name, capture.args = "", nil
			if err := test.call(); err != nil {
				t.Fatalf("route call returned error: %v", err)
			}
			if capture.name != "ip" {
				t.Errorf("ran %q, want %q", capture.name, "ip")
			}
			if !reflect.DeepEqual(capture.args, test.want) {
				t.Errorf("argv = %q, want %q", capture.args, test.want)
			}
			// The untrusted value must remain a single argv element so it
			// cannot inject extra `ip` subcommand arguments.
			for _, a := range capture.args {
				if a == "via" || a == "table" {
					t.Errorf("injected token %q became a separate argv element: %q", a, capture.args)
				}
			}
		})
	}
}
