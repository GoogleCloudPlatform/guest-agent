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
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"testing"
)

func TestMain(m *testing.M) {
	config, _ = parseConfig("")
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

func TestGetMetadata(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"key1":"value1","key2":"value2"}`)
	}))
	defer ts.Close()

	metadataURL = ts.URL
	metadataHang = ""

	want := map[string]string{"key1": "value1", "key2": "value2"}
	got, err := getMetadataAttributes("")
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
			"windows-startup-script-url",
			"gs://gcs-bucket/binary.exe",
			"C:/Temp/file.exe",
		},
		{
			"windows-startup-script-url",
			"gs://gcs-bucket/binary",
			"C:/Temp/file",
		},
		{
			"windows-startup-script-ps1",
			"gs://gcs-bucket/binary.ps1",
			"C:/Temp/file.ps1",
		},
		{
			"windows-startup-script-ps1",
			"gs://gcs-bucket/binary",
			"C:/Temp/file.ps1",
		},
		{
			"windows-startup-script-bat",
			"gs://gcs-bucket/binary.bat",
			"C:/Temp/file.bat",
		},
		{
			"windows-startup-script-cmd",
			"gs://gcs-bucket/binary.cmd",
			"C:/Temp/file.cmd",
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
