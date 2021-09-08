// +build integration

package main

import (
	"os/exec"
	"strings"
	"testing"
)

const (
	testUser       = "integration-test-user"
	expectedGroups = []string{"adm", "dip", "docker", "lxd", "plugdev", "video", "google-sudoers"}
)

func TestCreateGoogleUser(t *testing.T) {
	if exist, err := userExists(testUser); exist == true {
		t.Errorf("test user should not exist")
	}
	if err := createGoogleUser(testUser); err != nil {
		t.Errorf("failed creating test user")
	}
	if exist, err := userExists(testUser); exist != true || err != nil {
		t.Errorf("tesr user should exist")
	}
	cmd := exec.Command("groups", testUser)
	ret := runCmdOutput(cmd)
	if ret.ExitCode() != 0 {
		t.Errorf("test user should be added to group")
	}
	groups := strings.Split(strings.TrimSpace(strings.Split(ret.Stdout(), ":")[1]), " ")
	for _, group := range groups {
		if !contains(group, expectedGroups) {
			t.Errorf("test user has been added to an unexpected group %s", group)
		}
	}
	for _, expected := range expectedGroups {
		if !contains(expected, groups) {
			t.Errorf("test user has not been added to group %s", expected)
		}
	}
	if err := createGoogleUser(testUser); err == nil {
		t.Errorf("user should exist and return error but not")
	}
}

func TestRemoveGoogleUser(t *testing.T) {
	if exist, err := userExists(testUser); exist == true {
		t.Errorf("test user should not exist")
	}
	if err := createGoogleUser(testUser); err != nil {
		t.Errorf("failed creating test user")
	}
	if err := removeGoogleUser(testUser); err != nil {
		t.Errorf("failed when remove google user")
	}
	if exist, err := userExists(testUser); exist == true {
		t.Errorf("test user should not exist")
	}
	if err := removeGoogleUser(testUser); err == nil {
		t.Errorf("user has been removed and should return error but not")
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
