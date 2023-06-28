#!/usr/bin/env bash

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <version>" >&2
    exit 1
fi

GOOS="linux"
GOARCH="amd64"
VERSION=${1}

CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build -ldflags="-s -w -X main.version=${VERSION}" -o ./out/google_guest_agent ./google_guest_agent/
tar -czf ./out/google_guest_agent-${VERSION}.${GOOS}-${GOARCH}.tar.gz -C ./out ./google_guest_agent
