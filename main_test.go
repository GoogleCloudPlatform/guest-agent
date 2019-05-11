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
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/compute-image-windows/logger"
)

func TestRunUpdate(t *testing.T) {
	logger.Init("TestRunUpdate", "")
	var buf bytes.Buffer
	logger.Log = log.New(&buf, "", 0)

	oldMetadata = &metadataJSON{}
	newMetadata = &metadataJSON{
		Instance: instanceJSON{
			Attributes: attributesJSON{
				WindowsKeys:           "{}",
				Diagnostics:           "{}",
				DisableAddressManager: "false",
				DisableAccountManager: "false",
				EnableDiagnostics:     "true",
				EnableWSFC:            "true",
				WSFCAddresses:         "1.1.1.1",
				WSFCAgentPort:         "8000",
			},
		},
	}
	// This test is a bit simplistic, but should catch any unexpected errors or
	// race conditions.
	runUpdate()

	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "ERROR") {
			t.Errorf("error in runUpdate(): %s", line)
		}
	}
}

func TestContainsString(t *testing.T) {
	table := []struct {
		a     string
		slice []string
		want  bool
	}{
		{"a", []string{"a", "b"}, true},
		{"c", []string{"a", "b"}, false},
	}
	for _, tt := range table {
		if got, want := containsString(tt.a, tt.slice), tt.want; got != want {
			t.Errorf("containsString(%s, %v) incorrect return: got %v, want %t", tt.a, tt.slice, got, want)
		}
	}
}
