//  Copyright 2022 Google Inc. All Rights Reserved.
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
		if got, want := containsString(tt.a, tt.slice), tt.want; got != want {
			t.Errorf("containsString(%s, %v) incorrect return: got %v, want %t", tt.a, tt.slice, got, want)
		}
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func TestValidateKey(t *testing.T) {
	table := []struct {
		key   string
		val_key []string
	}{
		{`usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`,
		[]string{"usera", `ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`}},
		{`usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2021-04-23T12:34:56+0000"}`, nil},
		{`usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"Apri 4, 2056"}`, nil},
		{`usera:ssh-rsa AAAA1234 google-ssh`, nil},
		{"    ", nil},
		{"ssh-rsa AAAA1234", nil},
		{":ssh-rsa AAAA1234", nil},
	}

	for _, tt := range table {
		if got, want := validateKey(tt.key), tt.val_key; !stringSliceEqual(got, want) {
			t.Errorf("ValidateKey(%s) incorrect return: got %v, want %v", tt.key, got, want)
		}
	}
}

func TestParseSshKeys(t *testing.T) {
	raw_keys := `
# Here is some random data in the file.
usera:ssh-rsa AAAA1234USERA
userb:ssh-rsa AAAA1234USERB
usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}
usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2020-04-23T12:34:56+0000"}

`
    expected := []string{
		"ssh-rsa AAAA1234USERA",
		`ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`,
	}

	user := "usera"

    if got, want := parseSshKeys(user, raw_keys), expected; !stringSliceEqual(got, want) {
		t.Errorf("ParseSshKeys(%s,%s) incorrect return: got %v, want %v", user, raw_keys, got, want)
	}


}

