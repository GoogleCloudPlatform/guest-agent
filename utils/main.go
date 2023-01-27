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
	"regexp"
	"strings"
	"time"

	"github.com/tarm/serial"
)

// ContainsString checks for the presence of a string in a slice.
func ContainsString(s string, ss []string) bool {
	for _, a := range ss {
		if a == s {
			return true
		}
	}
	return false
}

type sshExpiration struct {
	ExpireOn string
	UserName string
}

// CheckExpiredKey validates whether a key has expired.
// Keys with invalid expiration formats will result in an error.
func CheckExpiredKey(key string) error {
	trimmedKey := strings.Trim(key, " ")
	if trimmedKey == "" {
		return errors.New("Invalid ssh key entry - empty key")
	}
	fields := strings.SplitN(trimmedKey, " ", 4)
	if len(fields) < 3 {
		// Non-expiring key.
		return nil
	}
	if len(fields) == 3 && fields[2] == "google-ssh" {
		// expiring key without expiration format.
		return errors.New("Invalid ssh key entry - expiration missing")
	}
	if len(fields) >= 3 && fields[2] != "google-ssh" {
		// Non-expiring key with an arbitrary comment part
		return nil
	}
	if len(fields) > 3 {
		lkey := sshExpiration{}
		if err := json.Unmarshal([]byte(fields[3]), &lkey); err != nil {
			// invalid expiration format.
			return err
		}
		expired, err := CheckExpired(lkey.ExpireOn)
		if err != nil {
			return err
		}
		if expired {
			return errors.New("Invalid ssh key entry - expired key")
		}
	}

	return nil
}

// CheckExpired takes a time string and determines if it represents a time in the past.
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

// ValidateUser checks for the presence of a characters which should not be
// allowed in a username string, returns an error if any such characters are
// detected, nil otherwise.
// Currently, the only banned characters are whitespace characters.
func ValidateUser(user string) error {
	if user == "" {
		return errors.New("Invalid username - it is empty")
	}

	whiteSpaceRegexp, _ := regexp.Compile("\\s")

	if whiteSpaceRegexp.MatchString(user) {
		return errors.New("Invalid username - whitespace detected")
	}
	return nil
}

// GetUserKey returns a user and a SSH key if a rawKey has a correct format, nil otherwise.
// It doesn't validate entries.
func GetUserKey(rawKey string) (string, string, error) {
	key := strings.Trim(rawKey, " ")
	if key == "" {
		return "", "", errors.New("Invalid ssh key entry - empty key")
	}
	idx := strings.Index(key, ":")
	if idx == -1 {
		return "", "", errors.New("Invalid ssh key entry - unrecognized format. Expecting user:ssh-key")
	}
	user := key[:idx]
	if user == "" {
		return "", "", errors.New("Invalid ssh key entry - user missing")
	}
	if key[idx+1:] == "" {
		return "", "", errors.New("Invalid ssh key entry - key missing")
	}

	return user, key[idx+1:], nil
}

// ValidateUserKey takes an user and a key received from GetUserKey() and
// validate the user for special characters and the key for expiration
func ValidateUserKey(user, key string) error {
	if err := ValidateUser(user); err != nil {
		return err
	}
	if err := CheckExpiredKey(key); err != nil {
		return err
	}

	return nil
}

// SerialPort is a type for writing to a named serial port.
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
