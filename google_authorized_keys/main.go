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
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/tarm/serial"
)

var (
	programName       = "GoogleAuthorizedKeysCommand"
	version           = "dev"
	metadataURL       = "http://169.254.169.254/computeMetadata/v1/"
	metadataRecursive = "/?recursive=true&alt=json"
	metadataHang      = "&wait_for_change=true&timeout_sec=2"
	defaultTimeout    = 2 * time.Second
)

func logFormat(e logger.LogEntry) string {
	now := time.Now().Format("2006/01/02 15:04:05")
	return fmt.Sprintf("%s %s: %s", now, programName, e.Message)
}

type serialPort struct {
	port string
}

func (s *serialPort) Write(b []byte) (int, error) {
	c := &serial.Config{Name: s.port, Baud: 115200}
	p, err := serial.OpenPort(c)
	if err != nil {
		return 0, err
	}
	defer p.Close()

	return p.Write(b)
}

func getMetadataKey(key string) (string, error) {
	md, err := getMetadata(key, false)
	if err != nil {
		return "", err
	}
	return string(md), nil
}

func getMetadataAttributes(key string) (map[string]string, error) {
	md, err := getMetadata(key, true)
	if err != nil {
		return nil, err
	}
	var att map[string]string
	return att, json.Unmarshal(md, &att)
}

func getMetadata(key string, recurse bool) ([]byte, error) {
	client := &http.Client{
		Timeout: defaultTimeout,
	}

	url := metadataURL + key
	if recurse {
		url += metadataHang
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

func main() {
	ctx := context.Background()
	username := os.Args[1]

	opts := logger.LogOpts{
		LoggerName:     programName,
		FormatFunction: logFormat,
	}
	opts.Writers = []io.Writer{&serialPort{"COM1"}, os.Stdout}
	logger.Init(ctx, opts)
	logger.Infof("Starting %s version %s for user %s.", programName, version, username)

	projectID, err := getMetadataKey("/project/project-id")
	if err == nil {
		opts.ProjectName = projectID
	} else {
		// TODO: just consider it disabled if no project is set..
		opts.DisableCloudLogging = true
	}

}
