//  Copyright 2019 Google Inc. All Rights Reserved.
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
	"net"
	"runtime"
	"sort"
	"strings"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

func forwardEntryExists(fes []ipForwardEntry, fe ipForwardEntry) bool {
	for _, e := range fes {
		if e.ipForwardIfIndex == fe.ipForwardIfIndex && e.ipForwardDest.Equal(fe.ipForwardDest) {
			return true
		}
	}
	return false
}

func agentInit() error {
	// On Windows:
	//  - Add route to metadata server
	if runtime.GOOS == "windows" {
		fes, err := getIPForwardEntries()
		if err != nil {
			return err
		}

		interfaces, err := net.Interfaces()
		if err != nil {
			return err
		}

		// We only want to set this for the first adapter, pre Windows 10 (2016) this was not guaranteed to be in metric order.
		sort.SliceStable(interfaces, func(i, j int) bool {
			return interfaces[i].Index < interfaces[j].Index
		})

		for _, iface := range interfaces {
			// Only take action on interfaces that are up, are not Loopback, and are not vEtherent (commonly setup by docker).
			if strings.Contains(iface.Name, "vEthernet") || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}

			fe := ipForwardEntry{
				ipForwardDest:    net.ParseIP("169.254.169.254"),
				ipForwardMask:    net.IPv4Mask(255, 255, 255, 255),
				ipForwardNextHop: net.ParseIP("0.0.0.0"),
				ipForwardMetric1: 1,
				ipForwardIfIndex: int32(iface.Index),
			}

			if forwardEntryExists(fes, fe) {
				break
			}

			logger.Infof("Adding route to metadata server on %q (index: %d)", iface.Name, iface.Index)
			if err := addIPForwardEntry(fe); err != nil {
				logger.Errorf("Error adding route to metadata server on %q (index: %d): %v", iface.Name, iface.Index, err)
			}
			break
		}
	}
	return nil
}
