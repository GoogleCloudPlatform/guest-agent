// Copyright 2018 Google LLC

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
	"encoding/json"
	"reflect"
	"runtime"
	"slices"
	"sync/atomic"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const diagnosticsCmd = `C:\Program Files\Google\Compute Engine\diagnostics\diagnostics.exe`

var (
	diagnosticsRegKey   = "Diagnostics"
	diagnosticsDisabled = false
	// Indicate whether an existing job is runing to collect logs
	// 0 -> not running, 1 -> running
	isDiagnosticsRunning int32 = 0
)

type diagnosticsEntry struct {
	SignedURL string
	ExpireOn  string
	Trace     bool
}

type diagnosticsMgr struct {
	// fakeWindows forces Disabled to run as if it was running in a windows system.
	// mostly target for unit tests.
	fakeWindows bool
}

func (d *diagnosticsMgr) Diff(ctx context.Context) (bool, error) {
	return !reflect.DeepEqual(newMetadata.Instance.Attributes.Diagnostics, oldMetadata.Instance.Attributes.Diagnostics), nil
}

func (d *diagnosticsMgr) Timeout(ctx context.Context) (bool, error) {
	return false, nil
}

func (d *diagnosticsMgr) Disabled(ctx context.Context) (bool, error) {
	var disabled bool
	config := cfg.Get()

	if !d.fakeWindows && runtime.GOOS != "windows" {
		return true, nil
	}

	defer func() {
		if disabled != diagnosticsDisabled {
			diagnosticsDisabled = disabled
			logStatus("diagnostics", disabled)
		}
	}()

	// Diagnostics are opt-in and enabled by default.
	if config.Diagnostics != nil {
		return !config.Diagnostics.Enable, nil
	}

	if newMetadata.Instance.Attributes.EnableDiagnostics != nil {
		return !*newMetadata.Instance.Attributes.EnableDiagnostics, nil
	}
	if newMetadata.Project.Attributes.EnableDiagnostics != nil {
		return !*newMetadata.Project.Attributes.EnableDiagnostics, nil
	}
	return diagnosticsDisabled, nil
}

func (d *diagnosticsMgr) Set(ctx context.Context) error {
	logger.Infof("Diagnostics: logs export requested.")
	diagnosticsEntries, err := readRegMultiString(regKeyBase, diagnosticsRegKey)
	if err != nil && err != errRegNotExist {
		return err
	}

	strEntry := newMetadata.Instance.Attributes.Diagnostics
	if slices.Contains(diagnosticsEntries, strEntry) {
		return nil
	}
	diagnosticsEntries = append(diagnosticsEntries, strEntry)

	var entry diagnosticsEntry
	if err := json.Unmarshal([]byte(strEntry), &entry); err != nil {
		return err
	}

	expired, _ := utils.CheckExpired(entry.ExpireOn)
	if entry.SignedURL == "" || expired {
		return nil
	}

	args := []string{
		"-signedUrl",
		entry.SignedURL,
	}
	if entry.Trace {
		args = append(args, "-trace")
	}
	// If no existing running job, set it to 1 and block other requests
	if !atomic.CompareAndSwapInt32(&isDiagnosticsRunning, 0, 1) {
		logger.Infof("Diagnostics: reject the request, as an existing process is collecting logs from the system")
		return nil
	}

	go func() {
		logger.Infof("Diagnostics: collecting logs from the system.")
		res := run.WithCombinedOutput(ctx, diagnosticsCmd, args...)
		logger.Infof(res.Combined)
		if res.ExitCode != 0 {
			logger.Warningf("Error collecting logs: %v", res.Error())
		}
		// Job is done, unblock the following requests
		atomic.SwapInt32(&isDiagnosticsRunning, 0)
	}()

	return writeRegMultiString(regKeyBase, diagnosticsRegKey, diagnosticsEntries)
}
