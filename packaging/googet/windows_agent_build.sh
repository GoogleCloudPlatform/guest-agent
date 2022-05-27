#!/bin/bash

# Should be run from the guest-agent directory.
# Usage:
#   ./packaging/googet/windows_agent_build.sh <version>

version=$1

GOOS=windows /tmp/go/bin/go build -ldflags "-X main.version=$version" -mod=readonly -o GCEWindowsAgent.exe ./google_guest_agent
GOOS=windows /tmp/go/bin/go build -ldflags "-X main.version=$version" -mod=readonly -o GCEAuthorizedKeysCommand.exe ./google_authorized_keys
