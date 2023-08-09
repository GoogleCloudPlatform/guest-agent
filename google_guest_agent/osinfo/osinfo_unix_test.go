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

//go:build unix

package osinfo

import (
	"reflect"
	"testing"
)

func TestParseSystemRelease(t *testing.T) {
	tests := []struct {
		desc    string
		file    string
		want    OSInfo
		wantErr bool
	}{
		{"rhel 6.10", "Red Hat Enterprise Linux Server release 6.10 (Santiago)", OSInfo{OS: "rhel", Version: Ver{6, 10, 0, 2}}, false},
		{"rhel 6.10.1", "Red Hat Enterprise Linux Server release 6.10.1", OSInfo{OS: "rhel", Version: Ver{6, 10, 1, 3}}, false},
		{"centos 7.6.1810", "CentOS Linux release 7.6.1810 (Core)", OSInfo{OS: "centos", Version: Ver{7, 6, 1810, 3}}, false},
		{"bad format", "CentOS Linux", OSInfo{}, true},
		{"bad version", "CentOS Linux release Core", OSInfo{OS: "centos"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := parseSystemRelease(tc.file)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseSystemRelease(%s) incorrect return: got %v, want %v", tc.file, got, tc.want)
			}
			if (err != nil && !tc.wantErr) || (err == nil && tc.wantErr) {
				t.Errorf("want error return: %T, got error: %v", tc.wantErr, err)
			}
		})
	}
}

func TestParseOSRelease(t *testing.T) {
	tests := []struct {
		desc    string
		file    string
		want    OSInfo
		wantErr bool
	}{
		{"sles 12", "ID=\"sles\"\nPRETTY_NAME=\"SLES\"\nVERSION=\"12-SP4\"\nVERSION_ID=12", OSInfo{OS: "sles", PrettyName: "SLES", VersionID: "12", Version: Ver{12, 0, 0, 1}}, false},
		{"sles 12.4", "ID=sles\nPRETTY_NAME=\"SLES\"\nVERSION=\"12-SP4\"\nVERSION_ID=\"12.4\"", OSInfo{OS: "sles", PrettyName: "SLES", VersionID: "12.4", Version: Ver{12, 4, 0, 2}}, false},
		{"debian 9 (stretch)", "ID=debian\nPRETTY_NAME=\"Debian GNU/Linux\"\nVERSION=\"9 (stretch)\"\nVERSION_ID=\"9\"", OSInfo{OS: "debian", PrettyName: "Debian GNU/Linux", VersionID: "9", Version: Ver{9, 0, 0, 1}}, false},
		{"debian 9", "ID=\"debian\"\nPRETTY_NAME=\"Debian GNU/Linux\"\nVERSION=9\nVERSION_ID=\"9\"", OSInfo{OS: "debian", VersionID: "9", PrettyName: "Debian GNU/Linux", Version: Ver{9, 0, 0, 1}}, false},
		{"error version parsing", "ID=\"debian\"\nPRETTY_NAME=\"Debian GNU/Linux\"\nVERSION=9\nVERSION_ID=\"something\"", OSInfo{OS: "debian", PrettyName: "Debian GNU/Linux", VersionID: "something"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := parseOSRelease(tc.file)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseOSRelease(%s) incorrect return: got %+v, want %+v", tc.file, got, tc.want)
			}
			if (err != nil && !tc.wantErr) || (err == nil && tc.wantErr) {
				t.Errorf("want error return: %T, got error: %v", tc.wantErr, err)
			}
		})
	}
}
