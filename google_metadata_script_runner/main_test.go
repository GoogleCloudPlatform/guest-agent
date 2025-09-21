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
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"google.golang.org/api/option"
)

func TestMain(m *testing.M) {
	if err := cfg.Load(nil); err != nil {
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestGetWantedArgs(t *testing.T) {
	getWantedTests := []struct {
		arg  string
		os   string
		want []string
	}{
		{
			"specialize",
			"windows",
			[]string{
				"sysprep-specialize-script-ps1",
				"sysprep-specialize-script-cmd",
				"sysprep-specialize-script-bat",
				"sysprep-specialize-script-url",
			},
		},
		{
			"startup",
			"windows",
			[]string{
				"windows-startup-script-ps1",
				"windows-startup-script-cmd",
				"windows-startup-script-bat",
				"windows-startup-script-url",
			},
		},
		{
			"shutdown",
			"windows",
			[]string{
				"windows-shutdown-script-ps1",
				"windows-shutdown-script-cmd",
				"windows-shutdown-script-bat",
				"windows-shutdown-script-url",
			},
		},
		{
			"startup",
			"linux",
			[]string{
				"startup-script",
				"startup-script-url",
			},
		},
		{
			"shutdown",
			"linux",
			[]string{
				"shutdown-script",
				"shutdown-script-url",
			},
		},
	}

	for _, tt := range getWantedTests {
		got, err := getWantedKeys([]string{"", tt.arg}, tt.os)
		if err != nil {
			t.Fatalf("validateArgs returned error: %v", err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("returned slice does not match expected one: got %v, want %v", got, tt.want)
		}
		_, err = getWantedKeys([]string{""}, "")
		if err == nil {
			t.Errorf("0 args should produce an error")
		}
		_, err = getWantedKeys([]string{"", "", ""}, "")
		if err == nil {
			t.Errorf("3 args should produce an error")
		}
	}
}

func TestGetExistingKeys(t *testing.T) {
	wantedKeys := []string{
		"sysprep-specialize-script-cmd",
		"sysprep-specialize-script-ps1",
		"sysprep-specialize-script-bat",
		"sysprep-specialize-script-url",
	}
	md := map[string]string{
		"sysprep-specialize-script-cmd": "cmd",
		"startup-script-cmd":            "cmd",
		"shutdown-script-ps1":           "ps1",
		"sysprep-specialize-script-url": "url",
		"sysprep-specialize-script-ps1": "ps1",
		"key":                           "value",
		"sysprep-specialize-script-bat": "bat",
	}
	want := map[string]string{
		"sysprep-specialize-script-ps1": "ps1",
		"sysprep-specialize-script-cmd": "cmd",
		"sysprep-specialize-script-bat": "bat",
		"sysprep-specialize-script-url": "url",
	}
	got := parseMetadata(md, wantedKeys)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsed metadata does not match expectation, got: %v, want: %v", got, want)
	}
}

func TestParseGCS(t *testing.T) {
	matchTests := []struct {
		path, bucket, object string
	}{
		{"gs://bucket/object", "bucket", "object"},
		{"gs://bucket/some/object", "bucket", "some/object"},
		{"http://bucket.storage.googleapis.com/object", "bucket", "object"},
		{"https://bucket.storage.googleapis.com/object", "bucket", "object"},
		{"https://bucket.storage.googleapis.com/some/object", "bucket", "some/object"},
		{"http://storage.googleapis.com/bucket/object", "bucket", "object"},
		{"http://commondatastorage.googleapis.com/bucket/object", "bucket", "object"},
		{"https://storage.googleapis.com/bucket/object", "bucket", "object"},
		{"https://commondatastorage.googleapis.com/bucket/object", "bucket", "object"},
		{"https://storage.googleapis.com/bucket/some/object", "bucket", "some/object"},
		{"https://commondatastorage.googleapis.com/bucket/some/object", "bucket", "some/object"},
	}

	for _, tt := range matchTests {
		bucket, object := parseGCS(tt.path)
		if bucket != tt.bucket {
			t.Errorf("returned bucket does not match expected one for %q:\n  got %q, want: %q", tt.path, bucket, tt.bucket)
		}
		if object != tt.object {
			t.Errorf("returned object does not match expected one for %q\n:  got %q, want: %q", tt.path, object, tt.object)
		}
	}
}

type mdsClient struct{}

func (mds *mdsClient) Get(ctx context.Context) (*metadata.Descriptor, error) {
	return nil, fmt.Errorf("Get() not yet implemented")
}

func (mds *mdsClient) GetKey(ctx context.Context, key string, headers map[string]string) (string, error) {
	return "", fmt.Errorf("GetKey() not yet implemented")
}

func (mds *mdsClient) GetKeyRecursive(ctx context.Context, key string) (string, error) {
	return `{"key1":"value1","key2":"value2"}`, nil
}

func (mds *mdsClient) Watch(ctx context.Context) (*metadata.Descriptor, error) {
	return nil, fmt.Errorf("Watch() not yet implemented")
}

func (mds *mdsClient) WriteGuestAttributes(ctx context.Context, key string, value string) error {
	return fmt.Errorf("WriteGuestattributes() not yet implemented")
}

func TestGetMetadata(t *testing.T) {
	ctx := context.Background()
	client = &mdsClient{}
	want := map[string]string{"key1": "value1", "key2": "value2"}
	got, err := getMetadataAttributes(ctx, "")
	if err != nil {
		t.Fatalf("error running getMetadataAttributes: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("metadata does not match expectation, got: %q, want: %q", got, want)
	}
}

func TestNormalizeFilePathForWindows(t *testing.T) {
	tmpFilePath := "C:/Temp/file"

	testCases := []struct {
		metadataKey      string
		gcsScriptURLPath string
		want             string
	}{
		{
			metadataKey:      "windows-startup-script-url",
			gcsScriptURLPath: "gs://gcs-bucket/binary.exe",
			want:             "C:/Temp/file.exe",
		},
		{
			metadataKey:      "windows-startup-script-url",
			gcsScriptURLPath: "gs://gcs-bucket/binary",
			want:             "C:/Temp/file",
		},
		{
			metadataKey:      "windows-startup-script-ps1",
			gcsScriptURLPath: "gs://gcs-bucket/binary.ps1",
			want:             "C:/Temp/file.ps1",
		},
		{
			metadataKey:      "windows-startup-script-ps1",
			gcsScriptURLPath: "gs://gcs-bucket/binary",
			want:             "C:/Temp/file.ps1",
		},
		{
			metadataKey:      "windows-startup-script-bat",
			gcsScriptURLPath: "gs://gcs-bucket/binary.bat",
			want:             "C:/Temp/file.bat",
		},
		{
			metadataKey:      "windows-startup-script-cmd",
			gcsScriptURLPath: "gs://gcs-bucket/binary.cmd",
			want:             "C:/Temp/file.cmd",
		},
	}

	for _, tc := range testCases {
		url := url.URL{
			Path: tc.gcsScriptURLPath,
		}
		got := normalizeFilePathForWindows(tmpFilePath, tc.metadataKey, &url)

		if got != tc.want {
			t.Errorf("Return didn't match expected output for inputs:\n fileName: %s, metadataKey: %s, gcsScriptUrl: %s\n Expected: %s\n Got: %s",
				tmpFilePath, tc.metadataKey, tc.gcsScriptURLPath, tc.want, got)
		}
	}
}

func TestGetWantedKeysError(t *testing.T) {
	// Reset original value.
	defer cfg.Load(nil)

	tests := []struct {
		cfg string
		arg string
		os  string
	}{
		{
			cfg: `[MetadataScripts]
			shutdown = false`,
			arg: "shutdown",
			os:  "linux",
		},
		{
			cfg: `[MetadataScripts]
			startup = false`,
			arg: "startup",
			os:  "linux",
		},
		{
			cfg: `[MetadataScripts]
			shutdown-windows = false`,
			arg: "shutdown",
			os:  "windows",
		},
		{
			cfg: `[MetadataScripts]
			startup-windows = false`,
			arg: "startup",
			os:  "windows",
		},
	}

	for _, test := range tests {
		t.Run(test.os+"-"+test.arg, func(t *testing.T) {
			if err := cfg.Load([]byte(test.cfg)); err != nil {
				t.Errorf("cfg.Load(%s) failed unexpectedly with error: %v", test.cfg, err)
			}
			if _, err := getWantedKeys([]string{"", test.arg}, test.os); err == nil {
				t.Errorf("getWantedKeys(%s, %s) succeeded for disabled config, want error", test.arg, test.os)
			}
		})
	}
}

func TestDownloadURL(t *testing.T) {
	ctx := context.Background()
	ctr := make(map[string]int)
	// No need to wait longer, override for testing.
	defaultRetryPolicy.Jitter = time.Millisecond

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /retry should succeed within 2 retries; /fail should always fail.
		if (r.URL.Path == "/retry" && ctr["/retry"] != 1) || strings.Contains(r.URL.Path, "fail") {
			w.WriteHeader(400)
		}

		fmt.Fprintf(w, "%v", r.URL.Path)
		ctr[r.URL.Path] = ctr[r.URL.Path] + 1
	}))
	defer server.Close()

	tests := []struct {
		name    string
		key     string
		wantErr bool
		retries int
	}{
		{
			name:    "succeed_immediately",
			key:     "/immediate_download",
			wantErr: false,
			retries: 1,
		},
		{
			name:    "succeed_after_retry",
			key:     "/retry",
			wantErr: false,
			retries: 2,
		},
		{
			name:    "fail_retry_exhaust",
			key:     "/fail",
			wantErr: true,
			retries: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.OpenFile(filepath.Join(t.TempDir(), tt.name), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
			if err != nil {
				t.Fatalf("Failed to setup test file: %v", err)
			}
			defer f.Close()
			url := server.URL + tt.key
			if err := downloadURL(ctx, url, f); (err != nil) != tt.wantErr {
				t.Errorf("downloadURL(ctx, %s, %s) error = [%v], wantErr %t", url, f.Name(), err, tt.wantErr)
			}

			if !tt.wantErr {
				gotBytes, err := os.ReadFile(f.Name())
				if err != nil {
					t.Errorf("failed to read output file %q, with error: %v", f.Name(), err)
				}
				if string(gotBytes) != tt.key {
					t.Errorf("downloadURL(ctx, %s, %s) wrote = [%s], want [%s]", url, f.Name(), string(gotBytes), tt.key)
				}
			}

			if ctr[tt.key] != tt.retries {
				t.Errorf("downloadURL(ctx, %s, %s) retried [%d] times, should have returned after [%d] retries", url, f.Name(), ctr[tt.key], tt.retries)
			}
		})
	}
}

func TestDownloadGSURL(t *testing.T) {
	ctx := context.Background()
	ctr := make(map[string]int)
	// No need to wait longer, override for testing.
	defaultRetryPolicy.Jitter = time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fake error for invalid object request.
		if strings.Contains(r.URL.Path, "invalid") {
			w.WriteHeader(404)
		}
		fmt.Fprintf(w, "%v", r.URL.Path)
		ctr[r.URL.Path] = ctr[r.URL.Path] + 1
	}))
	defer server.Close()

	var err error
	httpClient := &http.Client{Transport: &http.Transport{}}
	testStorageClient, err = storage.NewClient(ctx, option.WithHTTPClient(httpClient), option.WithEndpoint(server.URL))
	if err != nil {
		t.Fatalf("Failed to setup test storage client, err: %+v", err)
	}
	defer testStorageClient.Close()

	tests := []struct {
		name    string
		bucket  string
		object  string
		wantErr bool
		retries int
	}{
		{
			name:    "valid_object",
			bucket:  "valid",
			object:  "obj1",
			wantErr: false,
			retries: 1,
		},
		{
			name:    "invalid_object",
			bucket:  "invalid",
			object:  "obj1",
			wantErr: true,
			retries: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.OpenFile(filepath.Join(t.TempDir(), tt.name), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
			if err != nil {
				t.Fatalf("Failed to setup test file: %v", err)
			}
			defer f.Close()

			if err := downloadGSURL(ctx, tt.bucket, tt.object, f); (err != nil) != tt.wantErr {
				t.Errorf("downloadGSURL(ctx, %s, %s, %s) error = [%+v], wantErr %t", tt.bucket, tt.object, f.Name(), err, tt.wantErr)
			}

			want := fmt.Sprintf("/%s/%s", tt.bucket, tt.object)

			if !tt.wantErr {
				gotBytes, err := os.ReadFile(f.Name())
				if err != nil {
					t.Errorf("failed to read output file %q, with error: %v", f.Name(), err)
				}

				if string(gotBytes) != want {
					t.Errorf("downloadGSURL(ctx, %s, %s, %s) wrote = [%s], want [%s]", tt.bucket, tt.object, f.Name(), string(gotBytes), want)
				}
			}

			if ctr[want] != tt.retries {
				t.Errorf("downloadGSURL(ctx, %s, %s, %s) retried [%d] times, should have returned after [%d] retries", tt.bucket, tt.object, f.Name(), ctr[want], tt.retries)
			}
		})
	}
}
