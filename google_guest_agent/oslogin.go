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

func (o *osloginMgr) diff() bool {
	// True on first run.
	return oldMetadata.Project.ProjectID == "" ||
		// True if any value has changed.
		(oldMetadata.Instance.Attributes.EnableOSLogin != newMetadata.Instance.Attributes.EnableOSLogin) ||
		(oldMetadata.Instance.Attributes.TwoFactor != newMetadata.Instance.Attributes.TwoFactor) ||
		(oldMetadata.Project.Attributes.EnableOSLogin != newMetadata.Project.Attributes.EnableOSLogin) ||
		(oldMetadata.Project.Attributes.TwoFactor != newMetadata.Project.Attributes.TwoFactor)
}

func (o *osloginMgr) timeout() bool {
	return false
}

func (o *osloginMgr) disabled(os string) bool {
	return os == "windows"
}

func (o *osloginMgr) set() error {
	oldEnable := oldMetadata.Instance.Attributes.EnableOSLogin || oldMetadata.Project.Attributes.EnableOSLogin
	enable := newMetadata.Instance.Attributes.EnableOSLogin || newMetadata.Project.Attributes.EnableOSLogin
	twofactor := newMetadata.Instance.Attributes.TwoFactor || newMetadata.Project.Attributes.TwoFactor

	if enable && !oldEnable {
		logger.Infof("Enabling OS Login")
		newMetadata.Instance.Attributes.SSHKeys = nil
		newMetadata.Project.Attributes.SSHKeys = nil
		(&accountsMgr{}).set()
	}

	if err := updateSSHConfig(enable, twofactor); err != nil {
		logger.Errorf("Error updating SSH config: %v.", err)
	}

	if err := updateNSSwitchConfig(enable); err != nil {
		logger.Errorf("Error updating NSS config: %v.", err)
	}

	if err := updatePAMConfig(enable, twofactor); err != nil {
		logger.Errorf("Error updating PAM config: %v.", err)
	}

	if err := createOSLoginDirs(); err != nil {
		logger.Errorf("Error creating OS Login directory: %v.", err)
	}

	if err := createOSLoginSudoersFile(); err != nil {
		logger.Errorf("Error creating OS Login sudoers file: %v.", err)
	}

	// Services which need to be restarted primarily due to caching issues.
	for _, svc := range []string{"ssh", "sshd", "nscd", "unscd", "systemd-logind", "cron", "crond"} {
		if err := restartService(svc); err != nil {
			logger.Errorf("Error restarting service: %v.", err)
		}
	}

	if enable {
		if err := exec.Command("google_oslogin_nss_cache").Run(); err != nil {
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
		case strings.Contains(line, googleComment):
			isgoogle = true
		case isgoogle:
			isgoogle = false
		case isgoogleblock, strings.Contains(line, googleBlockStart):
			isgoogleblock = true
		case strings.Contains(line, googleBlockEnd):
			isgoogleblock = false
		default:
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func updateSSHConfig(enable, twofactor bool) error {
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

	sshConfig, err := ioutil.ReadFile("/etc/ssh/sshd_config")
	if err != nil {
		return err
	}

	filtered := filterGoogleLines(string(sshConfig))

	if enable {
		osLoginBlock := []string{googleBlockStart, authorizedKeysCommand, authorizedKeysUser}
		twofactorblock := []string{twoFactorAuthMethods, challengeResponseEnable}
		if twofactor {
			osLoginBlock = append(osLoginBlock, twofactorblock...)
		}
		osLoginBlock = append(osLoginBlock, googleBlockEnd)
		filtered = append(osLoginBlock, filtered...)
	}
	proposed := strings.Join(filtered, "\n")
	if proposed != string(sshConfig) {
		file, err := os.OpenFile("/etc/ssh/sshd_config", os.O_WRONLY|os.O_TRUNC, 0777)
		if err != nil {
			return err
		}
		defer closeFile(file)
		file.WriteString(proposed)
	}

	return nil
}

func updateNSSwitchConfig(enable bool) error {
	oslogin := " cache_oslogin oslogin"

	nsswitch, err := ioutil.ReadFile("/etc/nsswitch.conf")
	if err != nil {
		return err
	}

	var filtered []string
	for _, line := range strings.Split(string(nsswitch), "\n") {
		if strings.HasPrefix(line, "passwd:") {
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
	proposed := strings.Join(filtered, "\n")
	if proposed != string(nsswitch) {
		file, err := os.OpenFile("/etc/nsswitch.conf", os.O_WRONLY|os.O_TRUNC, 0777)
		if err != nil {
			return err
		}
		defer closeFile(file)
		file.WriteString(proposed)
	}
	return nil
}

func updatePAMConfig(enable, twofactor bool) error {
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

	pamsshd, err := ioutil.ReadFile("/etc/pam.d/sshd")
	if err != nil {
		return err
	}
	filtered := filterGoogleLines(string(pamsshd))
	if enable {
		topOfFile := []string{googleBlockStart}
		if twofactor {
			topOfFile = append(topOfFile, authOSLogin)
		}
		topOfFile = append(topOfFile, []string{authGroup, googleBlockEnd}...)
		bottomOfFile := []string{googleBlockStart, accountOSLogin, accountOSLoginAdmin, sessionHomeDir, googleBlockEnd}
		filtered = append(topOfFile, filtered...)
		filtered = append(filtered, bottomOfFile...)
	}
	proposed := strings.Join(filtered, "\n")
	if proposed != string(pamsshd) {
		file, err := os.OpenFile("/etc/pam.d/sshd", os.O_WRONLY|os.O_TRUNC, 0777)
		if err != nil {
			return err
		}
		defer closeFile(file)
		file.WriteString(proposed)
	}

	accountSu := "account    [success=bad ignore=ignore] pam_oslogin_login.so"

	pamsu, err := ioutil.ReadFile("/etc/pam.d/su")
	if err != nil {
		return err
	}
	filtered = filterGoogleLines(string(pamsu))
	if enable {
		filtered = append([]string{googleComment, accountSu}, filtered...)
	}
	proposed = strings.Join(filtered, "\n")
	if proposed != string(pamsu) {
		file2, err := os.OpenFile("/etc/pam.d/su", os.O_WRONLY|os.O_TRUNC, 0777)
		if err != nil {
			return err
		}
		defer closeFile(file2)
		file2.WriteString(proposed)
	}

	return nil
}

func createOSLoginDirs() error {
	restorecon, err := exec.LookPath("restorecon")
	if err != nil {
		restorecon = ""
	}

	for _, dir := range []string{"/var/google-sudoers.d", "/var/google-users.d"} {
		err := os.Mkdir(dir, 0750)
		if err != nil && !os.IsExist(err) {
			return err
		}
		if restorecon != "" {
			exec.Command(restorecon, dir).Run()
		}
	}
	return nil
}

func createOSLoginSudoersFile() error {
	osloginSudoers := "/etc/sudoers.d/google-oslogin"
	if runtime.GOOS == "freebsd" {
		osloginSudoers = "/usr/local/etc/sudoers.d/google-oslogin"
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

// restartService tries to restart a service on linux-like systems. It attempts
// to find and use the following mechanisms in order:
// 1. The `systemctl` utility, if in a systemd environment.
// 2. The `service` command, if present.
// 3. A SysVinit script directly, if present.
// Missing mechanisms and missing or disabled services are ignored.
func restartService(servicename string) error {
	init, err := os.Readlink("/sbin/init")
	if err == nil && strings.Contains(init, "systemd") {
		if systemctl, err := exec.LookPath("systemctl"); err == nil {
			if exec.Command(systemctl, "is-active", servicename+".service").Run() == nil {
				return exec.Command(systemctl, "restart", servicename+".service").Run()
			}
			return nil
		}
	}
	service, err := exec.LookPath("service")
	if err == nil {
		if exec.Command(service, servicename, "status").Run() == nil {
			return exec.Command(service, servicename, "restart").Run()
		}
		return nil
	}
	initService := "/etc/init.d/" + servicename
	if _, err := os.Stat(initService); err == nil {
		if exec.Command(initService, "status").Run() == nil {
			return exec.Command(initService, "restart").Run()
		}
		return nil
	}

	return nil
}
