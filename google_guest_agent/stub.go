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

// +build !windows

package main

import (
	"errors"
)

var errRegNotExist = errors.New("error")

func resetPwd(username, pwd string) error {
	return nil
}

func readRegMultiString(key, name string) ([]string, error) {
	return nil, nil
}

func writeRegMultiString(key, name string, value []string) error {
	return nil
}

func deleteRegKey(key, name string) error {
	return nil
}

func getWindowsServiceImagePath(regKey string) (string, error) {
	return "", nil
}

type versionInfo struct {
	major int
	minor int
}

func getWindowsExeVersion(path string) (versionInfo, error) {
	return versionInfo{0, 0}, nil
}

func checkMinimumVersion(checkVersion versionInfo, minVersion versionInfo) bool {
	return false
}

func windowsServiceStart(servicename string) error {
	return nil
}

func windowsServiceStop(servicename string) error {
	return nil
}

func setWindowsServiceStartModeAuto(servicename string) error {
	return nil
}

func setWindowsServiceStartModeDisable(servicename string) error {
	return nil
}

func checkWindowsServiceStartMode(servicename string) bool {
	return false
}
