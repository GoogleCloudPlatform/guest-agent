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

package utils

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
		if got, want := ContainsString(tt.a, tt.slice), tt.want; got != want {
			t.Errorf("containsString(%s, %v) incorrect return: got %v, want %t", tt.a, tt.slice, got, want)
		}
	}
}

func TestGetUserKey(t *testing.T) {
	table := []struct {
		key    string
		user   string
		keyVal string
		haserr bool
	}{
		{`usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`,
			"usera", `ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, false},
		{`usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2021-04-23T12:34:56+0000"}`, "", "", true},
		{`usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"Apri 4, 2056"}`, "", "", true},
		{`usera:ssh-rsa AAAA1234 google-ssh`, "", "", true},
		{"    ", "", "", true},
		{"ssh-rsa AAAA1234", "", "", true},
		{":ssh-rsa AAAA1234", "", "", true},
	}

	for _, tt := range table {
		u, k, err := GetUserKey(tt.key)
		e := err != nil
		if u != tt.user || k != tt.keyVal || e != tt.haserr {
			t.Errorf("GetUserKey(%s) incorrect return: got user: %s, key: %s, error: %v - want user %s, key: %s, error: %v", tt.key, u, k, e, tt.user, tt.keyVal, tt.haserr)
		}
	}
}

func TestCheckExpiredKey(t *testing.T) {
	table := []struct {
		key     string
		expired bool
	}{
		{`usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`, false},
		{`usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2021-04-23T12:34:56+0000"}`, true},
		{`usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"Apri 4, 2056"}`, true},
		{`usera:ssh-rsa AAAA1234 google-ssh`, true},
		{"    ", true},
		{"ssh-rsa AAAA1234", false},
		{":ssh-rsa AAAA1234", false},
		{"usera:ssh-rsa AAAA1234", false},
	}

	for _, tt := range table {
		err := CheckExpiredKey(tt.key)
		isExpired := err != nil
		if isExpired != tt.expired {
			t.Errorf("CheckExpiredKey(%s) incorrect return: expired: %t - want expired: %t", tt.key, isExpired, tt.expired)
		}
	}
}

func TestValidateUserKey(t *testing.T) {
	table := []struct {
		user  string
		valid bool
	}{
		{"username", true},
		{"usernamekey", true},
		{"user-g", true},
		{"user g 27", false},
		{"_underscore_user", true},
		{"user_ending_in_dollar$", true},
		{"user\t-g", false},
		{"user\n-g", false},
		{"username\t-g\n27", false},
		{"badusername.23", false},
		{"badusername/25\\", false},
		{"baduser../#$@", false},
		
	}
	for _, tt := range table {
		err := ValidateUserKey(tt.user)
		isValid := err == nil
		if isValid != tt.valid {
			t.Errorf("Invalid ValidateUserKey(%s) return: expected: %t - got: %t", tt.user, isValid, tt.valid)
		}
	}
}
