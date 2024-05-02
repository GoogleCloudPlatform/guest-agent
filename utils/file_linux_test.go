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

//go:build linux

package utils

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestSaferWriteFileWithUserAndGroup(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil {
		t.Fatalf("could not get current user: %v", err)
	}
	uid, err := strconv.Atoi(currentUser.Uid)
	if err != nil {
		t.Fatalf("current uid is not an int: %v", err)
	}
	gid, err := strconv.Atoi(currentUser.Gid)
	if err != nil {
		t.Fatalf("current gid is not an int: %v", err)
	}
	currentGroups, err := currentUser.GroupIds()
	if err != nil {
		t.Fatalf("could not get user groups for %s: %v", currentUser.Name, err)
	}
	// Try to use a supplemental group that doesn't match the user's default group
	// for testing purposes.
	for _, grp := range currentGroups {
		if grp != currentUser.Gid {
			var err error
			gid, err = strconv.Atoi(currentUser.Gid)
			if err == nil {
				break
			}
		}
	}
	f := filepath.Join(t.TempDir(), "file")
	want := "test-data"

	if err := SaferWriteFile([]byte(want), f, FileOptions{Perm: 0644, UID: &uid, GID: &gid}); err != nil {
		t.Errorf("SaferWriteFile(%s, %s) failed unexpectedly with err: %+v", "test-data", f, err)
	}

	got, err := os.ReadFile(f)
	if err != nil {
		t.Errorf("os.ReadFile(%s) failed unexpectedly with err: %+v", f, err)
	}
	if string(got) != want {
		t.Errorf("os.ReadFile(%s) = %s, want %s", f, string(got), want)
	}

	i, err := os.Stat(f)
	if err != nil {
		t.Errorf("os.Stat(%s) failed unexpectedly with err: %+v", f, err)
	}
	statT, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		t.Errorf("could not determine owner of %s", f)
	}
	if int(statT.Uid) != uid {
		t.Errorf("unexepected uid, got %d want %d", statT.Uid, uid)
	}
	if int(statT.Gid) != gid {
		t.Errorf("unexepected gid, got %d want %d", statT.Gid, gid)
	}

	if i.Mode().Perm() != 0o644 {
		t.Errorf("SaferWriteFile(%s) set incorrect permissions, os.Stat(%s) = %o, want %o", f, f, i.Mode().Perm(), 0o644)
	}
}
