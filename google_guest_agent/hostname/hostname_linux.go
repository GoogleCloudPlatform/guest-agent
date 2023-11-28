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
	"net"
	"os"
	"regexp"
	"syscall"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/network"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
)

const (
	platformHostsFile = "/etc/hosts"
	newline           = "\n"
)

func setHostname(ctx context.Context, hostname string) error {
	if hostname == "metadata.google.internal" {
		return fmt.Errorf("invalid hostname %s", hostname)
	}
	if err := syscall.Sethostname([]byte(hostname)); err != nil {
		return err
	}
	if run.Quiet(ctx, "which", "nmcli") != nil {
		if err := run.Quiet(ctx, "nmcli", "general", "hostname", hostname); err != nil {
			return err
		}
	}
	if run.Quiet(ctx, "which", "systemctl") != nil {
		r := run.WithOutput(ctx, "systemctl")
		if r.ExitCode != 0 {
			return fmt.Errorf("error checking for rsyslog: %s", r.StdErr)
		}
		if regexp.MustCompile("rsyslog.service.*running").MatchString(r.StdOut) {
			if err := run.Quiet(ctx, "systemctl", "--no-block", "restart", "rsyslog"); err != nil {
				return err
			}
		}
	} else {
		if err := run.Quiet(ctx, "pkill", "-HUP", "syslogd"); err != nil {
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

func triggerHostnameReconfigure(ctx context.Context) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", pipePath)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(network.HostnameReconfigureEvent))
	return err
}
