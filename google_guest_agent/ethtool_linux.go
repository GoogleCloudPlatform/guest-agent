//  Copyright 2020 Google Inc. All Rights Reserved.
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

//+build !windows

package main

import (
	"path"
	"path/filepath"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/safchain/ethtool"
)

//Output `ethtool -l eth0`
//Channel parameters for eth0:
//Pre-set maximums:
//RX:		0
//TX:		0
//Other:		0
//Combined:	16
//Current hardware settings:
//RX:		0
//TX:		0
//Other:		0
//Combined:	16
func enableMultiQueue(dev string) error {
	ethDevs, err := filepath.Glob(dev + "/net/*")
	if err != nil {
		return err
	}
	ethHandle, err := ethtool.NewEthtool()
	if err != nil {
		return err
	}
	defer ethHandle.Close()

	for _, ethDev := range ethDevs {
		ethDev = path.Base(ethDev)
		ch, err := ethHandle.GetChannels(ethDev)
		if err != nil {
			logger.Warningf("Could not get channels for %s.", ethDev)
			return err
		}
		numMaxChannels := ch.MaxCombined
		if numMaxChannels == 1 {
			continue
		}
		ch.CombinedCount = numMaxChannels
		if _, err := ethHandle.SetChannels(ethDev, ch); err != nil {
			logger.Warningf("Could not set channels for %s to %d.", ethDev, numMaxChannels)
			return err
		}
		logger.Infof("Set channels for %s to %d.", ethDev, numMaxChannels)
	}
	return nil
}
