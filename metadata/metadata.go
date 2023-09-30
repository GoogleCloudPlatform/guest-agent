//  Copyright 2017 Google Inc. All Rights Reserved.
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

package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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
	ID                json.Number
	MachineType       string
	Attributes        Attributes
	NetworkInterfaces []NetworkInterfaces
	VirtualClock      virtualClock
}

// NetworkInterfaces describes the instances network interfaces configurations.
type NetworkInterfaces struct {
	ForwardedIps      []string
	ForwardedIpv6s    []string
	TargetInstanceIps []string
	IPAliases         []string
	Mac               string
	DHCPv6Refresh     string
}

// Project describes the projects instance's attributes.
type Project struct {
	Attributes       Attributes
	ProjectID        string
	NumericProjectID json.Number
}

// Attributes describes the project's attributes keys.
type Attributes struct {
	BlockProjectKeys      bool
	EnableOSLogin         *bool
	EnableWindowsSSH      *bool
	TwoFactor             *bool
	SecurityKey           *bool
	SSHKeys               []string
	WindowsKeys           WindowsKeys
	Diagnostics           string
	DisableAddressManager *bool
	DisableAccountManager *bool
	EnableDiagnostics     *bool
	EnableWSFC            *bool
	WSFCAddresses         string
	WSFCAgentPort         string
	DisableTelemetry      bool
	DisableIOScheduler    bool
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
		BlockProjectKeys      string      `json:"block-project-ssh-keys"`
		Diagnostics           string      `json:"diagnostics"`
		DisableAccountManager string      `json:"disable-account-manager"`
		DisableAddressManager string      `json:"disable-address-manager"`
		EnableDiagnostics     string      `json:"enable-diagnostics"`
		EnableOSLogin         string      `json:"enable-oslogin"`
		EnableWindowsSSH      string      `json:"enable-windows-ssh"`
		EnableWSFC            string      `json:"enable-wsfc"`
		OldSSHKeys            string      `json:"sshKeys"`
		SSHKeys               string      `json:"ssh-keys"`
		TwoFactor             string      `json:"enable-oslogin-2fa"`
		SecurityKey           string      `json:"enable-oslogin-sk"`
		WindowsKeys           WindowsKeys `json:"windows-keys"`
		WSFCAddresses         string      `json:"wsfc-addrs"`
		WSFCAgentPort         string      `json:"wsfc-agent-port"`
		DisableTelemetry      string      `json:"disable-guest-telemetry"`
		DisableIOScheduler    string      `json:"disable-io-scheduler"`
	}
	var temp inner
	if err := json.Unmarshal(b, &temp); err != nil {
		return err
	}
	a.Diagnostics = temp.Diagnostics
	a.WSFCAddresses = temp.WSFCAddresses
	a.WSFCAgentPort = temp.WSFCAgentPort
	a.WindowsKeys = temp.WindowsKeys

	value, err := strconv.ParseBool(temp.BlockProjectKeys)
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
	value, err = strconv.ParseBool(temp.DisableTelemetry)
	if err == nil {
		a.DisableTelemetry = value
	}
	value, err = strconv.ParseBool(temp.DisableIOScheduler)
	if err == nil {
		a.DisableIOScheduler = value
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

func shouldRetry(resp *http.Response, err error) bool {
	// If the context was canceled just return the error and don't retry.
	if err != nil && errors.Is(err, context.Canceled) {
		return false
	}

	// Known non-retriable status codes.
	if resp != nil && resp.StatusCode == 404 {
		return false
	}

	return true
}

func (c *Client) retry(ctx context.Context, cfg requestConfig) (string, error) {
	var ferr error
	for i := 1; i <= backoffAttempts; i++ {
		resp, err := c.do(ctx, cfg)
		ferr = err
		// Check if error is retriable, if not just return the error and don't retry.
		if err != nil && !shouldRetry(resp, err) {
			return "", err
		}

		// Apply the backoff strategy.
		if err != nil {
			logger.Debugf("Attempt %d: failed to connect to metadata server: %+v", i, err)
			time.Sleep(time.Duration(i) * backoffDuration)
			continue
		}

		defer resp.Body.Close()
		md, err := io.ReadAll(resp.Body)
		if err != nil {
			ferr = err
			logger.Debugf("Attempt %d: failed to read metadata server response bytes: %+v", i, err)
			time.Sleep(time.Duration(i) * backoffDuration)
			continue
		}

		return string(md), nil
	}
	logger.Errorf("Exhausted %d retry attempts to connect to MDS, failed with an error: %+v", backoffAttempts, ferr)
	return "", fmt.Errorf("reached max attempts to connect to metadata")
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

	req, err := http.NewRequest("PUT", finalURL, strings.NewReader(value))
	if err != nil {
		return err
	}

	req.Header.Add("Metadata-Flavor", "Google")
	req = req.WithContext(ctx)

	_, err = c.httpClient.Do(req)
	return err
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

	statusCodeMsg := "error connecting to metadata server, status code: %d"
	switch resp.StatusCode {
	case 404, 412:
		return resp, fmt.Errorf(statusCodeMsg, resp.StatusCode)
	}

	if cfg.hang {
		c.updateEtag(resp)
	}

	return resp, nil
}
