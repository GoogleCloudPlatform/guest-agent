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

package main

import (
	"fmt"
)

type info struct {
	os            string
	versionID     string
	prettyName    string
	kernelRelease string
	kernelVersion string

	// This is used by oslogin.go
	version ver
}

type ver struct {
	major, minor, patch, length int
}

func (v ver) String() string {
	if v.major == 0 {
		return ""
	}
	ret := fmt.Sprintf("%d", v.major)
	if v.length > 1 {
		ret = fmt.Sprintf("%s.%d", ret, v.minor)
	}
	if v.length > 2 {
		ret = fmt.Sprintf("%s.%d", ret, v.patch)
	}
	return ret
}
