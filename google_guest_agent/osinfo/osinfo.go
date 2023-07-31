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

package osinfo

import (
	"fmt"
)

// OSInfo contains OS information about the system.
type OSInfo struct {
	// OS name in short form.
	OS string
	// OS version ID.
	VersionID string
	// The name the OS uses to fully describe itself.
	PrettyName string
	// The kernel release.
	KernelRelease string
	// The kernel version.
	KernelVersion string

	// This is used by oslogin.go
	Version Ver
}

// Ver describes the system version
type Ver struct {
	Major, Minor, Patch, Length int
}

func (v Ver) String() string {
	if v.Major == 0 {
		return ""
	}
	ret := fmt.Sprintf("%d", v.Major)
	if v.Length > 1 {
		ret = fmt.Sprintf("%s.%d", ret, v.Minor)
	}
	if v.Length > 2 {
		ret = fmt.Sprintf("%s.%d", ret, v.Patch)
	}
	return ret
}
