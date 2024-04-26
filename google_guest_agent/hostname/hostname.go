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

// Package hostname reconfigures the guest hostname (linux only) and fqdn as
// necessary. It will do so on a detected change to the metadata hostname, when
// a new interface is configured, or a new ip address is acquired. It does so
// by triggering the HostnameReconfigure event, which is also triggerable
// outside the guest agent through named pipes on windows and unix sockets on
// linux. All of this behavior is configurable in the guest agent configuration.
package hostname

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/command"
	mds "github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	// ReconfigureHostnameCommand is the command id registered for hostname
	// configuration.
	ReconfigureHostnameCommand = "agent.hostname.reconfigurehostname"
	disallowedConfigurations   = map[string]bool{"": true, "metadata.google.internal": true}
	hostnameFqdnMu             = new(sync.Mutex)
	hostname                   string //As retrieved from MDS
	fqdn                       string //As retrieved from MDS
)

// ReconfigureHostnameRequest is the structure of requests to the
// ReconfigureHostnameCommand.
type ReconfigureHostnameRequest struct {
	command.Request
}

// ReconfigureHostnameResponse is the structure of responses from the
// ReconfigureHostnameCommand.
// Status code meanings:
// 0: everything ok
// 1: error setting hostname
// 2: error setting fqdn
// 3: error setting hostname and fqdn
type ReconfigureHostnameResponse struct {
	command.Response
	// Hostname is the hostname which was set. Empty if unset, either due to
	// configuration or error.
	Hostname string
	// Fqdn is the hostname which was set. Empty if unset, either due to
	// configuration or error.
	Fqdn string
}

// Init registers a hostname command handler and subscribes to the metadata
// longpoll event if the user has enabled network interface setup and either
// hostname or fqdn management.
func Init(ctx context.Context, mdsclient mds.MDSClientInterface) {
	if !cfg.Get().Unstable.SetFqdn && !cfg.Get().Unstable.SetHostname {
		return
	}
	mdshostname, err := mdsclient.GetKey(ctx, path.Join("instance", "hostname"), nil)
	if err != nil {
		logger.Errorf("could not get metadata hostname from MDS: %v", err)
		return
	}
	hostname = mdshostname
	fqdn = mdshostname
	if !cfg.Get().Unstable.FqdnAsHostname {
		mdsshorthostname, _, ok := strings.Cut(mdshostname, ".")
		if !ok {
			logger.Errorf("metadata hostname %s is not an fqdn", fqdn)
		} else {
			hostname = mdsshorthostname
		}
	}
	b, err := ReconfigureHostname(nil)
	if err != nil {
		logger.Errorf("failed to call reconfigurehostname during setup: %v", err)
	} else {
		var resp ReconfigureHostnameResponse
		err := json.Unmarshal(b, &resp)
		if err != nil {
			logger.Errorf("malformed response from reconfigurehostname: %v", err)
		} else if resp.Status != 0 {
			logger.Errorf("error %d reconfiguring hostname: %s", resp.Status, resp.StatusMessage)
		}
	}
	command.Get().RegisterHandler(ReconfigureHostnameCommand, ReconfigureHostname)
	initPlatform(ctx)
}

// Close stops listening for events and unregisters command handlers
func Close() {
	command.Get().UnregisterHandler(ReconfigureHostnameCommand)
}

// ReconfigureHostname takes a ReconfigureHostnameRequest as a []byte-encoded
// json blob and returns a ReconfigureHostnameResponse []byte-encoded json blob.
func ReconfigureHostname(b []byte) ([]byte, error) {
	hostnameFqdnMu.Lock()
	defer hostnameFqdnMu.Unlock()

	var req ReconfigureHostnameRequest
	err := json.Unmarshal(b, &req)
	if err != nil {
		return nil, err
	}

	var resp ReconfigureHostnameResponse
	if cfg.Get().Unstable.SetHostname {
		if disallowedConfigurations[hostname] {
			resp.Status++
			resp.StatusMessage += fmt.Sprintf("disallowed hostname: %q", hostname)
		} else if err := setHostname(hostname); err != nil {
			resp.Status++
			resp.StatusMessage += err.Error()
		} else {
			resp.Hostname = hostname
		}
	}
	if cfg.Get().Unstable.SetFqdn {
		h := hostname
		var err error
		if runtime.GOOS != "windows" {
			// Get the hostname from the OS in case we are configured to manage only the
			// fqdn. Don't do this on windows because:
			// 1) The hostname is always managed on Windows (albeit not by the agent: see
			// https://github.com/GoogleCloudPlatform/compute-image-windows/blob/master/sysprep/activate_instance.ps1)
			// 2) Windows truncates hostnames to 15 characters when they are set so we
			// cannot rely on the OS to report the full hostname.
			h, err = os.Hostname()
		}
		if disallowedConfigurations[fqdn] {
			err = fmt.Errorf("disallowed fqdn: %q", fqdn)
		}
		if err == nil {
			err = setFqdn(h, fqdn)
		}
		if err != nil {
			resp.Status += 2
			resp.StatusMessage += err.Error()
		} else {
			resp.Fqdn = fqdn
		}
	}
	return json.Marshal(resp)
}

var setFqdn = func(hostname, fqdn string) error {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return err
	}
	return writeHosts(hostname, fqdn, platformHostsFile, addrs)
}

func writeHosts(hostname, fqdn, hostsFile string, addrs []net.Addr) error {
	var gcehosts []byte
	var aliases string
	hosts, err := os.ReadFile(hostsFile)
	if err != nil {
		return err
	}
	for _, l := range strings.Split(string(hosts), newline) {
		if strings.HasSuffix(l, "# Added by Google") || l == "" {
			continue
		}
		gcehosts = append(gcehosts, []byte(l)...)
		gcehosts = append(gcehosts, []byte(newline)...)
	}
	for _, a := range strings.Split(cfg.Get().Unstable.AdditionalAliases, ",") {
		aliases += a + " "
	}
	gcehosts = append(gcehosts, []byte(fmt.Sprintf("169.254.169.254 metadata.google.internal # Added by Google%s", newline))...)
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			logger.Errorf("Could not parse address %s: %v", addr, err)
			continue
		}
		if !ip.IsLoopback() {
			gcehosts = append(gcehosts, []byte(fmt.Sprintf("%s %s %s %s # Added by Google%s", ip, fqdn, hostname, aliases, newline))...)
		}
	}
	return overwrite(hostsFile, gcehosts)
}
