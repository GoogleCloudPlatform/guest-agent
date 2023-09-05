//  Copyright 2023 Google Inc. All Rights Reserved.
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

package run

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
	"time"
)

// builds a set of data to be used in the tests
func buildDataContent(t *testing.T) string {
	t.Helper()
	rootDir := path.Join(t.TempDir(), fmt.Sprintf("%d", time.Now().UnixNano()))
	if err := os.MkdirAll(rootDir, 0744); err != nil {
		t.Fatalf("Failed to make test dir: %+v", err)
	}

	if err := os.WriteFile(path.Join(rootDir, "data"), []byte("random data"), 0644); err != nil {
		t.Fatalf("Failed to write test data: %+v", err)
	}

	return rootDir
}

func TestQuietSuccess(t *testing.T) {
	testDir := buildDataContent(t)
	tests := []string{
		"grep -R data " + testDir,
		"echo 'foobar' >> " + path.Join(testDir, "foobar"),
		"rm -Rf " + path.Join(testDir, "foobar"),
		"echo",
	}

	for _, curr := range tests {
		t.Run(curr, func(t *testing.T) {
			tokens := strings.Split(curr, " ")
			if err := Quiet(context.Background(), tokens[0], tokens[1:]...); err != nil {
				t.Errorf("run.Quiet(%s) failed with error: %+v, expected success.", curr, err)
			}
		})
	}
}

func TestQuietFail(t *testing.T) {
	testDir := buildDataContent(t)
	tests := []string{
		"grep -R datax " + testDir,
		"rm -R /root/data",
	}

	for _, curr := range tests {
		t.Run(curr, func(t *testing.T) {
			tokens := strings.Split(curr, " ")
			if err := Quiet(context.Background(), tokens[0], tokens[1:]...); err == nil {
				t.Errorf("run.Quiet(%s) command succeed, expected failure.", curr)
			}
		})
	}
}

func TestOutputSuccess(t *testing.T) {
	testDir := buildDataContent(t)
	tests := []struct {
		cmd    string
		output string
	}{
		{"grep -R data " + testDir, path.Join(testDir, "data") + ":random data\n"},
		{"echo foobar", "foobar\n"},
		{"echo -n foobar", "foobar"},
		{"cat " + path.Join(testDir, "data"), "random data"},
	}

	for _, curr := range tests {
		t.Run(curr.cmd, func(t *testing.T) {
			tokens := strings.Split(curr.cmd, " ")
			res := WithOutput(context.Background(), tokens[0], tokens[1:]...)
			if res.ExitCode != 0 {
				t.Errorf("run.WithOutput(%s) failed with exitCode: %b, expected success.", curr, res.ExitCode)
			}
			if res.StdOut != curr.output {
				t.Errorf("run.WithOutput(%s) failed with stdout: %s, expected empty stdout.", curr.cmd, res.StdOut)
			}
		})
	}
}

func TestOutputFail(t *testing.T) {
	testDir := buildDataContent(t)
	tests := []string{
		"grep -R foobar " + testDir,
		"cat /root/foobar",
	}

	for _, curr := range tests {
		t.Run(curr, func(t *testing.T) {
			tokens := strings.Split(curr, " ")
			res := WithOutput(context.Background(), tokens[0], tokens[1:]...)
			if res.ExitCode == 0 {
				t.Errorf("run.WithOutput(%s) command succeeded, expected failure.", curr)
			}
		})
	}
}

func TestCombinedOutputSuccess(t *testing.T) {
	testDir := buildDataContent(t)
	tests := []struct {
		cmd    string
		output string
	}{
		{"grep -R data " + testDir, path.Join(testDir, "data") + ":random data\n"},
		{"echo foobar", "foobar\n"},
		{"echo -n foobar", "foobar"},
		{"cat " + path.Join(testDir, "data"), "random data"},
	}

	for _, curr := range tests {
		t.Run(curr.cmd, func(t *testing.T) {
			tokens := strings.Split(curr.cmd, " ")
			res := WithCombinedOutput(context.Background(), tokens[0], tokens[1:]...)
			if res.ExitCode != 0 {
				t.Errorf("run.WithCombinedOutput(%s) failed with exitCode: %b, expected success.", curr, res.ExitCode)
			}
			if res.Combined != curr.output {
				t.Errorf("run.WithCombinedOutput(%s) failed with stdout: %s, expected empty stdout.", curr.cmd, res.StdOut)
			}
		})
	}
}

func TestCombinedOutputFail(t *testing.T) {
	testDir := buildDataContent(t)
	tests := []string{
		"grep -R foobar " + testDir,
		"cat /root/foobar",
	}

	for _, curr := range tests {
		t.Run(curr, func(t *testing.T) {
			tokens := strings.Split(curr, " ")
			res := WithCombinedOutput(context.Background(), tokens[0], tokens[1:]...)
			if res.ExitCode == 0 {
				t.Errorf("run.WithCombinedoutput(%s) command succeeded, expected failure.", curr)
			}
		})
	}
}

func TestOutputTimeoutSuccess(t *testing.T) {
	testDir := buildDataContent(t)
	tests := []struct {
		cmd    string
		output string
	}{
		{"grep -R data " + testDir, path.Join(testDir, "data") + ":random data\n"},
		{"echo foobar", "foobar\n"},
		{"echo -n foobar", "foobar"},
		{"cat " + path.Join(testDir, "data"), "random data"},
	}

	for _, curr := range tests {
		t.Run(curr.cmd, func(t *testing.T) {
			tokens := strings.Split(curr.cmd, " ")
			res := WithOutputTimeout(context.Background(), 1*time.Second, tokens[0], tokens[1:]...)
			if res.ExitCode != 0 {
				t.Errorf("run.WithOutputTimeout(%s) command failed with exitcode: %d, expected 0.", curr, res.ExitCode)
			}
			if res.StdOut != curr.output {
				t.Errorf("run.WithOutputTimeout(%s) command failed with stdout: %s, expected empty stdout.", curr.cmd, res.StdOut)
			}
		})
	}
}

func TestOutputTimeoutFail(t *testing.T) {
	testDir := buildDataContent(t)
	tests := []string{
		"grep -R foobar " + testDir,
		"cat /root/foobar",
	}

	for _, curr := range tests {
		t.Run(curr, func(t *testing.T) {
			tokens := strings.Split(curr, " ")
			res := WithOutputTimeout(context.Background(), 1*time.Second, tokens[0], tokens[1:]...)
			if res.ExitCode == 0 {
				t.Errorf("run.WithOutputTimeout(%s) command succeeded, expected failure.", curr)
			}
		})
	}
}
