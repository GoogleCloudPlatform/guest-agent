// Copyright 2021 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build integration
// +build integration

package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
)

const testIp = "192.168.0.0"

func TestAddAndRemoveLocalRoute(t *testing.T) {
	ctx := context.Background()
	config, _ := getConfig(t)
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("failed to get interfaces: %v", err)
	}
	iface := interfaces[1]

	// test add local route
	if err = removeLocalRoute(ctx, config, testIp, iface.Name); err != nil {
		t.Fatalf("failed to remove local route: %v", err)
	}
	if err = addLocalRoute(ctx, config, testIp, iface.Name); err != nil {
		t.Fatalf("add test local route should not fail: %v", err)
	}

	res, err := getLocalRoutes(ctx, config, iface.Name)
	if err != nil {
		t.Fatalf("get local route should not fail: %v", err)
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
	if err = removeLocalRoute(ctx, config, testIp, iface.Name); err != nil {
		t.Fatalf("add test local route should not fail: %s", err)
	}
	res, err = getLocalRoutes(ctx, config, iface.Name)
	if err != nil {
		t.Fatalf("ip route list should not fail: %s", err)
	}

	for _, route := range res {
		if strings.Contains(route, fmt.Sprintf("local %s/24", testIp)) {
			t.Fatalf("route %s should be removed but exist", testIp)
		}
	}
}

func TestGetLocalRoute(t *testing.T) {
	ctx := context.Background()
	config, _ := getConfig(t)
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("failed to get interfaces: %v", err)
	}
	iface := interfaces[1]

	if err = addLocalRoute(ctx, config, testIp, iface.Name); err != nil {
		t.Fatalf("add local route should not fail: %v", err)
	}
	routes, err := getLocalRoutes(ctx, config, iface.Name)
	if err != nil {
		t.Fatalf("get local routes should not fail: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("found unexpected local route %s.", routes[0])
	}
	if routes[0] != testIp {
		t.Fatalf("found unexpected local route %s.", routes[0])
	}
}
