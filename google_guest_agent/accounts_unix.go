// Copyright 2019 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"syscall"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
)

func getUID(path string) string {
	if dir, err := os.Stat(path); err == nil {
		if stat, ok := dir.Sys().(*syscall.Stat_t); ok {
			return fmt.Sprintf("%d", stat.Uid)
		}
	}
	return ""
}

func createUser(ctx context.Context, username, uid string) error {
	config := cfg.Get()
	useradd := config.Accounts.UserAddCmd
	if uid != "" {
		useradd = fmt.Sprintf("%s -u %s", useradd, uid)
	}
	cmd, args := createUserGroupCmd(useradd, username, "")
	return run.Quiet(ctx, cmd, args...)
}

func addUserToGroup(ctx context.Context, user, group string) error {
	config := cfg.Get()
	gpasswdadd := config.Accounts.GPasswdAddCmd
	cmd, args := createUserGroupCmd(gpasswdadd, user, group)
	return run.Quiet(ctx, cmd, args...)
}

func userExists(name string) (bool, error) {
	if _, err := user.Lookup(name); err != nil {
		return false, err
	}
	return true, nil
}
