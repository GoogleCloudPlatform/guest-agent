// Copyright 2017 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"context"
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
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	accountRegKey = "PublicKeys"
	credsWriter   = &utils.SerialPort{Port: "COM4"}
	minSSHVersion = versionInfo{8, 6}
	sshdRegKey    = `SYSTEM\CurrentControlSet\Services\sshd`
)

// newPwd will generate a random password that meets Windows complexity
// requirements: https://technet.microsoft.com/en-us/library/cc786468.
// Characters that are difficult for users to type on a command line (quotes,
// non english characters) are not used.
func newPwd(userPwLgth int) (string, error) {
	var pwLgth int
	minPwLgth := 15
	maxPwLgth := 255
	lower := []byte("abcdefghijklmnopqrstuvwxyz")
	upper := []byte("ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	numbers := []byte("0123456789")
	special := []byte(`~!@#$%^&*_-+=|\(){}[]:;<>,.?/`)
	chars := bytes.Join([][]byte{lower, upper, numbers, special}, nil)
	pwLgth = minPwLgth
	if userPwLgth > minPwLgth {
		pwLgth = userPwLgth
	}
	if userPwLgth > maxPwLgth {
		pwLgth = maxPwLgth
	}

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
	_, err = credsWriter.Write(append(data, []byte("\n")...))
	return err
}

func createOrResetPwd(ctx context.Context, k metadata.WindowsKey) (*credsJSON, error) {
	pwd, err := newPwd(k.PasswordLength)
	if err != nil {
		return nil, fmt.Errorf("error creating password: %v", err)
	}
	if _, err := userExists(k.UserName); err == nil {
		logger.Infof("Resetting password for user %s", k.UserName)
		if err := resetPwd(k.UserName, pwd); err != nil {
			return nil, fmt.Errorf("error running resetPwd: %v", err)
		}
		if k.AddToAdministrators != nil && *k.AddToAdministrators {
			if err := addUserToGroup(ctx, k.UserName, "Administrators"); err != nil {
				return nil, fmt.Errorf("error running addUserToGroup: %v", err)
			}
		}
	} else {
		logger.Infof("Creating user %s", k.UserName)
		if err := createUser(ctx, k.UserName, pwd); err != nil {
			return nil, fmt.Errorf("error running createUser: %v", err)
		}
		if k.AddToAdministrators == nil || *k.AddToAdministrators {
			if err := addUserToGroup(ctx, k.UserName, "Administrators"); err != nil {
				return nil, fmt.Errorf("error running addUserToGroup: %v", err)
			}
		}
	}

	return createcredsJSON(k, pwd)
}

func createSSHUser(ctx context.Context, user string) error {
	pwd, err := newPwd(20)
	if err != nil {
		return fmt.Errorf("error creating password: %v", err)
	}
	if _, err := userExists(user); err == nil {
		return nil
	}
	logger.Infof("Creating user %s", user)
	if err := createUser(ctx, user, pwd); err != nil {
		return fmt.Errorf("error running createUser: %v", err)
	}

	if err := addUserToGroup(ctx, user, "Administrators"); err != nil {
		return fmt.Errorf("error running addUserToGroup: %v", err)
	}
	return nil
}

func createcredsJSON(k metadata.WindowsKey, pwd string) (*credsJSON, error) {
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

func getWinSSHEnabled(md *metadata.Descriptor) bool {
	var enable bool
	if md.Project.Attributes.EnableWindowsSSH != nil {
		enable = *md.Project.Attributes.EnableWindowsSSH
	}
	if md.Instance.Attributes.EnableWindowsSSH != nil {
		enable = *md.Instance.Attributes.EnableWindowsSSH
	}
	return enable
}

type winAccountsMgr struct {
	// fakeWindows forces Disabled to run as if it was running in a windows system.
	// mostly target for unit tests.
	fakeWindows bool
}

func (a *winAccountsMgr) Diff(ctx context.Context) (bool, error) {
	oldSSHEnable := getWinSSHEnabled(oldMetadata)

	sshEnable := getWinSSHEnabled(newMetadata)
	if sshEnable != oldSSHEnable {
		return true, nil
	}
	if !reflect.DeepEqual(newMetadata.Instance.Attributes.WindowsKeys, oldMetadata.Instance.Attributes.WindowsKeys) {
		return true, nil
	}
	if !compareStringSlice(newMetadata.Instance.Attributes.SSHKeys, oldMetadata.Instance.Attributes.SSHKeys) {
		return true, nil
	}
	if !compareStringSlice(newMetadata.Project.Attributes.SSHKeys, oldMetadata.Project.Attributes.SSHKeys) {
		return true, nil
	}
	if newMetadata.Instance.Attributes.BlockProjectKeys != oldMetadata.Instance.Attributes.BlockProjectKeys {
		return true, nil
	}

	return false, nil
}

func (a *winAccountsMgr) Timeout(ctx context.Context) (bool, error) {
	return false, nil
}

func (a *winAccountsMgr) Disabled(ctx context.Context) (bool, error) {
	if !a.fakeWindows && runtime.GOOS != "windows" {
		return true, nil
	}

	config := cfg.Get()
	if config.AccountManager != nil {
		return config.AccountManager.Disable, nil
	}

	if newMetadata.Instance.Attributes.DisableAccountManager != nil {
		return *newMetadata.Instance.Attributes.DisableAccountManager, nil
	}
	if newMetadata.Project.Attributes.DisableAccountManager != nil {
		return *newMetadata.Project.Attributes.DisableAccountManager, nil
	}
	return false, nil
}

type versionInfo struct {
	major int
	minor int
}

func (v versionInfo) String() string {
	return fmt.Sprintf("%d.%d", v.major, v.minor)
}

func parseVersionInfo(psOutput []byte) (versionInfo, error) {
	verInfo := versionInfo{0, 0}
	verStr := strings.TrimSpace(string(psOutput))
	splitVer := strings.Split(verStr, ".")

	if len(splitVer) < 2 {
		return verInfo, fmt.Errorf("cannot parse OpenSSH version string: %v", verStr)
	}

	majorVer, err := strconv.Atoi(splitVer[0])
	if err != nil {
		return verInfo, err
	}
	verInfo.major = majorVer

	minorVer, err := strconv.Atoi(splitVer[1])
	if err != nil {
		return verInfo, err
	}
	verInfo.minor = minorVer

	return verInfo, nil
}

func versionOk(checkVersion versionInfo, minVersion versionInfo) error {
	versionError := fmt.Errorf("detected OpenSSH version may be incompatible with enable_windows_ssh. Found version %s, Need Version: %s", checkVersion, minVersion)

	if checkVersion.major < minVersion.major {
		return versionError
	}

	if checkVersion.major == minVersion.major && checkVersion.minor < minVersion.minor {
		return versionError
	}

	return nil
}

func verifyWinSSHVersion(ctx context.Context) error {
	sshdPath, err := getWindowsServiceImagePath(sshdRegKey)
	if err != nil {
		return fmt.Errorf("cannot determine sshd path: %v", err)
	}

	sshdVersion, err := getWindowsExeVersion(ctx, sshdPath)
	if err != nil {
		return fmt.Errorf("cannot determine OpenSSH Version: %v", err)
	}

	return versionOk(sshdVersion, minSSHVersion)
}

func (a *winAccountsMgr) Set(ctx context.Context) error {
	oldSSHEnable := getWinSSHEnabled(oldMetadata)
	sshEnable := getWinSSHEnabled(newMetadata)

	if sshEnable {
		if sshEnable != oldSSHEnable {
			err := verifyWinSSHVersion(ctx)
			if err != nil {
				logger.Warningf(err.Error())
			}

			if !checkWindowsServiceRunning(ctx, "sshd") {
				logger.Warningf("The 'enable-windows-ssh' metadata key is set to 'true' " +
					"but sshd does not appear to be running.")
			}
		}

		if sshKeys == nil {
			logger.Debugf("initialize sshKeys map")
			sshKeys = make(map[string][]string)
		}
		mdkeys := newMetadata.Instance.Attributes.SSHKeys
		if !newMetadata.Instance.Attributes.BlockProjectKeys {
			mdkeys = append(mdkeys, newMetadata.Project.Attributes.SSHKeys...)
		}

		mdKeyMap := getUserKeys(mdkeys)

		for user := range mdKeyMap {
			if err := createSSHUser(ctx, user); err != nil {
				logger.Errorf("Error creating user: %s", err)
			}
		}
	}

	newKeys := newMetadata.Instance.Attributes.WindowsKeys
	regKeys, err := readRegMultiString(regKeyBase, accountRegKey)
	if err != nil && err != errRegNotExist {
		return err
	}

	toAdd := compareAccounts(newKeys, regKeys)

	for _, key := range toAdd {
		creds, err := createOrResetPwd(ctx, key)
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

func compareAccounts(newKeys metadata.WindowsKeys, oldStrKeys []string) metadata.WindowsKeys {
	if len(newKeys) == 0 {
		return nil
	}
	if len(oldStrKeys) == 0 {
		return newKeys
	}

	var oldKeys metadata.WindowsKeys
	for _, s := range oldStrKeys {
		var key metadata.WindowsKey
		if err := json.Unmarshal([]byte(s), &key); err != nil {
			if !slices.Contains(badReg, s) {
				logger.Errorf("Bad windows key from registry: %s", err)
				badReg = append(badReg, s)
			}
			continue
		}
		oldKeys = append(oldKeys, key)
	}

	var toAdd metadata.WindowsKeys
	for _, key := range newKeys {
		if func(key metadata.WindowsKey, oldKeys metadata.WindowsKeys) bool {
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
