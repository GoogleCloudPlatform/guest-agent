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
	"regexp"
	"strconv"
)

// LinuxClient is for finding processes on linux distributions.
type LinuxClient struct{}

const (
	// defaultLinuxProcDir is the default location of proc filesystem mount point in
	// a linux system.
	defaultLinuxProcDir = "/proc/"
)

var (
	// linuxProcDir is the location of proc filesystem mout point currently set up
	// in the current execution. Unit tests may want to adjust it in order to simulate
	// the target system.
	linuxProcDir = defaultLinuxProcDir
)

// init creates the Linux process finder.
func init() {
	Client = &LinuxClient{}
}

// Find finds all processes with the executable path matching the provided regex.
func (p LinuxClient) Find(exeMatch string) ([]Process, error) {
	var result []Process

	procExpression, err := regexp.Compile("^[0-9]*$")
	if err != nil {
		return nil, fmt.Errorf("failed to compile process dir expression: %+v", err)
	}

	exeExpression, err := regexp.Compile(exeMatch)
	if err != nil {
		return nil, fmt.Errorf("failed to compile process exec matching expression: %+v", err)
	}

	files, err := os.ReadDir(linuxProcDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read linux proc dir: %+v", err)
	}

	for _, file := range files {
		if !file.IsDir() {
			continue
		}

		if !procExpression.MatchString(file.Name()) {
			continue
		}

		processRootDir := path.Join(linuxProcDir, file.Name())
		exeLinkPath := path.Join(processRootDir, "exe")

		exePath, err := os.Readlink(exeLinkPath)
		if err != nil {
			continue
		}

		if !exeExpression.MatchString(exePath) {
			continue
		}

		cmdlinePath := path.Join(processRootDir, "cmdline")
		dat, err := os.ReadFile(cmdlinePath)
		if err != nil {
			return nil, fmt.Errorf("error reading cmdline file: %v", err)
		}

		var commandLine []string
		var token []byte
		for _, curr := range dat {
			if curr == 0 {
				commandLine = append(commandLine, string(token))
				token = nil
			} else {
				token = append(token, curr)
			}
		}

		pid, err := strconv.Atoi(file.Name())
		if err != nil {
			return nil, fmt.Errorf("error parsing PID: %v", err)
		}

		result = append(result, Process{
			Pid:         pid,
			Exe:         exePath,
			CommandLine: commandLine,
		})
	}

	return result, nil
}

// Find finds all processes with the executable matching the provided regex.
func Find(exeMatch string) ([]Process, error) {
	return Client.Find(exeMatch)
}
