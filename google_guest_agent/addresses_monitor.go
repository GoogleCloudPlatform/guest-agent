//  Copyright 2026 Google LLC
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
	"sync"
	"sync/atomic"
	"time"

	"github.com/GoogleCloudPlatform/guest-agent/google_guest_agent/cfg"
	"github.com/GoogleCloudPlatform/guest-agent/metadata"
	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const (
	// monitorID is the ID of the routes monitor scheduled job.
	monitorID = "routes_monitor"
	// routesMonitorFallbackMonitor is the fallback routes monitor interval to use
	// if the user-specified interval is invalid.
	fallbackInterval = 10 * time.Second
)

// routesMonitor is the routes monitor scheduled job implementation.
type routesMonitor struct {
	// mds is the metadata descriptor to use for monitoring routes.
	mds *metadata.Descriptor
	// mdsMu is the mutex protecting the mds descriptor.
	mdsMu sync.Mutex
	// ShouldSkip indicates whether the next run of the monitor should be skipped.
	ShouldSkip atomic.Bool
}

// ID returns the job id.
func (*routesMonitor) ID() string {
	return monitorID
}

// Interval returns the interval at which job should be rescheduled and
// a bool determining if job should be scheduled starting now.
// If false, first run will be at time now+interval.
func (*routesMonitor) Interval() (time.Duration, bool) {
	interval, err := time.ParseDuration(cfg.Get().Routes.MonitorInterval)
	if err != nil {
		logger.Infof("Invalid routes monitor interval (err %v), using fallback of %v", err, fallbackInterval)
		interval = fallbackInterval
	}
	return interval, false
}

// ShouldEnable specifies if the job should be enabled for scheduling.
func (*routesMonitor) ShouldEnable(context.Context) bool {
	return cfg.Get().Routes.EnableMonitor
}

// Run triggers the job for single execution. It returns error if any
// and a bool stating if scheduler should continue or stop scheduling.
func (m *routesMonitor) Run(ctx context.Context) (bool, error) {
	// Skip the routes setup if the setup should be skipped. This variable
	// should be reset by the job or manager that originally set this to
	// true so that the monitor isn't indefinitely skipped.
	if m.ShouldSkip.Load() {
		return true, nil
	}

	m.mdsMu.Lock()
	defer m.mdsMu.Unlock()

	setupRoutes(ctx, m.mds, cfg.Get())
	return true, nil
}
