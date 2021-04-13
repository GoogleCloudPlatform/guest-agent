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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	googleComment    = "# Added by Google Compute Engine OS Login."
	googleBlockStart = "#### Google OS Login control. Do not edit this section. ####"
	googleBlockEnd   = "#### End Google OS Login control section. ####"
)

type osloginMgr struct{}

// We also read project keys first, letting instance-level keys take
// precedence.
func getOSLoginEnabled(md *metadata) (bool, bool) {
	var enable bool
	if md.Project.Attributes.EnableOSLogin != nil {
		enable = *md.Project.Attributes.EnableOSLogin
	}
	if md.Instance.Attributes.EnableOSLogin != nil {
		enable = *md.Instance.Attributes.EnableOSLogin
	}
	var twofactor bool
	if md.Project.Attributes.TwoFactor != nil {
		twofactor = *md.Project.Attributes.TwoFactor
	}
	if md.Instance.Attributes.TwoFactor != nil {
		twofactor = *md.Instance.Attributes.TwoFactor
	}
	return enable, twofactor
}

func (o *osloginMgr) diff() bool {
	oldEnable, oldTwoFactor := getOSLoginEnabled(oldMetadata)
	enable, twofactor := getOSLoginEnabled(newMetadata)
	return oldMetadata.Project.ProjectID == "" ||
		// True on first run or if any value has changed.
		(oldTwoFactor != twofactor) ||
		(oldEnable != enable)
}

func (o *osloginMgr) timeout() bool {
	return false
}

func (o *osloginMgr) disabled(os string) bool {
	return os == "windows"
}

func (o *osloginMgr) set() error {
	// We need to know if it was previously enabled for the clearing of
	// metadata-based SSH keys.
	oldEnable, _ := getOSLoginEnabled(oldMetadata)
	enable, twofactor := getOSLoginEnabled(newMetadata)

	if enable && !oldEnable {
		logger.Infof("Enabling OS Login")
		newMetadata.Instance.Attributes.SSHKeys = nil
		newMetadata.Project.Attributes.SSHKeys = nil
		(&accountsMgr{}).set()
	}

	if !enable && oldEnable {
		logger.Infof("Disabling OS Login")
	}

	if err := writeSSHConfig(enable, twofactor); err != nil {
		logger.Errorf("Error updating SSH config: %v.", err)
	}

	if err := writeNSSwitchConfig(enable); err != nil {
		logger.Errorf("Error updating NSS config: %v.", err)
	}

	if err := writePAMConfig(enable, twofactor); err != nil {
		logger.Errorf("Error updating PAM config: %v.", err)
	}

	if err := writeGroupConf(enable); err != nil {
		logger.Errorf("Error updating group.conf: %v.", err)
	}

	for _, svc := range []string{"nscd", "unscd", "systemd-logind", "cron", "crond"} {
		if err := restartService(svc); err != nil {
			logger.Errorf("Error restarting service: %v.", err)
		}
	}

	// SSH should be explicitly started if not running.
	for _, svc := range []string{"ssh", "sshd"} {
		if err := startService(svc, true); err != nil {
			logger.Errorf("Error restarting service: %v.", err)
		} else {
			// Stop on first matching, to avoid double restarting.
			break
		}
	}

	if enable {
		if err := createOSLoginDirs(); err != nil {
			logger.Errorf("Error creating OS Login directory: %v.", err)
		}

		if err := createOSLoginSudoersFile(); err != nil {
			logger.Errorf("Error creating OS Login sudoers file: %v.", err)
		}

		if err := runCmd(exec.Command("google_oslogin_nss_cache")); err != nil {
			logger.Errorf("Error updating NSS cache: %v.", err)
		}

	}

	return nil
}

func filterGoogleLines(contents string) []string {
	var isgoogle, isgoogleblock bool
	var filtered []string
	for _, line := range strings.Split(contents, "\n") {
		switch {
		case strings.Contains(line, googleComment) && !isgoogleblock:
			isgoogle = true
		case strings.Contains(line, googleBlockEnd):
			isgoogleblock = false
			isgoogle = false
		case isgoogleblock, strings.Contains(line, googleBlockStart):
			isgoogleblock = true
		case isgoogle:
			isgoogle = false
		default:
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func writeConfigFile(path, contents string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	defer closeFile(file)
	file.WriteString(contents)
	return nil
}

func updateSSHConfig(sshConfig string, enable, twofactor bool) string {
	// TODO: this feels like a case for a text/template
	challengeResponseEnable := "ChallengeResponseAuthentication yes"
	authorizedKeysCommand := "AuthorizedKeysCommand /usr/bin/google_authorized_keys"
	if runtime.GOOS == "freebsd" {
		authorizedKeysCommand = "AuthorizedKeysCommand /usr/local/bin/google_authorized_keys"
	}
	authorizedKeysUser := "AuthorizedKeysCommandUser root"
	twoFactorAuthMethods := "AuthenticationMethods publickey,keyboard-interactive"
	if (osRelease.os == "rhel" || osRelease.os == "centos") && osRelease.version.major == 6 {
		authorizedKeysUser = "AuthorizedKeysCommandRunAs root"
		twoFactorAuthMethods = "RequiredAuthentications2 publickey,keyboard-interactive"
	}
	matchblock1 := `Match User sa_*`
	matchblock2 := `       AuthenticationMethods publickey`

	filtered := filterGoogleLines(string(sshConfig))

	if enable {
		osLoginBlock := []string{googleBlockStart, authorizedKeysCommand, authorizedKeysUser}
		if twofactor {
			osLoginBlock = append(osLoginBlock, twoFactorAuthMethods, challengeResponseEnable)
		}
		osLoginBlock = append(osLoginBlock, googleBlockEnd)
		filtered = append(osLoginBlock, filtered...)
		if twofactor {
			filtered = append(filtered, googleBlockStart, matchblock1, matchblock2, googleBlockEnd)
		}
	}

	return strings.Join(filtered, "\n")
}

func writeSSHConfig(enable, twofactor bool) error {
	sshConfig, err := ioutil.ReadFile("/etc/ssh/sshd_config")
	if err != nil {
		return err
	}
	proposed := updateSSHConfig(string(sshConfig), enable, twofactor)
	if proposed == string(sshConfig) {
		return nil
	}
	return writeConfigFile("/etc/ssh/sshd_config", proposed)
}

func updateNSSwitchConfig(nsswitch string, enable bool) string {
	oslogin := " cache_oslogin oslogin"

	var filtered []string
	for _, line := range strings.Split(string(nsswitch), "\n") {
		if strings.HasPrefix(line, "passwd:") || strings.HasPrefix(line, "group:") {
			present := strings.Contains(line, "oslogin")
			if enable && !present {
				line += oslogin
			} else if !enable && present {
				line = strings.TrimSuffix(line, oslogin)
			}

			if runtime.GOOS == "freebsd" {
				line = strings.Replace(line, "compat", "files", 1)
			}
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func writeNSSwitchConfig(enable bool) error {
	nsswitch, err := ioutil.ReadFile("/etc/nsswitch.conf")
	if err != nil {
		return err
	}
	proposed := updateNSSwitchConfig(string(nsswitch), enable)
	if proposed == string(nsswitch) {
		return nil
	}
	return writeConfigFile("/etc/nsswitch.conf", proposed)
}

// Adds entries to the PAM config for sshd and su which reflect the current
// enablements. Only writes files if they have changed from what's on disk.
func updatePAMsshd(pamsshd string, enable, twofactor bool) string {
	authOSLogin := "auth       [success=done perm_denied=die default=ignore] pam_oslogin_login.so"
	authGroup := "auth       [default=ignore] pam_group.so"
	accountOSLogin := "account    [success=ok ignore=ignore default=die] pam_oslogin_login.so"
	accountOSLoginAdmin := "account    [success=ok default=ignore] pam_oslogin_admin.so"
	sessionHomeDir := "session    [success=ok default=ignore] pam_mkhomedir.so"

	if runtime.GOOS == "freebsd" {
		authOSLogin = "auth       optional pam_oslogin_login.so"
		authGroup = "auth       optional pam_group.so"
		accountOSLogin = "account    requisite pam_oslogin_login.so"
		accountOSLoginAdmin = "account    optional pam_oslogin_admin.so"
		sessionHomeDir = "session    optional pam_mkhomedir.so"
	}

	filtered := filterGoogleLines(string(pamsshd))
	if enable {
		topOfFile := []string{googleBlockStart}
		if twofactor {
			topOfFile = append(topOfFile, authOSLogin)
		}
		topOfFile = append(topOfFile, authGroup, googleBlockEnd)
		bottomOfFile := []string{googleBlockStart, accountOSLogin, accountOSLoginAdmin, sessionHomeDir, googleBlockEnd}
		filtered = append(topOfFile, filtered...)
		filtered = append(filtered, bottomOfFile...)
	}
	return strings.Join(filtered, "\n")
}

func writePAMConfig(enable, twofactor bool) error {
	pamsshd, err := ioutil.ReadFile("/etc/pam.d/sshd")
	if err != nil {
		return err
	}
	proposed := updatePAMsshd(string(pamsshd), enable, twofactor)
	if proposed != string(pamsshd) {
		if err := writeConfigFile("/etc/pam.d/sshd", proposed); err != nil {
			return err
		}
	}

	return nil
}

func updateGroupConf(groupconf string, enable bool) string {
	config := "sshd;*;*;Al0000-2400;video\n"

	filtered := filterGoogleLines(groupconf)
	if enable {
		filtered = append(filtered, []string{googleComment, config}...)
	}

	return strings.Join(filtered, "\n")
}

func writeGroupConf(enable bool) error {
	groupconf, err := ioutil.ReadFile("/etc/security/group.conf")
	if err != nil {
		return err
	}
	proposed := updateGroupConf(string(groupconf), enable)
	if proposed != string(groupconf) {
		if err := writeConfigFile("/etc/security/group.conf", proposed); err != nil {
			return err
		}
	}
	return nil
}

// Creates necessary OS Login directories if they don't exist.
func createOSLoginDirs() error {
	restorecon, restoreconerr := exec.LookPath("restorecon")

	for _, dir := range []string{"/var/google-sudoers.d", "/var/google-users.d"} {
		err := os.Mkdir(dir, 0750)
		if err != nil && !os.IsExist(err) {
			return err
		}
		if restoreconerr == nil {
			runCmd(exec.Command(restorecon, dir))
		}
	}
	return nil
}

func createOSLoginSudoersFile() error {
	osloginSudoers := "/etc/sudoers.d/google-oslogin"
	if runtime.GOOS == "freebsd" {
		osloginSudoers = "/usr/local" + osloginSudoers
	}
	sudoFile, err := os.OpenFile(osloginSudoers, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0440)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	fmt.Fprintf(sudoFile, "#includedir /var/google-sudoers.d\n")
	return sudoFile.Close()
}

// restartService tries to restart a systemd service if it is already running.
func restartService(servicename string) error {
	if systemctl, err := exec.LookPath("systemctl"); err == nil {
		if err := runCmd(exec.Command(systemctl, "is-active", servicename+".service")); err == nil {
			return runCmd(exec.Command(systemctl, "restart", servicename+".service"))
		}
	}

	return nil
}

// startService tries to start a systemd service. If the service is already
// running and restart is true, the service is restarted.
func startService(servicename string, restart bool) error {
	if systemctl, err := exec.LookPath("systemctl"); err == nil {
		started := nil == runCmd(exec.Command(systemctl, "is-active", servicename+".service"))
		if !started {
			return runCmd(exec.Command(systemctl, "start", servicename+".service"))
		}
		if started && restart {
			return runCmd(exec.Command(systemctl, "restart", servicename+".service"))
		}
	}

	return nil
}
