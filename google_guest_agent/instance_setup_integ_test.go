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

const (
	botoCfg = "/etc/boto.cfg"
)

// TestInstanceSetupSSHKeys validates SSH keys are generated on first boot and not changed afterward.
func TestInstanceSetupSSHKeys(t *testing.T) {
	cfg, err := parseConfig("") // get empty config
	if err != nil {
		t.Fatal("failed to init config object")
	}
	config = cfg                    // set the global
	defer func() { config = nil }() // unset at end of test

	tempdir, err := ioutil.TempDir("/tmp", "test_instance_setup")
	if err != nil {
		t.Fatal("failed to create working dir")
	}

	// Configure a non-standard instance ID dir for us to play with.
	config.Section("Instance").Key("instance_id_dir").SetValue(tempdir)
	config.Section("InstanceSetup").Key("host_key_dir").SetValue(tempdir)

	ctx := context.Background()
	agentInit(ctx)

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
	if err := os.Remove(tempdir + "/" + keys[0]); err != nil {
		t.Fatal("failed to remove key file")
	}

	agentInit(ctx)

	if _, err := dir.Seek(0, 0); err != nil {
		t.Fatal("failed to rewind dir for second check")
	}
	files2, err := dir.Readdirnames(0)
	if err != nil {
		t.Fatal("failed to read files")
	}

	var keys2 []string
	for _, file := range files2 {
		if strings.HasPrefix(file, "ssh_host_") {
			keys2 = append(keys2, file)
		}
		if file == keys[0] {
			t.Fatalf("agentInit recreated key %s", file)
		}
	}

	if len(keys) == len(keys2) {
		t.Fatal("agentInit recreated SSH host keys")
	}
}

// TestInstanceSetupSSHKeysDisabled validates the config option to disable host
// key generation is respected.
func TestInstanceSetupSSHKeysDisabled(t *testing.T) {
	cfg, err := parseConfig("") // get empty config
	if err != nil {
		t.Fatal("failed to init config object")
	}
	config = cfg                    // set the global
	defer func() { config = nil }() // unset at end of test

	tempdir, err := ioutil.TempDir("/tmp", "test_instance_setup")
	if err != nil {
		t.Fatal("failed to create working dir")
	}

	// Configure a non-standard instance ID dir for us to play with.
	config.Section("Instance").Key("instance_id_dir").SetValue(tempdir)
	config.Section("InstanceSetup").Key("host_key_dir").SetValue(tempdir)

	// Disable SSH host key generation.
	config.Section("InstanceSetup").Key("set_host_keys").SetValue("false")

	ctx := context.Background()
	agentInit(ctx)

	dir, err := os.Open(tempdir)
	if err != nil {
		t.Fatal("failed to open working dir")
	}
	defer dir.Close()

	files, err := dir.Readdirnames(0)
	if err != nil {
		t.Fatal("failed to read files")
	}

	for _, file := range files {
		if strings.HasPrefix(file, "ssh_host_") {
			t.Fatal("agentInit created SSH host keys when disabled")
		}
	}
}

func TestInstanceSetupBotoConfig(t *testing.T) {
	cfg, err := parseConfig("") // get empty config
	if err != nil {
		t.Fatal("failed to init config object")
	}
	config = cfg                    // set the global
	defer func() { config = nil }() // unset at end of test

	tempdir, err := ioutil.TempDir("/tmp", "test_instance_setup")
	if err != nil {
		t.Fatal("failed to create working dir")
	}

	// Configure a non-standard instance ID dir for us to play with.
	config.Section("Instance").Key("instance_id_dir").SetValue(tempdir)
	config.Section("InstanceSetup").Key("host_key_dir").SetValue(tempdir)

	ctx := context.Background()

	if err := os.Rename(botoCfg, botoCfg+".bak"); err != nil {
		t.Fatalf("failed to move boto config: %v", err)
	}
	defer func() {
		// Restore file at end of test.
		if err := os.Rename(botoCfg+".bak", botoCfg); err != nil {
			t.Fatalf("failed to restore boto config: %v", err)
		}
	}()

	// Test it is created by default on first boot
	agentInit(ctx)
	if _, err := os.Stat(botoCfg); err != nil {
		t.Fatal("boto config was not created on first boot")
	}

	// Test it is not recreated on subsequent invocations
	if err := os.Remove(botoCfg); err != nil {
		t.Fatal("failed to remove boto config")
	}
	agentInit(ctx)
	if _, err := os.Stat(botoCfg); err == nil || !os.IsNotExist(err) {
		// If we didn't get an error, or if we got some other kind of error
		t.Fatal("boto config was recreated after first boot")
	}
}

func TestInstanceSetupBotoConfigDisabled(t *testing.T) {
	cfg, err := parseConfig("") // get empty config
	if err != nil {
		t.Fatal("failed to init config object")
	}
	config = cfg                    // set the global
	defer func() { config = nil }() // unset at end of test

	tempdir, err := ioutil.TempDir("/tmp", "test_instance_setup")
	if err != nil {
		t.Fatal("failed to create working dir")
	}

	// Configure a non-standard instance ID dir for us to play with.
	config.Section("Instance").Key("instance_id_dir").SetValue(tempdir)
	config.Section("InstanceSetup").Key("host_key_dir").SetValue(tempdir)

	ctx := context.Background()

	if err := os.Rename(botoCfg, botoCfg+".bak"); err != nil {
		t.Fatalf("failed to move boto config: %v", err)
	}
	defer func() {
		// Restore file at end of test.
		if err := os.Rename(botoCfg+".bak", botoCfg); err != nil {
			t.Fatalf("failed to restore boto config: %v", err)
		}
	}()

	// Test it is not created if disabled in config.
	config.Section("InstanceSetup").Key("set_boto_config").SetValue("false")
	agentInit(ctx)

	if _, err := os.Stat(botoCfg); err == nil || !os.IsNotExist(err) {
		// If we didn't get an error, or if we got some other kind of error
		t.Fatal("boto config was created when disabled in config")
	}
}
