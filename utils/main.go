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
	"errors"
	"strings"
	"time"

	"github.com/tarm/serial"
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

//CheckExpired takes a time string and determines if it represents a time in the past.
func CheckExpired(expireOn string) (bool, error) {
	t, err := time.Parse(time.RFC3339, expireOn)
	if err != nil {
		t2, err2 := time.Parse("2006-01-02T15:04:05-0700", expireOn)
		if err2 != nil {
			return true, err //Return RFC3339 error
		}
		t = t2
	}
	return t.Before(time.Now()), nil

}

//GetUserKey takes a string and determines if it is a valid SSH key and returns
//the user and key if valid, nil otherwise.
func GetUserKey(rawKey string) (string, string, error) {

	key := strings.Trim(rawKey, " ")
	if key == "" {
		return "", "", errors.New("Invalid ssh key entry - empty key")
	}
	idx := strings.Index(key, ":")
	if idx == -1 {
		return "", "", errors.New("Invalid ssh key entry - unrecognized format")
	}
	user := key[:idx]
	if user == "" {
		return "", "", errors.New("Invalid ssh key entry - user missing")
	}
	fields := strings.SplitN(key, " ", 4)
	if len(fields) == 3 && fields[2] == "google-ssh" {
		// expiring key without expiration format.
		return "", "", errors.New("Invalid ssh key entry - expiration missing")
	}
	if len(fields) > 3 {
		lkey := sshKeyData{}
		if err := json.Unmarshal([]byte(fields[3]), &lkey); err != nil {
			// invalid expiration format.
			return "", "", err
		}
		expired, err := CheckExpired(lkey.ExpireOn)
		if err != nil {
			return "", "", err
		}
		if expired {
			return "", "", errors.New("Invalid ssh key entry - expired key")
		}
	}

	return user, key[idx+1:], nil
}

//SerialPort is a type for writing to a named serial port.
type SerialPort struct {
	Port string
}

func (s *SerialPort) Write(b []byte) (int, error) {
	c := &serial.Config{Name: s.Port, Baud: 115200}
	p, err := serial.OpenPort(c)
	if err != nil {
		return 0, err
	}
	defer p.Close()

	return p.Write(b)
}
