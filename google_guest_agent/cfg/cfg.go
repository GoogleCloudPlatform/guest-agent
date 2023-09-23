//  Copyright 2023 Google Inc. All Rights Reserved.
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

// Package cfg is package responsible to loading and accessing the guest environment configuration.
package cfg

import (
	"fmt"
	"runtime"

	"github.com/go-ini/ini"
)

var (
	// instance is the single instance of configuration sections, once loaded this package
	// should always return it.
	instance *Sections

	// dataSource is a pointer to a data source loading/defining function, unit tests will
	// want to change this pointer to whatever makes sense to its implementation.
	dataSources = defaultDataSources
)

const (
	winConfigPath  = `C:\Program Files\Google\Compute Engine\instance_configs.cfg`
	unixConfigPath = `/etc/default/instance_configs.cfg`

	defaultConfig = `
[Accounts]
deprovision_remove = false
gpasswd_add_cmd = gpasswd -a {user} {group}
gpasswd_remove_cmd = gpasswd -d {user} {group}
groupadd_cmd = groupadd {group}
groups = adm,dip,docker,lxd,plugdev,video
reuse_homedir = false
useradd_cmd = useradd -m -s /bin/bash -p * {user}
userdel_cmd = userdel -r {user}

[Daemons]
accounts_daemon = true
clock_skew_daemon = true
network_daemon = true

[IpForwarding]
ethernet_proto_id = 66
ip_aliases = true
target_instance_ips = true

[Instance]
instance_id =
instance_id_dir = /etc/google_instance_id

[InstanceSetup]
host_key_dir = /etc/ssh
host_key_types = ecdsa,ed25519,rsa
network_enabled = true
optimize_local_ssd = true
set_boto_config = true
set_host_keys = true
set_multiqueue = true

[MetadataScripts]
default_shell = /bin/bash
run_dir =
shutdown = true
shutdown-windows = true
startup = true
startup-windows = true
sysprep-specialize = true

[NetworkInterfaces]
dhcp_command =
ip_forwarding = true
setup = true

[OSLogin]
cert_authentication = true

[Snapshots]
enabled = false
snapshot_service_ip = 169.254.169.254
snapshot_service_port = 8081
timeout_in_seconds = 60

[Unstable]
pamless_auth_stack = false
mds_mtls = false
`
)

// Sections encapsulates all the configuration sections.
type Sections struct {
	// AccountManager defines the address management configurations. It takes precedence over instance's
	// and project's metadata configuration. The default configuration doesn't define values to it, if the
	// user has defined it then we shouldn't even consider metadata values. Users must check if this
	// pointer is nil or not.
	AccountManager *AccountManager `ini:"accountManager,omitempty"`

	// Accounts defines the non windows account management options, behaviors and commands.
	Accounts *Accounts `ini:"Accounts,omitempty"`

	// AddressManager defines the address management configurations. It takes precedence over instance's
	// and project's metadata configuration. The default configuration doesn't define values to it, if the
	// user has defined it then we shouldn't even consider metadata values. Users must check if this
	// pointer is nil or not.
	AddressManager *AddressManager `ini:"addressManager,omitempty"`

	// Daemons defines the availability of clock skew, network and account managers.
	Daemons *Daemons `ini:"Daemons,omitempty"`

	// Diagnostics defines the diagnostics configurations. It takes precedence over instance's
	// and project's metadata configuration. The default configuration doesn't define values to it, if the
	// user has defined it then we shouldn't even consider metadata values. Users must check if this
	// pointer is nil or not.
	Diagnostics *Diagnostics `ini:"diagnostics,omitempty"`

	// IPForwarding defines the ip forwarding configuration options.
	IPForwarding *IPForwarding `ini:"IpForwarding,omitempty"`

	// Instance defines the instance ID handling behaviors, i.e. where to read the ID from etc.
	Instance *Instance `ini:"Instance,omitempty"`

	// InstanceSetup defines options to basic instance setup options i.e. optimize local ssd, network,
	// host keys etc.
	InstanceSetup *InstanceSetup `ini:"InstanceSetup,omitempty"`

	// MetadataScripts contains the configurations of the metadata-scripts service.
	MetadataScripts *MetadataScripts `ini:"MetadataScripts,omitempty"`

	// NetworkInterfaces defines if the network interfaces should be managed/configured by guest-agent
	// as well as the commands definitions for network configuration.
	NetworkInterfaces *NetworkInterfaces `ini:"NetworkInterfaces,omitempty"`

	// OSLogin defines the OS Login configuration options.
	OSLogin *OSLogin `ini:"OSLogin,omitempty"`

	// Snpashots defines the snapshot listener configuration and behavior i.e. the server address and port.
	Snapshots *Snapshots `ini:"Snapshots,omitempty"`

	// Unstable is a "under development feature flags" section. No stability or long term support is
	// guaranteed for any keys under this section. No application, script or utility should rely on it.
	Unstable *Unstable `ini:"Unstable,omitempty"`

	// WSFC defines the wsfc configurations. It takes precedence over instance's and project's
	// metadata configuration. The default configuration doesn't define values to it, if the user
	// has defined it then we shouldn't even consider metadata values. Users must check if this
	// pointer is nil or not.
	WSFC *WSFC `ini:"wsfc,omitempty"`
}

// AccountManager contains the configurations of AccountManager section.
type AccountManager struct {
	Disable bool `ini:"disable,omitempty"`
}

// Accounts contains the configurations of Accounts section.
type Accounts struct {
	DeprovisionRemove bool   `ini:"deprovision_remove,omitempty"`
	GPasswdAddCmd     string `ini:"gpasswd_add_cmd,omitempty"`
	GPasswdRemoveCmd  string `ini:"gpasswd_remove_cmd,omitempty"`
	GroupAddCmd       string `ini:"groupadd_cmd,omitempty"`
	Groups            string `ini:"groups,omitempty"`
	ReuseHomedir      bool   `ini:"reuse_homedir,omitempty"`
	UserAddCmd        string `ini:"useradd_cmd,omitempty"`
	UserDelCmd        string `ini:"userdel_cmd,omitempty"`
}

// AddressManager contains the configuration of addressManager section.
type AddressManager struct {
	Disable bool `ini:"disable,omitempty"`
}

// Daemons contains the configurations of Daemons section.
type Daemons struct {
	AccountsDaemon  bool `ini:"accounts_daemon,omitempty"`
	ClockSkewDaemon bool `ini:"clock_skew_daemon,omitempty"`
	NetworkDaemon   bool `ini:"network_daemon,omitempty"`
}

// Diagnostics contains the configurations of Diagnostics section.
type Diagnostics struct {
	Enable bool `ini:"enable,omitempty"`
}

// IPForwarding contains the configurations of IPForwarding section.
type IPForwarding struct {
	EthernetProtoID   string `ini:"ethernet_proto_id,omitempty"`
	IPAliases         bool   `ini:"ip_aliases,omitempty"`
	TargetInstanceIPs bool   `ini:"target_instance_ips,omitempty"`
}

// Instance contains the configurations of Instance section.
type Instance struct {
	// InstanceID is a backward compatible key. In the past the instance id was only
	// supported/setup via config file, if we can't read the instance_id file then
	// try honoring this configuration key.
	InstanceID string `ini:"instance_id,omitempty"`

	// InstanceIDDir defines where the instance id file should be read from.
	InstanceIDDir string `ini:"instance_id_dir,omitempty"`
}

// InstanceSetup contains the configurations of InstanceSetup section.
type InstanceSetup struct {
	HostKeyDir       string `ini:"host_key_dir,omitempty"`
	HostKeyTypes     string `ini:"host_key_types,omitempty"`
	NetworkEnabled   bool   `ini:"network_enabled,omitempty"`
	OptimizeLocalSSD bool   `ini:"optimize_local_ssd,omitempty"`
	SetBotoConfig    bool   `ini:"set_boto_config,omitempty"`
	SetHostKeys      bool   `ini:"set_host_keys,omitempty"`
	SetMultiqueue    bool   `ini:"set_multiqueue,omitempty"`
}

// MetadataScripts contains the configurations of MetadataScripts section.
type MetadataScripts struct {
	DefaultShell      string `ini:"default_shell,omitempty"`
	RunDir            string `ini:"run_dir,omitempty"`
	Shutdown          bool   `ini:"shutdown,omitempty"`
	ShutdownWindows   bool   `ini:"shutdown-windows,omitempty"`
	Startup           bool   `ini:"startup,omitempty"`
	StartupWindows    bool   `ini:"startup-windows,omitempty"`
	SysprepSpecialize bool   `ini:"sysprep_specialize,omitempty"`
}

// OSLogin contains the configurations of OSLogin section.
type OSLogin struct {
	CertAuthentication bool `ini:"cert_authentication,omitempty"`
}

// NetworkInterfaces contains the configurations of NetworkInterfaces section.
type NetworkInterfaces struct {
	DHCPCommand  string `ini:"dhcp_command,omitempty"`
	IPForwarding bool   `ini:"ip_forwarding,omitempty"`
	Setup        bool   `ini:"setup,omitempty"`
}

// Snapshots contains the configurations of Snapshots section.
type Snapshots struct {
	Enabled             bool   `ini:"enabled,omitempty"`
	SnapshotServiceIP   string `ini:"snapshot_service_ip,omitempty"`
	SnapshotServicePort int    `ini:"snapshot_service_port,omitempty"`
	TimeoutInSeconds    int    `ini:"timeout_in_seconds,omitempty"`
}

// Unstable contains the configurations of Unstable section. No long term stability or support
// is guaranteed for configurations defined in the Unstable section. By default all flags defined
// in this section is disabled and is intended to isolate under development features.
type Unstable struct {
	PAMLessAuthStack bool `ini:"pamless_auth_stack,omitempty"`
	MDSMTLS          bool `ini:"mds_mtls,omitempty"`
}

// WSFC contains the configurations of WSFC section.
type WSFC struct {
	Addresses string `ini:"addresses,omitempty"`
	Enable    bool   `ini:"enable,omitempty"`
	Port      string `ini:"port,omitempty"`
}

func defaultConfigFile(osName string) string {
	if osName == "windows" {
		return winConfigPath
	}
	return unixConfigPath
}

func defaultDataSources(extraDefaults []byte) []interface{} {
	var res []interface{}
	configFile := defaultConfigFile(runtime.GOOS)

	if len(extraDefaults) > 0 {
		res = append(res, extraDefaults)
	}

	return append(res, []interface{}{
		[]byte(defaultConfig),
		configFile,
		configFile + ".distro",
		configFile + ".template",
	}...)
}

// Load loads default configuration and the configuration from default config files.
func Load(extraDefaults []byte) error {
	opts := ini.LoadOptions{
		Loose:       true,
		Insensitive: true,
	}

	sources := dataSources(extraDefaults)
	cfg, err := ini.LoadSources(opts, sources[0], sources[1:]...)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %+v", err)
	}

	sections := new(Sections)
	if err := cfg.MapTo(sections); err != nil {
		return fmt.Errorf("failed to map configuration to object: %+v", err)
	}

	instance = sections
	return nil
}

// Get returns the configuration's instance previously loaded with Load().
func Get() *Sections {
	if instance == nil {
		panic("cfg package was not initialized, Load() " +
			"should be called in the early initialization code path")
	}
	return instance
}
