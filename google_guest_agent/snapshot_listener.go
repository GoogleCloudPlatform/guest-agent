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
	seenPreSnapshotOperationIds  = lru.New(128)
	seenPostSnapshotOperationIds = lru.New(128)
)

type snapshotConfig struct {
	timeout               time.Duration // seconds
	preSnapshotScriptURL  string
	postSnapshotScriptURL string
	enabled               bool
	snapshotServiceIP     string
	snapshotServicePort   int
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
	conf.preSnapshotScriptURL = config.Section("Snapshots").Key("pre_snapshot_script").String()
	conf.postSnapshotScriptURL = config.Section("Snapshots").Key("post_snapshot_script").String()
	conf.snapshotServiceIP = config.Section("Snapshots").Key("snapshot_service_ip").MustString("169.254.169.254")
	conf.snapshotServicePort = config.Section("Snapshots").Key("snapshot_service_port").MustInt(8081)

	if conf.preSnapshotScriptURL == "" && conf.postSnapshotScriptURL == "" {
		return conf, &invalidSnapshotConfig{"neither pre or post snapshot script has been configured"}
	}

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
			logger.Errorf("Failed to connect: %v.", err)
			continue
		}

		c := sspb.NewSnapshotServiceClient(conn)
		ctx, cancel := context.WithCancel(context.Background())
		guestReady := sspb.GuestReady{
			RequestServerInfo: false,
		}
		r, err := c.CreateConnection(ctx, &guestReady)
		if err != nil {
			logger.Errorf("Error creating connection: %v.", err)
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
		cancel()
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
			url = config.preSnapshotScriptURL
		case sspb.OperationType_POST_SNAPSHOT:
			logger.Infof("Handling post snapshot request for operation id %d.", request.GetOperationId())
			_, found := seenPostSnapshotOperationIds.Get(request.GetOperationId())
			if found {
				logger.Infof("Duplicate post snapshot request operation id %d.", request.GetOperationId())
				return nil
			}
			seenPostSnapshotOperationIds.Add(request.GetOperationId(), request.GetOperationId())
			url = config.postSnapshotScriptURL
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
			logger.Errorf("Failed to connect: %v.", err)
			time.Sleep(1 * time.Second)
			continue
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
	go listenForSnapshotRequests(address, requestChan)
	go handleSnapshotRequests(address, requestChan)
}
