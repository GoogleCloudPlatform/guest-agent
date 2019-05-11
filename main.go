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
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/go-ini/ini"
	"github.com/tarm/serial"
)

var (
	version                  string
	ticker                   = time.Tick(70 * time.Second)
	oldMetadata, newMetadata *metadataJSON
	config                   *ini.File
)

const (
	configPath = `C:\Program Files\Google\Compute Engine\instance_configs.cfg`
	regKeyBase = `SOFTWARE\Google\ComputeEngine`
)

func writeSerial(port string, msg []byte) error {
	c := &serial.Config{Name: port, Baud: 115200}
	s, err := serial.OpenPort(c)
	if err != nil {
		return err
	}
	defer s.Close()

	_, err = s.Write(msg)
	if err != nil {
		return err
	}

	return nil
}

type manager interface {
	diff() bool
	disabled() bool
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
	d, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return ini.InsensitiveLoad(d)
}

func runUpdate() {
	cfg, err := parseConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		logger.Errorf(err.Error())
	}
	if cfg == nil {
		cfg, _ = ini.InsensitiveLoad([]byte{})
	}

	config = cfg

	var wg sync.WaitGroup
	for _, mgr := range []manager{newWsfcManager(), &addressMgr{}, &accountsMgr{}, &diagnosticsMgr{}} {
		wg.Add(1)
		go func(mgr manager) {
			defer wg.Done()
			if mgr.disabled() || (!mgr.timeout() && !mgr.diff()) {
				return
			}
			if err := mgr.set(); err != nil {
				logger.Errorf(err.Error())
			}
		}(mgr)
	}
	wg.Wait()
}

func run(ctx context.Context) {
	logger.Infof("GCE Agent Started (version %s)", version)

	go func() {
		oldMetadata = &metadataJSON{}
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
					logger.Errorf(err.Error())
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

// TODO: this doesn't get used in this file, doesn't belong here
func containsString(s string, ss []string) bool {
	for _, a := range ss {
		if a == s {
			return true
		}
	}
	return false
}

func main() {
	opts := logger.LogOpts{LoggerName: "GCEWindowsAgent"}

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

	// TODO: this is undocumented
	if action == "noservice" {
		run(ctx)
		os.Exit(0)
	}

	// TODO: more argv parsing is handled in register, rather than all in one place
	if err := register(ctx, "GCEAgent", "GCEAgent", "", run, action); err != nil {
		logger.Fatalf(err.Error())
	}
}
