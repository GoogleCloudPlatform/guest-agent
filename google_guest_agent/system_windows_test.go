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
	"testing"
)

func TestParseVersionInfo(t *testing.T) {
	tests := []struct {
		psOutput    []byte
		expectedVer versionInfo
		expectErr   bool
	}{
		{
			psOutput:    []byte("8.6.0.0\r\n"),
			expectedVer: versionInfo{8, 6},
			expectErr:   false,
		},
		{
			psOutput:    []byte("8.6.0.0"),
			expectedVer: versionInfo{8, 6},
			expectErr:   false,
		},
		{
			psOutput:    []byte("8.6\r\n"),
			expectedVer: versionInfo{8, 6},
			expectErr:   false,
		},
		{
			psOutput:    []byte("12345.34567.34566.3463456\r\n"),
			expectedVer: versionInfo{12345, 34567},
			expectErr:   false,
		},
		{
			psOutput:    []byte("8\r\n"),
			expectedVer: versionInfo{0, 0},
			expectErr:   true,
		},
		{
			psOutput:    []byte("\r\n"),
			expectedVer: versionInfo{0, 0},
			expectErr:   true,
		},
	}

	for _, tt := range tests {
		verInfo, err := parseVersionInfo(tt.psOutput)
		hasErr := err != nil
		if verInfo != tt.expectedVer || hasErr != tt.expectErr {
			t.Errorf("parseVersionInfo(%v) not correct: Got: %v, Error: %v, Want: %v, Error: %v",
				tt.psOutput, verInfo, hasErr, tt.expectedVer, tt.expectErr)
		}
	}
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
