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

package main

import (
	"bytes"
	"io/ioutil"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"golang.org/x/sys/unix"
)

func parseOSRelease(osRelease string) info {
	var ret info
	for _, line := range strings.Split(osRelease, "\n") {
		var id = line
		if id = strings.TrimPrefix(line, "ID="); id != line {
			if len(id) > 0 && id[0] == '"' {
				id = id[1:]
			}
			if len(id) > 0 && id[len(id)-1] == '"' {
				id = id[:len(id)-1]
			}
			ret.os = parseID(id)
		}
		if id = strings.TrimPrefix(line, "VERSION_ID="); id != line {
			if len(id) > 0 && id[0] == '"' {
				id = id[1:]
			}
			if len(id) > 0 && id[len(id)-1] == '"' {
				id = id[:len(id)-1]
			}
			ret.versionID = id
			version, err := parseVersion(id)
			if err != nil {
				logger.Warningf("Couldn't parse version id: %v", err)
				return ret
			}
			ret.version = version
		}
	}
	return ret
}

func parseSystemRelease(systemRelease string) info {
	var ret info
	var key = " release "
	idx := strings.Index(systemRelease, key)
	if idx == -1 {
		logger.Warningf("SystemRelease does not match expected format")
		return ret
	}
	ret.os = parseID(systemRelease[:idx])

	versionFromRelease := strings.Split(systemRelease[idx+len(key):], " ")[0]
	version, err := parseVersion(versionFromRelease)
	if err != nil {
		logger.Warningf("Couldn't parse version: %v", err)
		return ret
	}
	ret.version = version
	return ret
}

func parseVersion(version string) (ver, error) {
	versionparts := strings.Split(version, ".")
	ret := ver{length: len(versionparts)}

	// Must have at least major version.
	var err error
	ret.major, err = strconv.Atoi(versionparts[0])
	if err != nil {
		return ret, err
	}
	if ret.length > 1 {
		ret.minor, err = strconv.Atoi(versionparts[1])
		if err != nil {
			return ret, err
		}
	}
	if ret.length > 2 {
		ret.patch, err = strconv.Atoi(versionparts[2])
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

func getOSInfo() info {
	var osInfo info

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
	osInfo.kernelVersion = string(bytes.TrimRight(uts.Version[:], "\x00"))
	osInfo.kernelRelease = string(bytes.TrimRight(uts.Release[:], "\x00"))

	return osInfo
}
