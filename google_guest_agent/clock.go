// Copyright 2019 Google LLC

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
	"runtime"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

type clockskewMgr struct{}

func (a *clockskewMgr) Diff(ctx context.Context) (bool, error) {
	return oldMetadata.Instance.VirtualClock.DriftToken != newMetadata.Instance.VirtualClock.DriftToken, nil
}

func (a *clockskewMgr) Timeout(ctx context.Context) (bool, error) {
	return false, nil
}

func (a *clockskewMgr) Disabled(ctx context.Context) (bool, error) {
	enabled := cfg.Get().Daemons.ClockSkewDaemon
	return runtime.GOOS == "windows" || !enabled, nil
}

func (a *clockskewMgr) Set(ctx context.Context) error {
	if runtime.GOOS == "freebsd" {
		err := run.Quiet(ctx, "service", "ntpd", "status")
		if err == nil {
			if err := run.Quiet(ctx, "service", "ntpd", "stop"); err != nil {
				return err
			}
			defer func() {
				if err := run.Quiet(ctx, "service", "ntpd", "start"); err != nil {
					logger.Warningf("Error starting 'ntpd' after clock sync: %v.", err)
				}
			}()
		}
		// TODO get server
		return run.Quiet(ctx, "ntpdate", "169.254.169.254")
	}

	res := run.WithOutput(ctx, "/sbin/hwclock", "--hctosys", "-u", "--noadjfile")
	if res.ExitCode != 0 || res.StdErr != "" {
		return error(res)
	}
	return nil
}
