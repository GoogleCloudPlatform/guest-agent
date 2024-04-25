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
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path"
	"runtime"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/command"
	mds "github.com/GoogleCloudPlatform/guest-agent/metadata"
)

func TestReconfigureHostname(t *testing.T) {
	cfg.Load(nil)
	setFqdnOrig := setFqdn
	setHostnameOrig := setHostname
	t.Cleanup(func() { setFqdn = setFqdnOrig; setHostname = setHostnameOrig })
	testcases := []struct {
		name         string
		cfg          *cfg.Sections
		lastHostname string
		lastFqdn     string
		setFqdn      func(string, string) error
		setHostname  func(string) error
		req          ReconfigureHostnameRequest
		resp         ReconfigureHostnameResponse
	}{
		{
			name: "successful reconfigure all",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    true,
					SetFqdn:        true,
				},
			},
			setFqdn:     func(string, string) error { return nil },
			setHostname: func(string) error { return nil },
			req:         ReconfigureHostnameRequest{},
			resp: ReconfigureHostnameResponse{
				Hostname: "host1",
				Fqdn:     "host1.example.com",
			},
			lastHostname: "host1",
			lastFqdn:     "host1.example.com",
		},
		{
			name: "fqdn as hostname",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: true,
					SetHostname:    true,
					SetFqdn:        true,
				},
			},
			setFqdn:     func(string, string) error { return nil },
			setHostname: func(string) error { return nil },
			req:         ReconfigureHostnameRequest{},
			resp: ReconfigureHostnameResponse{
				Hostname: "host1.example.com",
				Fqdn:     "host1.example.com",
			},
			lastHostname: "host1",
			lastFqdn:     "host1.example.com",
		},
		{
			name: "reconfigure hostname",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    true,
					SetFqdn:        false,
				},
			},
			setFqdn:     func(string, string) error { return nil },
			setHostname: func(string) error { return nil },
			req:         ReconfigureHostnameRequest{},
			resp: ReconfigureHostnameResponse{
				Hostname: "host1",
			},
			lastHostname: "host1",
			lastFqdn:     "host1.example.com",
		},
		{
			name: "reconfigure fqdn",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    false,
					SetFqdn:        true,
				},
			},
			setFqdn:     func(string, string) error { return nil },
			setHostname: func(string) error { return nil },
			req:         ReconfigureHostnameRequest{},
			resp: ReconfigureHostnameResponse{
				Fqdn: "host1.example.com",
			},
			lastHostname: "host1",
			lastFqdn:     "host1.example.com",
		},
		{
			name: "fail to reconfigure hostname",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    true,
					SetFqdn:        true,
				},
			},
			setFqdn:     func(string, string) error { return nil },
			setHostname: func(string) error { return fmt.Errorf("hostname failure") },
			req:         ReconfigureHostnameRequest{},
			resp: ReconfigureHostnameResponse{
				Response: command.Response{Status: 1, StatusMessage: "hostname failure"},
				Fqdn:     "host1.example.com",
			},
			lastHostname: "host1",
			lastFqdn:     "host1.example.com",
		},
		{
			name: "fail to reconfigure fqdn",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    true,
					SetFqdn:        true,
				},
			},
			setFqdn:     func(string, string) error { return fmt.Errorf("fqdn failure") },
			setHostname: func(string) error { return nil },
			req:         ReconfigureHostnameRequest{},
			resp: ReconfigureHostnameResponse{
				Response: command.Response{Status: 2, StatusMessage: "fqdn failure"},
				Hostname: "host1",
			},
			lastHostname: "host1",
			lastFqdn:     "host1.example.com",
		},
		{
			name: "fail to reconfigure hostname and fqdn",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    true,
					SetFqdn:        true,
				},
			},
			setFqdn:     func(string, string) error { return fmt.Errorf("fqdn failure") },
			setHostname: func(string) error { return fmt.Errorf("hostname failure") },
			req:         ReconfigureHostnameRequest{},
			resp: ReconfigureHostnameResponse{
				Response: command.Response{Status: 3, StatusMessage: "hostname failurefqdn failure"},
			},
			lastHostname: "host1",
			lastFqdn:     "host1.example.com",
		},
		{
			name: "empty hostname",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    true,
					SetFqdn:        true,
				},
			},
			setFqdn:     func(string, string) error { return nil },
			setHostname: func(string) error { return nil },
			req:         ReconfigureHostnameRequest{},
			resp: ReconfigureHostnameResponse{
				Response: command.Response{Status: 1, StatusMessage: "disallowed hostname: \"\""},
				Fqdn:     "host1.example.com",
			},
			lastHostname: "",
			lastFqdn:     "host1.example.com",
		},
		{
			name: "empty fqdn",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    true,
					SetFqdn:        true,
				},
			},
			setFqdn:     func(string, string) error { return nil },
			setHostname: func(string) error { return nil },
			req:         ReconfigureHostnameRequest{},
			resp: ReconfigureHostnameResponse{
				Response: command.Response{Status: 2, StatusMessage: "disallowed fqdn: \"\""},
				Hostname: "host1",
			},
			lastHostname: "host1",
			lastFqdn:     "",
		},
		{
			name: "mds name as hostname",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    true,
					SetFqdn:        true,
				},
			},
			setFqdn:     func(string, string) error { return nil },
			setHostname: func(string) error { return nil },
			req:         ReconfigureHostnameRequest{},
			resp: ReconfigureHostnameResponse{
				Response: command.Response{Status: 3, StatusMessage: "disallowed hostname: \"metadata.google.internal\"disallowed fqdn: \"metadata.google.internal\""},
			},
			lastHostname: "metadata.google.internal",
			lastFqdn:     "metadata.google.internal",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			cfg.Get().Unstable = tc.cfg.Unstable
			setFqdn = tc.setFqdn
			setHostname = tc.setHostname
			lastHostname = tc.lastHostname
			lastFqdn = tc.lastFqdn
			b, err := json.Marshal(tc.req)
			if err != nil {
				t.Fatal(err)
			}
			b, err = ReconfigureHostname(b)
			if err != nil {
				t.Fatal(err)
			}
			var resp ReconfigureHostnameResponse
			err = json.Unmarshal(b, &resp)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Status != tc.resp.Status {
				t.Errorf("unexpected status code from reconfigurehostname, got %d want %d", resp.Status, tc.resp.Status)
			}
			if resp.StatusMessage != tc.resp.StatusMessage {
				t.Errorf("unexpected status message from reconfigurehostname, got %s want %s", resp.StatusMessage, tc.resp.StatusMessage)
			}
			if resp.Hostname != tc.resp.Hostname {
				t.Errorf("unexpected hostname from reconfigurehostname, got %s want %s", resp.Hostname, tc.resp.Hostname)
			}
			if resp.Fqdn != tc.resp.Fqdn {
				t.Errorf("unexpected fqdn from reconfigurehostname, got %s want %s", resp.Fqdn, tc.resp.Fqdn)
			}
		})
	}
}

type fakeMdsClient struct{}

func (f *fakeMdsClient) Get(context.Context) (*mds.Descriptor, error) { return nil, nil }
func (f *fakeMdsClient) GetKey(context.Context, string, map[string]string) (string, error) {
	return "host1.example.com", nil
}
func (f *fakeMdsClient) GetKeyRecursive(context.Context, string) (string, error)    { return "", nil }
func (f *fakeMdsClient) Watch(context.Context) (*mds.Descriptor, error)             { return nil, nil }
func (f *fakeMdsClient) WriteGuestAttributes(context.Context, string, string) error { return nil }

func TestCommandRoundTrip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg.Load(nil)
	setFqdnOrig := setFqdn
	setHostnameOrig := setHostname
	t.Cleanup(func() { setFqdn = setFqdnOrig; setHostname = setHostnameOrig })
	setFqdn = func(hostname, fqdn string) error {
		if fqdn != "host1.example.com" {
			return fmt.Errorf("bad fqdn")
		}
		return nil
	}
	setHostname = func(hostname string) error {
		if hostname != "host1" {
			return fmt.Errorf("bad hostname")
		}
		return nil
	}
	testpipe := path.Join(t.TempDir(), "commands.sock")
	if runtime.GOOS == "windows" {
		testpipe = `\\.\pipe\google-guest-agent-hostname-test-round-trip`
	}
	cfg.Get().Unstable = &cfg.Unstable{
		CommandMonitorEnabled: true,
		CommandPipePath:       testpipe,
		FqdnAsHostname:        false,
		SetHostname:           true,
		SetFqdn:               true,
	}
	req := []byte(fmt.Sprintf(`{"Command":"%s"}`, ReconfigureHostnameCommand))
	client := &fakeMdsClient{}
	command.Init(ctx)
	t.Cleanup(func() { command.Close() })
	Init(ctx, client)
	t.Cleanup(Close)
	var resp ReconfigureHostnameResponse
	b := command.SendCommand(ctx, req)
	err := json.Unmarshal(b, &resp)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 0 {
		t.Errorf("unexpected status code from reconfigurehostname, got %d want %d", resp.Status, 0)
	}
	if resp.StatusMessage != "" {
		t.Errorf("unexpected status message from reconfigurehostname, got %s want %s", resp.StatusMessage, "")
	}
	if resp.Hostname != "host1" {
		t.Errorf("unexpected hostname from reconfigurehostname, got %s want %s", resp.Hostname, "host1")
	}
	if resp.Fqdn != "host1.example.com" {
		t.Errorf("unexpected fqdn from reconfigurehostname, got %s want %s", resp.Fqdn, "host1.example.com")
	}
}

func TestShouldReconfigure(t *testing.T) {
	cfg.Load(nil)
	testcases := []struct {
		name               string
		cfg                *cfg.Sections
		lastHostname       string
		lastFqdn           string
		descriptor         mds.Descriptor
		eventShouldTrigger bool
	}{
		{
			name: "hostname changed",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    true,
					SetFqdn:        false,
				},
			},
			lastHostname:       "host1",
			lastFqdn:           "host1.example.com",
			descriptor:         mds.Descriptor{Instance: mds.Instance{Hostname: "host2.example.com"}},
			eventShouldTrigger: true,
		},
		{
			name: "fqdn changed",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    false,
					SetFqdn:        true,
				},
			},
			lastHostname:       "host1",
			lastFqdn:           "host1.example.net",
			descriptor:         mds.Descriptor{Instance: mds.Instance{Hostname: "host1.example.com"}},
			eventShouldTrigger: true,
		},
		{
			name: "no change",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    true,
					SetFqdn:        true,
				},
			},
			lastHostname:       "host1",
			lastFqdn:           "host1.example.com",
			descriptor:         mds.Descriptor{Instance: mds.Instance{Hostname: "host1.example.com"}},
			eventShouldTrigger: false,
		},
		{
			name: "ignore changes",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: false,
					SetHostname:    false,
					SetFqdn:        false,
				},
			},
			lastHostname:       "host1",
			lastFqdn:           "host1.example.net",
			descriptor:         mds.Descriptor{Instance: mds.Instance{Hostname: "host2.example.com"}},
			eventShouldTrigger: false,
		},
		{
			name: "fqnashostname changed",
			cfg: &cfg.Sections{
				Unstable: &cfg.Unstable{
					FqdnAsHostname: true,
					SetHostname:    true,
					SetFqdn:        false,
				},
			},
			lastHostname:       "host1",
			lastFqdn:           "host1.example.net",
			descriptor:         mds.Descriptor{Instance: mds.Instance{Hostname: "host1.example.net"}},
			eventShouldTrigger: true,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			cfg.Get().Unstable = tc.cfg.Unstable
			lastHostname = tc.lastHostname
			lastFqdn = tc.lastFqdn
			o := shouldReconfigure(tc.descriptor)
			if o != tc.eventShouldTrigger {
				t.Errorf("shouldReconfigure reported unexpected value, got %v want %v", o, tc.eventShouldTrigger)
			}
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
		cfg           *cfg.Sections
		inputhosts    string
		inputhostname string
		inputfqdn     string
		inputaddrs    []net.Addr
		output        string
	}{
		{
			name:          "empty hosts",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{}},
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1   # Added by Google" + newline,
		},
		{
			name:          "loopback addresses",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{}},
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}, testAddr{"127.0.0.1/8"}, testAddr{"::1/128"}},
			output:        "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1   # Added by Google" + newline,
		},
		{
			name:          "two addresses",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{}},
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}, testAddr{"10.0.0.20/16"}},
			output:        "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1   # Added by Google" + newline + "10.0.0.20 tc1.example.com tc1   # Added by Google" + newline,
		},
		{
			name:          "two aliases",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{AdditionalAliases: "tc2,tc3"}},
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1 tc2 tc3  # Added by Google" + newline,
		},
		{
			name:          "existing hosts at beginning",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{}},
			inputhosts:    "127.0.0.1 pre-existing.host.com" + newline + "12.12.12.12 tc1.example.com # Added by Google" + newline,
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "127.0.0.1 pre-existing.host.com" + newline + "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1   # Added by Google" + newline,
		},
		{
			name:          "existing hosts at end",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{}},
			inputhosts:    "12.12.12.12 tc1.example.com # Added by Google" + newline + "127.0.0.1 pre-existing.host.com" + newline + "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "127.0.0.1 pre-existing.host.com" + newline + "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1   # Added by Google" + newline,
		},
		{
			name:          "two gce hosts blocks",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{}},
			inputhosts:    "12.12.12.12 tc1.example.com # Added by Google" + newline + "127.0.0.1 pre-existing.host.com" + newline + "13.13.13.13 tc2.example.com # Added by Google" + newline,
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "127.0.0.1 pre-existing.host.com" + newline + "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1   # Added by Google" + newline,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			cfg.Get().Unstable = tc.cfg.Unstable
			testfile, err := os.CreateTemp(t.TempDir(), "test-writehosts-"+strings.ReplaceAll(tc.name, " ", "-"))
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
