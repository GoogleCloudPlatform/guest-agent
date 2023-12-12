//  Copyright 2023 Google Inc. All Rights Reserved.
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

package command

import (
	"context"
	"fmt"
	"net"
	"os/user"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/Microsoft/go-winio"
)

const (
	// DefaultPipePath is the default named pipe path for windows.
	DefaultPipePath = `\\.\pipe\google-guest-agent-commands`
	nullSID         = "S-1-0-0"
	worldSID        = "S-1-1-0"
	creatorOwnerSID = "S-1-3-0"
	creatorGroupSID = "S-1-3-1"
)

func genSecurityDescriptor(filemode int, grp string) string {
	// This function translates the intention of a unix file mode and owner group into an appropriate SDDL security descriptor for a windows named pipe.
	owner := creatorOwnerSID
	group := creatorGroupSID

	wPerm := filemode % 010
	filemode /= 010
	gPerm := filemode % 010
	filemode /= 010
	uPerm := filemode % 010

	// Having only read or only write access to a bidirectional pipe is pointless so we treat access for user/group as yes or no based on whether the permission grants RW access
	if uPerm < 06 {
		owner = nullSID
	}
	if gPerm < 06 {
		group = nullSID
	}
	// If permissions grant world RW, make world the owner
	if wPerm > 05 {
		owner = worldSID
		group = worldSID
	}

	// Group is handled as supplemental DACL, but ignore it if user specified no group rw permission
	var dacl string
	if gPerm > 05 {
		g, err := user.LookupGroup(grp)
		if err != nil {
			logger.Errorf("Could not lookup group %s SID, this group will not be included in the command server security descriptor: %v", grp, err)
		} else {
			// Allow access;Protected DACL;Allow all general access;Empty object guid;Empty inherit object guid;group sid from lookup
			dacl = fmt.Sprintf("D:(A;P;GA;;;%s)", g.Gid)
		}
	}

	sddl := "O:%sG:%s%s"
	return fmt.Sprintf(sddl, owner, group, dacl)
}

func listen(ctx context.Context, path string, filemode int, group string) (net.Listener, error) {
	// Winio library does not provide any method to listen on context. Failing to
	// specify a pipeconfig (or using the zero value) results in flaky ACCESS_DENIED
	// errors when re-opening the same pipe (~1/10).
	// https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-createnamedpipea#remarks
	// Even with a pipeconfig, this flakes ~1/200 runs, hence the retry until the
	// context is expired or listen is successful.
	var l net.Listener
	var lastError error
	for {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("context expired: %v before successful listen (last error: %v)", ctx.Err(), lastError)
		}
		config := &winio.PipeConfig{
			MessageMode:        false,
			InputBufferSize:    1024,
			OutputBufferSize:   1024,
			SecurityDescriptor: genSecurityDescriptor(filemode, group),
		}
		l, lastError = winio.ListenPipe(path, config)
		if lastError == nil {
			return l, lastError
		}
	}
}

func dialPipe(ctx context.Context, pipe string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipe)
}
