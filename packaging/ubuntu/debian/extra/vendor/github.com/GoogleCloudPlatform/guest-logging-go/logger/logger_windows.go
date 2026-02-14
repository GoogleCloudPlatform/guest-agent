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

// +build windows

// Package logger logs messages as appropriate.
package logger

import (
	"strings"

	"golang.org/x/sys/windows/svc/eventlog"
)

const EID = 882

var (
	el *eventlog.Log
)

func localSetup(loggerName string) error {
	err := eventlog.InstallAsEventCreate(loggerName, eventlog.Info|eventlog.Warning|eventlog.Error)
	if err != nil && !strings.Contains(err.Error(), "registry key already exists") {
		return err
	}

	el, err = eventlog.Open(loggerName)
	return err
}

func localClose() {
	if el != nil {
		el.Close()
	}
}

func local(e LogEntry) {
	if el != nil {
		msg := e.String()
		switch e.Severity {
		case Debug, Info:
			el.Info(EID, msg)
		case Warning:
			el.Warning(EID, msg)
		case Error, Critical:
			el.Error(EID, msg)
		}
	}
}
