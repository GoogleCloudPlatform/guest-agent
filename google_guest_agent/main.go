//  Copyright 2017 Google Inc. All Rights Reserved.
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

// GCEGuestAgent is the Google Compute Engine guest agent executable.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events"
	mdsEvent "github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/events/sshtrustedca"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-agent/utils"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/go-ini/ini"
)

var (
	programName              = "GCEGuestAgent"
	version                  string
	ticker                   = time.Tick(70 * time.Second)
	oldMetadata, newMetadata *metadata.Descriptor
	config                   *ini.File
	osRelease                release
	action                   string
	mdsClient                *metadata.Client
)

const (
	winConfigPath = `C:\Program Files\Google\Compute Engine\instance_configs.cfg`
	configPath    = `/etc/default/instance_configs.cfg`
	regKeyBase    = `SOFTWARE\Google\ComputeEngine`
)

type manager interface {
	diff() bool
	disabled(string) bool
	set(ctx context.Context) error
	timeout() bool
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

func parseConfig(file string) (*ini.File, error) {
	// Priority: file.cfg, file.cfg.distro, file.cfg.template
	cfg, err := ini.LoadSources(ini.LoadOptions{Loose: true, Insensitive: true}, file, file+".distro", file+".template")
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func closeFile(c io.Closer) {
	err := c.Close()
	if err != nil {
		logger.Warningf("Error closing file: %v.", err)
	}
}

func runUpdate(ctx context.Context) {
	var wg sync.WaitGroup
	mgrs := []manager{&addressMgr{}}
	switch runtime.GOOS {
	case "windows":
		mgrs = append(mgrs, []manager{newWsfcManager(), &winAccountsMgr{}, &diagnosticsMgr{}}...)
	default:
		mgrs = append(mgrs, []manager{&clockskewMgr{}, &osloginMgr{}, &accountsMgr{}}...)
	}
	for _, mgr := range mgrs {
		wg.Add(1)
		go func(mgr manager) {
			defer wg.Done()
			if mgr.disabled(runtime.GOOS) {
				logger.Debugf("manager %#v disabled, skipping", mgr)
				return
			}
			if !mgr.timeout() && !mgr.diff() {
				logger.Debugf("manager %#v reports no diff", mgr)
				return
			}
			logger.Debugf("running %#v manager", mgr)
			if err := mgr.set(ctx); err != nil {
				logger.Errorf("error running %#v manager: %s", mgr, err)
			}
		}(mgr)
	}
	wg.Wait()
}

func run(ctx context.Context) {
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

	mdsClient = metadata.New()

	var err error
	newMetadata, err = mdsClient.Get(ctx)
	if err == nil {
		opts.ProjectName = newMetadata.Project.ProjectID
	}

	if err := logger.Init(ctx, opts); err != nil {
		fmt.Printf("Error initializing logger: %v", err)
		os.Exit(1)
	}

	logger.Infof("GCE Agent Started (version %s)", version)

	osRelease, err = getRelease()
	if err != nil && runtime.GOOS != "windows" {
		logger.Warningf("Couldn't detect OS release")
	}

	cfgfile := configPath
	if runtime.GOOS == "windows" {
		cfgfile = winConfigPath
	}

	config, err = parseConfig(cfgfile)
	if err != nil && !os.IsNotExist(err) {
		logger.Errorf("Error parsing config %s: %s", cfgfile, err)
	}

	agentInit(ctx)

	eventsConfig := &events.Config{
		Watchers: []string{
			mdsEvent.WatcherID,
		},
	}

	// Only Enable sshtrustedca Watcher if osLogin is enabled.
	// TODO: ideally we should have a feature flag specifically for this.
	osLoginEnabled, _, _ := getOSLoginEnabled(newMetadata)
	if osLoginEnabled {
		eventsConfig.Watchers = append(eventsConfig.Watchers, sshtrustedca.WatcherID)
	}

	eventManager, err := events.New(eventsConfig)
	if err != nil {
		logger.Errorf("Error initializing event manager: %v", err)
		return
	}

	var cachedCertificate string
	eventManager.Subscribe(sshtrustedca.ReadEvent, nil, func(evType string, data interface{}, evData *events.EventData) bool {
		// There was some error on the pipe watcher, just ignore it.
		if evData.Error != nil {
			logger.Debugf("Not handling ssh trusted ca cert event, we got an error: %+v", evData.Error)
			return true
		}

		// Make sure we close the pipe after we've done writing to it.
		pipeData := evData.Data.(*sshtrustedca.PipeData)
		defer pipeData.File.Close()

		// The certificates key/endpoint is not cached, we can't rely on the metadata watcher data because of that.
		certificate, err := mdsClient.GetKey(ctx, "oslogin/certificates")
		if err != nil && cachedCertificate != "" {
			certificate = cachedCertificate
			logger.Warningf("Failed to get certificate, assuming/using previously cached one.")
		} else if err != nil {
			logger.Errorf("Failed to get certificate from metadata server: %+v", err)
			return true
		}

		// Keep a copy of the returned certificate for error fallback caching.
		cachedCertificate = certificate

		n, err := pipeData.File.WriteString(certificate)
		if err != nil {
			logger.Errorf("Failed to write certificate to the write end of the pipe: %+v", err)
		}

		if n != len(certificate) {
			logger.Errorf("Wrote the wrong ammout of data, wrote %d bytes instead of %d bytes", n, len(certificate))
		}

		return true
	})

	oldMetadata = &metadata.Descriptor{}
	eventManager.Subscribe(mdsEvent.LongpollEvent, nil, func(evType string, data interface{}, evData *events.EventData) bool {
		logger.Debugf("Handling metadata %q event.", evType)

		// If metadata watcher failed there isn't much we can do, just ignore the event and
		// allow the water to get it corrected.
		if evData.Error != nil {
			logger.Infof("Metadata event watcher failed, ignoring: %+v", evData.Error)
			return true
		}

		newMetadata = evData.Data.(*metadata.Descriptor)
		if newMetadata == nil {
			logger.Info("Metadata event watcher didn't pass in the metadata, ignoring.")
			return true
		}

		runUpdate(ctx)
		oldMetadata = newMetadata

		return true
	})

	eventManager.Run(ctx)
	logger.Infof("GCE Agent Stopped")
}

type execResult struct {
	// Return code. Set to -1 if we failed to run the command.
	code int
	// Stderr or err.Error if we failed to run the command.
	err string
	// Stdout or "" if we failed to run the command.
	out string
}

func (e execResult) Error() string {
	return strings.TrimSuffix(e.err, "\n")
}

func (e execResult) ExitCode() int {
	return e.code
}

func (e execResult) Stdout() string {
	return e.out
}

func (e execResult) Stderr() string {
	return e.err
}

func runCmd(cmd *exec.Cmd) error {
	res := runCmdOutput(cmd)
	if res.ExitCode() != 0 {
		return res
	}
	return nil
}

func runCmdOutput(cmd *exec.Cmd) *execResult {
	logger.Debugf("exec: %v", cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return &execResult{code: ee.ExitCode(), out: stdout.String(), err: stderr.String()}
		}
		return &execResult{code: -1, err: err.Error()}
	}
	return &execResult{code: 0, out: stdout.String()}
}

func runCmdOutputWithTimeout(timeoutSec time.Duration, name string, args ...string) *execResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutSec)
	defer cancel()
	execResult := runCmdOutput(exec.CommandContext(ctx, name, args...))
	if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		execResult.code = 124 // By convention
	}
	return execResult
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

	var action string
	if len(os.Args) < 2 {
		action = "run"
	} else {
		action = os.Args[1]
	}

	if action == "noservice" {
		run(ctx)
		os.Exit(0)
	}

	if err := register(ctx, "GCEAgent", "GCEAgent", "", run, action); err != nil {
		logger.Fatalf("error registering service: %s", err)
	}
}
