//  Copyright 2023 Google Inc. All Rights Reserved.
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
	"reflect"
	"testing"
)

func TestParseSystemRelease(t *testing.T) {
	tests := []struct {
		file string
		want info
	}{
		{"Red Hat Enterprise Linux Server release 6.10 (Santiago)", info{os: "rhel", version: ver{6, 10, 0, 2}}},
		{"Red Hat Enterprise Linux Server release 6.10.1", info{os: "rhel", version: ver{6, 10, 1, 3}}},
		{"CentOS Linux release 7.6.1810 (Core)", info{os: "centos", version: ver{7, 6, 1810, 3}}},
	}
	for _, tt := range tests {
		if got := parseSystemRelease(tt.file); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseSystemRelease(%s) incorrect return: got %v, want %v", tt.file, got, tt.want)
		}
	}
}

func TestParseOSRelease(t *testing.T) {
	tests := []struct {
		file string
		want info
	}{
		{"ID=\"sles\"\nNAME=\"SLES\"\nVERSION=\"12-SP4\"\nVERSION_ID=12", info{os: "sles", versionID: "12", version: ver{12, 0, 0, 1}}},
		{"ID=sles\nNAME=\"SLES\"\nVERSION=\"12-SP4\"\nVERSION_ID=\"12.4\"", info{os: "sles", versionID: "12.4", version: ver{12, 4, 0, 2}}},
		{"ID=debian\nNAME=\"Debian GNU/Linux\"\nVERSION=\"9 (stretch)\"\nVERSION_ID=\"9\"", info{os: "debian", versionID: "9", version: ver{9, 0, 0, 1}}},
		{"ID=\"debian\"\nNAME=\"Debian GNU/Linux\"\nVERSION=9\nVERSION_ID=\"9\"", info{os: "debian", versionID: "9", version: ver{9, 0, 0, 1}}},
	}
	for _, tt := range tests {
		if got := parseOSRelease(tt.file); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseOSRelease(%s) incorrect return: got %+v, want %+v", tt.file, got, tt.want)
		}
	}
}
