//  Copyright 2019 Google Inc. All Rights Reserved.
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
	"os/exec"
	"runtime"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

type clockskewMgr struct{}

func (a *clockskewMgr) diff() bool {
	return oldMetadata.Instance.VirtualClock.DriftToken != newMetadata.Instance.VirtualClock.DriftToken
}

func (a *clockskewMgr) timeout() bool {
	return false
}

func (a *clockskewMgr) disabled(os string) (disabled bool) {
	enabled := config.Section("Daemons").Key("clock_skew_daemon").MustBool(true)
	return os == "windows" || !enabled
}

func (a *clockskewMgr) set() error {
	if runtime.GOOS == "freebsd" {
		err := runCmd(exec.Command("service", "ntpd", "status"))
		if err == nil {
			if err := runCmd(exec.Command("service", "ntpd", "stop")); err != nil {
				return err
			}
			defer func() {
				if err := runCmd(exec.Command("service", "ntpd", "start")); err != nil {
					logger.Warningf("Error starting 'ntpd' after clock sync: %v.", err)
				}
			}()
		}
		// TODO get server
		return runCmd(exec.Command("ntpdate", "metadata.google.internal"))
	}

	return runCmd(exec.Command("/sbin/hwclock", "--hctosys", "-u", "--noadjtime"))
}
