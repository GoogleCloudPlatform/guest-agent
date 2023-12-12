// Copyright 2023 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package command

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/user"
	"path"
	"runtime"
	"strconv"
	"syscall"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

// DefaultPipePath is the default unix socket path for linux.
const DefaultPipePath = "/run/google-guest-agent/commands.sock"

func mkdirpWithPerms(dir string, p os.FileMode, uid, gid int) error {
	stat, err := os.Stat(dir)
	if err == nil {
		statT, ok := stat.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("could not determine owner of %s", dir)
		}
		if !stat.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", dir)
		}
		if morePermissive(int(stat.Mode()), int(p)) {
			if err := os.Chmod(dir, p); err != nil {
				return fmt.Errorf("could not correct %s permissions to %d: %v", dir, p, err)
			}
		}
		if statT.Uid != 0 && statT.Uid != uint32(uid) {
			if err := os.Chown(dir, uid, -1); err != nil {
				return fmt.Errorf("could not correct %s owner to %d: %v", dir, uid, err)
			}
		}
		if statT.Gid != 0 && statT.Gid != uint32(gid) {
			if err := os.Chown(dir, -1, gid); err != nil {
				return fmt.Errorf("could not correct %s group to %d: %v", dir, gid, err)
			}
		}
	} else {
		parent, _ := path.Split(dir)
		if parent != "/" && parent != "" {
			if err := mkdirpWithPerms(parent, p, uid, gid); err != nil {
				return err
			}
		}
		if err := os.Mkdir(dir, p); err != nil {
			return err
		}
	}
	return nil
}

func morePermissive(i, j int) bool {
	for k := 0; k < 3; k++ {
		if (i % 010) > (j % 10) {
			return true
		}
		i = i / 010
		j = j / 010
	}
	return false
}

func listen(ctx context.Context, pipe string, filemode int, grp string) (net.Listener, error) {
	// If grp is an int, use it as a GID
	gid, err := strconv.Atoi(grp)
	if err != nil {
		// Otherwise lookup GID
		group, err := user.LookupGroup(grp)
		if err != nil {
			logger.Errorf("guest agent command pipe group %s is not a GID nor a valid group, not changing socket ownership", grp)
			gid = -1
		} else {
			gid, err = strconv.Atoi(group.Gid)
			if err != nil {
				logger.Errorf("os reported group %s has gid %s which is not a valid int, not changing socket ownership. this should never happen", grp, group.Gid)
				gid = -1
			}
		}
	}
	// socket owner group does not need to have permissions to everything in the directory containing it, whatever user and group we are should own that
	user, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("could not lookup current user")
	}
	currentuid, err := strconv.Atoi(user.Uid)
	if err != nil {
		return nil, fmt.Errorf("os reported user %s has uid %s which is not a valid int, can't determine directory owner. this should never happen", user.Username, user.Uid)
	}
	currentgid, err := strconv.Atoi(user.Gid)
	if err != nil {
		return nil, fmt.Errorf("os reported user %s has gid %s which is not a valid int, can't determine directory owner. this should never happen", user.Username, user.Gid)
	}
	if err := mkdirpWithPerms(path.Dir(pipe), os.FileMode(filemode), currentuid, currentgid); err != nil {
		return nil, err
	}
	// Mutating the umask of the process for this is not ideal, but tightening permissions with chown after creation is not really secure.
	// Lock OS thread while mutating umask so we don't lose a thread with a mutated mask.
	runtime.LockOSThread()
	oldmask := syscall.Umask(777 - filemode)
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "unix", pipe)
	syscall.Umask(oldmask)
	runtime.UnlockOSThread()
	if err != nil {
		return nil, err
	}
	// But we need to chown anyway to loosen permissions to include whatever group the user has configured
	err = os.Chown(pipe, int(currentuid), gid)
	if err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

func dialPipe(ctx context.Context, pipe string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "unix", pipe)
}
