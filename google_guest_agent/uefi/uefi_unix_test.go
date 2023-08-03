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

//go:build unix

package uefi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVariablePath(t *testing.T) {
	v := VariableName{Name: "name", GUID: "guid"}
	want := "/sys/firmware/efi/efivars/name-guid"

	if got := v.Path(); got != want {
		t.Errorf("VariablePath(%+v) = %v, want %v", v, got, want)
	}
}

func TestReadVariable(t *testing.T) {
	root := t.TempDir()
	v := VariableName{Name: "testname", GUID: "testguid", RootDir: root}
	fakecert := `
	-----BEGIN CERTIFICATE-----
	sdfsd
	-----END CERTIFICATE-----
	`
	fakeUefi := []byte("attr" + fakecert)
	path := filepath.Join(root, "testname-testguid")

	if err := os.WriteFile(path, fakeUefi, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	defer os.Remove(path)

	got, err := ReadVariable(v)

	if err != nil {
		t.Errorf("ReadVariable(%+v) failed unexpectedly with error: %v", v, err)
	}

	if string(got.Attributes) != "attr" {
		t.Errorf("ReadVariable(%+v) = %s as attributes, want %s", v, string(got.Attributes), "attr")
	}
	if string(got.Content) != fakecert {
		t.Errorf("ReadVariable(%+v) = %s as content, want %s", v, string(got.Content), fakeUefi)
	}
}

func TestReadVariableError(t *testing.T) {
	root := t.TempDir()
	v := VariableName{Name: "testname", GUID: "testguid", RootDir: root}
	p := filepath.Join(root, "testname-testguid")

	// File not exist error.
	_, err := ReadVariable(v)
	if err == nil {
		t.Errorf("ReadVariable(%+v) succeeded for non-existent file, want error", v)
	}

	// Empty variable error.
	os.WriteFile(p, []byte(""), 0644)

	_, err = ReadVariable(v)
	if err == nil {
		t.Errorf("ReadVariable(%+v) succeeded for invalid format, want error", v)
	}
}
