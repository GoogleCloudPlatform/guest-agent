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

// Package ps provides a way to find a process in linux without using the ps CLI tool.
package ps

// Client for finding processes.
var Client ProcessInterface

// Process describes an OS process.
type Process struct {
	// Pid is the process id.
	Pid int

	// Exe is the path of the processes executable file.
	Exe string

	// CommandLine contains the processes executable path and its command
	// line arguments (honoring the order they were presented when executed).
	CommandLine []string
}

// ProcessInterface is the minimum required Ps interface for Guest Agent.
type ProcessInterface interface {
	Find(exeMatch string) ([]Process, error)
}
