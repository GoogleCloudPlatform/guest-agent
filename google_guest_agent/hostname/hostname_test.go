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

package hostname

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/network"
	mds "github.com/GoogleCloudPlatform/guest-agent/metadata"
)

type fakeMDSClient struct {
	mds.MDSClientInterface
	descriptor *mds.Descriptor
	key        string
	err        error
}

func (c *fakeMDSClient) Get(context.Context) (*mds.Descriptor, error) { return c.descriptor, c.err }

func (c *fakeMDSClient) GetKey(context.Context, string, map[string]string) (string, error) {
	return c.key, c.err
}

func TestFetchHostnameFromMds(t *testing.T) {
	cfg.Load(nil)
	testcases := []struct {
		name        string
		mdsResponse string
		cfg         *cfg.NetworkInterfaces
		hostname    string
		fqdn        string
	}{
		{
			name:        "default case",
			mdsResponse: "tc1.example.com",
			hostname:    "tc1",
			fqdn:        "tc1.example.com",
			cfg:         cfg.Get().NetworkInterfaces,
		},
		{
			name:        "hostname as fqdn",
			mdsResponse: "tc1.example.com",
			hostname:    "tc1.example.com",
			fqdn:        "tc1.example.com",
			cfg:         &cfg.NetworkInterfaces{FqdnAsHostname: true},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			netconfig = tc.cfg
			fake := &fakeMDSClient{key: tc.mdsResponse}
			h, f, err := fetchHostnameFromMds(context.Background(), fake)
			if err != nil {
				t.Fatal(err)
			}
			if h != tc.hostname {
				t.Errorf("unexpected hostname from mds, want %s got %s", tc.hostname, h)
			}
			if f != tc.fqdn {
				t.Errorf("unexpected fqdn from mds, want %s got %s", tc.fqdn, f)
			}
		})
	}
}

func TestFetchHostnameFromMdsError(t *testing.T) {
	cfg.Load(nil)
	testcases := []struct {
		name        string
		mdsResponse string
		cfg         *cfg.NetworkInterfaces
	}{
		{
			name:        "non fqdn",
			mdsResponse: "tc1",
			cfg:         cfg.Get().NetworkInterfaces,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			netconfig = tc.cfg
			fake := &fakeMDSClient{key: tc.mdsResponse}
			_, _, err := fetchHostnameFromMds(context.Background(), fake)
			if err == nil {
				t.Errorf("got nil error when parsing mds response %q", tc.mdsResponse)
			}
		})
	}
}

func TestCheckMdsHostname(t *testing.T) {
	testPipePath, err := getTestPipePath()
	pipePath = testPipePath
	if err != nil {
		t.Fatal(err)
	}
	netw := network.NewNetworkWatcher(testPipePath)
	cfg.Load(nil)
	testcases := []struct {
		name               string
		cfg                *cfg.NetworkInterfaces
		lastHostname       string
		lastFqdn           string
		descriptor         mds.Descriptor
		eventShouldTrigger bool
	}{
		{
			name: "hostname changed",
			cfg: &cfg.NetworkInterfaces{
				FqdnAsHostname: false,
				SetHostname:    true,
				SetFqdn:        false,
			},
			lastHostname:       "host1",
			lastFqdn:           "host1.example.com",
			descriptor:         mds.Descriptor{Instance: mds.Instance{Hostname: "host2.example.com"}},
			eventShouldTrigger: true,
		},
		{
			name: "fqdn changed",
			cfg: &cfg.NetworkInterfaces{
				FqdnAsHostname: false,
				SetHostname:    false,
				SetFqdn:        true,
			},
			lastHostname:       "host1",
			lastFqdn:           "host1.example.net",
			descriptor:         mds.Descriptor{Instance: mds.Instance{Hostname: "host1.example.com"}},
			eventShouldTrigger: true,
		},
		{
			name: "no change",
			cfg: &cfg.NetworkInterfaces{
				FqdnAsHostname: false,
				SetHostname:    true,
				SetFqdn:        true,
			},
			lastHostname:       "host1",
			lastFqdn:           "host1.example.com",
			descriptor:         mds.Descriptor{Instance: mds.Instance{Hostname: "host1.example.com"}},
			eventShouldTrigger: false,
		},
		{
			name: "ignore changes",
			cfg: &cfg.NetworkInterfaces{
				FqdnAsHostname: false,
				SetHostname:    false,
				SetFqdn:        false,
			},
			lastHostname:       "host1",
			lastFqdn:           "host1.example.net",
			descriptor:         mds.Descriptor{Instance: mds.Instance{Hostname: "host2.example.com"}},
			eventShouldTrigger: false,
		},
		{
			name: "fqnashostname changed",
			cfg: &cfg.NetworkInterfaces{
				FqdnAsHostname: true,
				SetHostname:    true,
				SetFqdn:        false,
			},
			lastHostname:       "host1",
			lastFqdn:           "host1.example.net",
			descriptor:         mds.Descriptor{Instance: mds.Instance{Hostname: "host1.example.net"}},
			eventShouldTrigger: true,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(300)*time.Millisecond)
			defer cancel()
			netconfig = tc.cfg
			lastHostname = tc.lastHostname
			lastFqdn = tc.lastFqdn
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, _, err := netw.Run(ctx, network.HostnameReconfigureEvent)
				// Exceeding deadline in cases where no event is triggered is intentional.
				if err != nil && err != context.DeadlineExceeded {
					t.Errorf("failure in watcher execution: %s", err)
				} else if r != tc.eventShouldTrigger {
					t.Errorf("unexpected event trigger, got %v want %v", r, tc.eventShouldTrigger)
				}
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Wait for the watcher to start listening
				time.Sleep(time.Duration(150) * time.Millisecond)
				r := checkMdsHostname(ctx, "", tc.descriptor, nil)
				if !r {
					t.Errorf("checkMdsHostname did not resubscribe to event")
				}
			}()
			wg.Wait()
		})
	}
}

type testAddr struct{ s string }

func (t testAddr) Network() string { return t.s }
func (t testAddr) String() string  { return t.s }

func TestWriteHosts(t *testing.T) {
	cfg.Load(nil)
	testcases := []struct {
		name          string
		cfg           *cfg.NetworkInterfaces
		inputhosts    string
		inputhostname string
		inputfqdn     string
		inputaddrs    []net.Addr
		output        string
	}{
		{
			name:          "empty hosts",
			cfg:           cfg.Get().NetworkInterfaces,
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "#google-guest-agent-hosts-begin" + newline + "# Changes in this section will be overwritten" + newline + "# See https://cloud.google.com/compute/docs/images/guest-environment for information on configuring the guest environment" + newline + "169.254.169.254 metadata.google.internal" + newline + "10.0.0.10 tc1.example.com tc1  " + newline + "#google-guest-agent-hosts-end" + newline + "",
		},
		{
			name:          "loopback addresses",
			cfg:           cfg.Get().NetworkInterfaces,
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}, testAddr{"127.0.0.1/8"}, testAddr{"::1/128"}},
			output:        "#google-guest-agent-hosts-begin" + newline + "# Changes in this section will be overwritten" + newline + "# See https://cloud.google.com/compute/docs/images/guest-environment for information on configuring the guest environment" + newline + "169.254.169.254 metadata.google.internal" + newline + "10.0.0.10 tc1.example.com tc1  " + newline + "#google-guest-agent-hosts-end" + newline + "",
		},
		{
			name:          "two addresses",
			cfg:           cfg.Get().NetworkInterfaces,
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}, testAddr{"10.0.0.20/16"}},
			output:        "#google-guest-agent-hosts-begin" + newline + "# Changes in this section will be overwritten" + newline + "# See https://cloud.google.com/compute/docs/images/guest-environment for information on configuring the guest environment" + newline + "169.254.169.254 metadata.google.internal" + newline + "10.0.0.10 tc1.example.com tc1  " + newline + "10.0.0.20 tc1.example.com tc1  " + newline + "#google-guest-agent-hosts-end" + newline + "",
		},
		{
			name:          "two aliases",
			cfg:           &cfg.NetworkInterfaces{AdditionalAliases: "tc2,tc3"},
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "#google-guest-agent-hosts-begin" + newline + "# Changes in this section will be overwritten" + newline + "# See https://cloud.google.com/compute/docs/images/guest-environment for information on configuring the guest environment" + newline + "169.254.169.254 metadata.google.internal" + newline + "10.0.0.10 tc1.example.com tc1 tc2 tc3 " + newline + "#google-guest-agent-hosts-end" + newline + "",
		},
		{
			name:          "existing hosts at beginning",
			cfg:           cfg.Get().NetworkInterfaces,
			inputhosts:    "127.0.0.1 pre-existing.host.com" + newline + "#google-guest-agent-hosts-begin" + newline + "#google-guest-agent-hosts-end" + newline + "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "127.0.0.1 pre-existing.host.com" + newline + "#google-guest-agent-hosts-begin" + newline + "# Changes in this section will be overwritten" + newline + "# See https://cloud.google.com/compute/docs/images/guest-environment for information on configuring the guest environment" + newline + "169.254.169.254 metadata.google.internal" + newline + "10.0.0.10 tc1.example.com tc1  " + newline + "#google-guest-agent-hosts-end" + newline + "",
		},
		{
			name:          "existing hosts at end",
			cfg:           cfg.Get().NetworkInterfaces,
			inputhosts:    "#google-guest-agent-hosts-begin" + newline + "#google-guest-agent-hosts-end" + newline + "127.0.0.1 pre-existing.host.com" + newline + "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "#google-guest-agent-hosts-begin" + newline + "# Changes in this section will be overwritten" + newline + "# See https://cloud.google.com/compute/docs/images/guest-environment for information on configuring the guest environment" + newline + "169.254.169.254 metadata.google.internal" + newline + "10.0.0.10 tc1.example.com tc1  " + newline + "#google-guest-agent-hosts-end" + newline + "127.0.0.1 pre-existing.host.com" + newline + "",
		},
		{
			name:          "two gce hosts blocks",
			cfg:           cfg.Get().NetworkInterfaces,
			inputhosts:    "#google-guest-agent-hosts-begin" + newline + "#google-guest-agent-hosts-end" + newline + "#google-guest-agent-hosts-begin" + newline + "127.0.0.1 pre-existing.host.com" + newline + "#google-guest-agent-hosts-end" + newline + "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "#google-guest-agent-hosts-begin" + newline + "# Changes in this section will be overwritten" + newline + "# See https://cloud.google.com/compute/docs/images/guest-environment for information on configuring the guest environment" + newline + "169.254.169.254 metadata.google.internal" + newline + "10.0.0.10 tc1.example.com tc1  " + newline + "#google-guest-agent-hosts-end" + newline + "#google-guest-agent-hosts-begin" + newline + "127.0.0.1 pre-existing.host.com" + newline + "#google-guest-agent-hosts-end" + newline + "",
		},
		{
			name:          "unterminated gce hosts block",
			cfg:           cfg.Get().NetworkInterfaces,
			inputhosts:    "#google-guest-agent-hosts-begin" + newline + "127.0.0.1 pre-existing.host.com" + newline + "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "#google-guest-agent-hosts-begin" + newline + "# Changes in this section will be overwritten" + newline + "# See https://cloud.google.com/compute/docs/images/guest-environment for information on configuring the guest environment" + newline + "169.254.169.254 metadata.google.internal" + newline + "10.0.0.10 tc1.example.com tc1  " + newline + "#google-guest-agent-hosts-end" + newline + "#google-guest-agent-hosts-begin" + newline + "127.0.0.1 pre-existing.host.com" + newline + "",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			netconfig = tc.cfg
			testfile, err := os.CreateTemp(os.TempDir(), "test-writehosts-"+strings.ReplaceAll(tc.name, " ", "-"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = testfile.Write([]byte(tc.inputhosts)); err != nil {
				t.Fatal(err)
			}
			hostsfile := testfile.Name()
			if err = testfile.Close(); err != nil {
				t.Fatal(err)
			}
			if err := writeHosts(tc.inputhostname, tc.inputfqdn, hostsfile, tc.inputaddrs); err != nil {
				t.Fatal(err)
			}
			output, err := os.ReadFile(hostsfile)
			if err != nil {
				t.Fatal(err)
			}
			if string(output) != tc.output {
				t.Errorf("unexpected output from writeHosts, want "+newline+"%q"+newline+"but got"+newline+"%q", tc.output, output)
			}
		})
	}
}
