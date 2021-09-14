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

//go:build !windows
// +build !windows

package main

import (
	"errors"
	"net"
)

// TODO: addLocalRoute and addRoute should be merged with the addition of ipForwardType to ipForwardEntry.
func addIPForwardEntry(route ipForwardEntry) error {
	return errors.New("addIPForwardEntry unimplemented on non Windows systems")
}

// TODO: getLocalRoutes and getIPForwardEntries should be merged.
func getIPForwardEntries() ([]ipForwardEntry, error) {
	return nil, errors.New("getIPForwardEntries unimplemented on non Windows systems")
}

func addAddress(ip net.IP, mask net.IPMask, index uint32) error {
	return errors.New("addAddress unimplemented on non Windows systems")
}

func removeAddress(ip net.IP, index uint32) error {
	return errors.New("removeAddress unimplemented on non Windows systems")
}
