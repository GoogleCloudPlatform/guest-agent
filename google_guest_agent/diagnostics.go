//  Copyright 2018 Google Inc. All Rights Reserved.
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
	"encoding/json"
	"os/exec"
	"reflect"
	"strconv"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const diagnosticsCmd = `C:\Program Files\Google\Compute Engine\diagnostics\diagnostics.exe`

var (
	diagnosticsRegKey   = "Diagnostics"
	diagnosticsDisabled = true
)

type diagnosticsEntry struct {
	SignedURL string
	ExpireOn  string
	TraceFlag bool
}

func (k diagnosticsEntry) expired() bool {
	t, err := time.Parse(time.RFC3339, k.ExpireOn)
	if err != nil {
		if !containsString(k.ExpireOn, badExpire) {
			logger.Errorf("Error parsing time: %s", err)
			badExpire = append(badExpire, k.ExpireOn)
		}
		return true
	}
	return t.Before(time.Now())
}

type diagnosticsMgr struct{}

func (d *diagnosticsMgr) diff() bool {
	return !reflect.DeepEqual(newMetadata.Instance.Attributes.Diagnostics, oldMetadata.Instance.Attributes.Diagnostics)
}

func (d *diagnosticsMgr) timeout() bool {
	return false
}

func (d *diagnosticsMgr) disabled(os string) (disabled bool) {
	if os != "windows" {
		return true
	}
	defer func() {
		if disabled != diagnosticsDisabled {
			diagnosticsDisabled = disabled
			logStatus("diagnostics", disabled)
		}
	}()

	// Diagnostics are opt-in and disabled by default.
	var err error
	var enabled bool
	enabled, err = strconv.ParseBool(config.Section("diagnostics").Key("enable").String())
	if err == nil {
		return !enabled
	}
	if newMetadata.Instance.Attributes.EnableDiagnostics != nil {
		enabled = *newMetadata.Instance.Attributes.EnableDiagnostics
		return !enabled
	}
	if newMetadata.Project.Attributes.EnableDiagnostics != nil {
		enabled = *newMetadata.Project.Attributes.EnableDiagnostics
		return !enabled
	}
	return diagnosticsDisabled
}

func (d *diagnosticsMgr) set() error {
	diagnosticsEntries, err := readRegMultiString(regKeyBase, diagnosticsRegKey)
	if err != nil && err != errRegNotExist {
		return err
	}

	strEntry := newMetadata.Instance.Attributes.Diagnostics
	if containsString(strEntry, diagnosticsEntries) {
		return nil
	}
	diagnosticsEntries = append(diagnosticsEntries, strEntry)

	var entry diagnosticsEntry
	if err := json.Unmarshal([]byte(strEntry), &entry); err != nil {
		return err
	}
	if entry.SignedURL == "" || entry.expired() {
		return nil
	}

	args := []string{
		"-signedUrl",
		entry.SignedURL,
	}
	if entry.TraceFlag {
		args = append(args, "-trace")
	}

	cmd := exec.Command(diagnosticsCmd, args...)
	go func() {
		logger.Infof("Collecting logs from the system:")
		out, err := cmd.CombinedOutput()
		logger.Infof(string(out[:]))
		if err != nil {
			logger.Infof("Error collecting logs: %v", err)
		}
	}()

	return writeRegMultiString(regKeyBase, diagnosticsRegKey, diagnosticsEntries)
}
