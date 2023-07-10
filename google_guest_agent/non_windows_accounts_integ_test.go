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

//go:build integration
// +build integration

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const (
	testUser           = "integration-test-user"
	defaultgroupstring = "adm,dip,docker,lxd,plugdev,video,google-sudoers"
)

func TestCreateAndRemoveGoogleUser(t *testing.T) {
	if exist, err := userExists(testUser); err != nil && exist {
		t.Fatalf("test user should not exist")
	}
	if err := createGoogleUser(testUser); err != nil {
		t.Errorf("createGoogleUser failed creating test user")
	}
	if exist, err := userExists(testUser); exist != true || err != nil {
		t.Errorf("test user should exist")
	}
	cmd := exec.Command("groups", testUser)
	ret := runCmdOutput(cmd)
	if ret.ExitCode() != 0 {
		t.Errorf("failed looking up groups for user: stdout:%s stderr:%s", ret.Stdout(), ret.Stderr())
	}
	groups := strings.Split(strings.TrimSpace(strings.Split(ret.Stdout(), ":")[1]), " ")
	expectedGroupString := config.Section("Accounts").Key("groups").MustString(defaultgroupstring)
	expectedGroups := strings.Split(expectedGroupString, ",")
	for _, group := range groups {
		if !contains(group, expectedGroups) {
			t.Errorf("test user has been added to an unexpected group %s", group)
		}
	}
	if _, err := os.Stat(fmt.Sprintf("/home/%s", testUser)); err != nil {
		t.Errorf("test user home directory does not exist")
	}
	if err := createGoogleUser(testUser); err == nil {
		t.Errorf("createGoogleUser did not return error when creating user that already exists")
	}
	if err := removeGoogleUser(testUser); err != nil {
		t.Errorf("removeGoogleUser did not remove user")
	}
	if exist, err := userExists(testUser); err != nil && exist == true {
		t.Errorf("test user should not exist")
	}
	if err := removeGoogleUser(testUser); err == nil {
		t.Errorf("removeGoogleUser did not return error when removing user that doesn't exist")
	}
}

func TestGroupaddDuplicates(t *testing.T) {
	cmd := exec.Command("groupadd", "integ-test-group")
	ret := runCmdOutput(cmd)
	if ret.ExitCode() != 0 {
		t.Fatalf("got wrong exit code running \"groupadd integ-test-group\", expected 0 got %v\n", ret.ExitCode())
	}
	cmd = exec.Command("groupadd", "integ-test-group")
	ret = runCmdOutput(cmd)
	if ret.ExitCode() != 9 {
		t.Fatalf("got wrong exit code running \"groupadd integ-test-group\", expected 9 got %v\n", ret.ExitCode())
	}
}

func contains(target string, expected []string) bool {
	for _, e := range expected {
		if e == target {
			return true
		}
	}
	return false
}
