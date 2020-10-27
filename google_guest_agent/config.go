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

package main

import (
	"github.com/go-ini/ini"
	"strings"
)

type AccountManagerConfig struct {
	ExplicitlyConfigured bool
	Disable              bool
}

type AccountsConfig struct {
	ReuseHomedir      bool
	Groups            string
	DeprovisionRemove bool
	UserdelCmd        string
	GpasswdRemoveCmd  string
	GpasswdAddCmd     string
	GroupaddCmd       string
	UseraddCmd        string
}

type AddressManagerConfig struct {
	ExplicitlyConfigured bool
	Disable              bool
}

type DaemonsConfig struct {
	ClockSkewDaemon bool
	AccountsDaemon  bool
	NetworkDaemon   bool
}

type DiagnosticsConfig struct {
	Enable string
}

type IpForwardingConfig struct {
	EthernetProtoId   string
	TargetInstanceIps bool
	IpAliases         bool
}

type NetworkInterfacesConfig struct {
	Setup          bool
	IpForwarding   bool
	DhcpCommand    string
	DhclientScript string
}

type SnapshotsConfig struct {
	Enabled             bool
	SnapshotServiceIp   string
	SnapshotServicePort int
	TimeoutInSeconds    int
	PreSnapshotScript   string
	PostSnapshotScript  string
}

type WsfcConfig struct {
	ExplicitlyConfigured bool
	Enable               bool
	Addresses            string
	Port                 string
}

type AgentConfig struct {
	raw               *ini.File
	AccountManager    AccountManagerConfig
	Accounts          AccountsConfig
	AddressManager    AddressManagerConfig
	Daemons           DaemonsConfig
	Diagnostics       DiagnosticsConfig
	IpForwarding      IpForwardingConfig
	NetworkInterfaces NetworkInterfacesConfig
	Snapshots         SnapshotsConfig
	Wsfc              WsfcConfig
}

var defaultConfig = AgentConfig{
	Accounts: AccountsConfig{
		ReuseHomedir:      false,
		Groups:            "adm,dip,docker,lxd,plugdev,video",
		DeprovisionRemove: false,
		UserdelCmd:        "userdel -r {user}",
		GpasswdRemoveCmd:  "gpasswd -d {user} {group}",
		GpasswdAddCmd:     "gpasswd -a {user} {group}",
		GroupaddCmd:       "groupadd {group}",
		UseraddCmd:        "useradd -m -s /bin/bash -p * {user}",
	},
	Daemons: DaemonsConfig{
		ClockSkewDaemon: true,
		AccountsDaemon:  true,
		NetworkDaemon:   true,
	},
	IpForwarding: IpForwardingConfig{
		EthernetProtoId:   "66",
		TargetInstanceIps: true,
		IpAliases:         true,
	},
	NetworkInterfaces: NetworkInterfacesConfig{
		Setup:          true,
		IpForwarding:   true,
		DhclientScript: "/sbin/google-dhclient-script",
	},
	Snapshots: SnapshotsConfig{
		SnapshotServiceIp:   "169.254.169.254",
		SnapshotServicePort: 8081,
		TimeoutInSeconds:    60,
	},
}

// agentConfigNameMapper is used to map field names in the AgentConfig
// to keys in an INI configuration file. Ideally, we would use a built
// in standard NameMapper like ini.TitleUnderscore (which maps fields
// from UpperCamelCase to lower_with_underscores), but unfortunately
// the mapping is slightly inconsistent.
// We have three possible ways a field can be mapped to a key:
// - UpperCamelCase field maps to lower_with_underscores key
//   e.g. SnapshotServicePort maps to snapshot_service_port
// - UpperCamelCase field maps to lowerCamelCase key
//   e.g. AccountManager maps to accountManager
// - UpperCamelCase field maps to UpperCamelCase key
//   e.g. InstanceSetup maps to InstanceSetup
var agentConfigNameMapper = func(raw string) string {
	if raw == "AccountManager" ||
		raw == "AddressManager" ||
		raw == "Diagnostics" ||
		raw == "Wsfc" {
		return strings.ToLower(string(raw[0])) + string(raw[1:])
	} else if raw == "Accounts" ||
		raw == "Daemons" ||
		raw == "IpForwarding" ||
		raw == "NetworkInterfaces" ||
		raw == "Snapshots" {
		return raw
	} else {
		return ini.TitleUnderscore(raw)
	}
}

// parseIni is only exposed for testing. parseConfig should almost
// always be used instead.
func parseIni(iniFile *ini.File) (AgentConfig, error) {
	config := defaultConfig
	config.raw = iniFile
	config.AccountManager.ExplicitlyConfigured = iniFile.Section("accountManager").HasKey("disable")
	config.AddressManager.ExplicitlyConfigured = iniFile.Section("addressManager").HasKey("disable")
	config.Wsfc.ExplicitlyConfigured = iniFile.Section("wsfc").HasKey("enable")
	iniFile.NameMapper = agentConfigNameMapper
	iniFile.StrictMapTo(&config)
	return config, nil
}

func parseConfig(file string) (AgentConfig, error) {
	// Priority: file.cfg, file.cfg.distro, file.cfg.template
	iniFile, err := ini.LoadSources(ini.LoadOptions{Loose: true, Insensitive: true}, file, file+".distro", file+".template")
	if err != nil {
		return defaultConfig, err
	}
	return parseIni(iniFile)
}
