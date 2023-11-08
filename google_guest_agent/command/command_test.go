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

package command

import (
	"context"
	"encoding/json"
	"io"
	"os/user"
	"math/rand"
	"path"
	"runtime"
	"testing"
	"time"
)

func getTestPipePath(t *testing.T) string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\google-guest-agent-network-events-test`
	}
	return path.Join(t.TempDir(), "run", "pipe")
}

func testctx(t *testing.T) context.Context {
	d, ok := t.Deadline()
	if !ok {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		return ctx
	}
	ctx, cancel := context.WithDeadline(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

type testRequest struct {
	Command       string
	ArbitraryData int
}

func TestStart(t *testing.T) {
	ctx := testctx(t)
	pipe := getTestPipePath(t)
	cs := NewCmdServer(pipe, 0770, "-1", time.Second)
	cs.Start()
	t.Cleanup(cs.Close)
	go cs.Wait(ctx)
	for !cs.Listening() {
		time.Sleep(time.Nanosecond)
	}
	if cs.srv == nil {
		t.Error("internal net.Listener is not set on command server")
	}
	c, err := dialPipe(ctx, pipe)
	if err != nil {
		t.Errorf("could not connect to pipe of listening server: %v", err)
	} else {
		c.Close()
	}
}

func TestStop(t *testing.T) {
	ctx := testctx(t)
	pipe := getTestPipePath(t)
	cs := NewCmdServer(pipe, 0770, "-1", time.Second)
	cs.Start()
	t.Cleanup(cs.Close)
	go cs.Wait(ctx)
	for !cs.Listening() {
		time.Sleep(time.Nanosecond)
	}
	c, err := dialPipe(ctx, pipe)
	if err != nil {
		t.Errorf("could not connect to pipe of listening server: %v", err)
	} else {
		c.Close()
	}
	cs.Stop()
	for cs.Listening() {
		time.Sleep(time.Nanosecond)
	}
	if cs.srv != nil {
		t.Error("net.Listener is still listening after Stop() call")
	}
	c, err = dialPipe(ctx, pipe)
	if err == nil {
		t.Error("connected to pipe of stopped server")
		c.Close()
	}
}

func TestClose(t *testing.T) {
	ctx := testctx(t)
	cs := NewCmdServer(getTestPipePath(t), 0770, "-1", time.Second)
	cs.Close()
	err := cs.Wait(ctx)
	if err != nil {
		t.Errorf("unexpected error waiting for commands: %v", err)
	}
}

func TestWaitCancel(t *testing.T) {
	ctx := testctx(t)
	listenctx, cancel := context.WithCancel(ctx)
	cancel()
	err := NewCmdServer(getTestPipePath(t), 0770, "-1", time.Second).Wait(listenctx)
	if err != context.Canceled {
		t.Errorf("unexpected error waiting for commands, got %v want %v", err, context.DeadlineExceeded)
	}
}

func TestListen(t *testing.T) {
	cu, err := user.Current()
	if err != nil {
		t.Fatalf("could not get current user: %v", err)
	}
	ug, err := cu.GroupIds()
	if err != nil {
		t.Fatalf("could not get user groups for %s: %v", cu.Name, err)
	}
	testcases := []struct{
		name string
		filemode int
		group string
	}{
		{
			name: "world read/writeable",
			filemode: 0770,
			group: "-1",
		},
		{
			name: "group read/writeable",
			filemode: 0770,
			group: "-1",
		},
		{
			name: "user read/writeable",
			filemode: 0700,
			group: "-1",
		},
		{
			name: "additional user group as group owner",
			filemode: 0770,
			group: ug[rand.Intn(len(ug))],
		},
	}

	ctx := testctx(t)
	pipe := getTestPipePath(t)
	resp := []byte(`{"Status":0,"StatusMessage":"OK"}`)
	errresp := []byte(`{"Status":1,"StatusMessage":"ERR"}`)
	req := []byte(`{"ArbitraryData":1234,"Command":"TestListen"}`)
	h := func(b []byte) []byte {
		var r testRequest
		err := json.Unmarshal(b, &r)
		if err != nil || r.ArbitraryData != 1234 {
			return errresp
		}
		return resp
	}
	RegisterHandler("TestListen", h)

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T){
			cs := NewCmdServer(pipe, tc.filemode, tc.group, time.Second)
			cs.Start()
			go cs.Wait(ctx)
			for !cs.Listening() {
				time.Sleep(time.Nanosecond)
			}
			d := SendCmdPipe(ctx, pipe, req)
			var r Response
			err := json.Unmarshal(d, &r)
			if err != nil {
				t.Error(err)
			}
			if r.Status != 0 || r.StatusMessage != "OK" {
				t.Errorf("unexpected status from test-cmd, want 0, \"OK\" but got %d, %q", r.Status, r.StatusMessage)
			}
			cs.Close()
			for !cs.Listening() {
				time.Sleep(time.Nanosecond)
			}
		})
	}
}

func TestListenTimeout(t *testing.T) {
	expect := timeoutError
	if runtime.GOOS == "windows" {
		// winio library does not surface timeouts from the underlying net.Conn as
		// timeouts, but as generic errors. Timeouts still work they just can't be
		// detected as timeouts, so they are generic connErrors here.
		expect = connError
	}
	ctx := testctx(t)
	pipe := getTestPipePath(t)
	cs := NewCmdServer(pipe, 0770, "-1", time.Millisecond)
	cs.Start()
	t.Cleanup(cs.Close)
	go cs.Wait(ctx)
	for !cs.Listening() {
		time.Sleep(time.Nanosecond)
	}
	conn, err := dialPipe(ctx, pipe)
	if err != nil {
		t.Errorf("could not connect to command server: %v", err)
	}
	data, err := io.ReadAll(conn)
	if err != nil {
		t.Errorf("error reading response from command server: %v", err)
	}
	if string(data) != string(expect) {
		t.Errorf("unexpected response from timed out connection, got %s but want %s", data, expect)
	}
}

func TestHandlerRegistration(t *testing.T) {
	handlers = make(map[string]Handler)
	noop := func([]byte) []byte { return nil }
	ctx := testctx(t)
	pipe := getTestPipePath(t)
	cmdserver = NewCmdServer(pipe, 0770, "-1", time.Second)
	t.Cleanup(cmdserver.Close)
	t.Cleanup(func() { cmdserver = nil })
	go cmdserver.Wait(ctx)
	time.Sleep(time.Millisecond)
	if cmdserver.srv != nil {
		t.Error("Command server is listening before handlers are registered")
	}
	c, err := dialPipe(ctx, pipe)
	if err == nil {
		t.Error("connected to pipe of stopped server")
		c.Close()
	}
	err = RegisterHandler("TestHandlerRegistration", noop)
	if err != nil {
		t.Errorf("Failed to register command handler: %v", err)
	}
	for !cmdserver.Listening() {
		time.Sleep(time.Nanosecond)
	}
	c, err = dialPipe(ctx, pipe)
	if err != nil {
		t.Errorf("could not connect to pipe of listening server: %v", err)
	} else {
		c.Close()
	}
	UnregisterHandler("TestHandlerRegistration")
	for cmdserver.Listening() {
		time.Sleep(time.Nanosecond)
	}
	c, err = dialPipe(ctx, pipe)
	if err == nil {
		t.Error("connected to pipe of stopped server")
		c.Close()
	}
}
