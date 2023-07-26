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

package agentcrypto

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/fakes"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/uefi"
)

func TestReadAndWriteRootCACert(t *testing.T) {
	root := t.TempDir()
	v := uefi.VariableName{Name: "testname", GUID: "testguid", RootDir: root}

	fakeUefi := []byte("attr" + validCertPEM)
	path := filepath.Join(root, "testname-testguid")

	if err := os.WriteFile(path, fakeUefi, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	defer os.Remove(path)

	crt := filepath.Join(root, "root.crt")
	if err := readAndWriteRootCACert(v, crt); err != nil {
		t.Errorf("readAndWriteRootCACert(%+v, %s) failed unexpectedly with error: %v", v, crt, err)
	}

	got, err := os.ReadFile(crt)
	if err != nil {
		t.Errorf("Failed to read expected root cert file: %v", err)
	}
	if string(got) != validCertPEM {
		t.Errorf("readAndWriteRootCACert(%+v, %s) = %s, want %s", v, crt, string(got), validCertPEM)
	}
}

func TestReadAndWriteRootCACertError(t *testing.T) {
	root := t.TempDir()
	v := uefi.VariableName{Name: "not", GUID: "exist", RootDir: root}

	crt := filepath.Join(root, "root.crt")
	// Non-existent UEFI variable.
	if err := readAndWriteRootCACert(v, crt); err == nil {
		t.Errorf("readAndWriteRootCACert(%+v, %s) succeeded unexpectedly for non-existent UEFI variable, want error", v, crt)
	}

	// Invalid PEM certificate.
	fakeUefi := []byte("attr" + invalidCertPEM)
	path := filepath.Join(root, "testname-testguid")

	if err := os.WriteFile(path, fakeUefi, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	defer os.Remove(path)

	if err := readAndWriteRootCACert(v, crt); err == nil {
		t.Errorf("readAndWriteRootCACert(%+v, %s) succeeded unexpectedly for invalid PEM certificate, want error", v, crt)
	}
}

func TestGetClientCredentials(t *testing.T) {
	ctx := context.WithValue(context.Background(), fakes.MDSOverride, "succeed")
	client := fakes.NewFakeMDSClient()

	if _, err := getClientCredentials(ctx, client); err != nil {
		t.Errorf("getClientCredentials(ctx, client) failed unexpectedly with error: %v", err)
	}
}

func TestGetClientCredentialsError(t *testing.T) {
	ctx := context.Background()
	client := fakes.NewFakeMDSClient()

	tests := []string{"fail_mds_connect", "fail_unmarshal"}

	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			ctx = context.WithValue(ctx, fakes.MDSOverride, test)
			if _, err := getClientCredentials(ctx, client); err == nil {
				t.Errorf("getClientCredentials(ctx, client) succeeded for %s, want error", test)
			}
		})
	}
}
