// build: integration

package main

import (
	"os/exec"
	"testing"
)

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
