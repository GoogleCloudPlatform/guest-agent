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
		name        string
		cfg         *cfg.Sections
		hostname    string
		fqdn        string
		setFqdn     func(string, string) error
		setHostname func(string) error
		req         ReconfigureHostnameRequest
		resp        ReconfigureHostnameResponse
	}{
		{
			name: "successful_reconfigure_all",
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
			hostname: "host1",
			fqdn:     "host1.example.com",
		},
		{
			name: "reconfigure_hostname",
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
			hostname: "host1",
			fqdn:     "host1.example.com",
		},
		{
			name: "reconfigure_fqdn",
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
			hostname: "host1",
			fqdn:     "host1.example.com",
		},
		{
			name: "fail_to_reconfigure_hostname",
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
			hostname: "host1",
			fqdn:     "host1.example.com",
		},
		{
			name: "fail_to_reconfigure_fqdn",
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
			hostname: "host1",
			fqdn:     "host1.example.com",
		},
		{
			name: "fail_to_reconfigure_hostname_and_fqdn",
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
			hostname: "host1",
			fqdn:     "host1.example.com",
		},
		{
			name: "empty_hostname",
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
			hostname: "",
			fqdn:     "host1.example.com",
		},
		{
			name: "empty_fqdn",
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
			hostname: "host1",
			fqdn:     "",
		},
		{
			name: "mds_name_as_hostname",
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
			hostname: "metadata.google.internal",
			fqdn:     "metadata.google.internal",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			cfg.Get().Unstable = tc.cfg.Unstable
			setFqdn = tc.setFqdn
			setHostname = tc.setHostname
			hostname = tc.hostname
			fqdn = tc.fqdn
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
	hostname = "host1"
	fqdn = "host1.example.com"
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
			name:          "empty_hosts",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{}},
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1   # Added by Google" + newline,
		},
		{
			name:          "loopback_addresses",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{}},
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}, testAddr{"127.0.0.1/8"}, testAddr{"::1/128"}},
			output:        "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1   # Added by Google" + newline,
		},
		{
			name:          "two_addresses",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{}},
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}, testAddr{"10.0.0.20/16"}},
			output:        "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1   # Added by Google" + newline + "10.0.0.20 tc1.example.com tc1   # Added by Google" + newline,
		},
		{
			name:          "two_aliases",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{AdditionalAliases: "tc2,tc3"}},
			inputhosts:    "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1 tc2 tc3  # Added by Google" + newline,
		},
		{
			name:          "existing_hosts_at_beginning",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{}},
			inputhosts:    "127.0.0.1 pre-existing.host.com" + newline + "12.12.12.12 tc1.example.com # Added by Google" + newline,
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "127.0.0.1 pre-existing.host.com" + newline + "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1   # Added by Google" + newline,
		},
		{
			name:          "existing_hosts_at_end",
			cfg:           &cfg.Sections{Unstable: &cfg.Unstable{}},
			inputhosts:    "12.12.12.12 tc1.example.com # Added by Google" + newline + "127.0.0.1 pre-existing.host.com" + newline + "",
			inputhostname: "tc1",
			inputfqdn:     "tc1.example.com",
			inputaddrs:    []net.Addr{testAddr{"10.0.0.10/16"}},
			output:        "127.0.0.1 pre-existing.host.com" + newline + "169.254.169.254 metadata.google.internal # Added by Google" + newline + "10.0.0.10 tc1.example.com tc1   # Added by Google" + newline,
		},
		{
			name:          "two_gce_hosts_blocks",
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
