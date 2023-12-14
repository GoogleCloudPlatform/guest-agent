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
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/run"
	sspb "github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/snapshot_service"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/golang/groupcache/lru"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	scriptsDir                   = "/etc/google/snapshots/"
	seenPreSnapshotOperationIds  = lru.New(128)
	seenPostSnapshotOperationIds = lru.New(128)
	maxRequestHandleAttempts     = 10 // completely arbitrary max attempt
)

type snapshotConfig struct {
	timeout time.Duration // seconds
}

func getSnapshotConfig(timeoutInSeconds int) (snapshotConfig, error) {
	var conf snapshotConfig
	conf.timeout = time.Duration(timeoutInSeconds) * time.Second
	return conf, nil
}

func runScript(ctx context.Context, path, disks string, config snapshotConfig) (int, sspb.AgentErrorCode) {
	logger.Infof("Running guest consistent snapshot script at: %s.", path)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return -1, sspb.AgentErrorCode_SCRIPT_NOT_FOUND
	}

	execResult := run.WithOutputTimeout(ctx, config.timeout, path, disks)
	if execResult.ExitCode == 124 {
		return execResult.ExitCode, sspb.AgentErrorCode_SCRIPT_TIMED_OUT
	}

	if execResult.ExitCode != 0 {
		return execResult.ExitCode, sspb.AgentErrorCode_UNHANDLED_SCRIPT_ERROR
	}

	return execResult.ExitCode, sspb.AgentErrorCode_NO_ERROR
}

func listenForSnapshotRequests(ctx context.Context, address string, requestChan chan<- *sspb.GuestMessage) {
	for context.Cause(ctx) == nil {
		// Start hanging connection on server that feeds to channel
		logger.Infof("Attempting to connect to snapshot service at %s.", address)
		conn, err := grpc.Dial(address, grpc.WithInsecure())
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

func getSnapshotResponse(ctx context.Context, timeoutInSeconds int, guestMessage *sspb.GuestMessage) *sspb.SnapshotResponse {
	switch {
	case guestMessage.GetSnapshotRequest() != nil:
		request := guestMessage.GetSnapshotRequest()
		response := &sspb.SnapshotResponse{
			OperationId: request.GetOperationId(),
			Type:        request.GetType(),
		}

		config, err := getSnapshotConfig(timeoutInSeconds)
		if err != nil {
			response.AgentReturnCode = sspb.AgentErrorCode_INVALID_CONFIG
			return response

		}

		var url string
		switch request.GetType() {
		case sspb.OperationType_PRE_SNAPSHOT:
			logger.Infof("Handling pre snapshot request for operation id %d.", request.GetOperationId())
			_, found := seenPreSnapshotOperationIds.Get(request.GetOperationId())
			if found {
				logger.Infof("Duplicate pre snapshot request operation id %d.", request.GetOperationId())
				return nil
			}
			seenPreSnapshotOperationIds.Add(request.GetOperationId(), request.GetOperationId())
			url = scriptsDir + "pre.sh"
		case sspb.OperationType_POST_SNAPSHOT:
			logger.Infof("Handling post snapshot request for operation id %d.", request.GetOperationId())
			_, found := seenPostSnapshotOperationIds.Get(request.GetOperationId())
			if found {
				logger.Infof("Duplicate post snapshot request operation id %d.", request.GetOperationId())
				return nil
			}
			seenPostSnapshotOperationIds.Add(request.GetOperationId(), request.GetOperationId())
			url = scriptsDir + "post.sh"
		default:
			logger.Errorf("Unhandled operation type %d.", request.GetType())
			return nil
		}

		scriptsReturnCode, agentErrorCode := runScript(ctx, url, request.GetDiskList(), config)
		response.ScriptsReturnCode = int32(scriptsReturnCode)
		response.AgentReturnCode = agentErrorCode

		return response
	default:
	}
	return nil
}

func handleSnapshotRequests(ctx context.Context, timeoutInSeconds int, address string, requestChan <-chan *sspb.GuestMessage) {
	for {
		conn, err := grpc.Dial(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		if err != nil {
			logger.Errorf("Failed to connect to snapshot service: %v.", err)
			return
		}
		for {
			// Listen on channel and respond
			guestMessage := <-requestChan
			response := getSnapshotResponse(ctx, timeoutInSeconds, guestMessage)

			// We either got a duplicated pre/post or an invalid request
			// in both cases we want to ignore it.
			if response == nil {
				continue
			}

			for i := 0; i < maxRequestHandleAttempts; i++ {
				logger.Infof("Attempt %d/%d of handling snapshot request.", i+1, maxRequestHandleAttempts)

				c := sspb.NewSnapshotServiceClient(conn)
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()

				_, err = c.HandleResponsesFromGuest(ctx, response)
				if err != nil {
					logger.Errorf("Error sending response: %v.", err)
					time.Sleep(1 * time.Second) // Avoid idle looping
					continue
				}

				logger.Debugf("Successfully handled snapshot request.")
				break
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

	go listenForSnapshotRequests(ctx, address, requestChan)
	go handleSnapshotRequests(ctx, timeoutInSeconds, address, requestChan)
}
