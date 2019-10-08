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

package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const defaultEtag = "NONE"

var (
	metadataURL       = "http://metadata.google.internal/computeMetadata/v1/"
	metadataRecursive = "/?recursive=true&alt=json"
	metadataHang      = "&wait_for_change=true&timeout_sec=60"
	defaultTimeout    = 70 * time.Second
	etag              = defaultEtag
)

type metadata struct {
	Instance instance
	Project  project
}

type virtualClock struct {
	DriftToken int
}

type instance struct {
	Attributes        attributes
	NetworkInterfaces []networkInterfaces
	VirtualClock      virtualClock
}

type networkInterfaces struct {
	ForwardedIps      []string
	TargetInstanceIps []string
	IPAliases         []string
	Mac               string
}

type project struct {
	Attributes attributes
	ProjectID  string
}

type attributes struct {
	BlockProjectKeys      bool
	EnableOSLogin         bool
	TwoFactor             bool
	SSHKeys               []string
	WindowsKeys           windowsKeys
	Diagnostics           string
	DisableAddressManager *bool
	DisableAccountManager *bool
	EnableDiagnostics     *bool
	EnableWSFC            *bool
	WSFCAddresses         string
	WSFCAgentPort         string
}

type windowsKey struct {
	Email        string
	ExpireOn     string
	Exponent     string
	Modulus      string
	UserName     string
	HashFunction string
}

type windowsKeys []windowsKey

func (a *attributes) UnmarshalJSON(b []byte) error {
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
		EnableWSFC            string      `json:"enable-wsfc"`
		OldSSHKeys            string      `json:"sshKeys"`
		SSHKeys               string      `json:"ssh-keys"`
		TwoFactor             string      `json:"enable-oslogin-2fa"`
		WindowsKeys           windowsKeys `json:"windows-keys"`
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
		a.EnableOSLogin = value
	}
	value, err = strconv.ParseBool(temp.EnableWSFC)
	if err == nil {
		a.EnableWSFC = mkbool(value)
	}
	value, err = strconv.ParseBool(temp.TwoFactor)
	if err == nil {
		a.TwoFactor = value
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

func (wks *windowsKeys) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	for _, jskey := range strings.Split(s, "\n") {
		var wk windowsKey
		if err := json.Unmarshal([]byte(jskey), &wk); err != nil {
			if !containsString(jskey, badKeys) {
				logger.Errorf("failed to unmarshal windows key from metadata: %s", err)
				badKeys = append(badKeys, jskey)
			}
			continue
		}
		if wk.Exponent != "" && wk.Modulus != "" && wk.UserName != "" && !wk.expired() {
			*wks = append(*wks, wk)
		}
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

func watchMetadata(ctx context.Context) (*metadata, error) {
	return getMetadata(ctx, true)
}

func getMetadata(ctx context.Context, hang bool) (*metadata, error) {
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
	var ret metadata
	return &ret, json.Unmarshal(md, &ret)
}
