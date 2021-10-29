//  Copyright 2021 Google LLC
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

// +build integration

package main

import (
	"context"
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

// Agent will:
// * systemd-notify --ready
// * start snapshot listener (if enabled)
// * run scripts
// * set IO scheduler
//
// If network enabled, agent will also:
//   * set metadata global
//   * sysctl overcommit (e2 only)
//
//   If instance ID missing or changes, will also:
//     * write instance ID (if missing)
//     * set host keys (if enabled)
//     * set boto config (if enabled)
//     * write instance ID (again)

func TestInstanceSetupSSHKeys(t *testing.T) {
	cfg, err := parseConfig("") // get empty config
	if err != nil {
		t.Fatal("failed to init config object")
	}
	config = cfg // set the global
	tempdir, err := ioutil.TempDir("test_instance_setup")
	if err != nil {
		t.Fatal("failed to create working dir")
	}

	// Configure a non-standard instance ID dir for us to play with.
	config.Section("Instance").Key("instance_id_dir").SetValue(tempdir)
	config.Section("InstanceSetup").Key("host_key_dir").SetValue(tempdir)

	ctx := context.Background()
	agentInit(ctx)

	// Confirm instance ID file was written
	if _, err := os.Stat(tempdir + "/google_instance_id"); err != nil {
		t.Fatal("instance ID File was not created by agentInit")
	}

	dir, err := os.Open(tempdir)
	if err != nil {
		t.Fatal("failed to open working dir")
	}
	defer dir.Close()

	files, err := dir.Readdirnames(0)
	if err != nil {
		t.Fatal("failed to read files")
	}

	var keys []string
	for _, file := range files {
		if strings.HasPrefix(file, "ssh_host_") {
			keys = append(keys, file)
		}
	}

	if len(keys) == 0 {
		t.Fatal("instance setup didn't create SSH host keys")
	}

	// Remove one key file and run again to confirm SSH keys have not
	// changed because the instance ID file has not changed.

	t.Logf("got keys %v, remove key %q\n", keys, keys[0])
	if err := os.Remove(tempdir + "/" + keys[0]); err != nil {
		t.Fatal("failed to remove key file")
	}

	agentInit(ctx)

	if _, err := dir.Seek(0, 0); err != nil {
		t.Fatal("failed to seek dir for second check")
	}

	files, err := dir.Readdirnames(0)
	if err != nil {
		t.Fatal("failed to read files")
	}

	var keys2 []string
	for _, file := range files {
		if strings.HasPrefix(file, "ssh_host_") {
			keys2 = append(keys, file)
		}
		if file == keys[0] {
			t.Fatal("agent recreated keys after boot")
		}
	}
	t.Logf("got keys2 %v\n", keys)

	if len(keys) == len(keys2) {
		t.Fatal("agent recreated keys after boot")
	}
}

func TestInstanceSetupBotoConfig(t *testing.T) {
}
