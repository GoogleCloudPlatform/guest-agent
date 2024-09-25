// Copyright 2018 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
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

	client := &Client{
		metadataURL: ts.URL,
		httpClient: &http.Client{
			Timeout: 1 * time.Second,
		},
	}

	truebool := new(bool)
	*truebool = true
	want := Attributes{
		EnableOSLogin: truebool,
		WSFCAddresses: "foo",
		WindowsKeys: WindowsKeys{
			WindowsKey{Exponent: "exponent", UserName: "username", Modulus: "modulus", ExpireOn: et, AddToAdministrators: nil},
			WindowsKey{Exponent: "exponent", UserName: "username", Modulus: "modulus", ExpireOn: et, AddToAdministrators: func() *bool { ret := true; return &ret }()},
		},
		SSHKeys:          []string{"name:ssh-rsa [KEY] hostname", "name:ssh-rsa [KEY] hostname"},
		DisableTelemetry: false,
	}
	for _, e := range []string{etag1, etag2} {
		got, err := client.Watch(context.Background())
		if err != nil {
			t.Fatalf("error running watchMetadata: %v", err)
		}

		gotA := got.Instance.Attributes
		if !reflect.DeepEqual(gotA, want) {
			t.Fatalf("Did not parse expected metadata.\ngot:\n'%+v'\nwant:\n'%+v'", gotA, want)
		}

		if client.etag != e {
			t.Fatalf("etag not updated as expected (%q != %q)", client.etag, e)
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
		var md Descriptor
		if err := json.Unmarshal([]byte(test.json), &md); err != nil {
			t.Errorf("failed to unmarshal JSON: %v", err)
		}
		if md.Instance.Attributes.BlockProjectKeys != test.res {
			t.Errorf("instance-level sshKeys didn't set block-project-keys (got %t expected %t)", md.Instance.Attributes.BlockProjectKeys, test.res)
		}
	}
}

func TestGetKey(t *testing.T) {
	var gotHeaders http.Header
	var gotReqURI string
	wantValue := "value"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		gotReqURI = r.RequestURI
		fmt.Fprint(w, wantValue)
	})
	testsrv := httptest.NewServer(handler)
	defer testsrv.Close()

	client := New()
	client.metadataURL = testsrv.URL

	key := "key"
	wantURI := "/" + key
	headers := map[string]string{"key": "value"}
	gotValue, err := client.GetKey(context.Background(), key, headers)
	if err != nil {
		t.Fatal(err)
	}

	headers["Metadata-Flavor"] = "Google"
	for k, v := range headers {
		if gotHeaders.Get(k) != v {
			t.Fatalf("received headers does not contain all expected headers, want: %q, got: %q", headers, gotHeaders)
		}
	}
	if wantValue != gotValue {
		t.Errorf("did not get expected return value, got :%q, want: %q", gotValue, wantValue)
	}
	if gotReqURI != wantURI {
		t.Errorf("did not get expected request uri, got :%q, want: %q", gotReqURI, wantURI)
	}
}

func TestGetKeyRecursive(t *testing.T) {
	var gotReqURI string
	wantValue := `{"ssh-keys":"name:ssh-rsa [KEY] instance1\nothername:ssh-rsa [KEY] instance2","block-project-ssh-keys":"false","other-metadata":"foo"}`

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReqURI = r.RequestURI
		fmt.Fprint(w, wantValue)
	})

	testsrv := httptest.NewServer(handler)
	defer testsrv.Close()

	client := New()
	client.metadataURL = testsrv.URL

	key := "key"
	wantURI := fmt.Sprintf("/%s?alt=json&recursive=true", key)
	gotValue, err := client.GetKeyRecursive(context.Background(), key)
	if err != nil {
		t.Errorf("client.GetKeyRecursive(ctx, %s) failed unexpectedly with error: %v", key, err)
	}

	if wantValue != gotValue {
		t.Errorf("client.GetKeyRecursive(ctx, %s) = %q, want: %q", key, gotValue, wantValue)
	}
	if gotReqURI != wantURI {
		t.Errorf("did not get expected request uri, got :%q, want: %q", gotReqURI, wantURI)
	}
}

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		desc   string
		status int
		err    error
		want   bool
	}{
		{
			desc:   "404_should_not_retry",
			status: 404,
			want:   false,
			err:    nil,
		},
		{
			desc:   "429_should_retry",
			status: 429,
			want:   true,
			err:    nil,
		},
		{
			desc:   "random_err_should_retry",
			status: 200,
			want:   true,
			err:    fmt.Errorf("fake retriable error"),
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			err := &MDSReqError{test.status, test.err}
			if got := shouldRetry(err); got != test.want {
				t.Errorf("shouldRetry(%+v) = %t, want %t", err, got, test.want)
			}
		})
	}
}

func TestRetry(t *testing.T) {
	want := "some-metadata"
	ctr := 0
	retries := 3

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ctr == retries {
			fmt.Fprint(w, want)
		} else {
			ctr++
			// 412 error code should be retried.
			w.WriteHeader(412)
		}
	}))
	defer ts.Close()

	client := &Client{
		metadataURL: ts.URL,
		httpClient: &http.Client{
			Timeout: 1 * time.Second,
		},
	}

	reqURL, err := url.JoinPath(ts.URL, "key")
	if err != nil {
		t.Fatalf("Failed to setup mock URL: %v", err)
	}
	req := requestConfig{baseURL: reqURL}

	got, err := client.retry(context.Background(), req)
	if err != nil {
		t.Errorf("retry(ctx, %+v) failed unexpectedly with error: %v", req, err)
	}
	if got != want {
		t.Errorf("retry(ctx, %+v) = %s, want %s", req, got, want)
	}
	if ctr != retries {
		t.Errorf("retry(ctx, %+v) retried %d times, should have returned after %d retries", req, ctr, retries)
	}
}

func TestRetryError(t *testing.T) {
	ctx := context.Background()
	ctr := make(map[string]int)
	backoffAttempts = 5

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "retry") {
			// Retriable status code.
			w.WriteHeader(412)
		} else {
			// Non-retriable status code.
			w.WriteHeader(404)
		}
		ctr[r.URL.Path] = ctr[r.URL.Path] + 1
	}))
	defer ts.Close()

	client := &Client{
		metadataURL: ts.URL,
		httpClient: &http.Client{
			Timeout: 1 * time.Second,
		},
	}

	tests := []struct {
		desc    string
		mdsKey  string
		wantCtr int
	}{
		{
			desc:    "retries_exhausted",
			wantCtr: backoffAttempts,
			mdsKey:  "/retry",
		},
		{
			desc:    "non_retriable_failure",
			wantCtr: 1,
			mdsKey:  "/fail_fast",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			reqURL, err := url.JoinPath(ts.URL, test.mdsKey)
			if err != nil {
				t.Fatalf("Failed to setup mock URL: %v", err)
			}
			req := requestConfig{baseURL: reqURL}

			_, err = client.retry(ctx, req)
			if err == nil {
				t.Errorf("retry(ctx, %+v) succeeded, want error", req)
			}
			if ctr[test.mdsKey] != test.wantCtr {
				t.Errorf("retry(ctx, %+v) retried %d times, should have returned after %d retries", req, ctr[test.mdsKey], test.wantCtr)
			}
		})
	}
}

func TestVlanInterfaces(t *testing.T) {
	vlan := `
{
  "0": {
    "5": {
      "ipv6": [
        "::0"
      ],
      "mac": "abcd_mac",
	  "parentInterface": "/computeMetadata/v1/instance/network-interfaces/0/",
      "mtu": 1460,
      "vlan": 5
    }
  }
}`

	cfg := fmt.Sprintf(`{"instance": {"vlanNetworkInterfaces": %s}}`, vlan)

	want := map[int]map[int]VlanInterface{
		0: {
			5: {
				Mac:             "abcd_mac",
				ParentInterface: "/computeMetadata/v1/instance/network-interfaces/0/",
				MTU:             1460,
				Vlan:            5,
				IPv6:            []string{"::0"},
			},
		},
	}

	var md *Descriptor
	if err := json.Unmarshal([]byte(cfg), &md); err != nil {
		t.Fatalf("json.Unmarshal(%s, &md) failed unexpectedly with error: %v", cfg, err)
	}

	if diff := cmp.Diff(want, md.Instance.VlanNetworkInterfaces); diff != "" {
		t.Errorf("json.Unmarshal(%s, &md) returned unexpected diff (-want,+got):\n %s", cfg, diff)
	}
}
