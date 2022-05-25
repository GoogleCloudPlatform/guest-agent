//  Copyright 2017 Google Inc. All Rights Reserved.
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
	"os/exec"
	"strings"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"golang.org/x/sys/windows/registry"
)

var errRegNotExist = registry.ErrNotExist
var startRegKey = "Start"

type (
	DWORD  uint32
	LPWSTR *uint16
)

func init() {
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, regKeyBase, registry.WRITE)
	if err != nil {
		logger.Fatalf(err.Error())
	}
	key.Close()
	key, _, err = registry.CreateKey(registry.LOCAL_MACHINE, addressKey, registry.WRITE)
	if err != nil {
		logger.Fatalf(err.Error())
	}
	key.Close()
}

func readRegMultiString(key, name string) ([]string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()

	s, _, err := k.GetStringsValue(name)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func readRegString(key, name string) (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()

	s, _, err := k.GetStringValue(name)
	if err != nil {
		return "", err
	}
	return s, nil
}

func readRegInteger(key, name string) (uint64, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.QUERY_VALUE)
	if err != nil {
		return 0, err
	}
	defer k.Close()

	i, _, err := k.GetIntegerValue(name)
	if err != nil {
		return 0, err
	}
	return i, nil
}

func writeRegMultiString(key, name string, value []string) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.WRITE)
	if err != nil {
		return err
	}
	defer k.Close()

	return k.SetStringsValue(name, value)
}

func deleteRegKey(key, name string) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.WRITE)
	if err != nil {
		return err
	}
	defer k.Close()

	return k.DeleteValue(name)
}

func checkWindowsServiceRunning(servicename string) bool {
	res := runCmdOutput(exec.Command("sc", "query", servicename))
	return strings.Contains(res.Stdout(), "RUNNING")
}

func getWindowsServiceImagePath(regKey string) (string, error) {
	regValue, err := readRegString(regKey, "ImagePath")
	if err != nil {
		return "", err
	}
	imagePath := strings.Trim(string(regValue), `"`)
	return imagePath, nil
}

func getWindowsExeVersion(path string) (versionInfo, error) {
	psCmd := "(Get-Item '" + path + "').VersionInfo.FileVersion"
	psVer, err := exec.Command("powershell", "-c", psCmd).Output()
	if err != nil {
		return versionInfo{0, 0}, err
	}
	return parseVersionInfo(psVer)
}
