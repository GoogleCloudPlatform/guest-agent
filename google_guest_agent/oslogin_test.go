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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/sshtrustedca"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/osinfo"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/google/go-cmp/cmp"
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
		// The additional "\n" at the end is because Unix text files are expected to end in a newline
		if res := filterGoogleLines(strings.Join(tt.contents, "\n") + "\n"); !cmpslice(res, tt.want) {
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
		{
			contents: []string{
				"line1",
				"passwd: line2" + oslogin + " some_other_service",
				"group: line3" + oslogin + " another_service",
			},
			want: []string{
				"line1",
				"passwd: line2 some_other_service",
				"group: line3 another_service",
			},
			enable: false,
		},
	}

	for idx, tt := range tests {
		contents := strings.Join(tt.contents, "\n") + "\n"
		want := strings.Join(tt.want, "\n") + "\n"

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
	includePerUserConfigs := "Include /var/google-users.d/*"
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
				includePerUserConfigs,
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
				includePerUserConfigs,
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
				includePerUserConfigs,
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
				includePerUserConfigs,
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
				googleBlockStart,
				includePerUserConfigs,
				googleBlockEnd,
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
				googleBlockStart,
				includePerUserConfigs,
				googleBlockEnd,
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
				authorizedKeysCommandSk,
				authorizedKeysUser,
				googleBlockEnd,
				"line1",
				"line2",
				googleBlockStart,
				includePerUserConfigs,
				googleBlockEnd,
			},
			enable:    true,
			twofactor: false,
			skey:      true,
			reqCerts:  false,
			cfgCert:   false,
		},
		{
			// Skey enablement disables certificates.
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
				googleBlockStart,
				includePerUserConfigs,
				googleBlockEnd,
			},
			enable:    true,
			twofactor: false,
			skey:      true,
			reqCerts:  true,
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
				googleBlockStart,
				includePerUserConfigs,
				googleBlockEnd,
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
				googleBlockStart,
				includePerUserConfigs,
				googleBlockEnd,
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
				googleBlockStart,
				includePerUserConfigs,
				googleBlockEnd,
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
		contents := strings.Join(tt.contents, "\n") + "\n"
		want := strings.Join(tt.want, "\n") + "\n"
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
			contents := strings.Join(tt.contents, "\n") + "\n"
			want := strings.Join(tt.want, "\n") + "\n"

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
		contents := strings.Join(tt.contents, "\n") + "\n"
		want := strings.Join(tt.want, "\n") + "\n"

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

func TestSetupUsrEtcOSLoginDirs(t *testing.T) {
	oldSles16Map := sles16Map
	oldOsinfoRead := osInfo
	t.Cleanup(func() {
		sles16Map = oldSles16Map
		osInfo = oldOsinfoRead
	})

	wantMap := map[string]string{
		"/usr/etc/ssh/sshd_config":     "/etc/ssh/sshd_config",
		"/usr/etc/nsswitch.conf":       "/etc/nsswitch.conf",
		"/usr/lib/pam.d/sshd":          "/etc/pam.d/sshd",
		"/usr/etc/security/group.conf": "/etc/security/group.conf",
	}
	if diff := cmp.Diff(wantMap, sles16Map); diff != "" {
		t.Fatalf("sles16Map unexpected diff (-want +got):\n%s", diff)
	}

	tests := []struct {
		name           string
		info           osinfo.OSInfo
		createSrc      bool
		createDst      bool
		dstContent     string
		dstShouldExist bool
		prevSetup      bool
	}{
		{
			name:           "debian12-no-copy",
			info:           osinfo.OSInfo{OS: "debian", Version: osinfo.Ver{Major: 12}},
			createSrc:      true,
			createDst:      false,
			dstShouldExist: false,
		},
		{
			name:           "sles15-no-copy",
			info:           osinfo.OSInfo{OS: "sles", Version: osinfo.Ver{Major: 15}},
			createSrc:      true,
			createDst:      false,
			dstShouldExist: false,
		},
		{
			name:           "opensuse15-no-copy",
			info:           osinfo.OSInfo{OS: "opensuse", Version: osinfo.Ver{Major: 15}},
			createSrc:      true,
			createDst:      false,
			dstShouldExist: false,
		},
		{
			name:           "sles16-copy",
			info:           osinfo.OSInfo{OS: "sles", Version: osinfo.Ver{Major: 16}},
			createSrc:      true,
			createDst:      false,
			dstShouldExist: true,
			dstContent:     "test",
		},
		{
			name:           "opensuse16-copy",
			info:           osinfo.OSInfo{OS: "opensuse", Version: osinfo.Ver{Major: 16}},
			createSrc:      true,
			createDst:      false,
			dstShouldExist: true,
			dstContent:     "test",
		},
		{
			name:           "sles16-no-copy-if-exists",
			info:           osinfo.OSInfo{OS: "sles", Version: osinfo.Ver{Major: 16}},
			createSrc:      false,
			createDst:      true,
			dstContent:     "exists",
			dstShouldExist: true,
		},
		{
			name:           "sles16-no-copy-if-already-setup",
			info:           osinfo.OSInfo{OS: "sles", Version: osinfo.Ver{Major: 16}},
			createSrc:      false,
			createDst:      true,
			dstContent:     "exists",
			dstShouldExist: true,
			prevSetup:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usrDir := t.TempDir()
			etcDir := t.TempDir()
			src := filepath.Join(usrDir, "nsswitch.conf")
			dst := filepath.Join(etcDir, "nsswitch.conf")
			sles16Map = map[string]string{
				src: dst,
			}

			if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
				t.Fatalf("Failed to create dir for %s: %v", src, err)
			}
			if tt.createSrc {
				if err := os.WriteFile(src, []byte("test"), 0644); err != nil {
					t.Fatalf("Failed to write to %s: %v", src, err)
				}
			}

			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				t.Fatalf("Failed to create dir for %s: %v", dst, err)
			}
			if tt.createDst {
				if err := os.WriteFile(dst, []byte(tt.dstContent), 0644); err != nil {
					t.Fatalf("Failed to write to %s: %v", dst, err)
				}
			}

			osInfo = tt.info

			if err := setupSles16OSLoginDirs(); err != nil {
				t.Fatalf("setupSles16OSLoginDirs() returned err: %v, want nil", err)
			}

			if got := utils.FileExists(dst, utils.TypeFile); got != tt.dstShouldExist {
				t.Errorf("Destination file %s exists: %t, want: %t", dst, got, tt.dstShouldExist)
			}
			if tt.dstShouldExist {
				got, err := os.ReadFile(dst)
				if err != nil {
					t.Fatalf("Failed to read destination file %s: %v", dst, err)
				}
				if string(got) != tt.dstContent {
					t.Errorf("Destination file %s content changed to %s, want %s", dst, string(got), tt.dstContent)
				}
			}
		})
	}
}
