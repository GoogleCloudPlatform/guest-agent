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

// Package scheduler maintains scheduler utility for scheduling arbitrary jobs.
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
	"github.com/robfig/cron/v3"
)

// Job defines the interface between the schedule manager and the actual job.
type Job interface {
	// ID returns the job id.
	ID() string
	// Interval returns the interval at which job should be rescheduled and
	// a bool determining if job should be scheduled starting now.
	// If false, first run will be at time now+interval.
	Interval() (time.Duration, bool)
	// ShouldEnable specifies if the job should be enabled for scheduling.
	ShouldEnable(context.Context) bool
	// Run triggers the job for single execution. It returns error if any
	// and a bool stating if scheduler should continue or stop scheduling.
	Run(context.Context) (bool, error)
}

// Scheduler implements job schedule manager and offers a way to schedule/unschedule new jobs.
type Scheduler struct {
	cron *cron.Cron
	jobs map[string]cron.EntryID
	mu   sync.RWMutex
}

var scheduler *Scheduler

func init() {
	taskIDs := make(map[string]cron.EntryID)
	cron := cron.New(cron.WithLogger(&cronLogger{}))

	scheduler = &Scheduler{
		cron: cron,
		jobs: taskIDs,
		mu:   sync.RWMutex{},
	}
}

// Get starts and returns scheduler instance.
func Get() *Scheduler {
	scheduler.start()
	return scheduler
}

// getFunc generates a wrapper function for cron scheduler.
func (s *Scheduler) getFunc(ctx context.Context, job Job) func() {
	f := func() {
		schedule, err := job.Run(ctx)
		if !schedule {
			s.UnscheduleJob(job.ID())
		}
		if err != nil {
			logger.Errorf("Failed to execute job %s: %v", job.ID(), err)
		}
	}
	return f
}

// ScheduleJob adds a job to schedule at defined interval.
func (s *Scheduler) ScheduleJob(ctx context.Context, job Job, synchronous bool) error {
	if !job.ShouldEnable(ctx) {
		return fmt.Errorf("ShouldEnable() returned false, cannot schedule job %s", job.ID())
	}

	logger.Infof("Scheduling job: %s", job.ID())

	interval, startNow := job.Interval()
	if err := s.jobInit(job.ID(), interval, s.getFunc(ctx, job), startNow, synchronous); err != nil {
		return err
	}

	return nil
}

func (s *Scheduler) setEntryID(jobID string, entryID cron.EntryID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[jobID] = entryID
}

// jobInit adds job to the schedule to run at specified interval.
// Setting startImmediately to true executes first run immediately, otherwise
// first run will be after interval (at now+interval).
// If startImmediately and synchronous both are true, init method will block
// until job is completed.
func (s *Scheduler) jobInit(jobID string, interval time.Duration, job func(), startImmediately, synchronous bool) error {
	logger.Infof("Scheduling job %q to run at %f hr interval", jobID, interval.Hours())

	_, found := s.jobs[jobID]
	// If found, job is already running, return.
	if found {
		logger.Infof("Skipping, job %q is already scheduled", jobID)
		return nil
	}

	entry, err := s.cron.AddFunc(fmt.Sprintf("@every %ds", int(interval.Seconds())), job)
	if err != nil {
		return fmt.Errorf("unable to schedule %q: %w", jobID, err)
	}
	s.setEntryID(jobID, entry)

	if startImmediately {
		if synchronous {
			job()
		} else {
			// Start job in a go routine to not block the caller.
			go job()
		}
	}

	return nil
}

// UnscheduleJob removes the job from schedule.
func (s *Scheduler) UnscheduleJob(jobID string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logger.Infof("Unscheduling job %q", jobID)

	entry, found := s.jobs[jobID]
	if found {
		s.cron.Remove(entry)
		delete(s.jobs, jobID)
	}
}

// start begins executing each job at defined interval.
func (s *Scheduler) start() {
	logger.Infof("Starting the scheduler to run jobs")
	s.cron.Start()
}

// Stop stops executing new jobs.
func (s *Scheduler) Stop() {
	logger.Infof("Stopping the scheduler")
	s.cron.Stop()
}

// ScheduleJobs schedules required jobs and waits for it to finish if synchronous is true.
func ScheduleJobs(ctx context.Context, jobs []Job, synchronous bool) {
	wg := sync.WaitGroup{}
	sched := Get()
	var ids []string

	for _, job := range jobs {
		wg.Add(1)
		ids = append(ids, job.ID())
		go func(job Job) {
			defer wg.Done()
			if err := sched.ScheduleJob(ctx, job, synchronous); err != nil {
				logger.Errorf("Failed to schedule job %s with error: %v", job.ID(), err)
			} else {
				logger.Infof("Successfully scheduled job %s", job.ID())
			}
		}(job)
	}

	if synchronous {
		logger.Debugf("Waiting for %v to finish...", ids)
		wg.Wait()
	}
}
