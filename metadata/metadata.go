// Copyright 2017 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/retry"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	defaultMetadataURL = "http://169.254.169.254/computeMetadata/v1/"
	defaultEtag        = "NONE"

	// defaultHangtimeout is the timeout parameter passed to metadata as the hang timeout.
	defaultHangTimeout = 60

	// defaultClientTimeout sets the http.Client time out, the delta of 10s between the
	// defaultHangTimeout and client timeout should be enough to avoid canceling the context
	// before headers and body are read.
	defaultClientTimeout = 70
)

var (
	// we backoff until 10s
	backoffDuration = 100 * time.Millisecond
	backoffAttempts = 100
)

// MDSClientInterface is the minimum required Metadata Server interface for Guest Agent.
type MDSClientInterface interface {
	Get(context.Context) (*Descriptor, error)
	GetKey(context.Context, string, map[string]string) (string, error)
	GetKeyRecursive(context.Context, string) (string, error)
	Watch(context.Context) (*Descriptor, error)
	WriteGuestAttributes(context.Context, string, string) error
}

// requestConfig is used internally to configure an http request given its context.
type requestConfig struct {
	baseURL    string
	hang       bool
	recursive  bool
	jsonOutput bool
	timeout    int
	headers    map[string]string
}

// Client defines the public interface between the core guest agent and
// the metadata layer.
type Client struct {
	metadataURL string
	etag        string
	httpClient  *http.Client
}

// New allocates and configures a new Client instance.
func New() *Client {
	return &Client{
		metadataURL: defaultMetadataURL,
		etag:        defaultEtag,
		httpClient: &http.Client{
			Timeout: defaultClientTimeout * time.Second,
		},
	}
}

// Descriptor wraps/holds all the metadata keys, the structure reflects the json
// descriptor returned with metadata call with alt=jason.
type Descriptor struct {
	Instance Instance
	Project  Project
}

// UnmarshalJSON unmarshals b into Descritor.
func (m *Descriptor) UnmarshalJSON(b []byte) error {
	// We can't unmarshal into metadata directly as it would create an infinite loop.
	type temp Descriptor
	var t temp
	err := json.Unmarshal(b, &t)
	if err == nil {
		*m = Descriptor(t)
		return nil
	}

	// If this is a syntax error return a useful error.
	sErr, ok := err.(*json.SyntaxError)
	if !ok {
		return err
	}

	// Byte number where the error line starts.
	start := bytes.LastIndex(b[:sErr.Offset], []byte("\n")) + 1
	// Assume end byte of error line is EOF unless this isn't the last line.
	end := len(b)
	if i := bytes.Index(b[start:], []byte("\n")); i >= 0 {
		end = start + i
	}

	// Position of error in line (where to place the '^').
	pos := int(sErr.Offset) - start
	if pos != 0 {
		pos = pos - 1
	}

	return fmt.Errorf("JSON syntax error: %s \n%s\n%s^", err, b[start:end], strings.Repeat(" ", pos))
}

type virtualClock struct {
	DriftToken int `json:"drift-token"`
}

// Instance describes the metadata's instance attributes/keys.
type Instance struct {
	// ID is the instance ID.
	ID json.Number

	// MachineType represents the instance's machine type.
	MachineType string

	// Attributes are the instance's attributes.
	Attributes Attributes

	// NetworkInterfaces contains all configured regular network interfaces (primary and secondary).
	NetworkInterfaces []NetworkInterfaces

	// VlanNetworkInterfaces contains all the vLAN network interfaces.
	VlanNetworkInterfaces map[int]map[int]VlanInterface

	// VirtualClock contains the drift-token attribute.
	VirtualClock virtualClock
}

// NetworkInterfaces describes the instances network interfaces configurations.
type NetworkInterfaces struct {
	ForwardedIps      []string
	ForwardedIpv6s    []string
	TargetInstanceIps []string
	IPAliases         []string
	Mac               string
	DHCPv6Refresh     string
	MTU               int
}

// VlanInterface describes the instances vlan network interfaces configurations.
type VlanInterface struct {
	// Mac is the vLAN interface's mac address.
	Mac string

	// ParentInterface is the mds reference of the parent/physical interface i.e.:
	// /computeMetadata/v1/instance/network-interfaces/0/
	ParentInterface string

	// Vlan is the vlan id.
	Vlan int

	// MTU is the vlan's MTU value.
	MTU int

	// IP is the vlan's ip address.
	IP string

	// IPv6 is the vlan's ipv6 address.
	IPv6 []string

	// Gateway is the vlan's gateway address.
	Gateway string

	// GatewayIPv6 is the vlan's IPv6 gateway address.
	GatewayIPv6 string

	// DHCPv6Refresh determine if VLAN NIC supports IPV6.
	DHCPv6Refresh string
}

// Project describes the projects instance's attributes.
type Project struct {
	Attributes       Attributes
	ProjectID        string
	NumericProjectID json.Number
}

// Attributes describes the project's attributes keys.
type Attributes struct {
	CreatedBy                 string
	BlockProjectKeys          bool
	HTTPSMDSEnableNativeStore *bool
	DisableHTTPSMdsSetup      *bool
	EnableOSLogin             *bool
	EnableWindowsSSH          *bool
	TwoFactor                 *bool
	SecurityKey               *bool
	RequireCerts              *bool
	SSHKeys                   []string
	WindowsKeys               WindowsKeys
	Diagnostics               string
	DisableAddressManager     *bool
	DisableAccountManager     *bool
	EnableDiagnostics         *bool
	EnableWSFC                *bool
	WSFCAddresses             string
	WSFCAgentPort             string
	DisableTelemetry          bool
}

// UnmarshalJSON unmarshals b into Attribute.
func (a *Attributes) UnmarshalJSON(b []byte) error {
	var mkbool = func(value bool) *bool {
		res := new(bool)
		*res = value
		return res
	}
	// Unmarshal to literal JSON types before doing anything else.
	type inner struct {
		CreatedBy                 string      `json:"created-by"`
		BlockProjectKeys          string      `json:"block-project-ssh-keys"`
		Diagnostics               string      `json:"diagnostics"`
		DisableAccountManager     string      `json:"disable-account-manager"`
		DisableAddressManager     string      `json:"disable-address-manager"`
		EnableDiagnostics         string      `json:"enable-diagnostics"`
		EnableOSLogin             string      `json:"enable-oslogin"`
		EnableWindowsSSH          string      `json:"enable-windows-ssh"`
		EnableWSFC                string      `json:"enable-wsfc"`
		OldSSHKeys                string      `json:"sshKeys"`
		SSHKeys                   string      `json:"ssh-keys"`
		TwoFactor                 string      `json:"enable-oslogin-2fa"`
		SecurityKey               string      `json:"enable-oslogin-sk"`
		RequireCerts              string      `json:"enable-oslogin-certificates"`
		WindowsKeys               WindowsKeys `json:"windows-keys"`
		WSFCAddresses             string      `json:"wsfc-addrs"`
		WSFCAgentPort             string      `json:"wsfc-agent-port"`
		DisableTelemetry          string      `json:"disable-guest-telemetry"`
		DisableHTTPSMdsSetup      string      `json:"disable-https-mds-setup"`
		HTTPSMDSEnableNativeStore string      `json:"enable-https-mds-native-cert-store"`
	}
	var temp inner
	if err := json.Unmarshal(b, &temp); err != nil {
		return err
	}
	a.Diagnostics = temp.Diagnostics
	a.WSFCAddresses = temp.WSFCAddresses
	a.WSFCAgentPort = temp.WSFCAgentPort
	a.WindowsKeys = temp.WindowsKeys
	a.CreatedBy = temp.CreatedBy

	value, err := strconv.ParseBool(temp.DisableHTTPSMdsSetup)
	if err == nil {
		a.DisableHTTPSMdsSetup = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.HTTPSMDSEnableNativeStore)
	if err == nil {
		a.HTTPSMDSEnableNativeStore = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.BlockProjectKeys)
	if err == nil {
		a.BlockProjectKeys = value
	}
	value, err = strconv.ParseBool(temp.EnableDiagnostics)
	if err == nil {
		a.EnableDiagnostics = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.DisableAccountManager)
	if err == nil {
		a.DisableAccountManager = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.DisableAddressManager)
	if err == nil {
		a.DisableAddressManager = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.EnableOSLogin)
	if err == nil {
		a.EnableOSLogin = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.EnableWindowsSSH)
	if err == nil {
		a.EnableWindowsSSH = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.EnableWSFC)
	if err == nil {
		a.EnableWSFC = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.TwoFactor)
	if err == nil {
		a.TwoFactor = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.SecurityKey)
	if err == nil {
		a.SecurityKey = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.RequireCerts)
	if err == nil {
		a.RequireCerts = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.DisableTelemetry)
	if err == nil {
		a.DisableTelemetry = value
	}
	// So SSHKeys will be nil instead of []string{}
	if temp.SSHKeys != "" {
		a.SSHKeys = strings.Split(temp.SSHKeys, "\n")
	}
	if temp.OldSSHKeys != "" {
		a.BlockProjectKeys = true
		a.SSHKeys = append(a.SSHKeys, strings.Split(temp.OldSSHKeys, "\n")...)
	}
	return nil
}

func (c *Client) updateEtag(resp *http.Response) bool {
	oldEtag := c.etag
	c.etag = resp.Header.Get("etag")
	if c.etag == "" {
		c.etag = defaultEtag
	}
	return c.etag != oldEtag
}

// MDSReqError represents custom error produced by HTTP requests made on MDS. It captures
// error and HTTP response for inspecting status code.
type MDSReqError struct {
	status int
	err    error
}

// Error implements method defined on error interface to transform custom type into error.
func (m *MDSReqError) Error() string {
	return fmt.Sprintf("request failed with status code: [%d], error: [%v]", m.status, m.err)
}

// shouldRetry method checks if MDSReqError is temporary and retriable or not.
func shouldRetry(err error) bool {
	e, ok := err.(*MDSReqError)
	if !ok {
		// Unknown error retry.
		return true
	}

	// Known non-retriable status codes.
	codes := []int{404}

	return !slices.Contains(codes, e.status)
}

func (c *Client) retry(ctx context.Context, cfg requestConfig) (string, error) {
	policy := retry.Policy{MaxAttempts: backoffAttempts, Jitter: backoffDuration, BackoffFactor: 1, ShouldRetry: shouldRetry}

	fn := func() (string, error) {
		resp, err := c.do(ctx, cfg)
		if err != nil {
			statusCode := -1
			if resp != nil {
				statusCode = resp.StatusCode
			}
			return "", &MDSReqError{statusCode, err}
		}
		defer resp.Body.Close()

		md, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to read metadata server response bytes: %+v", err)
		}

		return string(md), nil
	}

	return retry.RunWithResponse(ctx, policy, fn)
}

// GetKey gets a specific metadata key.
func (c *Client) GetKey(ctx context.Context, key string, headers map[string]string) (string, error) {
	reqURL, err := url.JoinPath(c.metadataURL, key)
	if err != nil {
		return "", fmt.Errorf("failed to form metadata url: %+v", err)
	}

	cfg := requestConfig{
		baseURL: reqURL,
		headers: headers,
	}
	return c.retry(ctx, cfg)
}

// GetKeyRecursive gets a specific metadata key recursively and returns JSON output.
func (c *Client) GetKeyRecursive(ctx context.Context, key string) (string, error) {
	reqURL, err := url.JoinPath(c.metadataURL, key)
	if err != nil {
		return "", fmt.Errorf("failed to form metadata url: %+v", err)
	}

	cfg := requestConfig{
		baseURL:    reqURL,
		jsonOutput: true,
		recursive:  true,
	}
	return c.retry(ctx, cfg)
}

// Watch runs a longpoll on metadata server.
func (c *Client) Watch(ctx context.Context) (*Descriptor, error) {
	return c.get(ctx, true)
}

// Get does a metadata call, if hang is set to true then it will do a longpoll.
func (c *Client) Get(ctx context.Context) (*Descriptor, error) {
	return c.get(ctx, false)
}

func (c *Client) get(ctx context.Context, hang bool) (*Descriptor, error) {
	cfg := requestConfig{
		baseURL:    c.metadataURL,
		timeout:    defaultHangTimeout,
		recursive:  true,
		jsonOutput: true,
	}

	if hang {
		cfg.hang = true
	}

	resp, err := c.retry(ctx, cfg)
	if err != nil {
		return nil, err
	}

	var ret Descriptor
	if err = json.Unmarshal([]byte(resp), &ret); err != nil {
		return nil, err
	}

	return &ret, nil
}

// WriteGuestAttributes does a put call to mds changing a guest attribute value.
func (c *Client) WriteGuestAttributes(ctx context.Context, key, value string) error {
	logger.Debugf("write guest attribute %q", key)

	finalURL, err := url.JoinPath(c.metadataURL, "instance/guest-attributes/", key)
	if err != nil {
		return fmt.Errorf("failed to form metadata url: %+v", err)
	}

	logger.Debugf("Requesting(PUT) MDS URL: %s", finalURL)

	// This is a arbitrary retry number.
	policy := retry.Policy{MaxAttempts: 10, Jitter: backoffDuration, BackoffFactor: 1}

	putCall := func() error {
		req, err := http.NewRequest("PUT", finalURL, strings.NewReader(value))
		if err != nil {
			return err
		}
		req.Header.Add("Metadata-Flavor", "Google")
		req = req.WithContext(ctx)
		_, err = c.httpClient.Do(req)

		return err
	}

	return retry.Run(ctx, policy, putCall)
}

func (c *Client) do(ctx context.Context, cfg requestConfig) (*http.Response, error) {
	finalURL, err := url.Parse(cfg.baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse url: %+v", err)
	}

	values := finalURL.Query()

	if cfg.hang {
		values.Add("wait_for_change", "true")
		values.Add("last_etag", c.etag)
	}

	if cfg.timeout > 0 {
		values.Add("timeout_sec", fmt.Sprintf("%d", cfg.timeout))
	}

	if cfg.recursive {
		values.Add("recursive", "true")
	}

	if cfg.jsonOutput {
		values.Add("alt", "json")
	}

	finalURL.RawQuery = values.Encode()
	logger.Debugf("Requesting(GET) MDS URL: %s", finalURL.String())

	req, err := http.NewRequestWithContext(ctx, "GET", finalURL.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Metadata-Flavor", "Google")
	for k, v := range cfg.headers {
		req.Header.Add(k, v)
	}
	resp, err := c.httpClient.Do(req)

	// If we are canceling httpClient will also wrap the context's error so
	// check first the context.
	if ctx.Err() != nil {
		return resp, ctx.Err()
	}

	if err != nil {
		return resp, fmt.Errorf("error connecting to metadata server: %+v", err)
	}

	if resp == nil {
		return nil, fmt.Errorf("got nil response from metadata server")
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		// Ignore read error as we are returning original error and wrapping MDS error code.
		r, _ := io.ReadAll(resp.Body)
		return resp, fmt.Errorf("invalid response from metadata server, status code: %d, reason: %s", resp.StatusCode, string(r))
	}

	if cfg.hang {
		c.updateEtag(resp)
	}

	return resp, nil
}
