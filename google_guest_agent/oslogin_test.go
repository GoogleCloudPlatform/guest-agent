// Copyright 2019 Google LLC

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
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/sshtrustedca"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
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
	authorizedKeysCommandSk := "AuthorizedKeysCommand /usr/bin/google_authorized_keys_sk"
	authorizedKeysUser := "AuthorizedKeysCommandUser root"
	authorizedPrincipalsCommand := "AuthorizedPrincipalsCommand /usr/bin/google_authorized_principals %u %k"
	authorizedPrincipalsUser := "AuthorizedPrincipalsCommandUser root"
	trustedUserCAKeys := "TrustedUserCAKeys " + sshtrustedca.DefaultPipePath
	twoFactorAuthMethods := "AuthenticationMethods publickey,keyboard-interactive"
	matchblock1 := `Match User sa_*`
	matchblock2 := `       AuthenticationMethods publickey`

	var tests = []struct {
		contents, want                             []string
		enable, twofactor, skey, reqCerts, cfgCert bool
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
				trustedUserCAKeys,
				authorizedPrincipalsCommand,
				authorizedPrincipalsUser,
				authorizedKeysCommand,
				authorizedKeysUser,
				twoFactorAuthMethods,
				challengeResponseEnable,
				googleBlockEnd,
				"line1",
				googleBlockStart,
				matchblock1,
				matchblock2,
				googleBlockEnd,
			},
			enable:    true,
			twofactor: true,
			skey:      false,
			reqCerts:  false,
			cfgCert:   true,
		},
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
				googleBlockStart,
				matchblock1,
				matchblock2,
				googleBlockEnd,
			},
			enable:    true,
			twofactor: true,
			skey:      false,
			reqCerts:  false,
			cfgCert:   false,
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
				trustedUserCAKeys,
				authorizedPrincipalsCommand,
				authorizedPrincipalsUser,
				authorizedKeysCommand,
				authorizedKeysUser,
				twoFactorAuthMethods,
				challengeResponseEnable,
				googleBlockEnd,
				"line1",
				"line3",
				googleBlockStart,
				matchblock1,
				matchblock2,
				googleBlockEnd,
			},
			enable:    true,
			twofactor: true,
			skey:      false,
			reqCerts:  false,
			cfgCert:   true,
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
				googleBlockStart,
				matchblock1,
				matchblock2,
				googleBlockEnd,
			},
			enable:    true,
			twofactor: true,
			skey:      false,
			reqCerts:  false,
			cfgCert:   false,
		},
		{
			// Block is created without two-factor options.
			contents: []string{
				"line1",
				"line2",
			},
			want: []string{
				googleBlockStart,
				trustedUserCAKeys,
				authorizedPrincipalsCommand,
				authorizedPrincipalsUser,
				authorizedKeysCommand,
				authorizedKeysUser,
				googleBlockEnd,
				"line1",
				"line2",
			},
			enable:    true,
			twofactor: false,
			skey:      false,
			reqCerts:  false,
			cfgCert:   true,
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
			skey:      false,
			reqCerts:  false,
			cfgCert:   false,
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
			skey:      false,
			reqCerts:  true,
			cfgCert:   true,
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
			skey:      false,
			reqCerts:  false,
			cfgCert:   false,
		},
		{
			// Skey binary is chosen instead.
			contents: []string{
				"line1",
				"line2",
				googleBlockStart,
				"line3",
				googleBlockEnd,
			},
			want: []string{
				googleBlockStart,
				trustedUserCAKeys,
				authorizedPrincipalsCommand,
				authorizedPrincipalsUser,
				authorizedKeysCommandSk,
				authorizedKeysUser,
				googleBlockEnd,
				"line1",
				"line2",
			},
			enable:    true,
			twofactor: false,
			skey:      true,
			reqCerts:  false,
			cfgCert:   true,
		},
		{
			// Skey binary is chosen instead.
			contents: []string{
				"line1",
				"line2",
				googleBlockStart,
				"line3",
				googleBlockEnd,
			},
			want: []string{
				googleBlockStart,
				authorizedKeysCommandSk,
				authorizedKeysUser,
				googleBlockEnd,
				"line1",
				"line2",
			},
			enable:    true,
			twofactor: false,
			skey:      true,
			reqCerts:  false,
			cfgCert:   false,
		},
		{
			// Keys are disabled by metadata.
			contents: []string{
				"line1",
				"line2",
				googleBlockStart,
				"line3",
				googleBlockEnd,
			},
			want: []string{
				googleBlockStart,
				trustedUserCAKeys,
				authorizedPrincipalsCommand,
				authorizedPrincipalsUser,
				googleBlockEnd,
				"line1",
				"line2",
			},
			enable:    true,
			twofactor: false,
			skey:      false,
			reqCerts:  true,
			cfgCert:   true,
		},
		{
			// Metadata overrides config.
			contents: []string{
				"line1",
				"line2",
				googleBlockStart,
				"line3",
				googleBlockEnd,
			},
			want: []string{
				googleBlockStart,
				trustedUserCAKeys,
				authorizedPrincipalsCommand,
				authorizedPrincipalsUser,
				googleBlockEnd,
				"line1",
				"line2",
			},
			enable:    true,
			twofactor: false,
			skey:      false,
			reqCerts:  true,
			cfgCert:   false,
		},
	}

	if err := cfg.Load(nil); err != nil {
		t.Fatalf("Failed to initialize configuration manager: %+v", err)
	}

	config := cfg.Get()
	defaultCertAuthConfig := config.OSLogin.CertAuthentication

	for idx, tt := range tests {
		contents := strings.Join(tt.contents, "\n")
		want := strings.Join(tt.want, "\n")
		config.OSLogin.CertAuthentication = tt.cfgCert

		if res := updateSSHConfig(contents, tt.enable, tt.twofactor, tt.skey, tt.reqCerts); res != want {
			t.Errorf("test %v\nwant:\n%v\ngot:\n%v\n", idx, want, res)
		}
	}

	config.OSLogin.CertAuthentication = defaultCertAuthConfig
}

func TestUpdatePAMsshdPamless(t *testing.T) {
	authOSLogin := "auth       [success=done perm_denied=die default=ignore] pam_oslogin_login.so"
	authGroup := "auth       [default=ignore] pam_group.so"
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
		t.Run(fmt.Sprintf("test-%d", idx), func(t *testing.T) {
			contents := strings.Join(tt.contents, "\n")
			want := strings.Join(tt.want, "\n")

			if res := updatePAMsshdPamless(contents, tt.enable, tt.twofactor); res != want {
				t.Errorf("want:\n%v\ngot:\n%v\n", want, res)
			}
		})
	}
}

func TestUpdateGroupConf(t *testing.T) {
	config := "sshd;*;*;Al0000-2400;video"

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
				"line1",
				"line2",
				googleComment,
				config,
			},
			enable: true,
		},
		{
			contents: []string{
				"line1",
				"line2",
			},
			want: []string{
				"line1",
				"line2",
			},
			enable: false,
		},
		{
			contents: []string{
				"line1",
				"line2",
				googleComment,
				"line3", // not the right line
			},
			want: []string{
				"line1",
				"line2",
				googleComment,
				config,
			},
			enable: true,
		},
		{
			contents: []string{
				"line1",
				"line2",
				googleComment,
				"line3",
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

		if res := updateGroupConf(contents, tt.enable); res != want {
			t.Errorf("test %v\nwant:\n%v\ngot:\n%v\n", idx, want, res)
		}
	}
}

func TestGetOSLoginEnabled(t *testing.T) {
	var tests = []struct {
		md                                string
		enable, twofactor, skey, reqCerts bool
	}{
		{
			md:        `{"instance": {"attributes": {"enable-oslogin": "true", "enable-oslogin-2fa": "true"}}}`,
			enable:    true,
			twofactor: true,
			skey:      false,
			reqCerts:  false,
		},
		{
			md:        `{"project": {"attributes": {"enable-oslogin": "true", "enable-oslogin-2fa": "true", "enable-oslogin-certificates": "true"}}}`,
			enable:    true,
			twofactor: true,
			skey:      false,
			reqCerts:  true,
		},
		{
			// Instance keys take precedence
			md:        `{"project": {"attributes": {"enable-oslogin": "false", "enable-oslogin-2fa": "false"}}, "instance": {"attributes": {"enable-oslogin": "true", "enable-oslogin-2fa": "true"}}}`,
			enable:    true,
			twofactor: true,
			skey:      false,
			reqCerts:  false,
		},
		{
			// Instance keys take precedence
			md:        `{"project": {"attributes": {"enable-oslogin": "true", "enable-oslogin-2fa": "true"}}, "instance": {"attributes": {"enable-oslogin": "false", "enable-oslogin-2fa": "false"}}}`,
			enable:    false,
			twofactor: false,
			skey:      false,
			reqCerts:  false,
		},
		{
			// Handle weird values
			md:        `{"instance": {"attributes": {"enable-oslogin": "TRUE", "enable-oslogin-2fa": "foobar"}}}`,
			enable:    true,
			twofactor: false,
			skey:      false,
			reqCerts:  false,
		},
		{
			// Mixed test
			md:        `{"project": {"attributes": {"enable-oslogin": "true", "enable-oslogin-2fa": "true"}}, "instance": {"attributes": {"enable-oslogin-2fa": "false"}}}`,
			enable:    true,
			twofactor: false,
			skey:      false,
			reqCerts:  false,
		},
		{
			// Skey test
			md:        `{"instance": {"attributes": {"enable-oslogin": "true", "enable-oslogin-2fa": "true", "enable-oslogin-sk": "true"}}}`,
			enable:    true,
			twofactor: true,
			skey:      true,
			reqCerts:  false,
		},
		{
			// ReqCerts test
			md:        `{"instance": {"attributes": {"enable-oslogin": "true", "enable-oslogin-2fa": "true", "enable-oslogin-certificates": "true"}}}`,
			enable:    true,
			twofactor: true,
			skey:      false,
			reqCerts:  true,
		},
	}

	for idx, tt := range tests {
		var md metadata.Descriptor
		if err := json.Unmarshal([]byte(tt.md), &md); err != nil {
			t.Errorf("Failed to unmarshal metadata JSON for test %v: %v", idx, err)
		}
		enable, twofactor, skey, reqCerts := getOSLoginEnabled(&md)
		if enable != tt.enable || twofactor != tt.twofactor || skey != tt.skey || reqCerts != tt.reqCerts {
			t.Errorf("Test %v failed. Expected: %v/%v/%v/%v Got: %v/%v/%v/%v", idx, tt.enable, tt.twofactor, tt.skey, tt.reqCerts, enable, twofactor, skey, reqCerts)
		}
	}
}
