# Copyright 2023 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#!/bin/bash

# Should be run from the guest-agent directory.
# Usage:
#   ./packaging/googet/windows_agent_build.sh <version>

version=$1

GOOS=windows /tmp/go/bin/go build -ldflags "-X main.version=$version" -mod=readonly -o GCEWindowsAgent.exe ./google_guest_agent
GOOS=windows /tmp/go/bin/go build -ldflags "-X main.version=$version" -mod=readonly -o GCEAuthorizedKeysCommand.exe ./google_authorized_keys
