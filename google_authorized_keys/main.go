//  Copyright 2022 Google Inc. All Rights Reserved.
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

// GoogleAuthorizedKeys obtains SSH keys from metadata.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	programName       = "GoogleAuthorizedKeysCommand"
	version           string
	metadataURL       = "http://169.254.169.254/computeMetadata/v1/"
	metadataRecursive = "/?recursive=true&alt=json"
	defaultTimeout    = 2 * time.Second
)

func logFormat(e logger.LogEntry) string {
	now := time.Now().Format("2006/01/02 15:04:05")
	return fmt.Sprintf("%s %s: %s", now, programName, e.Message)
}

func logFormatWindows(e logger.LogEntry) string {
	now := time.Now().Format("2006/01/02 15:04:05")
	// 2006/01/02 15:04:05 GCEMetadataScripts This is a log message.
	return fmt.Sprintf("%s %s: %s", now, programName, e.Message)
}

func getMetadata(key string, recurse bool) ([]byte, error) {
	client := &http.Client{
		Timeout: defaultTimeout,
	}

	url := metadataURL + key
	if recurse {
		url += metadataRecursive
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Metadata-Flavor", "Google")

	var res *http.Response
	// Retry forever, increase sleep between retries (up to 5 times) in order
	// to wait for slow network initialization.
	var rt time.Duration
	for i := 1; ; i++ {
		res, err = client.Do(req)
		if err == nil {
			break
		}
		if i < 6 {
			rt = time.Duration(3*i) * time.Second
		}
		logger.Errorf("error connecting to metadata server, retrying in %s, error: %v", rt, err)
		time.Sleep(rt)
	}
	defer res.Body.Close()

	md, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return md, nil
}

func parseSSHKeys(username string, keys []string) []string {
	var keyList []string
	for _, key := range keys {
		keySplit := strings.SplitN(key, ":", 2)
		if len(keySplit) != 2 {
			continue
		}

		user, keyVal, err := utils.GetUserKey(key)
		if err == nil {
			err = utils.ValidateUserKey(user, keyVal)
		}

		if err != nil {
			continue
		}

		if user == username {
			keyList = append(keyList, keyVal)
		}
	}
	return keyList
}

func getUserKeys(username string, instanceAttributes *attributes, projectAttributes *attributes) []string {
	var userKeyList []string

	instanceKeyList := parseSSHKeys(username, instanceAttributes.SSHKeys)
	userKeyList = append(userKeyList, instanceKeyList...)

	if !instanceAttributes.BlockProjectSSHKeys {

		projectKeyList := parseSSHKeys(username, projectAttributes.SSHKeys)
		userKeyList = append(userKeyList, projectKeyList...)

	}

	return userKeyList
}

func checkWinSSHEnabled(instanceAttributes *attributes, projectAttributes *attributes) bool {
	if instanceAttributes.EnableWindowsSSH != nil {
		return bool(*instanceAttributes.EnableWindowsSSH)
	} else if projectAttributes.EnableWindowsSSH != nil {
		return bool(*projectAttributes.EnableWindowsSSH)
	}
	return false
}

type attributes struct {
	EnableWindowsSSH    *bool
	BlockProjectSSHKeys bool
	SSHKeys             []string
}

func getMetadataAttributes(metadataKey string) (*attributes, error) {
	var a attributes
	type jsonAttributes struct {
		EnableWindowsSSH    string `json:"enable-windows-ssh"`
		BlockProjectSSHKeys string `json:"block-project-ssh-keys"`
		SSHKeys             string `json:"ssh-keys"`
	}
	var ja jsonAttributes
	metadata, err := getMetadata(metadataKey, true)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(metadata, &ja); err != nil {
		return nil, err
	}

	value, err := strconv.ParseBool(ja.BlockProjectSSHKeys)
	if err == nil {
		a.BlockProjectSSHKeys = value
	}

	value, err = strconv.ParseBool(ja.EnableWindowsSSH)
	if err == nil {
		a.EnableWindowsSSH = &value
	}
	if ja.SSHKeys != "" {
		a.SSHKeys = strings.Split(ja.SSHKeys, "\n")
	}
	return &a, nil
}

func main() {
	ctx := context.Background()
	username := os.Args[1]

	opts := logger.LogOpts{
		LoggerName:     programName,
		FormatFunction: logFormat,
	}

	if runtime.GOOS == "windows" {
		opts.Writers = []io.Writer{&utils.SerialPort{Port: "COM1"}, os.Stderr}
		opts.FormatFunction = logFormatWindows
	} else {
		opts.Writers = []io.Writer{os.Stderr}
	}
	logger.Init(ctx, opts)

	instanceAttributes, err := getMetadataAttributes("instance/attributes/")
	if err != nil {
		logger.Errorf("Cannot read instance metadata attributes: %v", err)
		os.Exit(1)
	}
	projectAttributes, err := getMetadataAttributes("project/attributes/")
	if err != nil {
		logger.Errorf("Cannot read project metadata attributes: %v", err)
		os.Exit(1)
	}

	if runtime.GOOS == "windows" && !checkWinSSHEnabled(instanceAttributes, projectAttributes) {
		logger.Errorf("Windows SSH not enabled with 'enable-windows-ssh' metadata key.")
		os.Exit(1)
	}

	userKeyList := getUserKeys(username, instanceAttributes, projectAttributes)
	fmt.Printf(strings.Join(userKeyList, "\n"))
}
