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

package command

import (
	"os"
	"os/user"
	"path"
	"strconv"
	"syscall"
	"testing"
)

func TestMkdirpWithPerms(t *testing.T) {
	self, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uidself, err := strconv.Atoi(self.Uid)
	if err != nil {
		t.Fatal(err)
	}
	gidself, err := strconv.Atoi(self.Gid)
	if err != nil {
		t.Fatal(err)
	}
	testcases := []struct {
		name     string
		dir      string
		filemode os.FileMode
		uid      int
		gid      int
	}{
		{
			name:     "standard create",
			dir:      path.Join(".", "test"),
			filemode: 020000000700, // 0700 with directory bit
			uid:      uidself,
			gid:      gidself,
		},
		{
			name:     "nested create",
			dir:      path.Join(".", "test", "test2", "test3"),
			filemode: 020000000700,
			uid:      uidself,
			gid:      gidself,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := mkdirpWithPerms(tc.dir, tc.filemode, tc.uid, tc.gid)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(tc.dir) })
			stat, err := os.Stat(tc.dir)
			if err != nil {
				t.Fatalf("directory %s does not exist: %v", tc.dir, err)
			}
			statT, ok := stat.Sys().(*syscall.Stat_t)
			if !ok {
				t.Errorf("could not determine owner of %s", tc.dir)
			}
			if !stat.IsDir() {
				t.Errorf("%s exists and is not a directory", tc.dir)
			}
			if filemode := stat.Mode(); filemode != tc.filemode {
				t.Errorf("incorrect permissions on %s: got %o want %o", tc.dir, filemode, tc.filemode)
			}
			if statT.Uid != uint32(tc.uid) {
				t.Errorf("incorrect owner of %s: got %d want %d", tc.dir, statT.Uid, tc.uid)
			}
			if statT.Gid != uint32(tc.gid) {
				t.Errorf("incorrect group owner of %s: got %d want %d", tc.dir, statT.Gid, tc.gid)
			}
		})
	}
}
