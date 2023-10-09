// Copyright 2022 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestContainsString(t *testing.T) {
	table := []struct {
		a     string
		slice []string
		want  bool
	}{
		{"a", []string{"a", "b"}, true},
		{"c", []string{"a", "b"}, false},
	}
	for _, tt := range table {
		if got, want := ContainsString(tt.a, tt.slice), tt.want; got != want {
			t.Errorf("containsString(%s, %v) incorrect return: got %v, want %t", tt.a, tt.slice, got, want)
		}
	}
}

func TestGetUserKey(t *testing.T) {
	pubKey := MakeRandRSAPubKey(t)

	table := []struct {
		key    string
		user   string
		keyVal string
		haserr bool
	}{
		{fmt.Sprintf(`usera:ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, pubKey),
			"usera", fmt.Sprintf(`ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, pubKey), false},
		{fmt.Sprintf(`usera:restrict,pty ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, pubKey),
			"usera", fmt.Sprintf(`restrict,pty ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, pubKey), false},
		{"    ", "", "", true},
		{fmt.Sprintf("ssh-rsa %s", pubKey), "", "", true},
		{fmt.Sprintf(":ssh-rsa %s", pubKey), "", "", true},
		{"userb:", "", "", true},
		{fmt.Sprintf("userc:ssh-rsa %s info text", pubKey), "userc", fmt.Sprintf("ssh-rsa %s info text", pubKey), false},
	}

	for _, tt := range table {
		u, k, err := GetUserKey(tt.key)
		e := err != nil
		if u != tt.user || k != tt.keyVal || e != tt.haserr {
			t.Errorf("GetUserKey(%s) incorrect return: got user: %s, key: %s, error: %v - want user: %s, key: %s, error: %v", tt.key, u, k, e, tt.user, tt.keyVal, tt.haserr)
		}
	}
}

func TestValidateUserKey(t *testing.T) {
	pubKey := MakeRandRSAPubKey(t)

	table := []struct {
		user   string
		key    string
		haserr bool
	}{
		{"usera", fmt.Sprintf(`ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, pubKey), false},
		{"user a", fmt.Sprintf(`ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, pubKey), true},
		{"usera", fmt.Sprintf(`ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2021-04-23T12:34:56+0000"}`, pubKey), true},
		{"usera", fmt.Sprintf(`ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"Apri 4, 2056"}`, pubKey), true},
		{"usera", fmt.Sprintf(`ssh-rsa %s google-ssh`, pubKey), true},
		{"usera", fmt.Sprintf(`ssh-rsa %s test info`, pubKey), false},
		{"", fmt.Sprintf("ssh-rsa %s", pubKey), true},
		{"usera", fmt.Sprintf(`command="echo hi" ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, pubKey), false},
		{"usera", fmt.Sprintf(`command="echo hi" ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2021-04-23T12:34:56+0000"}`, pubKey), true},
		{"usera", fmt.Sprintf(`restrict,pty ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, pubKey), false},
		{"    ", "", true},
		{"userb", "", true},
	}

	for _, tt := range table {
		err := ValidateUserKey(tt.user, tt.key)
		e := err != nil
		if e != tt.haserr {
			t.Errorf("ValidateUserKey(%s, %s) incorrect return: expected: %t - got: %t", tt.user, tt.key, tt.haserr, e)
		}
	}
}

func TestCheckExpiredKey(t *testing.T) {
	pubKey := MakeRandRSAPubKey(t)

	table := []struct {
		key     string
		expired bool
	}{
		{fmt.Sprintf(`usera:ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, pubKey), false},
		{fmt.Sprintf(`usera:ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"2021-04-23T12:34:56+0000"}`, pubKey), true},
		{fmt.Sprintf(`usera:ssh-rsa %s google-ssh {"userName":"usera@example.com","expireOn":"Apri 4, 2056"}`, pubKey), true},
		{fmt.Sprintf(`usera:ssh-rsa %s google-ssh`, pubKey), true},
		{"    ", true},
		{fmt.Sprintf("ssh-rsa %s", pubKey), false},
		{fmt.Sprintf(":ssh-rsa %s", pubKey), false},
		{fmt.Sprintf("usera:ssh-rsa %s", pubKey), false},
	}

	for _, tt := range table {
		err := CheckExpiredKey(tt.key)
		isExpired := err != nil
		if isExpired != tt.expired {
			t.Errorf("CheckExpiredKey(%s) incorrect return: expired: %t - want expired: %t, got err: %v", tt.key, isExpired, tt.expired, err)
		}
	}
}

func TestValidateUser(t *testing.T) {
	table := []struct {
		user  string
		valid bool
	}{
		{"username", true},
		{"username:key", true},
		{"user -g", false},
		{"user -g 27", false},
		{"user\t-g", false},
		{"user\n-g", false},
		{"username\t-g\n27", false},
	}
	for _, tt := range table {
		err := ValidateUser(tt.user)
		isValid := err == nil
		if isValid != tt.valid {
			t.Errorf("ValidateUser(%s) incorrect return: expected: %t - got: %t", tt.user, tt.valid, isValid)
		}
	}
}

func TestSaferWriteFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	want := "test-data"

	if err := SaferWriteFile([]byte(want), f, 0644); err != nil {
		t.Errorf("SaferWriteFile(%s, %s) failed unexpectedly with err: %+v", "test-data", f, err)
	}

	got, err := os.ReadFile(f)
	if err != nil {
		t.Errorf("os.ReadFile(%s) failed unexpectedly with err: %+v", f, err)
	}
	if string(got) != want {
		t.Errorf("os.ReadFile(%s) = %s, want %s", f, string(got), want)
	}

	i, err := os.Stat(f)
	if err != nil {
		t.Errorf("os.Stat(%s) failed unexpectedly with err: %+v", f, err)
	}

	if i.Mode().Perm() != 0o644 {
		t.Errorf("SaferWriteFile(%s) set incorrect permissions, os.Stat(%s) = %o, want %o", f, f, i.Mode().Perm(), 0o644)
	}
}

func TestCopyFile(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "dst")
	src := filepath.Join(tmp, "src")
	want := "testdata"
	if err := os.WriteFile(src, []byte(want), 0777); err != nil {
		t.Fatalf("failed to write test source file: %v", err)
	}
	if err := CopyFile(src, dst, 0644); err != nil {
		t.Errorf("CopyFile(%s, %s) failed unexpectedly with error: %v", src, dst, err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Errorf("unable to read %q: %v", dst, err)
	}
	if string(got) != want {
		t.Errorf("CopyFile(%s, %s) copied %q, expected %q", src, dst, string(got), want)
	}

	i, err := os.Stat(dst)
	if err != nil {
		t.Errorf("os.Stat(%s) failed unexpectedly with err: %+v", dst, err)
	}

	if i.Mode().Perm() != 0o644 {
		t.Errorf("SaferWriteFile(%s) set incorrect permissions, os.Stat(%s) = %o, want %o", dst, dst, i.Mode().Perm(), 0o644)
	}
}

func TestCopyFileError(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "dst")
	src := filepath.Join(tmp, "src")

	if err := CopyFile(src, dst, 0644); err == nil {
		t.Errorf("CopyFile(%s, %s) succeeded for non-existent file, want error", src, dst)
	}
}
