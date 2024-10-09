//  Copyright 2024 Google LLC
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

package manager

func init() {
	// knownNetworkManagers is a list of supported/available network managers.
	knownNetworkManagers = []Service{
		&netplan{
			netplanConfigDir:  "/run/netplan/",
			networkdDropinDir: "/run/systemd/network/",
			priority:          20,
		},
		&wicked{
			configDir: defaultWickedConfigDir,
		},
		&networkManager{
			configDir:         defaultNetworkManagerConfigDir,
			networkScriptsDir: defaultNetworkScriptsDir,
		},
		&systemdNetworkd{
			configDir:          "/usr/lib/systemd/network",
			networkCtlKeys:     []string{"AdministrativeState", "SetupState"},
			priority:           defaultSystemdNetworkdPriority,
			deprecatedPriority: deprecatedPriority,
		},
		&dhclient{},
	}
}
