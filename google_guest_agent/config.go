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

type WsfcConfig struct {
	ExplicitlyConfigured bool
	Enable               bool
	Addresses            string
	Port                 string
}

type AgentConfig struct {
	raw  *ini.File
	Wsfc WsfcConfig
}

var defaultConfig = AgentConfig{
	raw: ini.Empty(),
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
	if raw == "Wsfc" {
		return strings.ToLower(string(raw[0])) + string(raw[1:])
	} else {
		return ini.TitleUnderscore(raw)
	}
}

// parseIni is only exposed for testing. parseConfig should almost
// always be used instead.
func parseIni(iniFile *ini.File) (AgentConfig, error) {
	config := defaultConfig
	config.raw = iniFile
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
