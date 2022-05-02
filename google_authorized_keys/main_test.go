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

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"

	//"reflect"
	"testing"
	"time"
)

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func TestParseSSHKeys(t *testing.T) {
	rawKeys := `
# Here is some random data in the file.
usera:ssh-rsa AAAA1234USERA
userb:ssh-rsa AAAA1234USERB
usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}
usera:ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2020-04-23T12:34:56+0000"}

`
	expected := []string{
		"ssh-rsa AAAA1234USERA",
		`ssh-rsa AAAA1234 google-ssh {"userName":"usera@example.com","expireOn":"2095-04-23T12:34:56+0000"}`,
	}

	user := "usera"

	if got, want := parseSSHKeys(user, rawKeys), expected; !stringSliceEqual(got, want) {
		t.Errorf("ParseSSHKeys(%s,%s) incorrect return: got %v, want %v", user, rawKeys, got, want)
	}

}

func TestGetUserKeys(t *testing.T) {
	blockProjectKeys := "False"
	instanceKeys := "name:ssh-rsa [KEY] instance1\nothername:ssh-rsa [KEY] instance2"
	projectKeys := "name:ssh-rsa [KEY] project1\nothername:ssh-rsa [KEY] project2"
	var req int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if req == 0 {
			fmt.Fprintf(w, blockProjectKeys)
		} else if req == 1 {
			fmt.Fprintf(w, instanceKeys)
		} else {
			fmt.Fprintf(w, projectKeys)
		}
		req++
	}))
	defer ts.Close()

	metadataURL = ts.URL
	// So that the test wont timeout.
	defaultTimeout = 1 * time.Second

	want := []string{"ssh-rsa [KEY] instance1", "ssh-rsa [KEY] project1"}
	got := getUserKeys("name")

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Did not get expected keys.\ngot:\n'%#v'\nwant:\n'%#v'", got, want)
	}

}

func TestGetUserKeysBlockedProject(t *testing.T) {
	blockProjectKeys := "True"
	instanceKeys := "name:ssh-rsa [KEY] instance1\nothername:ssh-rsa [KEY] instance2"
	projectKeys := "name:ssh-rsa [KEY] project1\nothername:ssh-rsa [KEY] project2"
	var req int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if req == 0 {
			fmt.Fprintf(w, blockProjectKeys)
		} else if req == 1 {
			fmt.Fprintf(w, instanceKeys)
		} else {
			fmt.Fprintf(w, projectKeys)
		}
		req++
	}))
	defer ts.Close()

	metadataURL = ts.URL
	// So that the test wont timeout.
	defaultTimeout = 1 * time.Second

	want := []string{"ssh-rsa [KEY] instance1"}

	got := getUserKeys("name")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Did not get expected keys.\ngot:\n'%#v'\nwant:\n'%#v'", got, want)
	}

}

func TestGetUserKeysEmptyProject(t *testing.T) {
	blockProjectKeys := "False"
	instanceKeys := "name:ssh-rsa [KEY] instance1\nothername:ssh-rsa [KEY] instance2"
	projectKeys := ""
	var req int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if req == 0 {
			fmt.Fprintf(w, blockProjectKeys)
		} else if req == 1 {
			fmt.Fprintf(w, instanceKeys)
		} else {
			fmt.Fprintf(w, projectKeys)
		}
		req++
	}))
	defer ts.Close()

	metadataURL = ts.URL
	// So that the test wont timeout.
	defaultTimeout = 1 * time.Second

	want := []string{"ssh-rsa [KEY] instance1"}
	got := getUserKeys("name")

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Did not get expected keys.\ngot:\n'%#v'\nwant:\n'%#v'", got, want)
	}

}

func TestGetUserKeysEmptyInstance(t *testing.T) {
	blockProjectKeys := "False"
	instanceKeys := ""
	projectKeys := "name:ssh-rsa [KEY] project1\nothername:ssh-rsa [KEY] project2"
	var req int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if req == 0 {
			fmt.Fprintf(w, blockProjectKeys)
		} else if req == 1 {
			fmt.Fprintf(w, instanceKeys)
		} else {
			fmt.Fprintf(w, projectKeys)
		}
		req++
	}))
	defer ts.Close()

	metadataURL = ts.URL
	// So that the test wont timeout.
	defaultTimeout = 1 * time.Second

	want := []string{"ssh-rsa [KEY] project1"}
	got := getUserKeys("name")

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Did not get expected keys.\ngot:\n'%#v'\nwant:\n'%#v'", got, want)
	}

}
