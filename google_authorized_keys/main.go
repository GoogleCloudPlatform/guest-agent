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
	"strconv"
	"strings"

	//"strings"
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

func containsString(s string, ss []string) bool {
	for _, a := range ss {
		if a == s {
			return true
		}
	}
	return false
}

func validateKey(raw_key string) []string {

	
	key := strings.Trim(raw_key, " ")
	if key == "" {
		logger.Debugf("Invalid ssh key entry: %q", key)
		return nil
	}
	idx := strings.Index(key, ":")
	if idx == -1 {
		logger.Debugf("Invalid ssh key entry: %q", key)
		return nil
	}
	user := key[:idx]
	if user == "" {
		logger.Debugf("invalid ssh key entry: %q", key)
		return nil
	}
	fields := strings.SplitN(key, " ", 4)
	if len(fields) == 3 && fields[2] == "google-ssh" {
		logger.Debugf("invalid ssh key entry: %q", key)
		// expiring key without expiration format.
		return nil
	}
	if len(fields) > 3 {
		lkey := sshKeyData{}
		if err := json.Unmarshal([]byte(fields[3]), &lkey); err != nil {
			// invalid expiration format.
			logger.Debugf("invalid ssh key entry: %q", key)
			return nil
		}
		if lkey.expired() {
			logger.Debugf("expired ssh key entry: %q", key)
			return nil
		}
	}
	var validated_key_data []string
	validated_key_data = append(validated_key_data, user, key[idx+1:])
	return validated_key_data
}

type sshKeyData struct {
	ExpireOn            string
	UserName            string
}

var badExpire []string
// expired returns true if the key's expireOn field is in the past, false otherwise.
func (k sshKeyData) expired() bool {
	t, err := time.Parse("2006-01-02T15:04:05-0700", k.ExpireOn)
	if err != nil {
		if !containsString(k.ExpireOn, badExpire) {
			logger.Errorf("Error parsing time: %v.", err)
			badExpire = append(badExpire, k.ExpireOn)
		}
		return true
	}
	return t.Before(time.Now())
}


func parseSshKeys(username string, rawKeys string) ([]string) {
    var key_list []string
	keys := strings.Split(rawKeys, "\n")
   for _, key := range keys {
	   key_split := strings.SplitN(key, ":", 2)
	   if len(key_split) != 2 {
		   continue
	   }

	   validated_key_data := validateKey(key)
	   if validated_key_data == nil {
		   continue
	   }

	   user:= validated_key_data[0]
	   key_val := validated_key_data[1]
       
	   if user == username {
		   key_list = append(key_list, key_val)
	   }
   }
   return key_list
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
	logger.Debugf("Starting %s version %s for user %s.", programName, version, username)

	projectID, err := getMetadataKey("/project/project-id")
	if err == nil {
		opts.ProjectName = projectID
	} else {
		// TODO: just consider it disabled if no project is set..
		opts.DisableCloudLogging = true
		os.Exit(1) 
	}
	logger.Debugf("ProjectId %s", projectID)

	block_project_ssh_keys := false
	bpsk_str, err := getMetadataKey("/instance/attributes/block-project-ssh-keys")
	if err == nil {
		block_project_ssh_keys, err = strconv.ParseBool(bpsk_str)
	}

	instance_keys, err := getMetadataKey("/instance/attributes/ssh-keys")

	if err == nil {
	instance_key_list := parseSshKeys(username, instance_keys)
	fmt.Printf(strings.Join(instance_key_list, "\n"))
	if instance_key_list != nil {
	  fmt.Printf("\n")
	}
	}

	if !block_project_ssh_keys {
	  project_keys, err := getMetadataKey("/project/attributes/ssh-keys")
	
      if err == nil {
	    project_key_list := parseSshKeys(username, project_keys)
		fmt.Printf(strings.Join(project_key_list, "\n"))
	  }
	}

}