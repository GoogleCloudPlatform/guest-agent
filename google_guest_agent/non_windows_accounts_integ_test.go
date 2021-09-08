// +build integration

package main

import (
	"os/exec"
	"strings"
	"testing"
)

const testUser = "integration-test-user"

func TestCreateGoogleUser(t *testing.T) {
	if err := createGoogleUser(testUser); err != nil {
		t.Errorf("failed creating test user")
	}
	if exist, err := userExists(testUser); exist != true || err != nil {
		t.Errorf("tesr user %s should exist", testUser)
	}
	cmd := exec.Command("groups", testUser)
	out := runCmdOutput(cmd).Stdout()
	groups := strings.Split(strings.TrimSpace(strings.Split(out, ":")[1]), " ")
	for _, group := range groups {
		if !strings.Contains("adm,dip,docker,lxd,plugdev,video,google-sudoers", group) {
			t.Errorf("test user has been added to an unexpected group")
		}
	}
	if err := createGoogleUser(testUser); err == nil {
		t.Errorf("expected user exist and return error but not")
	}
}

func TestRemoveGoogleUser(t *testing.T) {
	if err := createGoogleUser(testUser); err != nil {
		t.Errorf("failed creating test user")
	}
	if err := removeGoogleUser(testUser); err != nil {
		t.Errorf("failed when remove google user")
	}
	cmd := exec.Command("groups", testUser)
	ret := runCmdOutput(cmd)
	if ret.ExitCode() != 1 {
		t.Errorf("expected groups command return error code 1, but actually error is %d, %s", ret.ExitCode(), ret.Stderr())
	}
	if err := removeGoogleUser(testUser); err == nil {
		t.Errorf("expected user has been removed and return error but not")
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