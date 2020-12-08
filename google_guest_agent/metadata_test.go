//  Copyright 2018 Google Inc. All Rights Reserved.
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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestWatchMetadata(t *testing.T) {
	etag1, etag2 := "foo", "bar"
	var req int
	et := time.Now().Add(10 * time.Second).Format(time.RFC3339)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if req == 0 {
			w.Header().Set("etag", etag1)
		} else {
			w.Header().Set("etag", etag2)
		}
		fmt.Fprintf(w, `{"instance":{"attributes":{"enable-oslogin":"true","ssh-keys":"name:ssh-rsa [KEY] hostname\nname:ssh-rsa [KEY] hostname","windows-keys":"{}\n{\"expireOn\":\"%[1]s\",\"exponent\":\"exponent\",\"modulus\":\"modulus\",\"username\":\"username\"}\n{\"expireOn\":\"%[1]s\",\"exponent\":\"exponent\",\"modulus\":\"modulus\",\"username\":\"username\",\"addToAdministrators\":true}","wsfc-addrs":"foo"}}}`, et)
		req++
	}))
	defer ts.Close()

	metadataURL = ts.URL
	// So that the test wont timeout.
	defaultTimeout = 1 * time.Second

	truebool := new(bool)
	*truebool = true
	want := attributes{
		EnableOSLogin: truebool,
		WSFCAddresses: "foo",
		WindowsKeys: windowsKeys{
			windowsKey{Exponent: "exponent", UserName: "username", Modulus: "modulus", ExpireOn: et, AddToAdministrators: nil},
			windowsKey{Exponent: "exponent", UserName: "username", Modulus: "modulus", ExpireOn: et, AddToAdministrators: func() *bool { ret := true; return &ret }()},
		},
		SSHKeys: []string{"name:ssh-rsa [KEY] hostname", "name:ssh-rsa [KEY] hostname"},
	}
	for _, e := range []string{etag1, etag2} {
		got, err := watchMetadata(context.Background())
		if err != nil {
			t.Fatalf("error running watchMetadata: %v", err)
		}

		gotA := got.Instance.Attributes
		if !reflect.DeepEqual(gotA, want) {
			t.Fatalf("Did not parse expected metadata.\ngot:\n'%+v'\nwant:\n'%+v'", gotA, want)
		}

		if etag != e {
			t.Fatalf("etag not updated as expected (%q != %q)", etag, e)
		}
	}
}

func TestBlockProjectKeys(t *testing.T) {
	tests := []struct {
		json string
		res  bool
	}{
		{
			`{"instance": {"attributes": {"ssh-keys": "name:ssh-rsa [KEY] hostname\nname:ssh-rsa [KEY] hostname"},"project": {"attributes": {"ssh-keys": "name:ssh-rsa [KEY] hostname\nname:ssh-rsa [KEY] hostname"}}}}`,
			false,
		},
		{
			`{"instance": {"attributes": {"sshKeys": "name:ssh-rsa [KEY] hostname\nname:ssh-rsa [KEY] hostname"},"project": {"attributes": {"ssh-keys": "name:ssh-rsa [KEY] hostname\nname:ssh-rsa [KEY] hostname"}}}}`,
			true,
		},
		{
			`{"instance": {"attributes": {"block-project-ssh-keys": "true", "ssh-keys": "name:ssh-rsa [KEY] hostname\nname:ssh-rsa [KEY] hostname"},"project": {"attributes": {"ssh-keys": "name:ssh-rsa [KEY] hostname\nname:ssh-rsa [KEY] hostname"}}}}`,
			true,
		},
	}
	for _, test := range tests {
		var md metadata
		if err := json.Unmarshal([]byte(test.json), &md); err != nil {
			t.Errorf("failed to unmarshal JSON: %v", err)
		}
		if md.Instance.Attributes.BlockProjectKeys != test.res {
			t.Errorf("instance-level sshKeys didn't set block-project-keys (got %t expected %t)", md.Instance.Attributes.BlockProjectKeys, test.res)
		}
	}
}
