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

//go:build integration
// +build integration

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

const testIp = "192.168.0.0"

func TestAddAndRemoveLocalRoute(t *testing.T) {
	metdata, err := getMetadata(context.Context(), false)
	if err != nil {
		t.Fatalf("failed to get metadata, err %v", err)
	}
	iface, err := getInterfaceByMAC(metdata.Instance.NetworkInterfaces[0].Mac)
	if err != nil {
		t.Fatalf("failed to get interface from mac, err %v", err)
	}
	// test add local route
	if err := removeLocalRoute(testIp, iface.Name); err != nil {
		t.Fatalf("failed to remove local route, err %v", err)
	}
	if err := addLocalRoute(testIp, iface.Name); err != nil {
		t.Fatalf("add test local route should not failed, err %v", err)
	}

	res, err := getLocalRoutes(iface.Name)
	if err != nil {
		t.Fatalf("get local route should not failed, err %v", err)
	}
	exist := false
	for _, route := range res {
		if strings.Contains(route, fmt.Sprintf("local %s/24", testIp)) {
			exist = true
		}
	}
	if !exist {
		t.Fatalf("route %s is not added", testIp)
	}

	// test remove local route
	if err := removeLocalRoute(testIp, iface.Name); err != nil {
		t.Fatalf("add test local route should not failed")
	}
	res, err := getLocalRoutes(iface.Name)
	if err != nil {
		t.Fatalf("ip route list should not failed, err %s", res.err)
	}

	for _, route := range res {
		if strings.Contains(route, fmt.Sprintf("local %s/24", testIp)) {
			t.Fatalf("route %s should be removed but exist", testIp)
		}
	}
}

func TestGetLocalRoute(t *testing.T) {
	metdata, err := getMetadata(context.Context(), false)
	if err != nil {
		t.Fatalf("failed to get metadata, err %v", err)
	}
	iface, err := getInterfaceByMAC(metdata.Instance.NetworkInterfaces[0].Mac)
	if err != nil {
		t.Fatalf("failed to get interface from mac, err %v", err)
	}

	if err := addLocalRoute(testIp, iface.Name); err != nil {
		t.Fatalf("add test local route should not failed, err %v", err)
	}
	routes, err := getLocalRoutes(iface.Name)
	if err != nil {
		t.Fatalf("get local routes should not failed, err %v", err)
	}
	if len(routes) != 1 {
		t.Fatal("find unexpected local route %s.", routes[0])
	}
	if routes[0] != testIp {
		t.Fatal("find unexpected local route %s.", routes[0])
	}
}
