// Copyright 2022 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// GoogleAuthorizedKeys obtains SSH keys from metadata.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	client      metadata.MDSClientInterface
	programName = path.Base(os.Args[0])
)

func init() {
	client = metadata.New()
}

func logFormat(e logger.LogEntry) string {
	now := time.Now().Format("2006/01/02 15:04:05")
	return fmt.Sprintf("%s %s: %s", now, programName, e.Message)
}

func logFormatWindows(e logger.LogEntry) string {
	now := time.Now().Format("2006/01/02 15:04:05")
	// 2006/01/02 15:04:05 GCEMetadataScripts This is a log message.
	return fmt.Sprintf("%s %s: %s", now, programName, e.Message)
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

func getMetadataAttributes(ctx context.Context, metadataKey string) (*attributes, error) {
	var a attributes
	type jsonAttributes struct {
		EnableWindowsSSH    string `json:"enable-windows-ssh"`
		BlockProjectSSHKeys string `json:"block-project-ssh-keys"`
		SSHKeys             string `json:"ssh-keys"`
	}
	var ja jsonAttributes
	metadata, err := client.GetKeyRecursive(ctx, metadataKey)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(metadata), &ja); err != nil {
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

	instanceAttributes, err := getMetadataAttributes(ctx, "instance/attributes/")
	if err != nil {
		logger.Errorf("Cannot read instance metadata attributes: %v", err)
		os.Exit(1)
	}
	projectAttributes, err := getMetadataAttributes(ctx, "project/attributes/")
	if err != nil {
		logger.Errorf("Cannot read project metadata attributes: %v", err)
		os.Exit(1)
	}

	if runtime.GOOS == "windows" && !checkWinSSHEnabled(instanceAttributes, projectAttributes) {
		logger.Errorf("Windows SSH not enabled with 'enable-windows-ssh' metadata key.")
		os.Exit(1)
	}

	userKeyList := getUserKeys(username, instanceAttributes, projectAttributes)
	fmt.Print(strings.Join(userKeyList, "\n"))
}
