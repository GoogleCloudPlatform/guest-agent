// Copyright 2023 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package run is a package with utilities for running command and handling results.
package run

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

// Client is the Runner running commands.
var Client RunnerInterface

// Result wraps a command execution result.
type Result struct {
	// Exit code. Set to -1 if we failed to run the command.
	ExitCode int
	// Stderr or err.Error if we failed to run the command.
	StdErr string
	// Stdout or "" if we failed to run the command.
	StdOut string
	// Combined is the process' stdout and stderr combined.
	Combined string
}

// RunnerInterface defines the runner running commands.
type RunnerInterface interface {
	// Quiet runs a command and doesn't return a result, but errors in case of failure.
	Quiet(ctx context.Context, name string, args ...string) error

	// WithOutput runs a command and returns the result.
	WithOutput(ctx context.Context, name string, args ...string) *Result

	// WithOutputTimeout runs a command with a defined timeout and returns its result.
	WithOutputTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) *Result

	// WithCombinedOutput runs a command and returns a result with stderr and stdout
	// combined in the Combined member of Result.
	WithCombinedOutput(ctx context.Context, name string, args ...string) *Result
}

// init initializes the RunClient.
func init() {
	Client = Runner{}
}

// Error return an error containing the stderr content.
func (e Result) Error() string {
	return strings.TrimSuffix(e.StdErr, "\n")
}

// Runner implements the RunnerInterface and represents the runner running commands.
type Runner struct{}

// Quiet runs a command and doesn't return a result, but an error in case of failure.
func (r Runner) Quiet(ctx context.Context, name string, args ...string) error {
	res := execCommand(exec.CommandContext(ctx, name, args...))
	if res.ExitCode != 0 {
		return res
	}
	return nil
}

// WithOutput runs a command and returns the result.
func (r Runner) WithOutput(ctx context.Context, name string, args ...string) *Result {
	return execCommand(exec.CommandContext(ctx, name, args...))
}

// WithOutputTimeout runs a command with a defined timeout and returns its result.
func (r Runner) WithOutputTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) *Result {
	child, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res := execCommand(exec.CommandContext(child, name, args...))
	if child.Err() != nil && errors.Is(child.Err(), context.DeadlineExceeded) {
		res.ExitCode = 124 // By convention
	}

	return res
}

// WithCombinedOutput returns a result with stderr and stdout combined in the Combined
// member of Result.
func (r Runner) WithCombinedOutput(ctx context.Context, name string, args ...string) *Result {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		exitCode := -1
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
		return &Result{
			ExitCode: exitCode,
			StdErr:   err.Error(),
		}
	}

	return &Result{
		Combined: string(output),
	}
}

// Quiet runs the current RunClient's Quiet() function.
func Quiet(ctx context.Context, name string, args ...string) error {
	return Client.Quiet(ctx, name, args...)
}

// WithOutput runs the current RunClient's WithOutput() function.
func WithOutput(ctx context.Context, name string, args ...string) *Result {
	return Client.WithOutput(ctx, name, args...)
}

// WithOutputTimeout runs the current RunClient's WithOutputTimeout() function.
func WithOutputTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) *Result {
	return Client.WithOutputTimeout(ctx, timeout, name, args...)
}

// WithCombinedOutput runs the current RunCLient's WithCombinedOutput function.
func WithCombinedOutput(ctx context.Context, name string, args ...string) *Result {
	return Client.WithCombinedOutput(ctx, name, args...)
}

func execCommand(cmd *exec.Cmd) *Result {
	var stdout, stderr bytes.Buffer

	logger.Debugf("exec: %v", cmd)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return &Result{
				ExitCode: ee.ExitCode(),
				StdOut:   stdout.String(),
				StdErr:   stderr.String(),
			}
		}
		return &Result{
			ExitCode: -1,
			StdErr:   err.Error(),
		}
	}

	return &Result{
		ExitCode: 0,
		StdOut:   stdout.String(),
	}
}
