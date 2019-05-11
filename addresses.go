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
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/compute-image-windows/logger"
)

var (
	addressDisabled  = false
	addressKey       = regKeyBase + `\ForwardedIps`
	oldWSFCAddresses string
	oldWSFCEnable    bool
)

type addressMgr struct{}

func (a *addressMgr) parseWSFCAddresses() string {
	wsfcAddresses := config.Section("wsfc").Key("addresses").String()
	if len(wsfcAddresses) > 0 {
		return wsfcAddresses
	}
	if len(newMetadata.Instance.Attributes.WSFCAddresses) > 0 {
		return newMetadata.Instance.Attributes.WSFCAddresses
	}
	if len(newMetadata.Project.Attributes.WSFCAddresses) > 0 {
		return newMetadata.Project.Attributes.WSFCAddresses
	}

	return ""
}

func (a *addressMgr) parseWSFCEnable() bool {
	wsfcEnable, err := config.Section("wsfc").Key("enable").Bool()
	if err == nil {
		return wsfcEnable
	}
	wsfcEnable, err = strconv.ParseBool(newMetadata.Instance.Attributes.EnableWSFC)
	if err == nil {
		return wsfcEnable
	}
	wsfcEnable, err = strconv.ParseBool(newMetadata.Project.Attributes.EnableWSFC)
	if err == nil {
		return wsfcEnable
	}
	return false
}

func (a *addressMgr) diff() bool {
	wsfcAddresses := a.parseWSFCAddresses()
	wsfcEnable := a.parseWSFCEnable()

	diff := !reflect.DeepEqual(newMetadata.Instance.NetworkInterfaces, oldMetadata.Instance.NetworkInterfaces) ||
		wsfcEnable != oldWSFCEnable || wsfcAddresses != oldWSFCAddresses

	oldWSFCAddresses = wsfcAddresses
	oldWSFCEnable = wsfcEnable
	return diff
}

func (a *addressMgr) timeout() bool {
	select {
	case <-ticker:
		return true
	default:
		return false
	}
}

func (a *addressMgr) disabled() (disabled bool) {
	var err error

	defer func() {
		if disabled != addressDisabled {
			addressDisabled = disabled
			logStatus("address", disabled)
		}
	}()

	disabled, err = strconv.ParseBool(config.Section("addressManager").Key("disable").String())
	if err == nil {
		return disabled
	}
	disabled, err = strconv.ParseBool(newMetadata.Instance.Attributes.DisableAddressManager)
	if err == nil {
		return disabled
	}
	disabled, err = strconv.ParseBool(newMetadata.Project.Attributes.DisableAddressManager)
	if err == nil {
		return disabled
	}
	return addressDisabled
}

func compareIPs(regFwdIPs, mdFwdIPs, cfgIPs []string) (toAdd []string, toRm []string) {
	for _, mdIP := range mdFwdIPs {
		if !containsString(mdIP, cfgIPs) {
			toAdd = append(toAdd, mdIP)
		}
	}

	for _, cfgIP := range cfgIPs {
		if containsString(cfgIP, regFwdIPs) && !containsString(cfgIP, mdFwdIPs) {
			toRm = append(toRm, cfgIP)
		}
	}

	return
}

var badMAC []string

func (a *addressMgr) set() error {
	ifs, err := net.Interfaces()
	if err != nil {
		return err
	}

	a.applyWSFCFilter()

	for _, ni := range newMetadata.Instance.NetworkInterfaces {
		mac, err := net.ParseMAC(ni.Mac)
		if err != nil {
			if !containsString(ni.Mac, badMAC) {
				logger.Error(err)
				badMAC = append(badMAC, ni.Mac)
			}
			continue
		}

		regFwdIPs, err := readRegMultiString(addressKey, mac.String())
		if err != nil && err != errRegNotExist {
			logger.Error(err)
			continue
		} else if err != nil && err == errRegNotExist {
			// The old agent stored MAC addresses without the ':',
			// check for those and clean them up.
			oldName := strings.Replace(mac.String(), ":", "", -1)
			regFwdIPs, err = readRegMultiString(addressKey, oldName)
			if err == nil {
				// Ignore error here as this is just cleanup.
				deleteRegKey(addressKey, oldName)
			} else {
				regFwdIPs = nil
			}
		}

		var iface net.Interface
		for _, i := range ifs {
			if i.HardwareAddr.String() == mac.String() {
				iface = i
			}
		}

		if reflect.DeepEqual(net.Interface{}, iface) {
			if !containsString(ni.Mac, badMAC) {
				logger.Errorf("no interface with mac %s exists on system", mac)
				badMAC = append(badMAC, ni.Mac)
			}
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			logger.Error(err)
			continue
		}

		var cfgIPs []string
		for _, addr := range addrs {
			cfgIPs = append(cfgIPs, strings.TrimSuffix(addr.String(), "/32"))
		}

		wantIps := append(ni.ForwardedIps, ni.TargetInstanceIps...)
		toAdd, toRm := compareIPs(regFwdIPs, wantIps, cfgIPs)
		if len(toAdd) != 0 || len(toRm) != 0 {
			// Remove non configured IPs from registry list.
			for _, ip := range toAdd {
				for i, rIP := range regFwdIPs {
					if ip == rIP {
						regFwdIPs = append(regFwdIPs[:i], regFwdIPs[i+1:]...)
						break
					}
				}
			}
			msg := fmt.Sprintf("Changing forwarded IPs for %s from %q to %q by", mac, regFwdIPs, wantIps)
			if len(toAdd) != 0 {
				msg += fmt.Sprintf(" adding %q", toAdd)
			}
			if len(toRm) != 0 {
				if len(toAdd) != 0 {
					msg += " and"
				}
				msg += fmt.Sprintf(" removing %q", toRm)
			}
			logger.Info(msg, ".")
		}

		reg := wantIps
		for _, ip := range toAdd {
			if err := addAddress(net.ParseIP(ip), net.ParseIP("255.255.255.255"), uint32(iface.Index)); err != nil {
				logger.Error(err)
				for i, rIP := range reg {
					if rIP == ip {
						reg = append(reg[:i], reg[i+1:]...)
						break
					}
				}
			}
		}

		for _, ip := range toRm {
			if err := removeAddress(net.ParseIP(ip), uint32(iface.Index)); err != nil {
				logger.Error(err)
				reg = append(reg, ip)
			}
		}

		if err := writeRegMultiString(addressKey, mac.String(), reg); err != nil {
			logger.Error(err)
		}
	}

	return nil
}

// Filter out forwarded ips based on WSFC (Windows Failover Cluster Settings).
// If only EnableWSFC is set, all ips in the ForwardedIps and TargetInstanceIps will be ignored.
// If WSFCAddresses is set (with or without EnableWSFC), only ips in the list will be filtered out.
func (a *addressMgr) applyWSFCFilter() {
	wsfcAddresses := a.parseWSFCAddresses()

	var wsfcAddrs []string
	for _, wsfcAddr := range strings.Split(wsfcAddresses, ",") {
		if len(wsfcAddr) == 0 {
			continue
		}

		if net.ParseIP(wsfcAddr) == nil {
			logger.Errorln("ip address for wsfc is not in valid form", wsfcAddr)
			continue
		}

		wsfcAddrs = append(wsfcAddrs, wsfcAddr)
	}

	if len(wsfcAddrs) != 0 {
		interfaces := newMetadata.Instance.NetworkInterfaces
		for idx := range interfaces {
			var filteredForwardedIps []string
			for _, ip := range interfaces[idx].ForwardedIps {
				if !containsString(ip, wsfcAddrs) {
					filteredForwardedIps = append(filteredForwardedIps, ip)
				}
			}
			interfaces[idx].ForwardedIps = filteredForwardedIps

			var filteredTargetInstanceIps []string
			for _, ip := range interfaces[idx].TargetInstanceIps {
				if !containsString(ip, wsfcAddrs) {
					filteredTargetInstanceIps = append(filteredTargetInstanceIps, ip)
				}
			}
			interfaces[idx].TargetInstanceIps = filteredTargetInstanceIps
		}
	} else {
		wsfcEnable := a.parseWSFCEnable()
		if wsfcEnable {
			for idx := range newMetadata.Instance.NetworkInterfaces {
				newMetadata.Instance.NetworkInterfaces[idx].ForwardedIps = nil
				newMetadata.Instance.NetworkInterfaces[idx].TargetInstanceIps = nil
			}
		}
	}
}
