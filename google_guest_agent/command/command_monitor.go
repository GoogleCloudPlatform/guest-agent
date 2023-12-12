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

/*
 * This file contains the details of command's internal communication protocol
 * listener. Most callers should not need to call anything in this file. The
 * command handler and caller API is contained in command.go.
 */

package command

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var cmdMonitor *Monitor = &Monitor{
	handlersMu: new(sync.RWMutex),
	handlers:   make(map[string]Handler),
}

// Init starts an internally managed command server. The agent configuration
// will decide the server options. Returns a reference to the internally managed
// command monitor which the caller can Close() when appropriate.
func Init(ctx context.Context) {
	if cmdMonitor.srv != nil {
		return
	}
	pipe := cfg.Get().Unstable.CommandPipePath
	if pipe == "" {
		pipe = DefaultPipePath
	}
	to, err := time.ParseDuration(cfg.Get().Unstable.CommandRequestTimeout)
	if err != nil {
		logger.Errorf("commmand request timeout configuration is not a valid duration string, falling back to 10s timeout")
		to = time.Duration(10) * time.Second
	}
	var pipemode int64 = 0770
	pipemode, err = strconv.ParseInt(cfg.Get().Unstable.CommandPipeMode, 8, 32)
	if err != nil {
		logger.Errorf("could not parse command_pipe_mode as octal integer: %v falling back to mode 0770", err)
	}
	cmdMonitor.srv = &Server{
		pipe:      pipe,
		pipeMode:  int(pipemode),
		pipeGroup: cfg.Get().Unstable.CommandPipeGroup,
		timeout:   to,
		monitor:   cmdMonitor,
	}
	err = cmdMonitor.srv.start(ctx)
	if err != nil {
		logger.Errorf("failed to start command server: %s", err)
	}
}

// Close will close the internally managed command server, if it was initialized.
func Close() error {
	if cmdMonitor.srv != nil {
		return cmdMonitor.srv.Close()
	}
	return nil
}

// Monitor is the structure which handles command registration and deregistration.
type Monitor struct {
	srv        *Server
	handlersMu *sync.RWMutex
	handlers   map[string]Handler
}

// Close stops the server from listening to commands.
func (m *Monitor) Close() error { return m.srv.Close() }

// Start begins listening for commands.
func (m *Monitor) Start(ctx context.Context) error { return m.srv.start(ctx) }

// Server is the server structure which will listen for command requests and
// route them to handlers. Most callers should not interact with this directly.
type Server struct {
	pipe      string
	pipeMode  int
	pipeGroup string
	timeout   time.Duration
	srv       net.Listener
	monitor   *Monitor
}

// Close signals the server to stop listening for commands and stop waiting to
// listen.
func (c *Server) Close() error {
	if c.srv != nil {
		return c.srv.Close()
	}
	return nil
}

func (c *Server) start(ctx context.Context) error {
	if c.srv != nil {
		return errors.New("server already listening")
	}
	srv, err := listen(ctx, c.pipe, c.pipeMode, c.pipeGroup)
	if err != nil {
		return err
	}
	go func() {
		defer srv.Close()
		for {
			if ctx.Err() != nil {
				return
			}
			conn, err := srv.Accept()
			if err != nil {
				if err == net.ErrClosed {
					break
				}
				logger.Infof("error on connection to pipe %s: %v", c.pipe, err)
				continue
			}
			go func(conn net.Conn) {
				defer conn.Close()
				// Go has lots of helpers to do this for us but none of them return the byte
				// slice afterwards, and we need it for the handler
				var b []byte
				r := bufio.NewReader(conn)
				var depth int
				deadline := time.Now().Add(c.timeout)
				e := conn.SetReadDeadline(deadline)
				if e != nil {
					logger.Infof("could not set read deadline on command request: %v", e)
					return
				}
				for {
					if time.Now().After(deadline) {
						if b, err := json.Marshal(TimeoutError); err != nil {
							conn.Write(internalError)
						} else {
							conn.Write(b)
						}
						return
					}
					rune, _, err := r.ReadRune()
					if err != nil {
						logger.Debugf("connection read error: %v", err)
						if errors.Is(err, os.ErrDeadlineExceeded) {
							if b, err := json.Marshal(TimeoutError); err != nil {
								conn.Write(internalError)
							} else {
								conn.Write(b)
							}
						} else {
							if b, err := json.Marshal(ConnError); err != nil {
								conn.Write(internalError)
							} else {
								conn.Write(b)
							}
						}
						return
					}
					b = append(b, byte(rune))
					switch rune {
					case '{':
						depth++
					case '}':
						depth--
					}
					// Must check here because the first pass always depth = 0
					if depth == 0 {
						break
					}
				}
				var req Request
				err := json.Unmarshal(b, &req)
				if err != nil {
					if b, err := json.Marshal(BadRequestError); err != nil {
						conn.Write(internalError)
					} else {
						conn.Write(b)
					}
					return
				}
				c.monitor.handlersMu.RLock()
				defer c.monitor.handlersMu.RUnlock()
				handler, ok := c.monitor.handlers[req.Command]
				if !ok {
					if b, err := json.Marshal(CmdNotFoundError); err != nil {
						conn.Write(internalError)
					} else {
						conn.Write(b)
					}
					return
				}
				resp, err := handler(b)
				if err != nil {
					re := Response{Status: HandlerError.Status, StatusMessage: err.Error()}
					if b, err := json.Marshal(re); err != nil {
						resp = internalError
					} else {
						resp = b
					}
				}
				conn.Write(resp)
			}(conn)
		}
	}()
	c.srv = srv
	return nil
}
