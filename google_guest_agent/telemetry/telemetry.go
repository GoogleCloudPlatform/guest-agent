//  Copyright 2023 Google Inc. All Rights Reserved.
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

package telemetry

import (
	"context"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"google.golang.org/protobuf/proto"

	tpb "github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/telemetry/proto"
)

var (
	metadataURL = "http://169.254.169.254/computeMetadata/v1/"
)

// Data is telemetry data on the current agent and OS.
type Data struct {
	// Name of the agent.
	AgentName string
	// Version of the Agent.
	AgentVersion string
	// Architecture of the Agent.
	AgentArch string

	// OS name.
	OS string
	// The name the OS uses to fully describe itself.
	LongName string
	// OS name in short form (aka distro name).
	ShortName string
	// Version of the OS.
	Version string
	// Kernel Release.
	KernelRelease string
	// Kernel Version.
	KernelVersion string
}

func formatGuestAgent(d Data) string {
	data, err := proto.Marshal(&tpb.AgentInfo{
		Name:         &d.AgentName,
		Version:      &d.AgentVersion,
		Architecture: &d.AgentArch,
	})
	if err != nil {
		logger.Warningf("Error marshalling AgentInfo: %v", err)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func formatGuestOS(d Data) string {
	data, err := proto.Marshal(&tpb.OSInfo{
		OsType:        &d.OS,
		LongName:      &d.LongName,
		ShortName:     &d.ShortName,
		Version:       &d.Version,
		KernelVersion: &d.KernelVersion,
		KernelRelease: &d.KernelRelease,
	})
	if err != nil {
		logger.Warningf("Error marshalling AgentInfo: %v", err)
	}
	return base64.StdEncoding.EncodeToString(data)
}

// Record records telemetry data.
func Record(ctx context.Context, d Data) error {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", metadataURL, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Metadata-Flavor", "Google")
	req.Header.Add("X-Google-Guest-Agent", formatGuestAgent(d))
	req.Header.Add("X-Google-Guest-OS", formatGuestOS(d))
	req = req.WithContext(ctx)

	_, err = client.Do(req)
	return err
}
