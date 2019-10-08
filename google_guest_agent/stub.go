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
	"net"
	"os/user"
)

var errRegNotExist = errors.New("error")

func resetPwd(username, pwd string) error {
	return nil
}

func createAdminUser(username, pwd string) error {
	return nil
}

func readRegMultiString(key, name string) ([]string, error) {
	return nil, nil
}

func writeRegMultiString(key, name string, value []string) error {
	return nil
}

func addAddressWindows(ip, mask net.IP, index uint32) error {
	return nil
}

func removeAddressWindows(ip net.IP, index uint32) error {
	return nil
}

func deleteRegKey(key, name string) error {
	return nil
}

func userExists(name string) (bool, error) {
	if _, err := user.Lookup(name); err != nil {
		return false, err
	}
	return true, nil
}
