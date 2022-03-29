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
	"strconv"
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

func windowsStartService(servicename string) error {
	if windowsServiceRunning(servicename) {
		return nil
	}
	return runCmd(exec.Command("net", "start", servicename))
}

func windowsStopService(servicename string) error {
	if !windowsServiceRunning(servicename) {
		return nil
	}
	return runCmd(exec.Command("net", "stop", servicename))
}

func windowsServiceStartAuto(servicename string) error {
	if windowsServiceStartStatus(servicename) {
		return nil
	}
	return runCmd(exec.Command("sc", "config", servicename, "start=auto"))
}

func windowsServiceStartDisable(servicename string) error {
	if !windowsServiceStartStatus(servicename) {
		return nil
	}
	return runCmd(exec.Command("sc", "config", servicename, "start=disabled"))
}

func windowsServiceStartStatus(servicename string) bool {
	regKey := `SYSTEM\CurrentControlSet\Services\` + servicename
	logger.Infof("regKey: %S", regKey)
	status, err := readRegInteger(regKey, startRegKey)
	logger.Infof("Status Value %s", status)
	logger.Infof("Update %s", "3")
	if err != nil && err != errRegNotExist {
	    logger.Infof("Error: %s", err)
		return false
	}
	return status == 2
}

func getSshdPath() (string, error) {
	regKey := `SYSTEM\CurrentControlSet\Services\sshd`
	sshd_bin, err := readRegString(regKey, "ImagePath")
	if err != nil {
		return "", err
	}
	return string(sshd_bin), nil
}

func validWindowsSshVersion() (bool , error) {
	sshd_bin, err := getSshdPath()
	if err != nil {
	    logger.Debugf("Cannot determine OpenSSH path.")
	    return false, err
	} 
	out, err := exec.Command(sshd_bin, "-V").Output()
	if err != nil {
		logger.Debugf("Cannot determine OpenSSH version.")
		return false, err 
	}
	ver_string := string(out)
	openssh_ver := strings.Fields(ver_string)[0]
	ver_num := strings.Trim(strings.TrimPrefix(openssh_ver, "OpenSSH_for_Windows_"), ",")
	ver_parts := strings.Split(ver_num, ".")
	major_version, err := strconv.Atoi(ver_parts[0])
	if err != nil {
		return false, err
	}
	if major_version > 8 {
		return true, nil
	}

	minor_version, err := strconv.Atoi(strings.Split(ver_parts[1], "p")[0])
	if err != nil {
		return false, err
	}
	if minor_version >= 6 {
		return true, nil
	}
	return false, nil
}

func windowsServiceRunning(servicename string) bool {
	res := runCmdOutput(exec.Command("sc", "query", servicename))
	return strings.Contains(res.Stdout(), "RUNNING")
}