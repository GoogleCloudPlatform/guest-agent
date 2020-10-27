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
	"testing"

	"github.com/go-ini/ini"
)

func TestEnsureIpForwardingConfigurationLoaded(t *testing.T) {
	iniConfig := []byte("[NetworkInterfaces]\nip_forwarding: false\n")
	iniFile, err := ini.InsensitiveLoad(iniConfig)
	if err != nil {
		t.Errorf("Error parsing ini data: %v", err)
	}
	config, err := parseIni(iniFile)
	if err != nil {
		t.Errorf("Error parsing config %s: %s", iniConfig, err)
	}
	if config.NetworkInterfaces.IpForwarding != false {
		t.Errorf("Error loading NetworkInterfaces.IpForwarding value")
	}
}

func TestEnsureDefaultConfigLoads(t *testing.T) {
	configFile := "../instance_configs.cfg"
	config, err := parseConfig(configFile)
	if err != nil {
		t.Errorf("Error parsing config %s: %s", configFile, err)
	}
	t.Logf("Config:%s", config)
}

func TestAgentConfigNameMapper(t *testing.T) {
	var tests = []struct {
		name            string
		expectedMapping string
	}{
		{"Accounts", "Accounts"},
		{"ReuseHomedir", "reuse_homedir"},
		{"Groups", "groups"},
		{"DeprovisionRemove", "deprovision_remove"},
		{"UserdelCmd", "userdel_cmd"},
		{"GpasswdRemoveCmd", "gpasswd_remove_cmd"},
		{"GpasswdAddCmd", "gpasswd_add_cmd"},
		{"GroupaddCmd", "groupadd_cmd"},
		{"UseraddCmd", "useradd_cmd"},
		{"Daemons", "Daemons"},
		{"ClockSkewDaemon", "clock_skew_daemon"},
		{"AccountsDaemon", "accounts_daemon"},
		{"NetworkDaemon", "network_daemon"},
		{"Instance", "Instance"},
		{"InstanceIdDir", "instance_id_dir"},
		{"InstanceSetup", "InstanceSetup"},
		{"OptimizeLocalSsd", "optimize_local_ssd"},
		{"SetMultiqueue", "set_multiqueue"},
		{"NetworkEnabled", "network_enabled"},
		{"HostKeyDir", "host_key_dir"},
		{"HostKeyTypes", "host_key_types"},
		{"SetBotoConfig", "set_boto_config"},
		{"SetHostKeys", "set_host_keys"},
		{"IpForwarding", "IpForwarding"},
		{"EthernetProtoId", "ethernet_proto_id"},
		{"TargetInstanceIps", "target_instance_ips"},
		{"IpAliases", "ip_aliases"},
		{"NetworkInterfaces", "NetworkInterfaces"},
		{"Setup", "setup"},
		{"IpForwarding", "IpForwarding"},
		{"DhclientScript", "dhclient_script"},
		{"Snapshots", "Snapshots"},
		{"SnapshotServiceIp", "snapshot_service_ip"},
		{"SnapshotServicePort", "snapshot_service_port"},
		{"TimeoutInSeconds", "timeout_in_seconds"},
	}

	for _, tt := range tests {
		mapping := agentConfigNameMapper(tt.name)
		if mapping != tt.expectedMapping {
			t.Errorf("Got '%s' instead of expected mapping '%s' for '%s'", mapping, tt.expectedMapping, tt.name)
		}
	}
}
