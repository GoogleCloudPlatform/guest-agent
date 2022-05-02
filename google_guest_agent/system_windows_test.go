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

func TestGetWindowsSSHVersion(t *testing.T) {

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
				getSSHdPath = func() (string, error) {
					return `C:\Program Files\OpenSSH\sshd.exe`, nil
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
				getSSHdPath = func() (string, error) {
					return `C:\Program Files\OpenSSH\sshd.exe`, nil
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
				getSSHdPath = func() (string, error) {
					return `C:\Program Files\OpenSSH\sshd.exe`, nil
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
				getSSHdPath = func() (string, error) {
					return `C:\Program Files\OpenSSH\sshd.exe`, nil
				}
			},
			major:     0,
			minor:     0,
			expectErr: true,
		},
		{
			name: "Test cannot get sshd path",
			fakePowershellOut: func() {
				getPowershellOutput = func(cmd string) ([]byte, error) {
					return powershellVersionOutput(cmd, []byte("8\r\n"), []byte("6\r\n")), nil
				}
				getSSHdPath = func() (string, error) {
					return "", errors.New("Test Error")
				}
			},
			major:     0,
			minor:     0,
			expectErr: true,
		},
	}

	origGetPowershellOutput := getPowershellOutput

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fakePowershellOut()
			mj, mn, err := getWindowsSSHVersion()
			errFound := err != nil
			if mj != tt.major || mn != tt.minor || errFound != tt.expectErr {
				t.Errorf("getWindowsSSHVersion incorrect return: got %d.%d, error: %v - want %d.%d, error: %v", mj, mn, errFound, tt.major, tt.minor, tt.expectErr)
			}
		})
	}

	getPowershellOutput = origGetPowershellOutput
}

func TestCheckWindowsSSHVersion(t *testing.T) {

	tests := []struct {
		name              string
		fakeGetSSHVersion func(int, int)
		major             int
		minor             int
		minMajor          int
		minMinor          int
		expectOk          bool
		expectErr         bool
	}{
		{
			name: "Test basic functionality",
			fakeGetSSHVersion: func(maj int, min int) {
				getWindowsSSHVersion = func() (int, int, error) {
					return maj, min, nil
				}
			},
			major:     8,
			minor:     6,
			minMajor:  8,
			minMinor:  6,
			expectOk:  true,
			expectErr: false,
		},
		{
			name: "Test newer major version",
			fakeGetSSHVersion: func(maj int, min int) {
				getWindowsSSHVersion = func() (int, int, error) {
					return maj, min, nil
				}
			},
			major:     9,
			minor:     3,
			minMajor:  8,
			minMinor:  6,
			expectOk:  true,
			expectErr: false,
		},
		{
			name: "Test older minor version",
			fakeGetSSHVersion: func(maj int, min int) {
				getWindowsSSHVersion = func() (int, int, error) {
					return maj, min, nil
				}
			},
			major:     8,
			minor:     3,
			minMajor:  8,
			minMinor:  6,
			expectOk:  false,
			expectErr: false,
		},
		{
			name: "Test older major version",
			fakeGetSSHVersion: func(maj int, min int) {
				getWindowsSSHVersion = func() (int, int, error) {
					return maj, min, nil
				}
			},
			major:     7,
			minor:     9,
			minMajor:  8,
			minMinor:  6,
			expectOk:  false,
			expectErr: false,
		},
		{
			name: "Test error from getting version",
			fakeGetSSHVersion: func(maj int, min int) {
				getWindowsSSHVersion = func() (int, int, error) {
					return maj, min, errors.New("Test Error")
				}
			},
			major:     0,
			minor:     0,
			minMajor:  8,
			minMinor:  6,
			expectOk:  false,
			expectErr: true,
		},
	}

	origGetWindowsSSHVersion := getWindowsSSHVersion

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fakeGetSSHVersion(tt.major, tt.minor)
			ok, err := checkWindowsSSHVersion(tt.minMajor, tt.minMinor)
			errFound := err != nil
			if ok != tt.expectOk || errFound != tt.expectErr {
				t.Errorf("checkWindowsSSHVersion(%d, %d) for version %d.%d incorrect return: got %v, error: %v - want %v, error: %v", tt.minMajor, tt.minMinor, tt.major, tt.minor, ok, errFound, tt.expectOk, tt.expectErr)
			}
		})
	}

	getWindowsSSHVersion = origGetWindowsSSHVersion
}
