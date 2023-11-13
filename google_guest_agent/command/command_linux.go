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
				logger.Errorf("os reported group %s has gid %s which is not a valid int, not changing socket ownership. this should never happen.", grp, group.Gid)
				gid = -1
			}
		}
	}
	if err := os.MkdirAll(path.Dir(pipe), os.FileMode(filemode)); err != nil {
		return nil, err
	}
	// Mutating the umask of the process for this is not ideal, but tightening permissions with chown after creation is not really secure.
	// Lock OS thread while mutatin umask so we don't lose a thread with a mutated mask.
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
	err = os.Chown(pipe, -1, gid)
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
