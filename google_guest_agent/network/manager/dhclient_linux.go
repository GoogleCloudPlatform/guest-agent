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

// Package manager is responsible for detecting the current network manager service, and
// writing and rolling back appropriate configurations for each network manager service.
package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/ps"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	// The base directory for dhclient files managed by guest agent.
	defaultBaseDhclientDir = "/var/google-dhclient.d"

	// For finer control of the execution, dhclient is invoked for
	// each interface individually such that each call will have its
	// own PID file. This is where those PID files are expected to be
	// written.
	pidFileDir = "pids"

	// Similar thing for PID files, but for DHClient leases.
	leaseFileDir = "leases"
)

var (
	// ipv4 is a wrapper containing the protocol version and its respective
	// dhclient argument.
	ipv4 = ipVersion{"ipv4", "-4"}

	// ipv6 is a wrapper containing the protocol version and its respective
	// dhclient argument.
	ipv6 = ipVersion{"ipv6", "-6"}
)

var (
	// baseDhclientDir points to the base directory for DHClient leases and PIDs.
	baseDhclientDir = defaultBaseDhclientDir
)

// ipVersion is a wrapper containing the human-readable version string and
// the respective dhclient argument.
type ipVersion struct {
	// version is the human-readable IP protocol version.
	version string

	// dhclientArg is the respective argument for DHClient invocation.
	dhclientArg string
}

// dhclient implements the manager.Service interface for dhclient use cases.
type dhclient struct{}

// init adds this network manager service to the list of known network managers.
// DHClient will be the default fallback.
func init() {
	registerManager(&dhclient{}, true)
}

// Name returns the name of the network manager service.
func (n dhclient) Name() string {
	return "dhclient"
}

// IsManaging checks if the dhclient CLI is available.
func (n dhclient) IsManaging(ctx context.Context, iface string) (bool, error) {
	// Check if the dhclient CLI exists.
	if _, err := execLookPath("dhclient"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("error looking up dhclient path: %v", err)
	}
	return true, nil
}

// Setup sets up the non-primary interfaces with dhclient, having different setup procedures
// for IPv6 network interfaces and IPv4 network interfaces.
func (n dhclient) Setup(ctx context.Context, config *cfg.Sections, payload []metadata.NetworkInterfaces) error {
	dhcpCommand := config.NetworkInterfaces.DHCPCommand
	if dhcpCommand != "" {
		tokens := strings.Split(dhcpCommand, " ")
		return run.Quiet(ctx, tokens[0], tokens[1:]...)
	}

	// Create the necessary directories.
	if err := os.MkdirAll(fmt.Sprintf("%s/%s", baseDhclientDir, pidFileDir), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}
	if err := os.MkdirAll(fmt.Sprintf("%s/%s", baseDhclientDir, leaseFileDir), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// Get all interfaces separated by ipv4 and ipv6.
	googleInterfaces, googleIpv6Interfaces := interfaceListsIpv4Ipv6(payload)
	obtainIpv4Interfaces, obtainIpv6Interfaces, releaseIpv6Interfaces, err := partitionInterfaces(ctx, googleInterfaces, googleIpv6Interfaces)
	if err != nil {
		return fmt.Errorf("error partitioning interfaces: %v", err)
	}

	// Release IPv6 leases.
	for _, iface := range releaseIpv6Interfaces {
		if err := runDhclient(ctx, ipv6, iface, true); err != nil {
			logger.Errorf("failed to run dhclient: %+x", err)
		}
	}

	// Setup IPV4.
	for _, iface := range obtainIpv4Interfaces {
		if err := runDhclient(ctx, ipv4, iface, false); err != nil {
			logger.Errorf("failed to run dhclient: %+x", err)
		}
	}

	if len(obtainIpv6Interfaces) == 0 {
		return nil
	}

	// Wait for tentative IPs to resolve as part of SLAAC for primary network interface.
	tentative := []string{"-6", "-o", "a", "s", "dev", googleInterfaces[0], "scope", "link", "tentative"}
	for i := 0; i < 5; i++ {
		res := run.WithOutput(ctx, "ip", tentative...)
		if res.ExitCode == 0 && res.StdOut == "" {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Setup IPv6.
	for _, iface := range obtainIpv6Interfaces {
		// Set appropriate system values.
		val := fmt.Sprintf("net.ipv6.conf.%s.accept_ra_rt_info_max_plen=128", iface)
		if err := run.Quiet(ctx, "sysctl", val); err != nil {
			return err
		}

		if err := runDhclient(ctx, ipv6, iface, false); err != nil {
			logger.Errorf("failed to run dhclient: %+x", err)
		}
	}
	return nil
}

// pidFilePath gets the expected file path for the PID pertaining to the provided
// interface and IP version.
func pidFilePath(iface string, ipVersion ipVersion) string {
	return path.Join(baseDhclientDir, pidFileDir, fmt.Sprintf("dhclient.google-guest-agent.%s.%s.pid", iface, ipVersion.version))
}

// leaseFilePath gets the expected file path for the leases pertaining to the provided
// interface and IP version.
func leaseFilePath(iface string, ipVersion ipVersion) string {
	return path.Join(baseDhclientDir, leaseFileDir, fmt.Sprintf("dhclient.google-guest-agent.%s.%s.pid", iface, ipVersion.version))
}

// runDhclient obtains a lease with the provided IP version for the given
// network interface. If release is set, this will release leases instead.
func runDhclient(ctx context.Context, ipVersion ipVersion, nic string, release bool) error {
	pidFile := pidFilePath(nic, ipVersion)
	leaseFile := leaseFilePath(nic, ipVersion)

	dhclientArgs := []string{ipVersion.dhclientArg, "-pf", pidFile, "-lf", leaseFile}

	if release {
		// Only release if the flag is set.
		releaseArgs := append(dhclientArgs, "-r", nic)
		if err := run.Quiet(ctx, "dhclient", releaseArgs...); err != nil {
			return fmt.Errorf("error releasing lease for %s: %v", nic, err)
		}
	} else {
		// Now obtain a lease if release is not set.
		dhclientArgs = append(dhclientArgs, nic)
		if err := run.Quiet(ctx, "dhclient", dhclientArgs...); err != nil {
			return fmt.Errorf("error running dhclient for %s: %v", nic, err)
		}
	}
	return nil
}

// paritionInterfaces creates 3 lists of interfaces
// The first list contains interfaces for which to obtain an IPv4 lease.
// The second list contains interfaces for which to obtain an IPv6 lease.
// The third list contains interfaces for which to release their IPv6 lease.
func partitionInterfaces(ctx context.Context, interfaces, ipv6Interfaces []string) ([]string, []string, []string, error) {
	var obtainIpv4Interfaces []string
	var obtainIpv6Interfaces []string
	var releaseIpv6Interfaces []string

	for i, iface := range interfaces {
		if !shouldManageInterface(i == 0) {
			// Do not setup anything for this interface to avoid duplicate processes.
			continue
		}

		// Check for IPv4 interfaces for which to obtain a lease.
		processExists, err := dhclientProcessExists(ctx, iface, ipv4)
		if err != nil {
			return nil, nil, nil, err
		}
		if !processExists {
			obtainIpv4Interfaces = append(obtainIpv4Interfaces, iface)
		}

		// Check for IPv6 interfaces for which to obtain a lease.
		processExists, err = dhclientProcessExists(ctx, iface, ipv6)
		if err != nil {
			return nil, nil, nil, err
		}

		if slices.Contains(ipv6Interfaces, iface) && !processExists {
			// Obtain a lease and spin up the DHClient process.
			obtainIpv6Interfaces = append(obtainIpv6Interfaces, iface)
		} else if !slices.Contains(ipv6Interfaces, iface) && processExists {
			// Release the lease since the DHClient IPv6 process is running,
			// but the interface is no longer IPv6.
			releaseIpv6Interfaces = append(releaseIpv6Interfaces, iface)
		}
	}

	return obtainIpv4Interfaces, obtainIpv6Interfaces, releaseIpv6Interfaces, nil
}

// dhclientProcessExists checks if a dhclient process for the provided
// interface and IP version exists.
func dhclientProcessExists(ctx context.Context, iface string, ipVersion ipVersion) (bool, error) {
	processes, err := ps.Find(".*dhclient.*")
	if err != nil {
		return false, fmt.Errorf("error finding dhclient process: %v", err)
	}

	// Check for any dhclient process that contains the iface and IP version provided.
	for _, process := range processes {
		commandLine := process.CommandLine

		containsInterface := slices.Contains(commandLine, iface)
		containsProtocolArg := slices.Contains(commandLine, ipVersion.dhclientArg)

		if containsInterface {
			// IPv4 DHClient calls don't necessarily have the '-4' flag set.
			if ipVersion == ipv6 {
				return containsProtocolArg, nil
			}
			if ipVersion == ipv4 && !slices.Contains(commandLine, ipv6.dhclientArg) {
				return true, nil
			}
		}
	}
	return false, nil
}

// Rollback releases all leases from DHClient, effectively undoing the dhclient configurations.
func (n dhclient) Rollback(ctx context.Context, payload []metadata.NetworkInterfaces) error {
	googleInterfaces, googleIpv6Interfaces := interfaceListsIpv4Ipv6(payload)

	// Release all the interface leases from dhclient.
	for _, iface := range googleInterfaces {
		if err := runDhclient(ctx, ipv4, iface, true); err != nil {
			return err
		}
	}
	for _, iface := range googleIpv6Interfaces {
		if err := runDhclient(ctx, ipv6, iface, true); err != nil {
			return err
		}
	}
	return nil
}
