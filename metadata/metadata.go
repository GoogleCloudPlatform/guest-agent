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
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const defaultEtag = "NONE"

var (
	metadataURL       = "http://169.254.169.254/computeMetadata/v1/"
	metadataRecursive = "/?recursive=true&alt=json"
	metadataHang      = "&wait_for_change=true&timeout_sec=60"
	defaultTimeout    = 70 * time.Second
	etag              = defaultEtag
)

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

func updateEtag(resp *http.Response) bool {
	oldEtag := etag
	etag = resp.Header.Get("etag")
	if etag == "" {
		etag = defaultEtag
	}
	return etag != oldEtag
}

// Watch runs a longpoll on metadata server.
func Watch(ctx context.Context) (*Descriptor, error) {
	return get(ctx, true)
}

// Get does a metadata call, if hang is set to true then it will do a longpoll.
func Get(ctx context.Context) (*Descriptor, error) {
	return get(ctx, false)
}

func get(ctx context.Context, hang bool) (*Descriptor, error) {
	logger.Debugf("Invoking Get metadata, wait for change: %t", hang)
	client := &http.Client{
		Timeout: defaultTimeout,
	}

	finalURL := metadataURL + metadataRecursive
	if hang {
		finalURL += metadataHang
	}
	finalURL += ("&last_etag=" + etag)

	req, err := http.NewRequest("GET", finalURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Metadata-Flavor", "Google")
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	// Don't return error on a canceled context.
	if err != nil && ctx.Err() != nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// We return the response even if the etag has not been updated.
	if hang {
		updateEtag(resp)
	}

	md, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var ret Descriptor
	return &ret, json.Unmarshal(md, &ret)
}

// WriteGuestAttributes does a put call to mds changing a guest attribute value.
func WriteGuestAttributes(key, value string) error {
	logger.Debugf("write guest attribute %q", key)
	client := &http.Client{Timeout: defaultTimeout}
	finalURL := metadataURL + "instance/guest-attributes/" + key
	req, err := http.NewRequest("PUT", finalURL, strings.NewReader(value))
	if err != nil {
		return err
	}
	req.Header.Add("Metadata-Flavor", "Google")

	_, err = client.Do(req)
	return err
}
