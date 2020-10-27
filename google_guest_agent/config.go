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
	"fmt"
	"github.com/go-ini/ini"
	"reflect"
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

type InstanceConfig struct {
	InstanceIdDir string
	InstanceId    string
}

type InstanceSetupConfig struct {
	OptimizeLocalSsd bool
	SetMultiqueue    bool
	NetworkEnabled   bool
	HostKeyDir       string
	HostKeyTypes     string
	SetBotoConfig    bool
	SetHostKeys      bool
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
	AccountManager    AccountManagerConfig
	Accounts          AccountsConfig
	AddressManager    AddressManagerConfig
	Daemons           DaemonsConfig
	Diagnostics       DiagnosticsConfig
	Instance          InstanceConfig
	InstanceSetup     InstanceSetupConfig
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
	Instance: InstanceConfig{
		InstanceIdDir: "/etc",
	},
	InstanceSetup: InstanceSetupConfig{
		OptimizeLocalSsd: true,
		SetMultiqueue:    true,
		NetworkEnabled:   true,
		HostKeyDir:       "/etc/ssh",
		HostKeyTypes:     "ecdsa,ed25519,rsa",
		SetBotoConfig:    true,
		SetHostKeys:      true,
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
		raw == "Instance" ||
		raw == "InstanceSetup" ||
		raw == "IpForwarding" ||
		raw == "NetworkInterfaces" ||
		raw == "Snapshots" {
		return raw
	} else {
		return ini.TitleUnderscore(raw)
	}
}

// agentConfigValueToString walks the fields in the AgentConfig struct
// recursively using reflection, and prints the name and value of each
func agentConfigValueToString(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Int:
		return fmt.Sprintf("%d", v.Int())
	case reflect.Bool:
		return fmt.Sprintf("%t", v.Bool())
	case reflect.String:
		return fmt.Sprintf("\"%s\"", v.String())
	case reflect.Struct:
		s := ""
		for i := 0; i < v.NumField(); i++ {
			fieldName := agentConfigNameMapper(v.Type().Field(i).Name)
			field := v.Field(i)
			if field.Kind() == reflect.Struct {
				fieldName = "\n[" + fieldName + "]"
			} else {
				fieldName = fieldName + ":"
			}
			s += fmt.Sprintf("\n%s ", fieldName) + agentConfigValueToString(field)
		}
		return s
	}
	return ""
}

func (c AgentConfig) String() string {
	return agentConfigValueToString(reflect.ValueOf(c)) + "\n\n"
}

// parseIni is only exposed for testing. parseConfig should almost
// always be used instead.
func parseIni(iniFile *ini.File) (AgentConfig, error) {
	config := defaultConfig
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
