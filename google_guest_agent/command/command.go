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
	"fmt"
	"io"
	"sync"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
)

// Handler functions are the business logic of commands. They must process json
// encoded as a byte slice which contains a Command field and optional arbitrary
// data, and return json which contains a Status, StatusMessage, and optional
// arbitrary data (again encoded as a byte slice).
type Handler func([]byte) []byte

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
	cmdNotFound  = []byte(`{"Status":101,"StatusMessage":"Command not found"}`)
	badRequest   = []byte(`{"Status":102,"StatusMessage":"Could not parse valid JSON from request"}`)
	connError    = []byte(`{"Status":103,"StatusMessage":"Connection error"}`)
	timeoutError = []byte(`{"Status":104,"StatusMessage":"Connection timeout before reading valid request"}`)
	handlersMu   = new(sync.RWMutex)
	handlers     = make(map[string]Handler)
)

// RegisterHandler registers f as the handler for cmd. If a command.Server has
// been initialized, it will be signalled to start listening for commands.
func RegisterHandler(cmd string, f Handler) error {
	if _, ok := handlers[cmd]; ok {
		return fmt.Errorf("cmd %s is already handled", cmd)
	}
	handlersMu.Lock()
	defer handlersMu.Unlock()
	handlers[cmd] = f
	if cmdserver != nil {
		cmdserver.Start()
	}
	return nil
}

// UnregisterHandler clears the handlers for cmd. If a command.Server has been
// intialized and there are no more handlers registered, the server will be
// signalled to stop listening for commands.
func UnregisterHandler(cmd string) error {
	if _, ok := handlers[cmd]; !ok {
		return fmt.Errorf("cmd %s is not registered", cmd)
	}
	handlersMu.Lock()
	defer handlersMu.Unlock()
	delete(handlers, cmd)
	if len(handlers) == 0 && cmdserver != nil {
		cmdserver.Stop()
	}
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
		return connError
	}
	i, err := conn.Write(req)
	if err != nil || i != len(req) {
		return connError
	}
	data, err := io.ReadAll(conn)
	if err != nil {
		return connError
	}
	return data
}
