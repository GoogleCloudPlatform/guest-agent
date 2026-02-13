# Copyright 2023 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
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


# Script expects guest-agent and google-guest-agent codebase are placed
# side-by-side within same directory and this script is executed from root of 
# guest-agent codebase. 
GUEST_AGENT_REPO="../google-guest-agent"

if [[ ! -f "$GUEST_AGENT_REPO/Makefile" ]]; then
    # This is a placeholder file for guest-agent package, google-compute-engine-windows.goospec
    # looks for this file during goopack packaging and will fail if not found.
    echo "This is a placeholder file so guest agent package build without error. Package will have actual Guest Agent Manager executable instead if both repos are cloned side-by-side." > GCEWindowsAgentManager.exe
    echo "This is a placeholder file so guest agent package build without error. Package will have actual Guest Agent Manager executable instead if both repos are cloned side-by-side." > ggactl_plugin.exe
    echo "This is a placeholder file so guest agent package build without error. Package will have actual Guest Agent Manager executable instead if both repos are cloned side-by-side." > GCEWindowsCompatManager.exe
    echo "This is a placeholder file so guest agent package build without error. Package will have actual Guest Agent Manager executable instead if both repos are cloned side-by-side." > CorePlugin.exe
    echo "This is a placeholder file so guest agent package build without error. Package will have actual Guest Agent Manager executable instead if both repos are cloned side-by-side." > GCEMetadataScriptRunner.exe
    echo "This is a placeholder file so guest agent package build without error. Package will have actual Guest Agent Manager executable instead if both repos are cloned side-by-side." > GCECompatMetadataScripts.exe
    echo "This is a placeholder file so guest agent package build without error. Package will have actual Guest Agent Manager executable instead if both repos are cloned side-by-side." > GCEAuthorizedKeys.exe
    echo "This is a placeholder file so guest agent package build without error. Package will have actual Guest Agent Manager executable instead if both repos are cloned side-by-side." > GCEAuthorizedKeysNew.exe
    echo "This is a placeholder file so guest agent package build without error. Package will have actual Guest Agent Manager executable instead if both repos are cloned side-by-side." > core_plugin.manifest.binpb
    exit 0
fi

BUILD_DIR=$(pwd)
pushd $GUEST_AGENT_REPO
GOOS=windows VERSION=$version make cmd/google_guest_agent/google_guest_agent
GOOS=windows VERSION=$version make cmd/ggactl/ggactl_plugin
GOOS=windows VERSION=$version make cmd/google_guest_compat_manager/google_guest_compat_manager
GOOS=windows VERSION=$version make cmd/core_plugin/core_plugin
GOOS=windows VERSION=$version make cmd/gce_metadata_script_runner/gce_metadata_script_runner
GOOS=windows VERSION=$version make cmd/metadata_script_runner_compat/gce_compat_metadata_script_runner
GOOS=windows VERSION=$version make cmd/google_authorized_keys_compat/google_authorized_keys_compat
GOOS=windows VERSION=$version make cmd/google_authorized_keys/google_authorized_keys

cp cmd/google_guest_agent/google_guest_agent $BUILD_DIR/GCEWindowsAgentManager.exe
cp cmd/ggactl/ggactl_plugin $BUILD_DIR/ggactl_plugin.exe
cp cmd/google_guest_compat_manager/google_guest_compat_manager $BUILD_DIR/GCEWindowsCompatManager.exe
cp cmd/core_plugin/core_plugin $BUILD_DIR/CorePlugin.exe
cp cmd/gce_metadata_script_runner/gce_metadata_script_runner $BUILD_DIR/GCEMetadataScriptRunner.exe
cp cmd/metadata_script_runner_compat/gce_compat_metadata_script_runner $BUILD_DIR/GCECompatMetadataScripts.exe
cp build/configs/usr/lib/google/guest_agent/GuestAgentCorePlugin/manifest.windows.binpb $BUILD_DIR/core_plugin.manifest.binpb

if [[ -f cmd/google_authorized_keys_compat/google_authorized_keys_compat ]]; then
  cp $BUILD_DIR/GCEAuthorizedKeysCommand.exe $BUILD_DIR/GCEAuthorizedKeys.exe
  cp cmd/google_authorized_keys_compat/google_authorized_keys_compat $BUILD_DIR/GCEAuthorizedKeysCommand.exe
  cp cmd/google_authorized_keys/google_authorized_keys $BUILD_DIR/GCEAuthorizedKeysNew.exe
fi
popd
