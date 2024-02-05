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
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/sshtrustedca"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/sshca"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

var (
	googleComment    = "# Added by Google Compute Engine OS Login."
	googleBlockStart = "#### Google OS Login control. Do not edit this section. ####"
	googleBlockEnd   = "#### End Google OS Login control section. ####"
	trustedCAWatcher events.Watcher

	// deprecatedConfigDirectives contains a list of configuration directives (or lines)
	// that we no longer support and should not be considered for updated versions of a
	// given configuration file.
	deprecatedConfigDirectives = map[string][]string{
		"/etc/pam.d/su": []string{"account    [success=bad ignore=ignore] pam_oslogin_login.so"},
	}
)

type osloginMgr struct{}

// We also read project keys first, letting instance-level keys take
// precedence.
func getOSLoginEnabled(md *metadata.Descriptor) (bool, bool, bool) {
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
	var skey bool
	if md.Project.Attributes.SecurityKey != nil {
		skey = *md.Project.Attributes.SecurityKey
	}
	if md.Instance.Attributes.SecurityKey != nil {
		skey = *md.Instance.Attributes.SecurityKey
	}
	return enable, twofactor, skey
}

func enableDisableOSLoginCertAuth(ctx context.Context) error {
	if newMetadata == nil {
		logger.Infof("Could not enable/disable OSLogin Cert Auth, metadata is not initialized.")
		return nil
	}

	eventManager := events.Get()
	osLoginEnabled, _, _ := getOSLoginEnabled(newMetadata)
	if osLoginEnabled {
		if trustedCAWatcher == nil {
			trustedCAWatcher = sshtrustedca.New(sshtrustedca.DefaultPipePath)
			if err := eventManager.AddWatcher(ctx, trustedCAWatcher); err != nil {
				return err
			}
			sshca.Init()
		}
	} else {
		if trustedCAWatcher != nil {
			if err := eventManager.RemoveWatcher(ctx, trustedCAWatcher); err != nil {
				return err
			}
			sshca.Close()
			trustedCAWatcher = nil
		}
	}

	return nil
}

func (o *osloginMgr) Diff(ctx context.Context) (bool, error) {
	oldEnable, oldTwoFactor, oldSkey := getOSLoginEnabled(oldMetadata)
	enable, twofactor, skey := getOSLoginEnabled(newMetadata)
	return oldMetadata.Project.ProjectID == "" ||
		// True on first run or if any value has changed.
		(oldTwoFactor != twofactor) ||
		(oldEnable != enable) ||
		(oldSkey != skey), nil
}

func (o *osloginMgr) Timeout(ctx context.Context) (bool, error) {
	return false, nil
}

func (o *osloginMgr) Disabled(ctx context.Context) (bool, error) {
	return runtime.GOOS == "windows", nil
}

func (o *osloginMgr) Set(ctx context.Context) error {
	// We need to know if it was previously enabled for the clearing of
	// metadata-based SSH keys.
	oldEnable, _, _ := getOSLoginEnabled(oldMetadata)
	enable, twofactor, skey := getOSLoginEnabled(newMetadata)

	cleanupDeprecatedDirectives()

	if enable && !oldEnable {
		logger.Infof("Enabling OS Login")
		newMetadata.Instance.Attributes.SSHKeys = nil
		newMetadata.Project.Attributes.SSHKeys = nil
		(&accountsMgr{}).Set(ctx)
	}

	if !enable && oldEnable {
		logger.Infof("Disabling OS Login")
	}

	if err := writeSSHConfig(enable, twofactor, skey); err != nil {
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
		// These services should be restarted if running
		logger.Debugf("systemctl try-restart %s, if it exists", svc)
		if err := systemctlTryRestart(ctx, svc); err != nil {
			logger.Errorf("Error restarting service: %v.", err)
		}
	}

	// SSH should be started if not running, reloaded otherwise.
	for _, svc := range []string{"ssh", "sshd"} {
		logger.Debugf("systemctl reload-or-restart %s, if it exists", svc)
		if err := systemctlReloadOrRestart(ctx, svc); err != nil {
			logger.Errorf("Error reloading service: %v.", err)
		}
	}

	now := fmt.Sprintf("%d", time.Now().Unix())
	mdsClient.WriteGuestAttributes(ctx, "guest-agent/sshable", now)

	if enable {
		logger.Debugf("Create OS Login dirs, if needed")
		if err := createOSLoginDirs(ctx); err != nil {
			logger.Errorf("Error creating OS Login directory: %v.", err)
		}

		logger.Debugf("create OS Login sudoers config, if needed")
		if err := createOSLoginSudoersFile(); err != nil {
			logger.Errorf("Error creating OS Login sudoers file: %v.", err)
		}

		logger.Debugf("starting OS Login nss cache fill")
		if err := run.Quiet(ctx, "google_oslogin_nss_cache"); err != nil {
			logger.Errorf("Error updating NSS cache: %v.", err)
		}

	}

	return nil
}

func cleanupDeprecatedLines(fpath string, directives []string) error {
	// If the file doesn't exist don't even try updating it.
	stat, err := os.Stat(fpath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat config file: %+v", err)
	}

	data, err := os.ReadFile(fpath)
	if err != nil {
		return fmt.Errorf("failed to read file: %+v", err)
	}

	var updatedLines []string
	var totalLines int

	for _, line := range strings.Split(string(data), "\n") {
		if !slices.Contains(directives, line) {
			updatedLines = append(updatedLines, line)
		}
		totalLines++
	}

	// Don't attempt to update the config file if no lines werer removed/avoided.
	if totalLines == len(updatedLines) {
		return nil
	}

	err = os.WriteFile(fpath, []byte(strings.Join(updatedLines, "\n")), stat.Mode())
	if err != nil {
		return fmt.Errorf("failed to update deprecated configuration directives: %+v", err)
	}

	return nil
}

// cleanupDeprecatedDirectives checks if a given configuration line is an old
// configuration that was deprecated and we should not consider it for the updated
// version.
func cleanupDeprecatedDirectives() {
	for k, v := range deprecatedConfigDirectives {
		if err := cleanupDeprecatedLines(k, v); err != nil {
			logger.Errorf("failed to clean up deprecated directives: %+v", err)
		}
	}
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
	logger.Debugf("writing %s", path)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	defer closeFile(file)
	file.WriteString(contents)
	return nil
}

func updateSSHConfig(sshConfig string, enable, twofactor, skey bool) string {
	// TODO: this feels like a case for a text/template
	challengeResponseEnable := "ChallengeResponseAuthentication yes"
	authorizedKeysCommand := "AuthorizedKeysCommand /usr/bin/google_authorized_keys"
	if skey {
		authorizedKeysCommand = "AuthorizedKeysCommand /usr/bin/google_authorized_keys_sk"
	}
	if runtime.GOOS == "freebsd" {
		authorizedKeysCommand = "AuthorizedKeysCommand /usr/local/bin/google_authorized_keys"
		if skey {
			authorizedKeysCommand = "AuthorizedKeysCommand /usr/local/bin/google_authorized_keys_sk"
		}
	}
	authorizedKeysUser := "AuthorizedKeysCommandUser root"

	// Certificate based authentication.
	authorizedPrincipalsCommand := "AuthorizedPrincipalsCommand /usr/bin/google_authorized_principals %u %k"
	authorizedPrincipalsUser := "AuthorizedPrincipalsCommandUser root"
	trustedUserCAKeys := "TrustedUserCAKeys " + sshtrustedca.DefaultPipePath

	twoFactorAuthMethods := "AuthenticationMethods publickey,keyboard-interactive"
	if (osInfo.OS == "rhel" || osInfo.OS == "centos") && osInfo.Version.Major == 6 {
		authorizedKeysUser = "AuthorizedKeysCommandRunAs root"
		twoFactorAuthMethods = "RequiredAuthentications2 publickey,keyboard-interactive"
	}
	matchblock1 := `Match User sa_*`
	matchblock2 := `       AuthenticationMethods publickey`

	filtered := filterGoogleLines(string(sshConfig))

	if enable {
		osLoginBlock := []string{googleBlockStart}

		if cfg.Get().OSLogin.CertAuthentication {
			osLoginBlock = append(osLoginBlock, trustedUserCAKeys, authorizedPrincipalsCommand, authorizedPrincipalsUser)
		}

		osLoginBlock = append(osLoginBlock, authorizedKeysCommand, authorizedKeysUser)

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

func writeSSHConfig(enable, twofactor, skey bool) error {
	sshConfig, err := os.ReadFile("/etc/ssh/sshd_config")
	if err != nil {
		return err
	}
	proposed := updateSSHConfig(string(sshConfig), enable, twofactor, skey)
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
	nsswitch, err := os.ReadFile("/etc/nsswitch.conf")
	if err != nil {
		return err
	}
	proposed := updateNSSwitchConfig(string(nsswitch), enable)
	if proposed == string(nsswitch) {
		return nil
	}
	return writeConfigFile("/etc/nsswitch.conf", proposed)
}

func updatePAMsshdPamless(pamsshd string, enable, twofactor bool) string {
	authOSLogin := "auth       [success=done perm_denied=die default=ignore] pam_oslogin_login.so"
	authGroup := "auth       [default=ignore] pam_group.so"
	sessionHomeDir := "session    [success=ok default=ignore] pam_mkhomedir.so"

	if runtime.GOOS == "freebsd" {
		authOSLogin = "auth       optional pam_oslogin_login.so"
		authGroup = "auth       optional pam_group.so"
		sessionHomeDir = "session    optional pam_mkhomedir.so"
	}

	filtered := filterGoogleLines(string(pamsshd))
	if enable {
		topOfFile := []string{googleBlockStart}
		if twofactor {
			topOfFile = append(topOfFile, authOSLogin)
		}
		topOfFile = append(topOfFile, authGroup, googleBlockEnd)
		bottomOfFile := []string{googleBlockStart, sessionHomeDir, googleBlockEnd}
		filtered = append(topOfFile, filtered...)
		filtered = append(filtered, bottomOfFile...)
	}
	return strings.Join(filtered, "\n")
}

func writePAMConfig(enable, twofactor bool) error {
	pamsshd, err := os.ReadFile("/etc/pam.d/sshd")
	if err != nil {
		return err
	}

	proposed := updatePAMsshdPamless(string(pamsshd), enable, twofactor)
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
	groupconf, err := os.ReadFile("/etc/security/group.conf")
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
func createOSLoginDirs(ctx context.Context) error {
	restorecon, restoreconerr := exec.LookPath("restorecon")

	for _, dir := range []string{"/var/google-sudoers.d", "/var/google-users.d"} {
		err := os.Mkdir(dir, 0750)
		if err != nil && !os.IsExist(err) {
			return err
		}
		if restoreconerr == nil {
			run.Quiet(ctx, restorecon, dir)
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

// systemctlTryRestart tries to restart a systemd service if it is already
// running. Stopped services will be ignored.
func systemctlTryRestart(ctx context.Context, servicename string) error {
	if !systemctlUnitExists(ctx, servicename) {
		return nil
	}
	return run.Quiet(ctx, "systemctl", "try-restart", servicename+".service")
}

// systemctlReloadOrRestart tries to reload a running systemd service if
// supported, restart otherwise. Stopped services will be started.
func systemctlReloadOrRestart(ctx context.Context, servicename string) error {
	if !systemctlUnitExists(ctx, servicename) {
		return nil
	}
	return run.Quiet(ctx, "systemctl", "reload-or-restart", servicename+".service")
}

// systemctlStart tries to start a stopped systemd service. Started services
// will be ignored.
func systemctlStart(ctx context.Context, servicename string) error {
	if !systemctlUnitExists(ctx, servicename) {
		return nil
	}
	return run.Quiet(ctx, "systemctl", "start", servicename+".service")
}

func systemctlUnitExists(ctx context.Context, servicename string) bool {
	res := run.WithOutput(ctx, "systemctl", "list-units", "--all", servicename+".service")
	return !strings.Contains(res.StdOut, "0 loaded units listed")
}
