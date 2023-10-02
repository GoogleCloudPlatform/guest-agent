// Copyright 2023 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package telemetry

import (
	"context"
	"encoding/base64"
	"runtime"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"google.golang.org/protobuf/proto"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/osinfo"
	tpb "github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/telemetry/proto"
)

var (
	telemetryJobID    = "telemetryJobID"
	telemetryInterval = 24 * time.Hour
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
func Record(ctx context.Context, client metadata.MDSClientInterface, d Data) error {
	headers := map[string]string{
		"X-Google-Guest-Agent": formatGuestAgent(d),
		"X-Google-Guest-OS":    formatGuestOS(d),
	}
	// This is the simplest metadata call we can make, and we dont care about any return value,
	// all we need to do is make some call with the telemetry headers.
	_, err := client.GetKey(ctx, "", headers)
	return err
}

// Job implements job scheduler interface for recording telemetry.
type Job struct {
	client       metadata.MDSClientInterface
	programName  string
	agentVersion string
}

// New initializes a new TelemetryJob.
func New(client metadata.MDSClientInterface, programName, agentVersion string) *Job {
	return &Job{
		client:       client,
		programName:  programName,
		agentVersion: agentVersion,
	}
}

// ID returns the ID for this job.
func (j *Job) ID() string {
	return telemetryJobID
}

// Run records telemetry data.
func (j *Job) Run(ctx context.Context) (bool, error) {
	osInfo := osinfo.Get()
	d := Data{
		AgentName:     j.programName,
		AgentVersion:  j.agentVersion,
		AgentArch:     runtime.GOARCH,
		OS:            runtime.GOOS,
		LongName:      osInfo.PrettyName,
		ShortName:     osInfo.OS,
		Version:       osInfo.VersionID,
		KernelRelease: osInfo.KernelRelease,
		KernelVersion: osInfo.KernelVersion,
	}
	if err := Record(ctx, j.client, d); err != nil {
		// Log this here in Debug mode as telemetry is best effort.
		logger.Debugf("Error recording telemetry: %v", err)
	}

	return j.ShouldEnable(ctx), nil
}

// Interval returns the interval at which job is executed.
func (j *Job) Interval() (time.Duration, bool) {
	return telemetryInterval, true
}

// ShouldEnable returns true as long as DisableTelemetry is not set in metadata.
func (j *Job) ShouldEnable(ctx context.Context) bool {
	md, err := j.client.Get(ctx)
	if err != nil {
		return false
	}

	return !md.Instance.Attributes.DisableTelemetry && !md.Project.Attributes.DisableTelemetry
}
