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
	"fmt"
	"io"
	"math/rand"
	"os/user"
	"path"
	"runtime"
	"testing"
	"time"
)

func getTestPipePath(t *testing.T) string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\google-guest-agent-network-events-test-` + t.Name()
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

func TestClose(t *testing.T) {
	ctx := testctx(t)
	cs := newCmdServer(getTestPipePath(t), 0770, "-1", time.Second)
	err := cs.Start(ctx)
	if err != nil {
		t.Errorf("unexpected error waiting for commands: %v", err)
	}
	err = cs.Close()
	if err != nil {
		t.Errorf("unexpected error closing server: %v", err)
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
	testcases := []struct {
		name     string
		filemode int
		group    string
	}{
		{
			name:     "world read/writeable",
			filemode: 0777,
			group:    "-1",
		},
		{
			name:     "group read/writeable",
			filemode: 0770,
			group:    "-1",
		},
		{
			name:     "user read/writeable",
			filemode: 0700,
			group:    "-1",
		},
		{
			name:     "additional user group as group owner",
			filemode: 0770,
			group:    ug[rand.Intn(len(ug))],
		},
	}

	ctx := testctx(t)
	resp := []byte(`{"Status":0,"StatusMessage":"OK"}`)
	errresp := []byte(`{"Status":1,"StatusMessage":"ERR"}`)
	req := []byte(`{"ArbitraryData":1234,"Command":"TestListen"}`)
	h := func(b []byte) ([]byte, error) {
		var r testRequest
		err := json.Unmarshal(b, &r)
		if err != nil || r.ArbitraryData != 1234 {
			return errresp, nil
		}
		return resp, nil
	}
	RegisterHandler("TestListen", h)

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			pipe := getTestPipePath(t)
			cs := newCmdServer(pipe, tc.filemode, tc.group, time.Second)
			err := cs.Start(ctx)
			if err != nil {
				t.Errorf("could not start server: %v", err)
			}
			d := SendCmdPipe(ctx, pipe, req)
			var r Response
			err = json.Unmarshal(d, &r)
			if err != nil {
				t.Error(err)
			}
			if r.Status != 0 || r.StatusMessage != "OK" {
				t.Errorf("unexpected status from test-cmd, want 0, \"OK\" but got %d, %q", r.Status, r.StatusMessage)
			}
			cs.Close()
		})
	}
}

func TestHandlerFailure(t *testing.T) {
	ctx := testctx(t)
	req := []byte(`{"Command":"TestHandlerFailure"}`)
	h := func(b []byte) ([]byte, error) {
		return nil, fmt.Errorf("always fail")
	}
	RegisterHandler("TestHandlerFailure", h)

	pipe := getTestPipePath(t)
	cs := newCmdServer(pipe, 0777, "-1", time.Second)
	err := cs.Start(ctx)
	if err != nil {
		t.Errorf("could not start server: %v", err)
	}
	d := SendCmdPipe(ctx, pipe, req)
	var r Response
	err = json.Unmarshal(d, &r)
	if err != nil {
		t.Error(err)
	}
	if r.Status != HandlerError.Status || r.StatusMessage != "always fail" {
		t.Errorf("unexpected status from TestHandlerFailure, want %d, \"always fail\" but got %d, %q", HandlerError.Status, r.Status, r.StatusMessage)
	}
	cs.Close()
}

func TestListenTimeout(t *testing.T) {
	expect, err := json.Marshal(TimeoutError)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		// winio library does not surface timeouts from the underlying net.Conn as
		// timeouts, but as generic errors. Timeouts still work they just can't be
		// detected as timeouts, so they are generic connErrors here.
		expect, err = json.Marshal(ConnError)
		if err != nil {
			t.Fatal(err)
		}
	}
	ctx := testctx(t)
	pipe := getTestPipePath(t)
	cs := newCmdServer(pipe, 0770, "-1", time.Millisecond)
	err = cs.Start(ctx)
	if err != nil {
		t.Errorf("could not start server: %v", err)
	}
	defer cs.Close()
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
