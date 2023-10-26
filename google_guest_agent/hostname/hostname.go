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

// Package hostname reconfigures the guest hostname (linux only) and fqdn as necessary. It will do so on a detected change to the metadata hostname, when a new interface is configured, or a new ip address is acquired. It does so by triggering the HostnameReconfigure event, which is also triggerable outside the guest agent through named pipes on windows and unix sockets on linux. All of this behavior is configurable in the guest agent config.
package hostname

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/network"
	mds "github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	lastHostname string //As retrieved from MDS
	lastFqdn     string //As retrieved from MDS
	config       *cfg.Sections
	pipePath     string //Path to dial when triggering events
)

// Init subscribes to the interface up and hostname reconfigure events if the user has enabled network interface setup and either hostname of fqdn management, and sends an initial event to trigger hostame and fqdn configuration.
func Init(ctx context.Context, eventManager *events.Manager) {
	pipePath = network.DefaultPipePath
	config = cfg.Get()
	if config.NetworkInterfaces.Setup && (config.Unstable.SetFqdn || config.Unstable.SetHostname) {
		eventManager.AddWatcher(ctx, network.NewNetworkWatcher(network.DefaultPipePath))
		eventManager.Subscribe(network.IfaceUpEvent, nil, handleIfaceUp)
		eventManager.Subscribe(network.HostnameReconfigureEvent, nil, hostnameReconfigure)
		eventManager.Subscribe(metadata.LongpollEvent, nil, checkMdsHostname)
		if err := triggerHostnameReconfigure(ctx); err != nil {
			logger.Errorf("could not trigger initial hostname configuration: %v", err)
		}
	}
}

func fetchHostnameFromMds(ctx context.Context, mdsclient mds.MDSClientInterface) (hostname string, fqdn string, err error) {
	mdshostname, err := mdsclient.GetKey(ctx, "instance/hostname", nil)
	if err != nil {
		return
	}
	fqdn = mdshostname
	if config.Unstable.FqdnAsHostname {
		hostname = fqdn
	} else {
		var ok bool
		hostname, _, ok = strings.Cut(fqdn, ".")
		if !ok {
			err = fmt.Errorf("metadata hostname %s is not an FQDN", fqdn)
		}
	}
	return
}

func handleIfaceUp(ctx context.Context, evType string, data interface{}, evData *events.EventData) bool {
	logger.Infof("Notified of new network interface, triggering hostname reconfigure.")
	triggerHostnameReconfigure(ctx)
	return true
}

func checkMdsHostname(ctx context.Context, evType string, data interface{}, evData *events.EventData) bool {
	descriptor, ok := data.(mds.Descriptor)
	if !ok {
		logger.Errorf("Bad descriptor from MDS longpoll event")
		return true
	}
	var hostname, fqdn string
	fqdn = descriptor.Instance.Hostname
	if config.Unstable.FqdnAsHostname {
		hostname = fqdn
	} else {
		var ok bool
		hostname, _, ok = strings.Cut(fqdn, ".")
		if !ok {
			logger.Errorf("metadata hostname %s is not an FQDN", fqdn)
			return true
		}
	}
	if (hostname != lastHostname && config.Unstable.SetHostname) || (fqdn != lastFqdn && config.Unstable.SetFqdn) {
		logger.Infof("hostname or fqdn changed in MDS and this change is managed by guest agent")
		logger.Debugf("old hostname: %s new hostname: %s", lastHostname, hostname)
		logger.Debugf("old fqdn: %s new fqdn: %s", lastFqdn, fqdn)
		err := triggerHostnameReconfigure(ctx)
		if err != nil {
			logger.Errorf("Error triggering hostname reconfigure: %v", err)
		}
	}
	return true
}

func hostnameReconfigure(ctx context.Context, evType string, data interface{}, evData *events.EventData) bool {
	logger.Infof("Notified of hostnameReconfigure event, reconfiguring hostname")
	h, f, err := fetchHostnameFromMds(ctx, mds.New())
	if err != nil {
		logger.Errorf("Could not get hostname and fqdn from MDS: %v", err)
		return true
	}
	lastHostname = h
	lastFqdn = f
	if config.Unstable.SetHostname {
		err := setHostname(ctx, h)
		if err != nil {
			logger.Errorf("could not set hostname: %v", err)
		}
	}
	if config.Unstable.SetFqdn {
		err := setFqdn(f)
		if err != nil {
			logger.Errorf("could not set fqdn: %v", err)
		}
	}
	return true
}

func setFqdn(fqdn string) error {
	var hostname string
	var err error
	if runtime.GOOS == "windows" {
		// Windows truncates hostnames to 15 characters when they are set so we cannot rely on the OS to report to full hostname from os.Hostname()
		// Use what we got from the MDS, even though this may differ from the actual system hostname.
		hostname = lastHostname
	} else {
		hostname, err = os.Hostname()
		if err != nil {
			return err
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return err
	}
	return writeHosts(hostname, fqdn, platformHostsFile, addrs)
}

func writeHosts(hostname, fqdn, hostsFile string, addrs []net.Addr) error {
	var gcehosts, aliases strings.Builder
	for _, a := range strings.Split(config.Unstable.AdditionalAliases, ",") {
		aliases.WriteString(a + " ")
	}
	gcehosts.WriteString("#google-guest-agent-hosts-begin" + newline + "# Changes in this section will be overwritten" + newline + "# See https://cloud.google.com/compute/docs/images/guest-environment for information on configuring the guest environment" + newline + "169.254.169.254 metadata.google.internal" + newline)
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			logger.Errorf("Could not parse address %s: %v", addr, err)
			continue
		}
		if !ip.IsLoopback() {
			gcehosts.WriteString(fmt.Sprintf("%s %s %s %s"+newline+"", ip, fqdn, hostname, aliases.String()))
		}
	}
	gcehosts.WriteString("#google-guest-agent-hosts-end" + newline)
	hosts, err := os.ReadFile(hostsFile)
	if err != nil {
		return err
	}
	gcehostsRe := regexp.MustCompile(`(?m)^#google-guest-agent-hosts-begin` + newline + `(.*` + newline + `)*?#google-guest-agent-hosts-end` + newline)
	if index := gcehostsRe.FindIndex(hosts); len(index) == 2 {
		hosts = append(hosts[:index[0]], append([]byte(gcehosts.String()), hosts[index[1]:]...)...)
	} else {
		hosts = append([]byte(gcehosts.String()), hosts...)
	}
	// Platform specific, overwrite x file with y contents as atomicly as possible.
	return overwrite(hostsFile, hosts)
}
