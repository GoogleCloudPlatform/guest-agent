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

// Utilities for Google Guest Agent and Google Authorized Keys

package utils

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

//ContainsString checks for the presence of a string in a slice.
func ContainsString(s string, ss []string) bool {
	for _, a := range ss {
		if a == s {
			return true
		}
	}
	return false
}

type sshKeyData struct {
	ExpireOn string
	UserName string
}

var badExpire []string

//CheckExpired takes a time string and determines if it represents a time in the past.
func CheckExpired(expireOn string) bool {
    t, err := time.Parse(time.RFC3339, expireOn)
	if err != nil {
		t2, err2 := time.Parse("2006-01-02T15:04:05-0700", expireOn)
		if err2 != nil {
			if !ContainsString(expireOn, badExpire) {
				logger.Errorf("Error parsing time %v.", err)
				badExpire = append(badExpire, expireOn)
			}
			return true
		}
		t = t2
	}
	return t.Before(time.Now())

}

func (k sshKeyData) expired() bool {
	return CheckExpired(k.ExpireOn)
}

//ValidateKey takes a string and determines if it is a valid SSH key and returns
//the user and key if valid, nil otherwise.
func ValidateKey(rawKey string) []string {

	key := strings.Trim(rawKey, " ")
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
	var validatedKeyData []string
	validatedKeyData = append(validatedKeyData, user, key[idx+1:])
	return validatedKeyData
}
