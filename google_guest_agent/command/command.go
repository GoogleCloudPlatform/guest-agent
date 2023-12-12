//  Copyright 2023 Google Inc. All Rights Reserved.
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

// Package command facilitates calling commands within the guest-agent.
package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
)

// Get returns the current command monitor which can be used to register command handlers.
func Get() *Monitor {
	return cmdMonitor
}

// Handler functions are the business logic of commands. They must process json
// encoded as a byte slice which contains a Command field and optional arbitrary
// data, and return json which contains a Status, StatusMessage, and optional
// arbitrary data (again encoded as a byte slice). Returned errors will be
// passed onto the command requester.
type Handler func([]byte) ([]byte, error)

// Request is the basic request structure. Command determines which handler the
// request is routed to. Callers may set additional arbitrary fields.
type Request struct {
	Command string
}

// Response is the basic response structure. Handlers may set additional
// arbitrary fields.
type Response struct {
	// Status code for the request. Meaning is defined by the caller, but
	// conventially zero is success.
	Status int
	// StatusMessage is an optional message defined by the caller. Should generally
	// help a human understand what happened.
	StatusMessage string
}

var (
	// CmdNotFoundError is return when there is no handler for the request command
	CmdNotFoundError = Response{
		Status:        101,
		StatusMessage: "Could not find a handler for the requested command",
	}
	// BadRequestError is returned for invalid or unparseable JSON
	BadRequestError = Response{
		Status:        102,
		StatusMessage: "Could not parse valid JSON from request",
	}
	// ConnError is returned for errors from the underlying communication protocol
	ConnError = Response{
		Status:        103,
		StatusMessage: "Connection error",
	}
	// TimeoutError is returned when the timeout period elapses before valid JSON is receieved
	TimeoutError = Response{
		Status:        104,
		StatusMessage: "Connection timeout before reading valid request",
	}
	// HandlerError is returned when the handler function returns an non-nil error. The status message will be replaced with the returnd error string.
	HandlerError = Response{
		Status:        105,
		StatusMessage: "The command handler encountered an error processing your request",
	}
	// InternalErrorCode is the error code for internal command server errors. Returned when failing to marshal a response.
	InternalErrorCode = 106
	internalError     = []byte(`{"Status":106,"StatusMessage":"The command server encountered an internal error trying to respond to your request"}`)
)

// RegisterHandler registers f as the handler for cmd. If a command.Server has
// been initialized, it will be signalled to start listening for commands.
func (m *Monitor) RegisterHandler(cmd string, f Handler) error {
	m.handlersMu.Lock()
	defer m.handlersMu.Unlock()
	if _, ok := m.handlers[cmd]; ok {
		return fmt.Errorf("cmd %s is already handled", cmd)
	}
	m.handlers[cmd] = f
	return nil
}

// UnregisterHandler clears the handlers for cmd. If a command.Server has been
// intialized and there are no more handlers registered, the server will be
// signalled to stop listening for commands.
func (m *Monitor) UnregisterHandler(cmd string) error {
	m.handlersMu.Lock()
	defer m.handlersMu.Unlock()
	if _, ok := m.handlers[cmd]; !ok {
		return fmt.Errorf("cmd %s is not registered", cmd)
	}
	delete(m.handlers, cmd)
	return nil
}

// SendCommand sends a command request over the configured pipe.
func SendCommand(ctx context.Context, req []byte) []byte {
	pipe := cfg.Get().Unstable.CommandPipePath
	if pipe == "" {
		pipe = DefaultPipePath
	}
	return SendCmdPipe(ctx, pipe, req)
}

// SendCmdPipe sends a command request over a specific pipe. Most callers
// should use SendCommand() instead.
func SendCmdPipe(ctx context.Context, pipe string, req []byte) []byte {
	conn, err := dialPipe(ctx, pipe)
	if err != nil {
		if b, err := json.Marshal(ConnError); err != nil {
			return b
		}
		return internalError
	}
	i, err := conn.Write(req)
	if err != nil || i != len(req) {
		if b, err := json.Marshal(ConnError); err != nil {
			return b
		}
		return internalError
	}
	data, err := io.ReadAll(conn)
	if err != nil {
		if b, err := json.Marshal(ConnError); err != nil {
			return b
		}
		return internalError
	}
	return data
}
