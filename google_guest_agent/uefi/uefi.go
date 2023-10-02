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

// Package uefi provides utility functions to read UEFI variables.
package uefi

// VariableName represents UEFI variable name and GUID.
// Format: {VariableName}-{VendorGUID}
type VariableName struct {
	RootDir string
	Name    string
	GUID    string
}

// Variable represents UEFI Variable and its contents.
// Attributes are not set in case of Windows.
type Variable struct {
	Name       VariableName
	Attributes []byte
	Content    []byte
}
