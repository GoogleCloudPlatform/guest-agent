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

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/network"
	"github.com/Microsoft/go-winio"
)

const (
	platformHostsFile = `C:\Windows\System32\Drivers\etc\hosts`
	newline           = "\r\n"
)

func setHostname(ctx context.Context, hostname string) error {
	return fmt.Errorf("setting hostnames in guest-agent is not supported on windows")
}

// Windows does not promise atomic moves so there is no point doing anything
// but writing directly to the file.
func overwrite(dst string, contents []byte) error {
	stat, err := os.Stat(dst)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, contents, stat.Mode())
}

func triggerHostnameReconfigure(ctx context.Context) error {
	conn, err := winio.DialPipeContext(ctx, pipePath)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(network.HostnameReconfigureEvent))
	return err
}
