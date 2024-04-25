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

package hostname

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"syscall"
)

const (
	platformHostsFile = "/etc/hosts"
	newline           = "\n"
)

func initPlatform(context.Context) {}

var setHostname = func(hostname string) error {
	if err := syscall.Sethostname([]byte(hostname)); err != nil {
		return err
	}
	if _, err := exec.LookPath("nmcli"); err == nil {
		if err := exec.Command("nmcli", "general", "hostname", hostname).Run(); err != nil {
			return err
		}
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		o, err := exec.Command("systemctl").Output()
		if err != nil {
			return fmt.Errorf("error checking for rsyslog: %s", err)
		}
		if regexp.MustCompile(`rsyslog.service[^\n]*running`).Match(o) {
			if err := exec.Command("systemctl", "--no-block", "restart", "rsyslog").Run(); err != nil {
				return err
			}
		}
	} else {
		if err := exec.Command("pkill", "-HUP", "syslogd").Run(); err != nil {
			return err
		}
	}
	return nil
}

// Make the write as atomic as possible by creating a temp file, restoring
// permissions & ownership, writing data, syncing, and then overwriting.
func overwrite(dst string, contents []byte) error {
	tmp, err := os.CreateTemp(os.TempDir(), "gcehosts")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	stat, err := os.Stat(dst)
	if err != nil {
		return err
	}
	if statT, ok := stat.Sys().(*syscall.Stat_t); !ok {
		return fmt.Errorf("could not determine owner of %s", dst)
	} else if err := os.Chown(tmp.Name(), int(statT.Uid), int(statT.Gid)); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), stat.Mode()); err != nil {
		return err
	}
	n, err := tmp.Write(contents)
	if err != nil {
		return err
	}
	if n != len(contents) {
		return fmt.Errorf("Could not write entire hosts file, tried to write %d bytes but wrote %d", len(contents), n)
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
