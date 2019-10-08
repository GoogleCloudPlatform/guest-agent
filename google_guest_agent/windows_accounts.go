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
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"math/big"
	"reflect"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	accountRegKey = "PublicKeys"
)

// newPwd will generate a random password that meets Windows complexity
// requirements: https://technet.microsoft.com/en-us/library/cc786468.
// Characters that are difficult for users to type on a command line (quotes,
// non english characters) are not used.
func newPwd() (string, error) {
	pwLgth := 15
	lower := []byte("abcdefghijklmnopqrstuvwxyz")
	upper := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	numbers := []byte("0123456789")
	special := []byte(`~!@#$%^&*_-+=|\(){}[]:;<>,.?/`)
	chars := bytes.Join([][]byte{lower, upper, numbers, special}, nil)

	for {
		b := make([]byte, pwLgth)
		for i := range b {
			ci, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
			if err != nil {
				return "", err
			}
			b[i] = chars[ci.Int64()]
		}

		var l, u, n, s int
		if bytes.ContainsAny(lower, string(b)) {
			l = 1
		}
		if bytes.ContainsAny(upper, string(b)) {
			u = 1
		}
		if bytes.ContainsAny(numbers, string(b)) {
			n = 1
		}
		if bytes.ContainsAny(special, string(b)) {
			s = 1
		}
		// If the password does not meet Windows complexity requirements, try again.
		// https://technet.microsoft.com/en-us/library/cc786468
		if l+u+n+s >= 3 {
			return string(b), nil
		}
	}
}

type credsJSON struct {
	ErrorMessage      string `json:"errorMessage,omitempty"`
	EncryptedPassword string `json:"encryptedPassword,omitempty"`
	UserName          string `json:"userName,omitempty"`
	PasswordFound     bool   `json:"passwordFound,omitempty"`
	Exponent          string `json:"exponent,omitempty"`
	Modulus           string `json:"modulus,omitempty"`
	HashFunction      string `json:"hashFunction,omitempty"`
}

func printCreds(creds *credsJSON) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	return writeSerial("COM4", append(data, []byte("\n")...))
}

var badExpire []string

func (k windowsKey) expired() bool {
	t, err := time.Parse(time.RFC3339, k.ExpireOn)
	if err != nil {
		if !containsString(k.ExpireOn, badExpire) {
			logger.Errorf("error parsing time: %s", err)
			badExpire = append(badExpire, k.ExpireOn)
		}
		return true
	}
	return t.Before(time.Now())
}

func (k windowsKey) createOrResetPwd() (*credsJSON, error) {
	pwd, err := newPwd()
	if err != nil {
		return nil, fmt.Errorf("error creating password: %v", err)
	}
	if _, err := userExists(k.UserName); err == nil {
		logger.Infof("Resetting password for user %s", k.UserName)
		if err := resetPwd(k.UserName, pwd); err != nil {
			return nil, fmt.Errorf("error running resetPwd: %v", err)
		}
	} else {
		logger.Infof("Creating user %s", k.UserName)
		if err := createAdminUser(k.UserName, pwd); err != nil {
			return nil, fmt.Errorf("error running createAdminUser: %v", err)
		}
	}

	return createcredsJSON(k, pwd)
}

func createcredsJSON(k windowsKey, pwd string) (*credsJSON, error) {
	mod, err := base64.StdEncoding.DecodeString(k.Modulus)
	if err != nil {
		return nil, fmt.Errorf("error decoding modulus: %v", err)
	}
	exp, err := base64.StdEncoding.DecodeString(k.Exponent)
	if err != nil {
		return nil, fmt.Errorf("error decoding exponent: %v", err)
	}

	key := &rsa.PublicKey{
		N: new(big.Int).SetBytes(mod),
		E: int(new(big.Int).SetBytes(exp).Int64()),
	}

	if k.HashFunction == "" {
		k.HashFunction = "sha1"
	}

	var hashFunc hash.Hash
	switch k.HashFunction {
	case "sha1":
		hashFunc = sha1.New()
	case "sha256":
		hashFunc = sha256.New()
	case "sha512":
		hashFunc = sha512.New()
	default:
		return nil, fmt.Errorf("unknown hash function requested: %q", k.HashFunction)
	}

	encPwd, err := rsa.EncryptOAEP(hashFunc, rand.Reader, key, []byte(pwd), nil)
	if err != nil {
		return nil, fmt.Errorf("error encrypting password: %v", err)
	}

	return &credsJSON{
		PasswordFound:     true,
		Exponent:          k.Exponent,
		Modulus:           k.Modulus,
		UserName:          k.UserName,
		HashFunction:      k.HashFunction,
		EncryptedPassword: base64.StdEncoding.EncodeToString(encPwd),
	}, nil
}

type winAccountsMgr struct{}

func (a *winAccountsMgr) diff() bool {
	return !reflect.DeepEqual(newMetadata.Instance.Attributes.WindowsKeys, oldMetadata.Instance.Attributes.WindowsKeys)
}

func (a *winAccountsMgr) timeout() bool {
	return false
}

func (a *winAccountsMgr) disabled(os string) (disabled bool) {
	if os != "windows" {
		return true
	}

	disabled, err := config.Section("accountManager").Key("disable").Bool()
	if err == nil {
		return disabled
	}
	if newMetadata.Instance.Attributes.DisableAccountManager != nil {
		return *newMetadata.Instance.Attributes.DisableAccountManager
	}
	if newMetadata.Project.Attributes.DisableAccountManager != nil {
		return *newMetadata.Project.Attributes.DisableAccountManager
	}
	return false
}

var badKeys []string

func (a *winAccountsMgr) set() error {
	newKeys := newMetadata.Instance.Attributes.WindowsKeys
	regKeys, err := readRegMultiString(regKeyBase, accountRegKey)
	if err != nil && err != errRegNotExist {
		return err
	}

	toAdd := compareAccounts(newKeys, regKeys)

	for _, key := range toAdd {
		creds, err := key.createOrResetPwd()
		if err == nil {
			printCreds(creds)
			continue
		}
		logger.Errorf("error setting password: %s", err)
		creds = &credsJSON{
			PasswordFound: false,
			Exponent:      key.Exponent,
			Modulus:       key.Modulus,
			UserName:      key.UserName,
			ErrorMessage:  err.Error(),
		}
		printCreds(creds)
	}

	var jsonKeys []string
	for _, key := range newKeys {
		jsn, err := json.Marshal(key)
		if err != nil {
			// This *should* never happen as each key was just Unmarshalled above.
			logger.Errorf("Failed to marshal windows key to JSON: %s", err)
			continue
		}
		jsonKeys = append(jsonKeys, string(jsn))
	}
	return writeRegMultiString(regKeyBase, accountRegKey, jsonKeys)
}

var badReg []string

func compareAccounts(newKeys windowsKeys, oldStrKeys []string) windowsKeys {
	if len(newKeys) == 0 {
		return nil
	}
	if len(oldStrKeys) == 0 {
		return newKeys
	}

	var oldKeys windowsKeys
	for _, s := range oldStrKeys {
		var key windowsKey
		if err := json.Unmarshal([]byte(s), &key); err != nil {
			if !containsString(s, badReg) {
				logger.Errorf("Bad windows key from registry: %s", err)
				badReg = append(badReg, s)
			}
			continue
		}
		oldKeys = append(oldKeys, key)
	}

	var toAdd windowsKeys
	for _, key := range newKeys {
		if func(key windowsKey, oldKeys windowsKeys) bool {
			for _, oldKey := range oldKeys {
				if oldKey.UserName == key.UserName &&
					oldKey.Modulus == key.Modulus &&
					oldKey.ExpireOn == key.ExpireOn {
					return false
				}
			}
			return true
		}(key, oldKeys) {
			toAdd = append(toAdd, key)
		}
	}
	return toAdd
}
