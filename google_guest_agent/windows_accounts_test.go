// Copyright 2017 Google LLC

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
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"hash"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
)

func mkptr(b bool) *bool {
	ret := b
	return &ret
}

func TestExpired(t *testing.T) {
	var tests = []struct {
		sTime string
		e     bool
	}{
		{time.Now().Add(5 * time.Minute).Format(time.RFC3339), false},
		{time.Now().Add(-5 * time.Minute).Format(time.RFC3339), true},
		{"some bad time", true},
	}

	for _, tt := range tests {
		k := metadata.WindowsKey{ExpireOn: tt.sTime}

		expired, _ := utils.CheckExpired(k.ExpireOn)
		if tt.e != expired {
			t.Errorf("windowsKey.expired() with ExpiredOn %q should return %t", k.ExpireOn, tt.e)
		}
	}
}

func TestAccountsDisabled(t *testing.T) {
	var tests = []struct {
		name string
		data []byte
		md   *metadata.Descriptor
		want bool
	}{
		{"not explicitly disabled", []byte(""), &metadata.Descriptor{}, false},
		{"enabled in cfg only", []byte("[accountManager]\ndisable=false"), &metadata.Descriptor{}, false},
		{"disabled in cfg only", []byte("[accountManager]\ndisable=true"), &metadata.Descriptor{}, true},
		{"disabled in cfg, enabled in instance metadata", []byte("[accountManager]\ndisable=true"), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{DisableAccountManager: mkptr(false)}}}, true},
		{"enabled in cfg, disabled in instance metadata", []byte("[accountManager]\ndisable=false"), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{DisableAccountManager: mkptr(true)}}}, false},
		{"enabled in instance metadata only", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{DisableAccountManager: mkptr(false)}}}, false},
		{"enabled in project metadata only", []byte(""), &metadata.Descriptor{Project: metadata.Project{Attributes: metadata.Attributes{DisableAccountManager: mkptr(false)}}}, false},
		{"disabled in instance metadata only", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{DisableAccountManager: mkptr(true)}}}, true},
		{"enabled in instance metadata, disabled in project metadata", []byte(""), &metadata.Descriptor{Instance: metadata.Instance{Attributes: metadata.Attributes{DisableAccountManager: mkptr(false)}}, Project: metadata.Project{Attributes: metadata.Attributes{DisableAccountManager: mkptr(true)}}}, false},
		{"disabled in project metadata only", []byte(""), &metadata.Descriptor{Project: metadata.Project{Attributes: metadata.Attributes{DisableAccountManager: mkptr(true)}}}, true},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reloadConfig(t, tt.data)

			newMetadata = tt.md
			mgr := &winAccountsMgr{
				fakeWindows: true,
			}

			got, err := mgr.Disabled(ctx)
			if err != nil {
				t.Errorf("Failed to run winAccountsMgr's Disabled() call: %+v", err)
			}

			if got != tt.want {
				t.Errorf("accounts.disabled() got: %t, want: %t", got, tt.want)
			}
		})
	}

	reloadConfig(t, nil)

	got, err := (&winAccountsMgr{}).Disabled(ctx)
	if err != nil {
		t.Errorf("Failed to run winAccountsMgr's Disabled() call: %+v", err)
	}

	if got != true {
		t.Errorf("winAccountsMgr.disabled(\"linux\") got: %t, want: true", got)
	}
}

// Test takes ~43 sec to complete and is resource intensive.
func TestNewPwd(t *testing.T) {
	minPasswordLength := 15
	maxPasswordLength := 255
	var tests = []struct {
		name               string
		passwordLength     int
		wantPasswordLength int
	}{
		{"0 characters, default value", 0, minPasswordLength},
		{"5 characters, below min", 5, minPasswordLength},
		{"15 characters", 5, minPasswordLength},
		{"30 characters", 30, 30},
		{"127 characters", 127, 127},
		{"254 characters", 254, 254},
		{"256 characters", 256, maxPasswordLength},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 100000; i++ {
				pwd, err := newPwd(tt.passwordLength)
				if err != nil {
					t.Fatal(err)
				}
				if len(pwd) != tt.wantPasswordLength {
					t.Errorf("Password is not %d characters: len(%s)=%d", tt.wantPasswordLength, pwd, len(pwd))
				}
				var l, u, n, s int
				for _, r := range pwd {
					switch {
					case unicode.IsLower(r):
						l = 1
					case unicode.IsUpper(r):
						u = 1
					case unicode.IsDigit(r):
						n = 1
					case unicode.IsPunct(r) || unicode.IsSymbol(r):
						s = 1
					}
				}
				if l+u+n+s < 3 {
					t.Errorf("Password does not have at least one character from 3 categories: '%v'", pwd)
				}
			}
		})
	}
}

func TestCreatecredsJSON(t *testing.T) {
	pwd := "password"
	prv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("error generating key: %v", err)
	}
	k := metadata.WindowsKey{
		Email:    "email",
		ExpireOn: "expire",
		Exponent: base64.StdEncoding.EncodeToString(new(big.Int).SetInt64(int64(prv.PublicKey.E)).Bytes()),
		Modulus:  base64.StdEncoding.EncodeToString(prv.PublicKey.N.Bytes()),
		UserName: "username",
	}
	for name, hashFunc := range map[string]hash.Hash{"": sha1.New(), "sha1": sha1.New(), "sha256": sha256.New(), "sha512": sha512.New()} {
		k.HashFunction = name
		c, err := createcredsJSON(k, pwd)
		if err != nil {
			t.Fatalf("error running createcredsJSON: %v", err)
		}
		if k.HashFunction == "" {
			k.HashFunction = "sha1"
		}

		bPwd, err := base64.StdEncoding.DecodeString(c.EncryptedPassword)
		if err != nil {
			t.Fatalf("error base64 decoding encoded pwd: %v", err)
		}
		decPwd, err := rsa.DecryptOAEP(hashFunc, rand.Reader, prv, bPwd, nil)
		if err != nil {
			t.Fatalf("error decrypting password: %v", err)
		}
		if pwd != string(decPwd) {
			t.Errorf("decrypted password does not match expected for hash func %q, got: %s, want: %s", name, string(decPwd), pwd)
		}
		if k.UserName != c.UserName {
			t.Errorf("returned credsJSON UserName field unexpected, got: %s, want: %s", c.UserName, k.UserName)
		}
		if k.HashFunction != c.HashFunction {
			t.Errorf("returned credsJSON HashFunction field unexpected, got: %s, want: %s", c.HashFunction, k.HashFunction)
		}
		if !c.PasswordFound {
			t.Error("returned credsJSON PasswordFound field is not true")
		}
	}
}

func TestCompareAccounts(t *testing.T) {
	var tests = []struct {
		newKeys    metadata.WindowsKeys
		oldStrKeys []string
		wantAdd    metadata.WindowsKeys
	}{
		// These should return toAdd:
		// In MD, not Reg
		{metadata.WindowsKeys{{UserName: "foo"}}, nil, metadata.WindowsKeys{{UserName: "foo"}}},
		{metadata.WindowsKeys{{UserName: "foo"}}, []string{`{"UserName":"bar"}`}, metadata.WindowsKeys{{UserName: "foo"}}},

		// These should return nothing:
		// In Reg and MD
		{metadata.WindowsKeys{{UserName: "foo"}}, []string{`{"UserName":"foo"}`}, nil},
		// In Reg, not MD
		{nil, []string{`{UserName":"foo"}`}, nil},
	}

	for _, tt := range tests {
		toAdd := compareAccounts(tt.newKeys, tt.oldStrKeys)
		if !reflect.DeepEqual(tt.wantAdd, toAdd) {
			t.Errorf("toAdd does not match expected: newKeys: %v, oldStrKeys: %q, got: %v, want: %v", tt.newKeys, tt.oldStrKeys, toAdd, tt.wantAdd)
		}
	}
}

func TestGetUserKeys(t *testing.T) {
	pubKey := utils.MakeRandRSAPubKey(t)

	var tests = []struct {
		key           string
		expectedValid int
	}{
		{fmt.Sprintf(`user:ssh-rsa %s google-ssh {"userName":"user@email.com", "expireOn":"2028-11-08T19:30:47+0000"}`, pubKey),
			1,
		},
		{fmt.Sprintf(`user:ssh-rsa %s google-ssh {"userName":"user@email.com", "expireOn":"2028-11-08T19:30:47+0700"}`, pubKey),
			1,
		},
		{fmt.Sprintf(`user:ssh-rsa %s google-ssh {"userName":"user@email.com", "expireOn":"2028-11-08T19:30:47+0700", "futureField": "UNUSED_FIELDS_IGNORED"}`, pubKey),
			1,
		},
		{fmt.Sprintf(`user:ssh-rsa %s google-ssh {"userName":"user@email.com", "expireOn":"2018-11-08T19:30:46+0000"}`, pubKey),
			0,
		},
		{fmt.Sprintf(`user:ssh-rsa %s google-ssh {"userName":"user@email.com", "expireOn":"2018-11-08T19:30:46+0700"}`, pubKey),
			0,
		},
		{fmt.Sprintf(`user:ssh-rsa %s google-ssh {"userName":"user@email.com", "expireOn":"INVALID_TIMESTAMP"}`, pubKey),
			0,
		},
		{fmt.Sprintf(`user:ssh-rsa %s google-ssh`, pubKey),
			0,
		},
		{fmt.Sprintf(`user:ssh-rsa %s user`, pubKey),
			1,
		},
		{fmt.Sprintf(`user:ssh-rsa %s`, pubKey),
			1,
		},
		{fmt.Sprintf(`malformed-ssh-keys %s google-ssh`, pubKey),
			0,
		},
		{fmt.Sprintf(`:malformed-ssh-keys %s google-ssh`, pubKey),
			0,
		},
	}

	for _, tt := range tests {
		ret := getUserKeys([]string{tt.key})
		if userKeys := ret["user"]; len(userKeys) != tt.expectedValid {
			t.Errorf("expected %d valid keys from getUserKeys, but %d", tt.expectedValid, len(userKeys))
		}
	}
}

func TestRemoveExpiredKeys(t *testing.T) {
	randKey := utils.MakeRandRSAPubKey(t)

	var tests = []struct {
		key   string
		valid bool
	}{
		{`user:ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"2028-11-08T19:30:47+0000"}`, true},
		{`user:ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"2028-11-08T19:30:47+0700"}`, true},
		{`user:ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"2028-11-08T19:30:47+0700", "futureField": "UNUSED_FIELDS_IGNORED"}`, true},
		{`user:ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"2018-11-08T19:30:46+0000"}`, false},
		{`user:ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"2018-11-08T19:30:46+0700"}`, false},
		{`user:ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"INVALID_TIMESTAMP"}`, false},
		{`user:ssh-rsa [KEY] google-ssh`, false},
		{`user:ssh-rsa [KEY] user`, true},
		{`user:ssh-rsa [KEY]`, true},
		// having the user: prefix should not affect whether a key is expired, repeat test cases without user: prefix
		{`ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"2028-11-08T19:30:47+0000"}`, true},
		{`ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"2028-11-08T19:30:47+0700"}`, true},
		{`ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"2028-11-08T19:30:47+0700", "futureField": "UNUSED_FIELDS_IGNORED"}`, true},
		{`ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"2018-11-08T19:30:46+0000"}`, false},
		{`ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"2018-11-08T19:30:46+0700"}`, false},
		{`ssh-rsa [KEY] google-ssh {"userName":"user@email.com", "expireOn":"INVALID_TIMESTAMP"}`, false},
		{`ssh-rsa [KEY] google-ssh`, false},
		{`ssh-rsa [KEY] user`, true},
		{`ssh-rsa [KEY]`, true},
		{},
	}

	for _, tt := range tests {
		currentKey := strings.Replace(tt.key, "[KEY]", randKey, 1)
		ret := removeExpiredKeys([]string{currentKey})
		if tt.valid {
			if len(ret) == 0 || ret[0] != currentKey {
				t.Errorf("valid key was removed: %q", currentKey)
			}
		}
		if !tt.valid && len(ret) == 1 {
			t.Errorf("invalid key was kept: %q", currentKey)
		}
	}
}

func TestVersionOk(t *testing.T) {
	tests := []struct {
		version    versionInfo
		minVersion versionInfo
		hasErr     bool
	}{
		{
			version:    versionInfo{8, 6},
			minVersion: versionInfo{8, 6},
			hasErr:     false,
		},
		{
			version:    versionInfo{9, 3},
			minVersion: versionInfo{8, 6},
			hasErr:     false,
		},
		{
			version:    versionInfo{8, 3},
			minVersion: versionInfo{8, 6},
			hasErr:     true,
		},
		{
			version:    versionInfo{7, 9},
			minVersion: versionInfo{8, 6},
			hasErr:     true,
		},
	}

	for _, tt := range tests {
		err := versionOk(tt.version, tt.minVersion)
		hasErr := err != nil
		if hasErr != tt.hasErr {
			t.Errorf("versionOk error not correct: Got: %v, Want: %v for Version %d.%d with Min Version of %d.%d",
				hasErr, tt.hasErr, tt.version.major, tt.version.minor, tt.minVersion.major, tt.minVersion.minor)
		}
	}
}

func TestParseVersionInfo(t *testing.T) {
	tests := []struct {
		psOutput    []byte
		expectedVer versionInfo
		expectErr   bool
	}{
		{
			psOutput:    []byte("8.6.0.0\r\n"),
			expectedVer: versionInfo{8, 6},
			expectErr:   false,
		},
		{
			psOutput:    []byte("8.6.0.0"),
			expectedVer: versionInfo{8, 6},
			expectErr:   false,
		},
		{
			psOutput:    []byte("8.6\r\n"),
			expectedVer: versionInfo{8, 6},
			expectErr:   false,
		},
		{
			psOutput:    []byte("12345.34567.34566.3463456\r\n"),
			expectedVer: versionInfo{12345, 34567},
			expectErr:   false,
		},
		{
			psOutput:    []byte("8\r\n"),
			expectedVer: versionInfo{0, 0},
			expectErr:   true,
		},
		{
			psOutput:    []byte("\r\n"),
			expectedVer: versionInfo{0, 0},
			expectErr:   true,
		},
	}

	for _, tt := range tests {
		verInfo, err := parseVersionInfo(tt.psOutput)
		hasErr := err != nil
		if verInfo != tt.expectedVer || hasErr != tt.expectErr {
			t.Errorf("parseVersionInfo(%v) not correct: Got: %v, Error: %v, Want: %v, Error: %v",
				tt.psOutput, verInfo, hasErr, tt.expectedVer, tt.expectErr)
		}
	}
}
