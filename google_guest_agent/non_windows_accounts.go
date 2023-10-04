// Copyright 2019 Google LLC

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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
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

func removeExpiredKeys(keys []string) []string {
	var validKeys []string
	for _, key := range keys {
		if err := utils.CheckExpiredKey(key); err == nil {
			validKeys = append(validKeys, key)
		}
	}
	return validKeys
}

type accountsMgr struct{}

func (a *accountsMgr) Diff(ctx context.Context) (bool, error) {
	// If any keys have changed.
	if !compareStringSlice(newMetadata.Instance.Attributes.SSHKeys, oldMetadata.Instance.Attributes.SSHKeys) {
		return true, nil
	}
	if !compareStringSlice(newMetadata.Project.Attributes.SSHKeys, oldMetadata.Project.Attributes.SSHKeys) {
		return true, nil
	}
	if newMetadata.Instance.Attributes.BlockProjectKeys != oldMetadata.Instance.Attributes.BlockProjectKeys {
		return true, nil
	}

	// If any on-disk keys have expired.
	for _, keys := range sshKeys {
		if len(keys) != len(removeExpiredKeys(keys)) {
			return true, nil
		}
	}
	// If we've just disabled OS Login.
	oldOslogin, _, _ := getOSLoginEnabled(oldMetadata)
	newOslogin, _, _ := getOSLoginEnabled(newMetadata)
	if oldOslogin && !newOslogin {
		return true, nil
	}

	return false, nil
}

func (a *accountsMgr) Timeout(ctx context.Context) (bool, error) {
	return false, nil
}

func (a *accountsMgr) Disabled(ctx context.Context) (bool, error) {
	config := cfg.Get()
	oslogin, _, _ := getOSLoginEnabled(newMetadata)
	return false || runtime.GOOS == "windows" || oslogin || !config.Daemons.AccountsDaemon, nil
}

func (a *accountsMgr) Set(ctx context.Context) error {
	config := cfg.Get()

	if sshKeys == nil {
		logger.Debugf("initialize sshKeys map")
		sshKeys = make(map[string][]string)
	}

	logger.Debugf("create sudoers file if needed")
	if err := createSudoersFile(); err != nil {
		logger.Errorf("Error creating google-sudoers file: %v.", err)
	}
	logger.Debugf("create sudoers group if needed")
	if err := createSudoersGroup(ctx, config); err != nil {
		logger.Errorf("Error creating google-sudoers group: %v.", err)
	}

	mdkeys := newMetadata.Instance.Attributes.SSHKeys
	if !newMetadata.Instance.Attributes.BlockProjectKeys {
		mdkeys = append(mdkeys, newMetadata.Project.Attributes.SSHKeys...)
	}

	mdKeyMap := getUserKeys(mdkeys)

	logger.Debugf("read google users file")
	gUsers, err := readGoogleUsersFile()
	if err != nil {
		// TODO: is this OK to continue past?
		logger.Errorf("Couldn't read google_users file: %v.", err)
	}

	// Update SSH keys, creating Google users as needed.
	for user, userKeys := range mdKeyMap {
		if _, err := getPasswd(user); err != nil {
			logger.Infof("Creating user %s.", user)
			if err := createGoogleUser(ctx, config, user); err != nil {
				logger.Errorf("Error creating user: %s.", err)
				continue
			}
			gUsers[user] = ""
		}
		if _, ok := gUsers[user]; !ok {
			logger.Infof("Adding existing user %s to google-sudoers group.", user)
			if err := addUserToGroup(ctx, user, "google-sudoers"); err != nil {
				logger.Errorf("%v.", err)
			}
		}
		if !compareStringSlice(userKeys, sshKeys[user]) {
			logger.Infof("Updating keys for user %s.", user)
			if err := updateAuthorizedKeysFile(ctx, user, userKeys); err != nil {
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
			err = removeGoogleUser(ctx, config, user)
			if err != nil {
				logger.Errorf("Error removing user: %v.", err)
			}
			delete(sshKeys, user)
		}
	}

	// Update the google_users file if we've added or removed any users.
	logger.Debugf("write google_users file")
	if err := writeGoogleUsersFile(); err != nil {
		logger.Errorf("Error writing google_users file: %v.", err)
	}

	// Start SSHD if not started. We do this in agent instead of adding a
	// Wants= directive, and here instead of instance setup, so that this
	// can be disabled by the instance configs file.
	for _, svc := range []string{"ssh", "sshd"} {
		// Ignore output, it's just a best effort.
		systemctlStart(ctx, svc)
	}

	return nil
}

var badSSHKeys []string

// getUserKeys returns the keys which are not expired and non-expiring key.
// valid formats are:
// user:ssh-rsa [KEY_VALUE] [USERNAME]
// user:ssh-rsa [KEY_VALUE]
// user:ssh-rsa [KEY_VALUE] google-ssh {"userName":"[USERNAME]","expireOn":"[EXPIRE_TIME]"}
func getUserKeys(mdkeys []string) map[string][]string {
	mdKeyMap := make(map[string][]string)
	for i := 0; i < len(mdkeys); i++ {
		trimmedKey := strings.Trim(mdkeys[i], " ")
		if trimmedKey != "" {
			user, keyVal, err := utils.GetUserKey(trimmedKey)
			if err == nil {
				err = utils.ValidateUserKey(user, keyVal)
			}

			if err != nil {
				if !utils.ContainsString(trimmedKey, badSSHKeys) {
					logger.Errorf("%s: %s", err.Error(), trimmedKey)
					badSSHKeys = append(badSSHKeys, trimmedKey)
				}
				continue
			}

			// key which is not expired or non-expiring key, add it.
			userKeys := mdKeyMap[user]
			userKeys = append(userKeys, keyVal)
			mdKeyMap[user] = userKeys
		}
	}
	return mdKeyMap
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
		if !bytes.HasPrefix(line, prefix) || bytes.Count(line, colon) < 6 {
			return nil, nil
		}
		// kevin:x:1005:1006::/home/kevin:/usr/bin/zsh
		parts := strings.SplitN(string(line), ":", 7)
		if len(parts) < 7 {
			return nil, fmt.Errorf("invalid passwd entry for %s", user)
		}
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
	return nil, fmt.Errorf("user not found")
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
	gUsers, err := os.ReadFile(googleUsersFile)
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

// Replaces {user} or {group} in command string. Supports legacy python-era
// user command overrides.
func createUserGroupCmd(cmd, user, group string) (string, []string) {
	cmd = strings.Replace(cmd, "{user}", user, 1)
	cmd = strings.Replace(cmd, "{group}", group, 1)

	// We don't run the command here because we might need the exit codes.
	tokens := strings.Fields(cmd)
	return tokens[0], tokens[1:]
}

// createGoogleUser creates a Google managed user account if needed and adds it
// to the configured groups.
func createGoogleUser(ctx context.Context, config *cfg.Sections, user string) error {
	var uid string
	if config.Accounts.ReuseHomedir {
		uid = getUID(fmt.Sprintf("/home/%s", user))
	}

	if err := createUser(ctx, user, uid); err != nil {
		return err
	}
	groups := config.Accounts.Groups
	for _, group := range strings.Split(groups, ",") {
		addUserToGroup(ctx, user, group)
	}
	return addUserToGroup(ctx, user, "google-sudoers")
}

// removeGoogleUser removes Google managed users. If deprovision_remove is true, the
// user and its home directory are removed. Otherwise, SSH keys and sudoer
// permissions are removed but the user remains on the system. Group membership
// is not changed.
func removeGoogleUser(ctx context.Context, config *cfg.Sections, user string) error {
	if config.Accounts.DeprovisionRemove {
		userdel := config.Accounts.UserDelCmd
		name, args := createUserGroupCmd(userdel, user, "")
		return run.Quiet(ctx, name, args...)
	}
	if err := updateAuthorizedKeysFile(ctx, user, []string{}); err != nil {
		return err
	}
	gpasswddel := config.Accounts.GPasswdRemoveCmd
	name, args := createUserGroupCmd(gpasswddel, user, "google-sudoers")
	return run.Quiet(ctx, name, args...)
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
func createSudoersGroup(ctx context.Context, config *cfg.Sections) error {
	groupadd := config.Accounts.GroupAddCmd
	name, args := createUserGroupCmd(groupadd, "", "google-sudoers")
	ret := run.WithOutput(ctx, name, args...)
	if ret.ExitCode == 9 {
		// 9 means group already exists.
		return nil
	}
	if ret.ExitCode != 0 {
		return error(ret)
	}
	logger.Infof("Created google sudoers file")
	return nil
}

// updateAuthorizedKeysFile adds provided keys to the user's SSH
// AuthorizedKeys file. The file and containing directory are created if it
// does not exist. Uses a temporary file to avoid partial updates in case of
// errors. If no keys are provided, the authorized keys file is removed.
func updateAuthorizedKeysFile(ctx context.Context, user string, keys []string) error {
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
	akcontents, err := os.ReadFile(akpath)
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

	newfile, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE, 0600)
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

	_, err = exec.LookPath("restorecon")
	if err == nil {
		if err := run.Quiet(ctx, "restorecon", tempPath); err != nil {
			return fmt.Errorf("error setting selinux context: %+v", err)
		}
	}

	return os.Rename(tempPath, akpath)
}
