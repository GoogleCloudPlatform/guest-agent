//  Copyright 2017 Google Inc. All Rights Reserved.
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
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"hash"
	"log"
	"math/big"
	"reflect"
	"testing"
	"time"
	"unicode"

	"github.com/GoogleCloudPlatform/compute-image-windows/logger"
	"github.com/go-ini/ini"
)

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
		k := windowsKeyJSON{ExpireOn: tt.sTime}
		if tt.e != k.expired() {
			t.Errorf("windowsKeyJSON.expired() with ExpiredOn %q should return %t", k.ExpireOn, tt.e)
		}
	}
}

func TestAccountsDisabled(t *testing.T) {
	var tests = []struct {
		name string
		data []byte
		md   *metadataJSON
		want bool
	}{
		{"not explicitly disabled", []byte(""), &metadataJSON{}, false},
		{"enabled in cfg only", []byte("[accountManager]\ndisable=false"), &metadataJSON{}, false},
		{"disabled in cfg only", []byte("[accountManager]\ndisable=true"), &metadataJSON{}, true},
		{"disabled in cfg, enabled in instance metadata", []byte("[accountManager]\ndisable=true"), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAccountManager: "false"}}}, true},
		{"enabled in cfg, disabled in instance metadata", []byte("[accountManager]\ndisable=false"), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAccountManager: "true"}}}, false},
		{"enabled in instance metadata only", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAccountManager: "false"}}}, false},
		{"enabled in project metadata only", []byte(""), &metadataJSON{Project: projectJSON{Attributes: attributesJSON{DisableAccountManager: "false"}}}, false},
		{"disabled in instance metadata only", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAccountManager: "true"}}}, true},
		{"enabled in instance metadata, disabled in project metadata", []byte(""), &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAccountManager: "false"}}, Project: projectJSON{Attributes: attributesJSON{DisableAccountManager: "true"}}}, false},
		{"disabled in project metadata only", []byte(""), &metadataJSON{Project: projectJSON{Attributes: attributesJSON{DisableAccountManager: "true"}}}, true},
	}

	for _, tt := range tests {
		cfg, err := ini.InsensitiveLoad(tt.data)
		if err != nil {
			t.Errorf("test case %q: error parsing config: %v", tt.name, err)
			continue
		}
		if cfg == nil {
			cfg = &ini.File{}
		}
		newMetadata = tt.md
		config = cfg
		got := (&accountsMgr{}).disabled()
		if got != tt.want {
			t.Errorf("test case %q, accounts.disabled() got: %t, want: %t", tt.name, got, tt.want)
		}
	}
}

func TestNewPwd(t *testing.T) {
	for i := 0; i < 100000; i++ {
		pwd, err := newPwd()
		if err != nil {
			t.Fatal(err)
		}
		if len(pwd) != 15 {
			t.Errorf("Password is not 15 characters: len(%s)=%d", pwd, len(pwd))
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
}

func TestCreatecredsJSON(t *testing.T) {
	pwd := "password"
	prv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("error generating key: %v", err)
	}
	k := windowsKeyJSON{
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
		newKeys    []windowsKeyJSON
		oldStrKeys []string
		wantAdd    []windowsKeyJSON
	}{
		// These should return toAdd:
		// In MD, not Reg
		{[]windowsKeyJSON{{UserName: "foo"}}, nil, []windowsKeyJSON{{UserName: "foo"}}},
		{[]windowsKeyJSON{{UserName: "foo"}}, []string{`{"UserName":"bar"}`}, []windowsKeyJSON{{UserName: "foo"}}},

		// These should return nothing:
		// In Reg and MD
		{[]windowsKeyJSON{{UserName: "foo"}}, []string{`{"UserName":"foo"}`}, nil},
		// In Reg, not MD
		{nil, []string{`{UserName":"foo"}`}, nil},
	}

	for _, tt := range tests {
		toAdd := compareAccounts(tt.newKeys, tt.oldStrKeys)
		if !reflect.DeepEqual(tt.wantAdd, toAdd) {
			t.Errorf("toAdd does not match expected: newKeys: %q, oldStrKeys: %q, got: %q, want: %q", tt.newKeys, tt.oldStrKeys, toAdd, tt.wantAdd)
		}
	}
}

func TestAccountsLogStatus(t *testing.T) {
	var buf bytes.Buffer
	logger.Init("test", "")
	logger.Log = log.New(&buf, "", 0)

	// Disable it.
	accountDisabled = false

	newMetadata = &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAccountManager: "true"}}}
	config = ini.Empty()
	disabled := (&accountsMgr{}).disabled()
	if !disabled {
		t.Fatal("expected true but got", disabled)
	}
	want := fmt.Sprintln("test: GCE account manager status: disabled")
	if buf.String() != want {
		t.Errorf("got: %q, want: %q", buf.String(), want)
	}
	buf.Reset()

	// Enable it.
	newMetadata = &metadataJSON{Instance: instanceJSON{Attributes: attributesJSON{DisableAccountManager: "false"}}}
	disabled = (&accountsMgr{}).disabled()
	if disabled {
		t.Fatal("expected false but got", disabled)
	}
	want = fmt.Sprintln("test: GCE account manager status: enabled")
	if buf.String() != want {
		t.Errorf("got: %q, want: %q", buf.String(), want)
	}
}
