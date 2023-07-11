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

package osinfo

import (
	"bytes"
	"io/ioutil"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"golang.org/x/sys/unix"
)

func parseOSRelease(osRelease string) OSInfo {
	var ret OSInfo
	for _, line := range strings.Split(osRelease, "\n") {
		var id = line
		if id = strings.TrimPrefix(line, "ID="); id != line {
			if len(id) > 0 && id[0] == '"' {
				id = id[1:]
			}
			if len(id) > 0 && id[len(id)-1] == '"' {
				id = id[:len(id)-1]
			}
			ret.OS = parseID(id)
		}
		if id = strings.TrimPrefix(line, "VERSION_ID="); id != line {
			if len(id) > 0 && id[0] == '"' {
				id = id[1:]
			}
			if len(id) > 0 && id[len(id)-1] == '"' {
				id = id[:len(id)-1]
			}
			ret.VersionID = id
			version, err := parseVersion(id)
			if err != nil {
				logger.Warningf("Couldn't parse version id: %v", err)
				return ret
			}
			ret.Version = version
		}
	}
	return ret
}

func parseSystemRelease(systemRelease string) OSInfo {
	var ret OSInfo
	var key = " release "
	idx := strings.Index(systemRelease, key)
	if idx == -1 {
		logger.Warningf("SystemRelease does not match expected format")
		return ret
	}
	ret.OS = parseID(systemRelease[:idx])

	versionFromRelease := strings.Split(systemRelease[idx+len(key):], " ")[0]
	version, err := parseVersion(versionFromRelease)
	if err != nil {
		logger.Warningf("Couldn't parse version: %v", err)
		return ret
	}
	ret.Version = version
	return ret
}

func parseVersion(version string) (Ver, error) {
	versionparts := strings.Split(version, ".")
	ret := Ver{Length: len(versionparts)}

	// Must have at least major version.
	var err error
	ret.Major, err = strconv.Atoi(versionparts[0])
	if err != nil {
		return ret, err
	}
	if ret.Length > 1 {
		ret.Minor, err = strconv.Atoi(versionparts[1])
		if err != nil {
			return ret, err
		}
	}
	if ret.Length > 2 {
		ret.Patch, err = strconv.Atoi(versionparts[2])
		if err != nil {
			return ret, err
		}
	}
	return ret, nil
}

func parseID(id string) string {
	switch id {
	case "Red Hat Enterprise Linux Server":
		return "rhel"
	case "CentOS", "CentOS Linux":
		return "centos"
	default:
		return id
	}
}

// Get returns OSInfo on the running system.
func Get() OSInfo {
	var osInfo OSInfo

	releaseFile, err := ioutil.ReadFile("/etc/os-release")
	if err == nil {
		osInfo = parseOSRelease(string(releaseFile))
	} else {
		releaseFile, err = ioutil.ReadFile("/etc/system-release")
		if err == nil {
			osInfo = parseSystemRelease(string(releaseFile))
		}
	}

	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		logger.Warningf("unix.Uname error: %v", err)
		return osInfo
	}
	osInfo.KernelVersion = string(bytes.TrimRight(uts.Version[:], "\x00"))
	osInfo.KernelRelease = string(bytes.TrimRight(uts.Release[:], "\x00"))

	return osInfo
}
