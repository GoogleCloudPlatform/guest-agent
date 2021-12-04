//  Copyright 2021 Google LLC
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

// +build integration

package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestAddAndRemoveLocalRoute(t *testing.T) {
	defaultInterface, err := getDefaultInterface()
	if err != nil {
		t.Fatalf("failed to get default interface, err %v", err)
	}
	ip, err := GetMetadata("network-interfaces/0/ip")
	if err != nil {
		t.Fatalf("failed to get network ip, err %v", err)
	}

	// test add local route need to make sure instance already remove local route
	if err := removeLocalRoute(ip, defaultInterface) {
		t.Fatalf("failed to remove local route, err %v", err)
	}
	if err := addLocalRoute(ip, defaultInterface) {
		t.Fatalf("add test local route should not failed, err %v", err)
	}

	res := runCmdOutput(fmt.Sprintf('ip route list table local type local scope host dev %s proto 66', defaultInterface))
	if res.ExitCode() != 0 {
		t.Fatalf("ip route list should not failed, err %v", err)
	}

	if !strings.Contains(res.Stdout(), fmt.Sprintf("local %s/24", ip)) {
		t.Fatalf("route %s is not added", ip)
	}

	// test remove local route
	if err := removeLocalRoute(ip, defaultInterface) {
		t.Fatalf("add test local route should not failed")
	}

	res := runCmdOutput(fmt.Sprintf('ip route list table local type local scope host dev %s proto 66', defaultInterface))
	if res.ExitCode() != 0 {
		t.Fatalf("ip route list should not failed, err %s", res.err)
	}
	if strings.Contains(res.Stdout(), fmt.Sprintf("local %s/24", ip)) {
		t.Fatalf("route %s should be removed but exist", ip)
	}
}

func TestGetLocalRoute(t *testing.T) {
	defaultInterface, err := getDefaultInterface()
	if err != nil {
		t.Fatalf("failed to get default interface, err %v", err)
	}
	routes, err := getLocalRoutes(defaultInterface)
	if err != nil {
		t.Fatalf("get local routes should not failed, err %v", err)
	}
	if len(routes) != 1 {
		t.Fatal("find unexpected local route %s.", routes[0])
	}
	ip, err := GetMetadata("network-interfaces/0/ip")
	if err != nil {
		t.Fatalf("failed to get network ip, err %v", err)
	}
	if routes[0] != ip {
		t.Fatal("find unexpected local route %s.", routes[0])
	}
}

func getDefaultInterface(t *testing.T) (string, error) {
	var defaultInterface string
	re, err := getRelease()
	if err != nil {
		return nil, err
	}
	if re.os == "debian" && (re.version.major == 10 || re.version.major == 11) || re.os == "ubuntu" {
		defaultInterface = "ens4"
	} else {
		defaultInterface = "eth0"
	}
	return defaultInterface, err
}
