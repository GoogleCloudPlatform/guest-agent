//go:build windows

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
	"os/user"
	"testing"
)

func TestGenSecurityDescriptor(t *testing.T) {
	guest, err := user.LookupGroup("Guests")
	if err != nil {
		t.Fatal(err)
	}
	testcases := []struct {
		name     string
		filemode int
		group    string
		output   string
	}{
		{
			name:     "world writeable",
			filemode: 0777,
			group:    nullSID,
			output:   "O:" + worldSID + "G:" + worldSID,
		},
		{
			name:     "user+group writable",
			filemode: 0770,
			group:    "",
			output:   "O:" + creatorOwnerSID + "G:" + creatorGroupSID,
		},
		{
			name:     "user writable",
			filemode: 0700,
			group:    nullSID,
			output:   "O:" + creatorOwnerSID + "G:" + nullSID,
		},
		{
			name:     "no write permissions",
			filemode: 000,
			group:    nullSID,
			output:   "O:" + nullSID + "G:" + nullSID,
		},
		{
			name:     "custom named group",
			filemode: 0770,
			group:    "Guests",
			output:   "O:" + creatorOwnerSID + "G:" + creatorGroupSID + "D:(A;P;GA;;;" + guest.Gid + ")",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			sd := genSecurityDescriptor(tc.filemode, tc.group)
			if sd != tc.output {
				t.Errorf("unexpected output from genSecurityDescriptor(%d, %s), got %s want %s", tc.filemode, tc.group, sd, tc.output)
			}
		})
	}
}
