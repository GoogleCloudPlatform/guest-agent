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
	"errors"
	"strings"
	"testing"
)

func powershellVersionOutput(cmd string, major []byte, minor []byte) []byte {
	if strings.Contains(cmd, "FileMajorPart") {
		return major
	}
	return minor
}

func TestGetWindowsExeVersion(t *testing.T) {

	tests := []struct {
		name              string
		fakePowershellOut func()
		major             int
		minor             int
		expectErr         bool
	}{
		{
			name: "Test basic functionality",
			fakePowershellOut: func() {
				getPowershellOutput = func(cmd string) ([]byte, error) {
					return powershellVersionOutput(cmd, []byte("8\r\n"), []byte("6\r\n")), nil
				}
			},
			major:     8,
			minor:     6,
			expectErr: false,
		},
		{
			name: "Test powershell error",
			fakePowershellOut: func() {
				getPowershellOutput = func(cmd string) ([]byte, error) {
					return powershellVersionOutput(cmd, []byte(""), []byte("")), errors.New("Test Error")
				}
			},
			major:     0,
			minor:     0,
			expectErr: true,
		},
		{
			name: "Test empty return value",
			fakePowershellOut: func() {
				getPowershellOutput = func(cmd string) ([]byte, error) {
					return powershellVersionOutput(cmd, []byte(""), []byte("")), nil
				}
			},
			major:     0,
			minor:     0,
			expectErr: true,
		},
		{
			name: "Test empty minor version",
			fakePowershellOut: func() {
				getPowershellOutput = func(cmd string) ([]byte, error) {
					return powershellVersionOutput(cmd, []byte("8\r\n"), []byte("")), nil
				}
			},
			major:     8,
			minor:     0,
			expectErr: true,
		},
	}

	origGetPowershellOutput := getPowershellOutput

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fakePowershellOut()
			verInfo, err := getWindowsExeVersion(`C:\Path\to\sshd.exe`)
			errFound := err != nil
			if verInfo.major != tt.major || verInfo.minor != tt.minor || errFound != tt.expectErr {
				t.Errorf("getWindowsSSHVersion incorrect return: got %d.%d, error: %v - want %d.%d, error: %v", verInfo.major, verInfo.minor, errFound, tt.major, tt.minor, tt.expectErr)
			}
		})
	}

	getPowershellOutput = origGetPowershellOutput
}

func TestCheckMinimumVersion(t *testing.T) {
	tests := []struct {
		version    versionInfo
		minVersion versionInfo
		ok         bool
	}{
		{
			version:    versionInfo{8, 6},
			minVersion: versionInfo{8, 6},
			ok:         true,
		},
		{
			version:    versionInfo{9, 3},
			minVersion: versionInfo{8, 6},
			ok:         true,
		},
		{
			version:    versionInfo{8, 3},
			minVersion: versionInfo{8, 6},
			ok:         false,
		},
		{
			version:    versionInfo{7, 9},
			minVersion: versionInfo{8, 6},
			ok:         false,
		},
	}

	for _, tt := range tests {
		check := checkMinimumVersion(tt.version, tt.minVersion)
		if check != tt.ok {
			t.Errorf("CheckMinimumVersion not correct: Got: %v, Want: %v for Version %d.%d with Min Version of %d.%d",
				check, tt.ok, tt.version.major, tt.version.minor, tt.minVersion.major, tt.minVersion.minor)
		}
	}
}
