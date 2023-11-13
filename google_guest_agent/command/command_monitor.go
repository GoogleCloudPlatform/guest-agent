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
 * listener. Outside of using NewCmdServer in unit tests involving commands, most
 * callers should not need to call anything in this file. The command handler
 * and caller API is contained in command.go.
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

// Init starts an internally managed command server which will begin listening
// when handlers register, and stop listening when all handlers unregister. The
// agent configuration will decide the server options. Returns a reference to
// the internally managed command server which the caller can Close() when
// appropriate.
func Init(ctx context.Context) *Server {
	pipe := cfg.Get().Unstable.CommandPipePath
	if pipe == "" {
		pipe = DefaultPipePath
	}
	to, err := time.ParseDuration(cfg.Get().Unstable.CommandRequestTimeout)
	if err != nil {
		logger.Errorf("commmand request timeout configuration is not a valid duration string, falling back to 30s timeout")
		to = time.Duration(10) * time.Second
	}
	var pipemode int64 = 0770
	pipemode, err = strconv.ParseInt(cfg.Get().Unstable.CommandPipeMode, 8, 32)
	if err != nil {
		logger.Errorf("could not parse command_pipe_mode as octal integer: %v falling back to mode 0770", err)
	}
	cmdserver = NewCmdServer(pipe, int(pipemode), cfg.Get().Unstable.CommandPipeGroup, to)
	go func() {
		err := cmdserver.Wait(ctx)
		if err != nil {
			logger.Infof("stopped waiting for commands: %v", err)
		}
	}()
	handlersMu.RLock()
	defer handlersMu.RUnlock()
	if len(handlers) > 0 {
		cmdserver.Start()
	}
	return cmdserver
}

// NewCmdServer returns a pointer to a new Server listening on pipe p. Few
// callers should be using this outside of unit testing, Init should be called
// once and Server should be managed internally instead.
func NewCmdServer(p string, fm int, group string, to time.Duration) *Server {
	cs := Server{
		pipe:      p,
		pipeMode:  fm,
		pipeGroup: group,
		timeout:   to,
		srvMu:     new(sync.Mutex),
		signal:    make(chan string, 1),
	}
	return &cs
}

var (
	cmdserver *Server
)

// Server is the server structure which will listen for command requests and
// route them to handlers. Most callers should not interact with this directly.
type Server struct {
	pipe      string
	pipeMode  int
	pipeGroup string
	timeout   time.Duration
	signal    chan (string)
	srvMu     *sync.Mutex
	srv       net.Listener
}

// Listening reports whether a Server is currently listening on the underlying
// communication protocol.
func (c Server) Listening() bool { return c.srv != nil }

// Close signals the server to stop listening for commands and stop waiting to
// listen.
func (c *Server) Close() {
	c.signal <- "CLOSE"
}

// Stop signals the server to stop listening for commands.
func (c *Server) Stop() {
	c.signal <- "STOP"
}

// Start signals the server to start listening for commands.
func (c *Server) Start() {
	c.signal <- "START"
}

func (c *Server) listen(ctx context.Context) error {
	// Do not call this function without holding c.srvMu. Returns when c.srv is
	// set and the listener is accepting connections in another goroutine
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
						conn.Write(marshalOrInternalError(TimeoutError))
						return
					}
					rune, _, err := r.ReadRune()
					if err != nil {
						logger.Debugf("connection read error: %v", err)
						if errors.Is(err, os.ErrDeadlineExceeded) {
							conn.Write(marshalOrInternalError(TimeoutError))
						} else {
							conn.Write(marshalOrInternalError(ConnError))
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
					conn.Write(marshalOrInternalError(BadRequestError))
					return
				}
				handlersMu.RLock()
				defer handlersMu.RUnlock()
				handler, ok := handlers[req.Command]
				if !ok {
					conn.Write(marshalOrInternalError(CmdNotFoundError))
					return
				}
				resp, err := handler(b)
				if err != nil {
					resp = marshalOrInternalError(Response{Status: HandlerError.Status, StatusMessage: err.Error()})
				}
				conn.Write(resp)
			}(conn)
		}
	}()
	c.srv = srv
	return nil
}

// Wait does not start the Server, it waits for a signal to begin listening
// on the given context, as long as it is valid. To signal the server to start
// or stop listening, call Server.Start() or Server.Stop(). To signal the
// Server to stop waiting, call Server.Close(). This function is
// thread-safe, but calling it multiple times is generally a bad idea, as Close()
// will only close a single Wait() call.
func (c *Server) Wait(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sig := <-c.signal:
			switch sig {
			case "START":
				c.srvMu.Lock()
				if c.srv == nil {
					logger.Debugf("starting command server")
					err := c.listen(ctx)
					if err != nil {
						logger.Errorf("could not listen for commands on pipe %s: %v", c.pipe, err)
					} else {
					}
				}
				c.srvMu.Unlock()
			case "STOP":
				c.srvMu.Lock()
				if c.srv != nil {
					logger.Debugf("stopping command server")
					c.srv.Close()
					c.srv = nil
				}
				c.srvMu.Unlock()
			case "CLOSE":
				logger.Debugf("closing command server")
				c.srvMu.Lock()
				if c.srv != nil {
					c.srv.Close()
					c.srv = nil
				}
				c.srvMu.Unlock()
				return nil
			}
		}
	}
}

func marshalOrInternalError(v any) []byte {
	// Successfully marshal v into []byte encoded JSON, or return an internal server error as []byte encoded JSON
	b, err := json.Marshal(v)
	if err != nil {
		return internalError
	}
	return b
}
