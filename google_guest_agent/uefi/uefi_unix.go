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

//go:build unix

// Package uefi provides utility functions to read UEFI variables.
package uefi

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	defaultEFIVarsDir = "/sys/firmware/efi/efivars"
)

// Path returns a path for UEFI variable on disk.
func (v VariableName) Path() string {
	root := v.RootDir
	if root == "" {
		root = defaultEFIVarsDir
	}
	return filepath.Join(root, v.Name+"-"+v.GUID)
}

// ReadVariable reads UEFI variable and returns as byte array.
// Throws an error if variable is invalid or empty.
func ReadVariable(v VariableName) (*Variable, error) {
	path := v.Path()
	b, err := os.ReadFile(path)

	if err != nil {
		return nil, fmt.Errorf("error reading %q: %v", path, err)
	}

	// According to UEFI specification the first four bytes of the contents are attributes.
	if len(b) < 4 {
		return nil, fmt.Errorf("%q contains %d bytes of data, it should have at least 4", path, len(b))
	}

	return &Variable{
		Name:       v,
		Attributes: b[:4],
		Content:    b[4:],
	}, nil
}
