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

// GCEWindowsAgent is the Google Compute Engine Windows agent executable.
package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/go-ini/ini"
	"github.com/tarm/serial"
)

var (
	programName              = "GCEWindowsAgent"
	version                  string
	ticker                   = time.Tick(70 * time.Second)
	oldMetadata, newMetadata *metadata
	config                   *ini.File
	osRelease                release
)

const (
	winConfigPath = `C:\Program Files\Google\Compute Engine\instance_configs.cfg`
	configPath    = `/etc/default/instance_configs.cfg`
	regKeyBase    = `SOFTWARE\Google\ComputeEngine`
)

func writeSerial(port string, msg []byte) error {
	c := &serial.Config{Name: port, Baud: 115200}
	s, err := serial.OpenPort(c)
	if err != nil {
		return err
	}
	defer closer(s)

	_, err = s.Write(msg)
	if err != nil {
		return err
	}

	return nil
}

type manager interface {
	diff() bool
	disabled(string) bool
	set() error
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
	empty, _ := ini.InsensitiveLoad([]byte{})
	d, err := ioutil.ReadFile(file)
	if err != nil {
		return empty, err
	}
	cfg, err := ini.InsensitiveLoad(d)
	if err != nil {
		return empty, err
	}
	return cfg, nil
}

func closeFile(c io.Closer) {
	err := c.Close()
	if err != nil {
		logger.Warningf("Error closing file: %v.", err)
	}
}

func runUpdate() {
	cfgPath := configPath
	if runtime.GOOS == "windows" {
		cfgPath = winConfigPath
	}

	var err error
	config, err = parseConfig(cfgPath)
	if err != nil && !os.IsNotExist(err) {
		logger.Errorf("error parsing config %s: %s", cfgPath, err)
	}

	var wg sync.WaitGroup
	var mgrs []manager
	if runtime.GOOS == "windows" {
		mgrs = []manager{newWsfcManager(), &addressMgr{}, &winAccountsMgr{}}
	} else {
		mgrs = []manager{&clockskewMgr{}}
	}
	for _, mgr := range mgrs {
		wg.Add(1)
		go func(mgr manager) {
			defer wg.Done()
			if mgr.disabled(runtime.GOOS) || (!mgr.timeout() && !mgr.diff()) {
				return
			}
			if err := mgr.set(); err != nil {
				logger.Errorf("error running %#v manager: %s", mgr, err)
			}
		}(mgr)
	}
	wg.Wait()
}

func run(ctx context.Context) {
	logger.Infof("GCE Agent Started (version %s)", version)
	var err error
	osRelease, err = getRelease()
	if err != nil && runtime.GOOS != "windows" {
		logger.Warningf("Couldn't detect OS release")
	}

	go func() {
		oldMetadata = &metadata{}
		webError := 0
		for {
			var err error
			newMetadata, err = watchMetadata(ctx)
			if err != nil {
				// Only log the second web error to avoid transient errors and
				// not to spam the log on network failures.
				if webError == 1 {
					if urlErr, ok := err.(*url.Error); ok {
						if _, ok := urlErr.Err.(*net.DNSError); ok {
							logger.Errorf("DNS error when requesting metadata, check DNS settings and ensure metadata.internal.google is setup in your hosts file.")
						}
						if _, ok := urlErr.Err.(*net.OpError); ok {
							logger.Errorf("Network error when requesting metadata, make sure your instance has an active network and can reach the metadata server.")
						}
					}
					logger.Errorf("Error watching metadata: %s", err)
				}
				webError++
				time.Sleep(5 * time.Second)
				continue
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
			runUpdate()
			oldMetadata = newMetadata
			webError = 0
		}
	}()

	<-ctx.Done()
	logger.Infof("GCE Agent Stopped")
}

// runCmd is exec.Cmd.Run() with a flattened error return.
func runCmd(cmd *exec.Cmd) error {
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf(string(ee.Stderr))
		}
		return err
	}
	return nil
}

func containsString(s string, ss []string) bool {
	for _, a := range ss {
		if a == s {
			return true
		}
	}
	return false
}

func logFormat(e logger.LogEntry) string {
	now := time.Now().Format("2006/01/02 15:04:05")
	return fmt.Sprintf("%s %s: %s", now, programName, e.Message)
}

func closer(c io.Closer) {
	err := c.Close()
	if err != nil {
		logger.Warningf("Error closing %v: %v.", c, err)
	}
}

func main() {
	var opts logger.LogOpts
	if runtime.GOOS == "windows" {
		opts = logger.LogOpts{LoggerName: programName, FormatFunction: logFormat}
	} else {
		opts = logger.LogOpts{LoggerName: programName, Stdout: true}
	}

	var err error
	ctx := context.Background()
	newMetadata, err = getMetadata(ctx, false)
	if err == nil {
		opts.ProjectName = newMetadata.Project.ProjectID
	}
	logger.Init(ctx, opts)

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
