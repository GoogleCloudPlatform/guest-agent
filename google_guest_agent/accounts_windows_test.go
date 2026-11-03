//go:build windows

// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"errors"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"golang.org/x/sys/windows"
)

const (
	// verNTWorkstation and verNTServer are the non-DC Windows
	// RTL_OSVERSIONINFOEXW wProductType values.
	// https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/wdm/ns-wdm-_osversioninfoexw
	verNTWorkstation = 0x1
	verNTServer      = 0x3

	allowOnDomainControllerConfig = "[Accounts]\nallow_on_domain_controller = true\n"
)

func TestCheckDomainController(t *testing.T) {
	tests := []struct {
		name        string
		productType byte
		config      *cfg.Sections
		wantErr     error
	}{
		{
			name:        "workstation",
			productType: verNTWorkstation,
			config:      &cfg.Sections{Accounts: &cfg.Accounts{}},
		},
		{
			name:        "member_or_standalone_server",
			productType: verNTServer,
			config:      &cfg.Sections{Accounts: &cfg.Accounts{}},
		},
		{
			name:        "domain_controller_default_refused",
			productType: verNTDomainController,
			config:      &cfg.Sections{Accounts: &cfg.Accounts{}},
			wantErr:     ErrDomainController,
		},
		{
			name:        "domain_controller_opt_in_allowed",
			productType: verNTDomainController,
			config:      &cfg.Sections{Accounts: &cfg.Accounts{AllowOnDomainController: true}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			version := &windows.OsVersionInfoEx{ProductType: test.productType}
			if err := checkDomainController(version, test.config); !errors.Is(err, test.wantErr) {
				t.Errorf("checkDomainController(%+v, %+v) = %v, want %v", version, test.config, err, test.wantErr)
			}
		})
	}
}

func TestCheckDomainControllerConfigFileOptIn(t *testing.T) {
	t.Cleanup(func() {
		if err := cfg.Load(nil); err != nil {
			t.Errorf("cfg.Load(nil) cleanup failed: %v", err)
		}
	})
	dcVersion := &windows.OsVersionInfoEx{ProductType: verNTDomainController}
	if err := cfg.Load(nil); err != nil {
		t.Fatalf("cfg.Load(nil) failed: %v", err)
	}
	if err := checkDomainController(dcVersion, cfg.Get()); !errors.Is(err, ErrDomainController) {
		t.Errorf("checkDomainController() with default config = %v, want %v", err, ErrDomainController)
	}
	if err := cfg.Load([]byte(allowOnDomainControllerConfig)); err != nil {
		t.Fatalf("cfg.Load(%q) failed: %v", allowOnDomainControllerConfig, err)
	}
	if err := checkDomainController(dcVersion, cfg.Get()); err != nil {
		t.Errorf("checkDomainController() with opt-in config = %v, want nil", err)
	}
}
