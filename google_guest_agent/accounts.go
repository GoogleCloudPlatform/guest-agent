//  Copyright 2019 Google Inc. All Rights Reserved.
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
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	// sshKeys is a cache of what we have added to each managed users' authorized
	// keys file. Avoids necessity of re-reading all files on every change.
	sshKeys         map[string][]string
	googleUsersFile = "/var/lib/google/google_users"
)

// compareStringSlice returns true if two string slices are equal, false
// otherwise. Does not modify the slices.
func compareStringSlice(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for _, list := range [][]string{first, second} {
		sortfunc := func(i, j int) bool { return list[i] < list[j] }
		list = append([]string{}, list...)
		sort.Slice(list, sortfunc)
	}
	for idx := range first {
		if first[idx] != second[idx] {
			return false
		}
	}
	return true
}

type accountsMgr struct{}

func (a *accountsMgr) diff() bool {
	// If any keys have changed.
	if !compareStringSlice(newMetadata.Instance.Attributes.SSHKeys, oldMetadata.Instance.Attributes.SSHKeys) {
		return true
	}
	if !compareStringSlice(newMetadata.Project.Attributes.SSHKeys, oldMetadata.Project.Attributes.SSHKeys) {
		return true
	}

	// If any existing keys have expired.
	for _, keys := range sshKeys {
		if len(keys) != len(removeExpiredKeys(keys)) {
			return true
		}
	}

	return false
}

func (a *accountsMgr) timeout() bool {
	return false
}

func (a *accountsMgr) disabled(os string) bool {
	return false ||
		os == "windows" ||
		!config.Section("Daemons").Key("accounts_daemon").MustBool(true) ||
		newMetadata.Instance.Attributes.EnableOSLogin ||
		newMetadata.Project.Attributes.EnableOSLogin
}

func (a *accountsMgr) set() error {
	if sshKeys == nil {
		sshKeys = make(map[string][]string)
	}

	if err := createSudoersFile(); err != nil {
		logger.Errorf("Error creating google-sudoers file: %v.", err)
	}
	if err := createSudoersGroup(); err != nil {
		logger.Errorf("Error creating google-sudoers group: %v.", err)
	}

	mdkeys := newMetadata.Instance.Attributes.SSHKeys
	if !newMetadata.Instance.Attributes.BlockProjectKeys {
		mdkeys = append(mdkeys, newMetadata.Project.Attributes.SSHKeys...)
	}

	mdKeyMap := make(map[string][]string)
	for _, key := range removeExpiredKeys(mdkeys) {
		idx := strings.Index(key, ":")
		if idx == -1 {
			continue
		}
		user := key[:idx]
		if user == "" {
			continue
		}
		userKeys := mdKeyMap[user]
		userKeys = append(userKeys, key[idx+1:])
		mdKeyMap[user] = userKeys
	}

	var writeFile bool
	gUsers, err := readGoogleUsersFile()
	if err != nil {
		logger.Errorf("Couldn't read google users file: %v.", err)
	}

	// Update SSH keys, creating Google users as needed.
	for user, userKeys := range mdKeyMap {
		if _, err := getPasswd(user); err != nil {
			logger.Infof("Creating user %s.", user)
			if err := createGoogleUser(user); err != nil {
				logger.Errorf("Error creating user: %s.", err)
				continue
			}
			gUsers[user] = ""
			writeFile = true
		}
		if _, ok := gUsers[user]; !ok {
			logger.Infof("Adding existing user %s to google-sudoers group.", user)
			if err := addUserToGroup(user, "google-sudoers"); err != nil {
				logger.Errorf("%v.", err)
			}
		}
		if !compareStringSlice(userKeys, sshKeys[user]) {
			logger.Infof("Updating keys for user %s.", user)
			if err := updateAuthorizedKeysFile(user, userKeys); err != nil {
				logger.Errorf("Error updating SSH keys for %s: %v.", user, err)
				continue
			}
			sshKeys[user] = userKeys
		}
	}

	// Remove Google users not found in metadata.
	for user := range gUsers {
		if _, ok := mdKeyMap[user]; !ok && user != "" {
			logger.Infof("Removing user %s.", user)
			err = removeGoogleUser(user)
			if err != nil {
				logger.Errorf("Error removing user: %v.", err)
			}
			delete(sshKeys, user)
			writeFile = true
		}
	}

	// Update the google_users file if we've added or removed any users.
	if writeFile {
		if err := writeGoogleUsersFile(); err != nil {
			logger.Errorf("Error writing google_users file: %v.", err)
		}
	}
	return nil
}

// passwdEntry is a user.User with omitted passwd fields restored.
type passwdEntry struct {
	Username string
	Passwd   string
	UID      int
	GID      int
	Name     string
	HomeDir  string
	Shell    string
}

// getPasswd returns a passwdEntry from the local passwd database. Code adapted from os/user
func getPasswd(user string) (*passwdEntry, error) {
	prefix := []byte(user + ":")
	colon := []byte{':'}

	parse := func(line []byte) (*passwdEntry, error) {
		if !bytes.Contains(line, prefix) || bytes.Count(line, colon) < 6 {
			return nil, nil
		}
		// kevin:x:1005:1006::/home/kevin:/usr/bin/zsh
		parts := strings.SplitN(string(line), ":", 7)
		uid, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("invalid passwd entry for %s", user)
		}
		gid, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil, fmt.Errorf("invalid passwd entry for %s", user)
		}
		u := &passwdEntry{
			Username: parts[0],
			Passwd:   parts[1],
			UID:      uid,
			GID:      gid,
			Name:     parts[4],
			HomeDir:  parts[5],
			Shell:    parts[6],
		}
		return u, nil
	}

	passwd, err := os.Open("/etc/passwd")
	if err != nil {
		return nil, err
	}
	bs := bufio.NewScanner(passwd)
	for bs.Scan() {
		line := bs.Bytes()
		// There's no spec for /etc/passwd or /etc/group, but we try to follow
		// the same rules as the glibc parser, which allows comments and blank
		// space at the beginning of a line.
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		v, err := parse(line)
		if v != nil || err != nil {
			return v, err
		}
	}
	return nil, fmt.Errorf("User not found")
}

func writeGoogleUsersFile() error {
	dir := path.Dir(googleUsersFile)
	if _, err := os.Stat(dir); err != nil {
		if err = os.Mkdir(dir, 0755); err != nil {
			return err
		}
	}

	gfile, err := os.OpenFile(googleUsersFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err == nil {
		defer gfile.Close()
		for user := range sshKeys {
			fmt.Fprintf(gfile, "%s\n", user)
		}
	}
	return err
}

func readGoogleUsersFile() (map[string]string, error) {
	res := make(map[string]string)
	gUsers, err := ioutil.ReadFile(googleUsersFile)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, user := range strings.Split(string(gUsers), "\n") {
		if user != "" {
			res[user] = ""
		}
	}
	return res, nil
}

type linuxKey windowsKey

// expired returns true if the key's expireOn field is in the past, false otherwise.
func (k linuxKey) expired() bool {
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

// removeExpiredKeys returns the provided list of keys with expired keys removed.
func removeExpiredKeys(keys []string) []string {
	var res []string
	for i := 0; i < len(keys); i++ {
		key := strings.Trim(keys[i], " ")
		if key == "" {
			continue
		}
		fields := strings.SplitN(key, " ", 4)
		if fields[2] != "google-ssh" {
			res = append(res, key)
			continue
		}
		jsonkey := fields[len(fields)-1]
		lkey := linuxKey{}
		if err := json.Unmarshal([]byte(jsonkey), &lkey); err != nil {
			continue
		}
		if !lkey.expired() {
			res = append(res, key)
		}
	}
	return res
}

func runUserGroupCmd(cmd, user, group string) error {
	cmd = strings.Replace(cmd, "{user}", user, 1)
	cmd = strings.Replace(cmd, "{group}", group, 1)
	cmds := strings.Fields(cmd)
	return exec.Command(cmds[0], cmds[1:]...).Run()
}

// createGoogleUser creates a Google managed user account if needed and adds it
// to the configured groups.
func createGoogleUser(user string) error {
	useradd := config.Section("Accounts").Key("useradd_cmd").MustString("useradd -m -s /bin/bash -p * {user}")
	err := runUserGroupCmd(useradd, user, "")
	if err != nil {
		return err
	}
	groups := config.Section("Accounts").Key("groups").MustString("adm,dip,docker,lxd,plugdev,video")
	for _, group := range strings.Split(groups, ",") {
		addUserToGroup(user, group)
	}
	return addUserToGroup(user, "google-sudoers")
}

// removeGoogleUser removes Google managed users. If deprovision_remove is true, the
// user and its home directory are removed. Otherwise, SSH keys and sudoer
// permissions are removed but the user remains on the system. Group membership
// is not changed.
func removeGoogleUser(user string) error {
	var err error
	if config.Section("Accounts").Key("deprovision_remove").MustBool(true) {
		userdel := config.Section("Accounts").Key("userdel_cmd").MustString("userdel -r {user}")
		err = runUserGroupCmd(userdel, user, "")
		if err != nil {
			return err
		}
	} else {
		err = updateAuthorizedKeysFile(user, []string{})
		if err != nil {
			return err
		}
		gpasswddel := config.Section("Accounts").Key("gpasswd_remove_cmd").MustString("gpasswd -d {user} {group}")
		err = runUserGroupCmd(gpasswddel, user, "google-sudoers")
		if err != nil {
			return err
		}
	}
	return nil
}

// createSudoersFile creates the google_sudoers configuration file if it does
// not exist and specifies the group 'google-sudoers' should have all
// permissions.
func createSudoersFile() error {
	sudoFile, err := os.OpenFile("/etc/sudoers.d/google_sudoers", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0440)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	defer sudoFile.Close()
	fmt.Fprintf(sudoFile, "%%google-sudoers ALL=(ALL:ALL) NOPASSWD:ALL\n")
	return nil
}

// createSudoersGroup creates the google-sudoers group if it does not exist.
func createSudoersGroup() error {
	groupadd := config.Section("Accounts").Key("groupadd_cmd").MustString("groupadd {group}")
	err := runUserGroupCmd(groupadd, "", "google-sudoers")
	if err != nil {
		if v, ok := err.(*exec.ExitError); ok && v.ExitCode() == 9 {
			// 9 means group already exists.
			return nil
		}
	}
	logger.Infof("Created google sudoers file")
	return nil
}

func addUserToGroup(user, group string) error {
	gpasswdadd := config.Section("Accounts").Key("useradd_").MustString("gpasswd -a {user}")
	return runUserGroupCmd(gpasswdadd, user, group)
}

// updateAuthorizedKeysFile adds provided keys to the user's SSH
// AuthorizedKeys file. The file and containing directory are created if it
// does not exist. Uses a temporary file to avoid partial updates in case of
// errors. If no keys are provided, the authorized keys file is removed.
func updateAuthorizedKeysFile(user string, keys []string) error {
	gcomment := "# Added by Google"

	passwd, err := getPasswd(user)
	if err != nil {
		return err
	}

	if passwd.HomeDir == "" {
		return fmt.Errorf("user %s has no homedir set", user)
	}
	if passwd.Shell == "/sbin/nologin" {
		return nil
	}

	sshpath := path.Join(passwd.HomeDir, ".ssh")
	if _, err := os.Stat(sshpath); err != nil {
		if os.IsNotExist(err) {
			if err = os.Mkdir(sshpath, 0700); err != nil {
				return err
			}
			if err = os.Chown(sshpath, passwd.UID, passwd.GID); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	akpath := path.Join(sshpath, "authorized_keys")
	// Remove empty file.
	if len(keys) == 0 {
		os.Remove(akpath)
		return nil
	}

	tempPath := akpath + ".google"
	akcontents, err := ioutil.ReadFile(akpath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var isgoogle bool
	var userKeys []string
	for _, key := range strings.Split(string(akcontents), "\n") {
		if key == "" {
			continue
		}
		if isgoogle {
			isgoogle = false
			continue
		}
		if key == gcomment {
			isgoogle = true
			continue
		}
		userKeys = append(userKeys, key)
	}

	newfile, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer newfile.Close()

	for _, key := range userKeys {
		fmt.Fprintf(newfile, "%s\n", key)
	}
	for _, key := range keys {
		fmt.Fprintf(newfile, "%s\n%s\n", gcomment, key)
	}
	err = os.Chown(tempPath, passwd.UID, passwd.GID)
	if err != nil {
		// Existence of temp file will block further updates for this user.
		// Don't catch remove error, nothing we can do. Return the
		// chown error which caused the issue.
		os.Remove(tempPath)
		return fmt.Errorf("error setting ownership of new keys file: %v", err)
	}
	return os.Rename(tempPath, akpath)
}
