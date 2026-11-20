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

// Package logger logs messages as appropriate.
package logger

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	logpb "google.golang.org/genproto/googleapis/logging/v2"
)

// Severity is the severity level of the log event.
type Severity int

// Log severity levels.
const (
	Debug Severity = iota
	Info
	Warning
	Error
	Critical
)

var severityName = map[Severity]string{
	Debug:    "Debug",
	Info:     "Info",
	Warning:  "Warning",
	Error:    "Error",
	Critical: "Critical",
}

func (v Severity) String() string {
	s, ok := severityName[v]
	if ok {
		return s
	}
	return ""
}

// LogEntry encapsulates a single log entry.
type LogEntry struct {
	Message string `json:"message"`
	// If present, this will be set as Payload to Cloud Logging
	// instead of Message and LocalTimeStamp.
	//
	// Note: Message is still sent to local logs.
	StructuredPayload interface{}       `json:"omitempty"`
	Labels            map[string]string `json:"-"`
	CallDepth         int               `json:"-"`
	Severity          Severity          `json:"-"`
	// Source will be overwritten, do not set.
	Source *logpb.LogEntrySourceLocation `json:"-"`
	// LocalTimestamp will be overwritten, do not set.
	LocalTimestamp string `json:"localTimestamp"`
}

func (e LogEntry) String() string {
	if formatFunction != nil {
		return formatFunction(e)
	}
	if e.Severity == Error || e.Severity == Critical {
		// 2006-01-02T15:04:05.999999Z07:00 LoggerName ERROR file.go:82: This is a log message.
		return fmt.Sprintf("%s %s %s %s:%d: %s", e.LocalTimestamp, loggerName, e.Severity, e.Source.File, e.Source.Line, e.Message)
	}
	// 2006-01-02T15:04:05.999999Z07:00 LoggerName INFO: This is a log message.
	return fmt.Sprintf("%s %s %s: %s", e.LocalTimestamp, loggerName, e.Severity, e.Message)
}

func (e LogEntry) bytes() []byte {
	return []byte(strings.TrimSpace(e.String()) + "\n")
}

func now() string {
	// RFC3339 with milliseconds.
	return time.Now().Format("2006-01-02T15:04:05.0000Z07:00")
}

func caller(depth int) *logpb.LogEntrySourceLocation {
	depth = depth + 1
	pc, file, line, ok := runtime.Caller(depth)
	if !ok {
		file = "???"
		line = 0
	}

	return &logpb.LogEntrySourceLocation{File: filepath.Base(file), Line: int64(line), Function: runtime.FuncForPC(pc).Name()}
}
