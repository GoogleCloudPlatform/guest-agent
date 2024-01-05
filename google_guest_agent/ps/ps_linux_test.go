//  Copyright 2024 Google LLC
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

package ps

import (
	"fmt"
	"os"
	"path"
	"testing"
)

// Setup a fake process directory.
func setupProc(t *testing.T, entries []*Process) {
	t.Helper()
	linuxProcDir = path.Join(t.TempDir(), "proc")

	if err := os.MkdirAll(linuxProcDir, 0755); err != nil {
		t.Fatalf("failed to make mocked proc dir: %+v", err)
	}

	if entries == nil {
		return
	}

	for _, curr := range entries {
		procDir := path.Join(linuxProcDir, fmt.Sprintf("%d", curr.Pid))
		if err := os.MkdirAll(procDir, 0755); err != nil {
			t.Fatalf("failed to make process dir: %+v", err)
		}

		// randomFilePath is the path of a random/unknown file in the processe's dir.
		randomFilePath := path.Join(procDir, "random-file")
		err := os.WriteFile(path.Join(randomFilePath), []byte("random\n"), 0644)
		if err != nil {
			t.Fatalf("failed to write random proc file: %+v", err)
		}

		// randomDirPath is the path of a random/unknown dir in the processe's dir.
		randomDirPath := path.Join(procDir, "random-dir")
		if err := os.MkdirAll(randomDirPath, 0755); err != nil {
			t.Fatalf("failed to make random dir in the proc dir: %+v", err)
		}

		if curr.Exe != "" {
			exeLinkPath := path.Join(procDir, "exe")
			if err := os.Symlink(curr.Exe, exeLinkPath); err != nil {
				t.Fatalf("failed to create exe sym link: %+v", err)
			}
		}

		if len(curr.CommandLine) > 0 {
			cmdlineFilePath := path.Join(procDir, "cmdline")

			var data []byte
			for _, line := range curr.CommandLine {
				data = append(data, []byte(line)...)
				data = append(data, 0)
			}

			err := os.WriteFile(cmdlineFilePath, data, 0644)
			if err != nil {
				t.Fatalf("failed to write random proc file: %+v", err)
			}
		}
	}
}

// Undo the changes made by each test.
func tearDown(t *testing.T) {
	t.Helper()
	linuxProcDir = defaultLinuxProcDir
}

// TestEmptyProcDir tests if Find correctly returns nil if the process directory is empty.
func TestEmptyProcDir(t *testing.T) {
	setupProc(t, nil)
	procs, err := Find(".*dhclient.*")
	if err != nil {
		t.Fatalf("ps.Find() returned error: %+v, expected: nil", err)
	}

	if procs != nil {
		t.Fatalf("ps.Find() returned: %+v, expected: nil", procs)
	}
	tearDown(t)
}

// TestMalformedExe tests if Find correctly returns nil if the process contains a bad exe.
func TestMalformedExe(t *testing.T) {
	procs := []*Process{
		&Process{1, "", nil},
	}
	setupProc(t, procs)

	res, err := Find(".*dhclient.*")
	if err != nil {
		t.Fatalf("ps.Find() returned error: %+v, expected: nil", err)
	}

	if res != nil {
		t.Fatalf("ps.Find() returned: %+v, expected: nil", res)
	}
	tearDown(t)
}

// TestFind tests whether Find() can find existing processes, and that it can
// return a nil response if the process does not exist.
func TestFind(t *testing.T) {
	tests := []struct {
		success bool
		expr    string
	}{
		{true, ".*dhclient.*"},
		{true, ".*google_guest_agent.*"},
		{false, ".*dhclientx.*"},
		{false, ".*google_guest_agentx.*"},
	}

	procs := []*Process{
		&Process{1, "/usr/bin/dhclient", []string{"dhclient", "eth0"}},
		&Process{2, "/usr/bin/google_guest_agent", []string{"google_guest_agent"}},
	}
	setupProc(t, procs)

	for i, curr := range tests {
		t.Run(fmt.Sprintf("test-%d-%s", i, curr.expr), func(t *testing.T) {
			res, err := Find(curr.expr)
			if curr.success {
				if err != nil {
					t.Errorf("ps.Find() returned error: %+v, expected: nil", err)
				}

				if res == nil {
					t.Fatalf("ps.Find() returned: nil, expected: non-nil")
				}
			} else {
				if res != nil {
					t.Fatalf("ps.Find() returned: non-nil, expected: nil")
				}
			}
		})
	}

	tearDown(t)
}
