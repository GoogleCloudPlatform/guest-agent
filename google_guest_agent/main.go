// Copyright 2017 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// GCEGuestAgent is the Google Compute Engine guest agent executable.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/agentcrypto"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events"
	mdsEvent "github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/sshtrustedca"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/osinfo"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/scheduler"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/sshca"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/telemetry"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

// Certificates wrapps a list of certificate authorities.
type Certificates struct {
	Certs []TrustedCert `json:"trustedCertificateAuthorities"`
}

// TrustedCert defines the object containing a public key.
type TrustedCert struct {
	PublicKey string `json:"publicKey"`
}

var (
	programName              = "GCEGuestAgent"
	version                  string
	oldMetadata, newMetadata *metadata.Descriptor
	osInfo                   osinfo.OSInfo
	mdsClient                *metadata.Client
)

const (
	regKeyBase = `SOFTWARE\Google\ComputeEngine`
)

type manager interface {
	Diff(ctx context.Context) (bool, error)
	Disabled(ctx context.Context) (bool, error)
	Set(ctx context.Context) error
	Timeout(ctx context.Context) (bool, error)
}

func logStatus(name string, disabled bool) {
	var status string
	switch disabled {
	case false:
		status = "enabled"
	case true:
		status = "disabled"
	}
	logger.Infof("GCE %s manager status: %s", name, status)
}

func closeFile(c io.Closer) {
	err := c.Close()
	if err != nil {
		logger.Warningf("Error closing file: %v.", err)
	}
}

func availableManagers() []manager {
	managers := []manager{
		&addressMgr{},
	}

	if runtime.GOOS == "windows" {
		return append(managers,
			newWsfcManager(),
			&winAccountsMgr{},
			&diagnosticsMgr{},
		)
	}

	return append(managers,
		&clockskewMgr{},
		&osloginMgr{},
		&accountsMgr{},
	)
}

func runUpdate(ctx context.Context) {
	var wg sync.WaitGroup
	for _, mgr := range availableManagers() {
		wg.Add(1)
		go func(mgr manager) {
			defer wg.Done()

			disabled, err := mgr.Disabled(ctx)
			if err != nil {
				logger.Errorf("Failed to run manager's Disabled() call: %+v", err)
				return
			}

			if disabled {
				logger.Debugf("manager %#v disabled, skipping", mgr)
				return
			}

			timeout, err := mgr.Timeout(ctx)
			if err != nil {
				logger.Errorf("[%#v] Failed to run manager Timeout() call: %+v", mgr, err)
				return
			}

			diff, err := mgr.Diff(ctx)
			if err != nil {
				logger.Errorf("[%#v] Failed to run manager Diff() call: %+v", mgr, err)
				return
			}

			if !timeout && !diff {
				logger.Debugf("[%#v] Manager reports no diff", mgr)
				return
			}

			logger.Debugf("running %#v manager", mgr)
			if err := mgr.Set(ctx); err != nil {
				logger.Errorf("[%#v] Failed to run manager Set() call: %s", mgr, err)
			}
		}(mgr)
	}
	wg.Wait()
}

func runAgent(ctx context.Context) {
	opts := logger.LogOpts{LoggerName: programName}
	if runtime.GOOS == "windows" {
		opts.FormatFunction = logFormatWindows
		opts.Writers = []io.Writer{&utils.SerialPort{Port: "COM1"}}
	} else {
		opts.FormatFunction = logFormat
		opts.Writers = []io.Writer{os.Stdout}
		// Local logging is syslog; we will just use stdout in Linux.
		opts.DisableLocalLogging = true
	}

	if os.Getenv("GUEST_AGENT_DEBUG") != "" {
		opts.Debug = true
	}

	if err := logger.Init(ctx, opts); err != nil {
		fmt.Printf("Error initializing logger: %v", err)
		os.Exit(1)
	}

	logger.Infof("GCE Agent Started (version %s)", version)

	osInfo = osinfo.Get()
	mdsClient = metadata.New()

	agentInit(ctx)

	// Previous request to metadata *may* not have worked becasue routes don't get added until agentInit.
	var err error
	if newMetadata == nil {
		/// Error here doesn't matter, if we cant get metadata, we cant record telemetry.
		newMetadata, err = mdsClient.Get(ctx)
		if err != nil {
			logger.Debugf("Error getting metdata: %v", err)
		}
	}

	// Try to re-initialize logger now, we know after agentInit() is more likely to have metadata available.
	// TODO: move all this metadata dependent code to its own metadata event handler.
	if newMetadata != nil {
		opts.ProjectName = newMetadata.Project.ProjectID
		if err := logger.Init(ctx, opts); err != nil {
			logger.Errorf("Error initializing logger: %v", err)
		}
	}

	// knownJobs is list of default jobs that run on a pre-defined schedule.
	knownJobs := []scheduler.Job{telemetry.New(mdsClient, programName, version)}
	scheduler.ScheduleJobs(ctx, knownJobs, false)

	// Schedules jobs that need to be started before notifying systemd Agent process has started.
	if cfg.Get().Unstable.MDSMTLS {
		scheduler.ScheduleJobs(ctx, []scheduler.Job{agentcrypto.New()}, true)
	}

	eventsConfig := &events.Config{
		Watchers: []string{
			mdsEvent.WatcherID,
			sshtrustedca.WatcherID,
		},
	}

	eventManager, err := events.New(eventsConfig)
	if err != nil {
		logger.Errorf("Error initializing event manager: %v", err)
		return
	}

	sshca.Init(eventManager)

	oldMetadata = &metadata.Descriptor{}
	eventManager.Subscribe(mdsEvent.LongpollEvent, nil, func(ctx context.Context, evType string, data interface{}, evData *events.EventData) bool {
		logger.Debugf("Handling metadata %q event.", evType)

		// If metadata watcher failed there isn't much we can do, just ignore the event and
		// allow the water to get it corrected.
		if evData.Error != nil {
			logger.Infof("Metadata event watcher failed, ignoring: %+v", evData.Error)
			return true
		}

		if evData.Data == nil {
			logger.Infof("Metadata event watcher didn't pass in the metadata, ignoring.")
			return true
		}

		newMetadata = evData.Data.(*metadata.Descriptor)
		runUpdate(ctx)
		oldMetadata = newMetadata

		return true
	})

	eventManager.Run(ctx)
	logger.Infof("GCE Agent Stopped")
}

func logFormatWindows(e logger.LogEntry) string {
	now := time.Now().Format("2006/01/02 15:04:05")
	// 2006/01/02 15:04:05 GCEGuestAgent This is a log message.
	return fmt.Sprintf("%s %s: %s", now, programName, e.Message)
}

func logFormat(e logger.LogEntry) string {
	switch e.Severity {
	case logger.Error, logger.Critical, logger.Debug:
		// ERROR file.go:82 This is a log message.
		return fmt.Sprintf("%s %s:%d %s", strings.ToUpper(e.Severity.String()), e.Source.File, e.Source.Line, e.Message)
	default:
		// This is a log message.
		return e.Message
	}
}

func closer(c io.Closer) {
	err := c.Close()
	if err != nil {
		logger.Warningf("Error closing %v: %v.", c, err)
	}
}

func main() {
	ctx := context.Background()

	if err := cfg.Load(nil); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %+v", err)
		os.Exit(1)
	}

	var action string
	if len(os.Args) < 2 {
		action = "run"
	} else {
		action = os.Args[1]
	}

	if action == "noservice" {
		runAgent(ctx)
		os.Exit(0)
	}

	if err := register(ctx, "GCEAgent", "GCEAgent", "", runAgent, action); err != nil {
		logger.Fatalf("error registering service: %s", err)
	}
}
