// Copyright 2024 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/command"
)

func pipePathForTest(t *testing.T) string {
	pipe := filepath.Join(t.TempDir(), "ggacli-test", "commands.sock")
	if runtime.GOOS == "windows" {
		pipe = `\\.\pipe\ggacli-test-commands`
	}
	return pipe
}

func setupCommandServerForTest(ctx context.Context, t *testing.T) {
	cfg.Load(nil)
	cfg.Get().Unstable = &cfg.Unstable{
		CommandMonitorEnabled: true,
		CommandPipePath:       pipePathForTest(t),
		CommandRequestTimeout: "1s",
	}
	command.Init(ctx)
	t.Cleanup(func() {
		if err := command.Close(); err != nil {
			t.Errorf("error closing command server: %v", err)
		}
	})
}

func TestFind(t *testing.T) {
	ctx := context.Background()
	as := ActionSet{
		"testaction": {
			fn: func(context.Context) (string, int) { return "", 0 },
		},
	}
	testaction := as.Find("testaction")
	if _, i := testaction(ctx); i != 0 {
		t.Errorf("testaction has unexpected exit code, got %d want 0", i)
	}
	missingaction := as.Find("missingaction")
	if _, i := missingaction(ctx); i != 1 {
		t.Errorf("missingaction has unexpected exit code, got %d want 1", i)
	}
}

func TestSendcmd(t *testing.T) {
	resp := []byte(`{"Status":2}`)
	handler := func([]byte) ([]byte, error) {
		return resp, nil
	}
	msg := `{"Command":"sendcmd"}`
	jsonPayload = &msg
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	setupCommandServerForTest(ctx, t)
	command.Get().RegisterHandler("sendcmd", handler)
	t.Cleanup(func() { command.Get().UnregisterHandler("sendcmd") })
	r, code := sendcmd(ctx)
	if r != string(resp) {
		t.Errorf("unexpected response from sendcmd, got %s want %s", r, resp)
	}
	if code != 2 {
		t.Errorf("unexpected exit code from sendcmd, got %d want 2", code)
	}
}

func TestCheckSocket(t *testing.T) {
	path := pipePathForTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg.Get().Unstable = &cfg.Unstable{CommandPipePath: path}
	_, i := checksocket(ctx)
	if i != 1 {
		t.Errorf("checksocket reported non-existent socket %s is OK", path)
	}
	setupCommandServerForTest(ctx, t)
	_, i = checksocket(ctx)
	if i != 0 {
		t.Errorf("checksocket reported socket %s does not exist", path)
	}
}
