//  Copyright 2019 Google Inc. All Rights Reserved.
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
	"strings"
	"testing"
)

func TestFilterGoogleLines(t *testing.T) {
	cmpslice := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for idx := 0; idx < len(a); idx++ {
			if a[idx] != b[idx] {
				return false
			}
		}
		return true
	}

	var tests = []struct {
		contents, want []string
	}{
		{
			[]string{
				"line1",
				"line2",
				googleComment,
				"line3 after google comment",
				"line4",
				googleBlockStart,
				"line5 inside google block",
				"line6 inside google block",
				googleBlockEnd,
				"line7",
			},
			[]string{
				"line1",
				"line2",
				"line4",
				"line7",
			},
		},
		{
			[]string{
				"line1",
				"line2",
				googleBlockEnd,
				"line3",
				"line4",
			},
			[]string{
				"line1",
				"line2",
				"line3",
				"line4",
			},
		},
		{
			[]string{
				googleBlockStart,
				"line1 inside google block",
				"line2 inside google block",
				googleBlockEnd,
				"line3",
			},
			[]string{
				"line3",
			},
		},
		{
			[]string{
				googleBlockStart,
				"line1 inside google block",
				googleBlockStart,
				"line2 inside google block",
				googleBlockEnd,
				"line3",
				googleBlockEnd,
				"line4",
			},
			[]string{
				"line3",
				"line4",
			},
		},
		{
			[]string{
				googleBlockEnd,
				googleBlockStart,
				"line1 inside google block",
				"line2 inside google block",
				googleComment,
				googleBlockEnd,
				"line3",
			},
			[]string{
				"line3",
			},
		},
	}

	for idx, tt := range tests {
		if res := filterGoogleLines(strings.Join(tt.contents, "\n")); !cmpslice(res, tt.want) {
			t.Errorf("test %v\nwant:\n%v\ngot:\n%v\n", idx, tt.want, res)
		}
	}
}

func TestUpdateNSSwitchConfig(t *testing.T) {
	oslogin := " cache_oslogin oslogin"

	var tests = []struct {
		contents, want []string
		enable         bool
	}{
		{
			contents: []string{
				"line1",
				"passwd: line2",
				"group: line3",
			},
			want: []string{
				"line1",
				"passwd: line2" + oslogin,
				"group: line3" + oslogin,
			},
			enable: true,
		},
		{
			contents: []string{
				"line1",
				"passwd: line2" + oslogin,
				"group: line3" + oslogin,
			},
			want: []string{
				"line1",
				"passwd: line2",
				"group: line3",
			},
			enable: false,
		},
		{
			contents: []string{
				"line1",
				"passwd: line2" + oslogin,
				"group: line3" + oslogin,
			},
			want: []string{
				"line1",
				"passwd: line2" + oslogin,
				"group: line3" + oslogin,
			},
			enable: true,
		},
	}

	for idx, tt := range tests {
		contents := strings.Join(tt.contents, "\n")
		want := strings.Join(tt.want, "\n")

		if res := updateNSSwitchConfig(contents, tt.enable); res != want {
			t.Errorf("test %v\nwant:\n%v\ngot:\n%v\n", idx, want, res)
		}
	}
}
func TestUpdateSSHConfig(t *testing.T) {
	challengeResponseEnable := "ChallengeResponseAuthentication yes"
	authorizedKeysCommand := "AuthorizedKeysCommand /usr/bin/google_authorized_keys"
	authorizedKeysUser := "AuthorizedKeysCommandUser root"
	twoFactorAuthMethods := "AuthenticationMethods publickey,keyboard-interactive"

	var tests = []struct {
		contents, want    []string
		enable, twofactor bool
	}{
		{
			// Full block is created, any others removed.
			contents: []string{
				"line1",
				googleBlockStart,
				"line2",
				googleBlockEnd,
			},
			want: []string{
				googleBlockStart,
				authorizedKeysCommand,
				authorizedKeysUser,
				twoFactorAuthMethods,
				challengeResponseEnable,
				googleBlockEnd,
				"line1",
			},
			enable:    true,
			twofactor: true,
		},
		{
			// Full block is created, google comments removed.
			contents: []string{
				"line1",
				googleComment,
				"line2",
				"line3",
			},
			want: []string{
				googleBlockStart,
				authorizedKeysCommand,
				authorizedKeysUser,
				twoFactorAuthMethods,
				challengeResponseEnable,
				googleBlockEnd,
				"line1",
				"line3",
			},
			enable:    true,
			twofactor: true,
		},
		{
			// Block is created without two-factor options.
			contents: []string{
				"line1",
				"line2",
			},
			want: []string{
				googleBlockStart,
				authorizedKeysCommand,
				authorizedKeysUser,
				googleBlockEnd,
				"line1",
				"line2",
			},
			enable:    true,
			twofactor: false,
		},
		{
			// Existing block is removed.
			contents: []string{
				"line1",
				"line2",
				googleBlockStart,
				"line3",
				googleBlockEnd,
			},
			want: []string{
				"line1",
				"line2",
			},
			enable:    false,
			twofactor: true,
		},
	}

	for idx, tt := range tests {
		contents := strings.Join(tt.contents, "\n")
		want := strings.Join(tt.want, "\n")

		if res := updateSSHConfig(contents, tt.enable, tt.twofactor); res != want {
			t.Errorf("test %v\nwant:\n%v\ngot:\n%v\n", idx, want, res)
		}
	}
}

func TestUpdatePAMsshd(t *testing.T) {
	authOSLogin := "auth       [success=done perm_denied=die default=ignore] pam_oslogin_login.so"
	authGroup := "auth       [default=ignore] pam_group.so"
	accountOSLogin := "account    [success=ok ignore=ignore default=die] pam_oslogin_login.so"
	accountOSLoginAdmin := "account    [success=ok default=ignore] pam_oslogin_admin.so"
	sessionHomeDir := "session    [success=ok default=ignore] pam_mkhomedir.so"

	var tests = []struct {
		contents, want    []string
		enable, twofactor bool
	}{
		{
			contents: []string{
				"line1",
				"line2",
			},
			want: []string{
				googleBlockStart,
				authOSLogin,
				authGroup,
				googleBlockEnd,
				"line1",
				"line2",
				googleBlockStart,
				accountOSLogin,
				accountOSLoginAdmin,
				sessionHomeDir,
				googleBlockEnd,
			},
			enable:    true,
			twofactor: true,
		},
		{
			contents: []string{
				"line1",
				"line2",
			},
			want: []string{
				googleBlockStart,
				authGroup,
				googleBlockEnd,
				"line1",
				"line2",
				googleBlockStart,
				accountOSLogin,
				accountOSLoginAdmin,
				sessionHomeDir,
				googleBlockEnd,
			},
			enable:    true,
			twofactor: false,
		},
		{
			contents: []string{
				googleBlockStart,
				"line1",
				googleBlockEnd,
				"line2",
				googleBlockStart,
				"line3",
				googleBlockEnd,
			},
			want: []string{
				"line2",
			},
			enable:    false,
			twofactor: true,
		},
	}

	for idx, tt := range tests {
		contents := strings.Join(tt.contents, "\n")
		want := strings.Join(tt.want, "\n")

		if res := updatePAMsshd(contents, tt.enable, tt.twofactor); res != want {
			t.Errorf("test %v\nwant:\n%v\ngot:\n%v\n", idx, want, res)
		}
	}
}
func TestUpdatePAMsu(t *testing.T) {
	accountSu := "account    [success=bad ignore=ignore] pam_oslogin_login.so"

	var tests = []struct {
		contents, want []string
		enable         bool
	}{
		{
			contents: []string{
				"line1",
				"line2",
			},
			want: []string{
				googleComment,
				accountSu,
				"line1",
				"line2",
			},
			enable: true,
		},
		{
			contents: []string{
				"line1",
				googleComment,
				accountSu,
				"line2",
			},
			want: []string{
				googleComment,
				accountSu,
				"line1",
				"line2",
			},
			enable: true,
		},
		{
			contents: []string{
				"line1",
				googleComment,
				accountSu,
				"line2",
			},
			want: []string{
				"line1",
				"line2",
			},
			enable: false,
		},
	}

	for idx, tt := range tests {
		contents := strings.Join(tt.contents, "\n")
		want := strings.Join(tt.want, "\n")

		if res := updatePAMsu(contents, tt.enable); res != want {
			t.Errorf("test %v\nwant:\n%v\ngot:\n%v\n", idx, want, res)
		}
	}
}
