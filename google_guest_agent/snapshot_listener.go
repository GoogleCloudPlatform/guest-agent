//  Copyright 2020 Google Inc. All Rights Reserved.
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

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	sspb "github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/snapshot_service"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/golang/groupcache/lru"
	"google.golang.org/grpc"
)

var (
	scriptsDir                   = "/etc/google/snapshots/"
	seenPreSnapshotOperationIds  = lru.New(128)
	seenPostSnapshotOperationIds = lru.New(128)
)

type snapshotConfig struct {
	timeout time.Duration // seconds
}

type invalidSnapshotConfig struct {
	msg string
}

func (e *invalidSnapshotConfig) Error() string {
	return fmt.Sprintf("invalid config: %s", e.msg)
}

func getSnapshotConfig() (snapshotConfig, error) {
	var conf snapshotConfig
	conf.timeout = time.Duration(config.Section("Snapshots").Key("timeout_in_seconds").MustInt(60)) * time.Second

	return conf, nil
}

func runScript(path, disks string, config snapshotConfig) (int, sspb.AgentErrorCode) {
	logger.Infof("Running guest consistent snapshot script at: %s.", path)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return -1, sspb.AgentErrorCode_SCRIPT_NOT_FOUND
	}

	execResult := runCmdOutputWithTimeout(config.timeout, path, disks)

	if execResult.code == 124 {
		return execResult.code, sspb.AgentErrorCode_SCRIPT_TIMED_OUT
	}

	if execResult.code != 0 {
		return execResult.code, sspb.AgentErrorCode_UNHANDLED_SCRIPT_ERROR
	}

	return execResult.code, sspb.AgentErrorCode_NO_ERROR
}

func listenForSnapshotRequests(address string, requestChan chan<- *sspb.GuestMessage) {
	for {
		// Start hanging connection on server that feeds to channel
		logger.Infof("Attempting to connect to snapshot service at %s.", address)
		conn, err := grpc.Dial(address, grpc.WithInsecure())
		if err != nil {
			logger.Errorf("Failed to connect to snapshot service: %v.", err)
			return
		}

		c := sspb.NewSnapshotServiceClient(conn)
		ctx, cancel := context.WithCancel(context.Background())
		guestReady := sspb.GuestReady{
			RequestServerInfo: false,
		}
		r, err := c.CreateConnection(ctx, &guestReady)
		if err != nil {
			logger.Errorf("Error creating connection: %v.", err)
			cancel()
			continue
		}
		for {
			request, err := r.Recv()
			if err != nil {
				logger.Errorf("Error reading snapshot request: %v.", err)
				cancel()
				break
			}
			logger.Infof("Received snapshot request.")
			requestChan <- request
		}
	}
}

func getSnapshotResponse(guestMessage *sspb.GuestMessage) *sspb.SnapshotResponse {
	switch {
	case guestMessage.GetSnapshotRequest() != nil:
		request := guestMessage.GetSnapshotRequest()
		response := &sspb.SnapshotResponse{
			OperationId: request.GetOperationId(),
			Type:        request.GetType(),
		}

		config, err := getSnapshotConfig()
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

		scriptsReturnCode, agentErrorCode := runScript(url, request.GetDiskList(), config)
		response.ScriptsReturnCode = int32(scriptsReturnCode)
		response.AgentReturnCode = agentErrorCode

		return response
	default:
	}
	return nil
}

func handleSnapshotRequests(address string, requestChan <-chan *sspb.GuestMessage) {
	for {
		conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			logger.Errorf("Failed to connect to snapshot service: %v.", err)
			return
		}
		for {
			// Listen on channel and respond
			guestMessage := <-requestChan
			response := getSnapshotResponse(guestMessage)
			for {
				c := sspb.NewSnapshotServiceClient(conn)
				ctx, cancel := context.WithCancel(context.Background())
				_, err = c.HandleResponsesFromGuest(ctx, response)
				if err == nil {
					cancel()
					break
				}
				logger.Errorf("Error sending response: %v.", err)
			}
		}
	}
}

func startSnapshotListener(snapshotServiceIP string, snapshotServicePort int) {
	requestChan := make(chan *sspb.GuestMessage)
	address := fmt.Sprintf("%s:%d", snapshotServiceIP, snapshotServicePort)

	// Create scripts directory if it doesn't exist.
	_, err := os.Stat(scriptsDir)
	if os.IsNotExist(err) {
		// Make the directory only readable/writable/executable by root.
		os.MkdirAll(scriptsDir, 0700)
	}

	go listenForSnapshotRequests(address, requestChan)
	go handleSnapshotRequests(address, requestChan)
}
