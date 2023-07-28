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
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	defaultMetadataURL = "http://169.254.169.254/computeMetadata/v1/"
	defaultEtag        = "NONE"
	defaultTimeout     = 60
)

var (
	// we backoff until 10s
	backoffDuration = 100 * time.Millisecond
	backoffAttempts = 100
)

// MDSClientInterface is the minimum required Metadata Server interface for Guest Agent.
type MDSClientInterface interface {
	Get(context.Context) (*Descriptor, error)
	GetKey(context.Context, string) (string, error)
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
			Timeout: defaultTimeout * time.Second,
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

func (c *Client) retry(ctx context.Context, cfg requestConfig) (string, error) {
	for i := 1; i <= backoffAttempts; i++ {
		resp, err := c.do(ctx, cfg)

		// If the context was canceled just return the error and don't retry.
		if err != nil && errors.Is(err, context.Canceled) {
			return "", err
		}

		// Apply the backoff strategy.
		if err != nil {
			logger.Errorf("Failed to connect to metadata server: %+v", err)
			time.Sleep(time.Duration(i) * backoffDuration)
			continue
		}

		return resp, nil
	}
	return "", fmt.Errorf("reached max attempts to connect to metadata")
}

// GetKey gets a specific metadata key.
func (c *Client) GetKey(ctx context.Context, key string) (string, error) {
	cfg := requestConfig{
		baseURL: c.metadataURL + key,
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
		baseURL: c.metadataURL,
		timeout: defaultTimeout,
	}

	if hang {
		cfg.hang = true
		cfg.recursive = true
		cfg.jsonOutput = true
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
	finalURL := c.metadataURL + "instance/guest-attributes/" + key
	req, err := http.NewRequest("PUT", finalURL, strings.NewReader(value))
	if err != nil {
		return err
	}

	req.Header.Add("Metadata-Flavor", "Google")
	req = req.WithContext(ctx)

	_, err = c.httpClient.Do(req)
	return err
}

func (c *Client) do(ctx context.Context, cfg requestConfig) (string, error) {
	var (
		urlTokens []string
		finalURL  string
	)

	if cfg.hang {
		urlTokens = append(urlTokens, "wait_for_change=true")
		urlTokens = append(urlTokens, "last_etag="+c.etag)
	}

	if cfg.timeout > 0 {
		urlTokens = append(urlTokens, fmt.Sprintf("timeout_sec=%d", cfg.timeout))
	}

	if cfg.recursive {
		urlTokens = append(urlTokens, "recursive=true")
	}

	if cfg.jsonOutput {
		urlTokens = append(urlTokens, "alt=json")
	}

	finalURL = cfg.baseURL

	if len(urlTokens) > 0 {
		finalURL = fmt.Sprintf("%s/?%s", finalURL, strings.Join(urlTokens, "&"))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", finalURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Add("Metadata-Flavor", "Google")
	resp, err := c.httpClient.Do(req)

	// If we are canceling httpClient will also wrap the context's error so
	// check first the context.
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	if err != nil {
		return "", fmt.Errorf("error connecting to metadata server: %+v", err)
	}

	statusCodeMsg := "error connecting to metadata server, status code: %d"
	switch resp.StatusCode {
	case 404, 412:
		return "", fmt.Errorf(statusCodeMsg, resp.StatusCode)
	}

	if cfg.hang {
		c.updateEtag(resp)
	}

	defer resp.Body.Close()
	md, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading metadata response data: %+v", err)
	}

	return string(md), nil
}
