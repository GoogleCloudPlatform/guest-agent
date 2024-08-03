// Copyright 2020 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	sspb "github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/snapshot_service/cloud_vmm"
	"github.com/GoogleCloudPlatform/guest-agent/retry"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/golang/groupcache/lru"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	seenPreSnapshotOperationIds  = lru.New(128)
	seenPostSnapshotOperationIds = lru.New(128)
	// policy is the retry policy used for sending snapshot response.
	policy = retry.Policy{MaxAttempts: 10, BackoffFactor: 1, Jitter: time.Second}
)

const (
	// scriptsDir is the directory with snapshot pre/post scripts to be executed on request.
	scriptsDir = "/etc/google/snapshots/"
)

func runScript(ctx context.Context, path, disks string, timeout time.Duration) (int, sspb.AgentErrorCode) {
	logger.Infof("Running guest consistent snapshot script at: %s", path)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		logger.Errorf("os.stat(%s) failed with error: %v", path, err)
		return -1, sspb.AgentErrorCode_SCRIPT_NOT_FOUND
	}

	execResult := run.WithOutputTimeout(ctx, timeout, path, disks)
	if execResult.ExitCode == 124 {
		logger.Errorf("Script %q with argument [%s] timedout with error: %+v", path, disks, execResult)
		return execResult.ExitCode, sspb.AgentErrorCode_SCRIPT_TIMED_OUT
	}

	if execResult.ExitCode != 0 {
		logger.Errorf("Script %q with argument [%s] failed with error: %+v", path, disks, execResult)
		return execResult.ExitCode, sspb.AgentErrorCode_UNHANDLED_SCRIPT_ERROR
	}

	return execResult.ExitCode, sspb.AgentErrorCode_NO_ERROR
}

func listenForSnapshotRequests(ctx context.Context, address string, requestChan chan<- *sspb.GuestMessage) {
	for context.Cause(ctx) == nil {
		// Start hanging connection on server that feeds to channel.
		logger.Infof("Attempting to connect to snapshot service at %s.", address)
		conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			logger.Errorf("Failed to connect to snapshot service: %v.", err)
			return
		}

		c := sspb.NewSnapshotServiceClient(conn)

		guestReady := sspb.GuestReady{
			RequestServerInfo: false,
		}

		r, err := c.CreateConnection(ctx, &guestReady)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				logger.Errorf("Error creating connection: %v.", err)
			}
			continue
		}
		for {
			request, err := r.Recv()
			if err != nil {
				logger.Errorf("Error reading snapshot request: %v.", err)
				break
			}
			logger.Infof("Received snapshot request.")
			requestChan <- request
		}
	}
}

func getSnapshotResponse(ctx context.Context, timeout time.Duration, guestMessage *sspb.GuestMessage) *sspb.SnapshotResponse {
	request := guestMessage.GetSnapshotRequest()

	if request == nil {
		logger.Warningf("Invalid snapshot request [%v], ignoring", request)
		return nil
	}

	response := &sspb.SnapshotResponse{
		OperationId: request.GetOperationId(),
		Type:        request.GetType(),
	}

	var scriptPath string
	switch request.GetType() {
	case sspb.OperationType_PRE_SNAPSHOT:
		logger.Infof("Handling pre snapshot request for operation id %d.", request.GetOperationId())
		_, found := seenPreSnapshotOperationIds.Get(request.GetOperationId())
		if found {
			logger.Infof("Duplicate pre snapshot request operation id %d.", request.GetOperationId())
			return nil
		}
		seenPreSnapshotOperationIds.Add(request.GetOperationId(), request.GetOperationId())
		scriptPath = filepath.Join(scriptsDir, "pre.sh")
	case sspb.OperationType_POST_SNAPSHOT:
		logger.Infof("Handling post snapshot request for operation id %d.", request.GetOperationId())
		_, found := seenPostSnapshotOperationIds.Get(request.GetOperationId())
		if found {
			logger.Infof("Duplicate post snapshot request operation id %d.", request.GetOperationId())
			return nil
		}
		seenPostSnapshotOperationIds.Add(request.GetOperationId(), request.GetOperationId())
		scriptPath = filepath.Join(scriptsDir, "post.sh")
	default:
		logger.Errorf("Unhandled operation type %d.", request.GetType())
		return nil
	}

	scriptsReturnCode, agentErrorCode := runScript(ctx, scriptPath, request.GetDiskList(), timeout)
	response.ScriptsReturnCode = int32(scriptsReturnCode)
	response.AgentReturnCode = agentErrorCode

	return response
}

func handleSnapshotRequests(ctx context.Context, timeout time.Duration, address string, requestChan <-chan *sspb.GuestMessage) {
	for context.Cause(ctx) == nil {
		conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		if err != nil {
			logger.Errorf("Failed to connect to snapshot service: %v.", err)
			return
		}
		for {
			// Listen on channel and respond
			guestMessage := <-requestChan
			response := getSnapshotResponse(ctx, timeout, guestMessage)

			// We either got a duplicated pre/post or an invalid request
			// in both cases we want to ignore it.
			if response == nil {
				continue
			}

			f := func() error {
				c := sspb.NewSnapshotServiceClient(conn)
				_, err = c.HandleResponsesFromGuest(ctx, response)
				return err
			}

			if err := retry.Run(ctx, policy, f); err != nil {
				logger.Warningf("Failed to send snapshot response: %v", err)
				break
			} else {
				logger.Debugf("Successfully handled snapshot request.")
			}
		}
	}
}

func startSnapshotListener(ctx context.Context, snapshotServiceIP string, snapshotServicePort int, timeoutInSeconds int) {
	requestChan := make(chan *sspb.GuestMessage)
	address := fmt.Sprintf("%s:%d", snapshotServiceIP, snapshotServicePort)

	// Create scripts directory if it doesn't exist.
	_, err := os.Stat(scriptsDir)
	if os.IsNotExist(err) {
		// Make the directory only readable/writable/executable by root.
		os.MkdirAll(scriptsDir, 0700)
	}
	timeout := time.Duration(timeoutInSeconds) * time.Second
	go listenForSnapshotRequests(ctx, address, requestChan)
	go handleSnapshotRequests(ctx, timeout, address, requestChan)
}
