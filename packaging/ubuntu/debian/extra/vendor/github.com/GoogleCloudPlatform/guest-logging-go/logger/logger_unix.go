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

// +build !windows

// Package logger logs messages as appropriate.
package logger

import (
	"log/syslog"
)

var (
	slWriter *syslog.Writer
)

func localSetup(loggerName string) error {
	var err error
	slWriter, err = syslog.New(syslog.LOG_DAEMON|syslog.LOG_INFO, loggerName)
	return err
}

func localClose() {
	if slWriter != nil {
		slWriter.Close()
	}
}

func local(e LogEntry) {
	if slWriter != nil {
		msg := e.String()
		switch e.Severity {
		case Debug:
			slWriter.Debug(msg)
		case Info:
			slWriter.Info(msg)
		case Warning:
			slWriter.Warning(msg)
		case Error:
			slWriter.Err(msg)
		case Critical:
			slWriter.Crit(msg)
		}
	}
}
