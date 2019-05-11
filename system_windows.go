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
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"golang.org/x/sys/windows/registry"
)

var errRegNotExist = registry.ErrNotExist

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
