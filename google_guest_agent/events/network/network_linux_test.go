//go:build linux

// Copyright 2023 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package network

import (
	"context"
	"net"
	"os"
	"path"
)

var testPipe string

func getTestPipePath() (string, error) {
	if testPipe == "" {
		tmpdir, err := os.MkdirTemp(os.TempDir(), "google-guest-agent-network-events-test")
		if err != nil {
			return "", err
		}
		testPipe = path.Join(tmpdir, "run", "pipe")
	}
	return testPipe, nil
}

func dialTestPipe(ctx context.Context, pipe string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", pipe)
}
