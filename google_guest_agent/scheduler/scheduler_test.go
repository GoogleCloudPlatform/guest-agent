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

package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

type testJob struct {
	interval     time.Duration
	shouldEnable bool
	startingNow  bool
	id           string
	ctr          int
	stopAfter    int
}

func (j *testJob) Run(_ context.Context) (bool, error) {
	j.ctr++
	if j.ctr == j.stopAfter {
		return false, nil
	}
	return true, nil
}

func (j *testJob) ID() string {
	return j.id
}

func (j *testJob) Interval() (time.Duration, bool) {
	return j.interval, j.startingNow
}

func (j *testJob) ShouldEnable(_ context.Context) bool {
	return j.shouldEnable
}

func TestSchedule(t *testing.T) {
	job := &testJob{
		interval:     time.Second / 2,
		id:           "test_job",
		shouldEnable: true,
		startingNow:  true,
		ctr:          0,
	}

	s := Scheduler{
		cron: cron.New(),
		jobs: make(map[string]cron.EntryID),
		mu:   sync.Mutex{},
	}

	if err := s.ScheduleJob(context.Background(), job); err != nil {
		t.Errorf("AddJob(%s) failed unexecptedly with error: %v", job.ID(), err)
	}

	s.start()

	if _, ok := s.jobs[job.ID()]; !ok {
		t.Errorf("Failed to schedule %s, expected an entry in scheduled jobs", job.ID())
	}

	time.Sleep(3 * time.Second)
	s.Stop()
	if job.ctr < 4 {
		t.Errorf("Scheduler failed to schedule job, counter value found %d, expcted atleast 3", job.ctr)
	}
}

func TestMultipleSchedules(t *testing.T) {
	ctx := context.Background()
	job1 := &testJob{
		interval:     time.Second / 2,
		id:           "test_job1",
		shouldEnable: true,
		startingNow:  true,
		ctr:          0,
	}

	job2 := &testJob{
		interval:     time.Second / 2,
		id:           "test_job2",
		shouldEnable: true,
		startingNow:  true,
		ctr:          0,
	}

	s := Scheduler{
		cron: cron.New(),
		jobs: make(map[string]cron.EntryID),
		mu:   sync.Mutex{},
	}
	defer s.Stop()

	// Schedule multiple jobs.
	if err := s.ScheduleJob(ctx, job1); err != nil {
		t.Errorf("AddJob(%s) failed unexecptedly with error: %v", job1.id, err)
	}
	if err := s.ScheduleJob(ctx, job2); err != nil {
		t.Errorf("AddJob(%s) failed unexecptedly with error: %v", job2.id, err)
	}

	s.start()

	time.Sleep(2 * time.Second)
	s.UnscheduleJob(job2.ID())

	if _, ok := s.jobs[job1.ID()]; !ok {
		t.Errorf("Failed to schedule %s, expected an entry in scheduled jobs", job1.ID())
	}
	if _, ok := s.jobs[job2.ID()]; ok {
		t.Errorf("Failed to unschedule %s, found an entry in scheduled jobs", job2.ID())
	}

	time.Sleep(time.Second)
	// Verify job1 is still running and job2 is unscheduled.
	if job1.ctr < 4 {
		t.Errorf("Scheduler failed to schedule job, counter value found %d, expcted atleast 3", job1.ctr)
	}

	if job2.ctr > 3 {
		t.Errorf("Scheduler failed to unschedule job, counter value found %d, expcted less than 3", job2.ctr)
	}
}

func TestStopSchedule(t *testing.T) {
	s := Scheduler{
		cron: cron.New(),
		jobs: make(map[string]cron.EntryID),
		mu:   sync.Mutex{},
	}
	defer s.Stop()
	s.start()

	job := &testJob{
		interval:     time.Second / 2,
		id:           "test_job",
		shouldEnable: true,
		startingNow:  true,
		stopAfter:    2,
		ctr:          0,
	}

	if err := s.ScheduleJob(context.Background(), job); err != nil {
		t.Errorf("AddJob(%s) failed unexecptedly with error: %v", job.ID(), err)
	}

	if _, ok := s.jobs[job.ID()]; !ok {
		t.Errorf("Failed to schedule %s, expected an entry in scheduled jobs", job.ID())
	}

	time.Sleep(3 * time.Second)
	if job.ctr > 3 {
		t.Errorf("Scheduler failed to stop the job, counter value found %d, should have stopped after max 3", job.ctr)
	}
}

func TestAddDefaultJobs(t *testing.T) {
	ctx := context.Background()
	s := Scheduler{
		cron: cron.New(),
		jobs: make(map[string]cron.EntryID),
		mu:   sync.Mutex{},
	}

	jobs := []Job{
		&testJob{
			interval:     time.Second / 2,
			id:           "test_job1",
			shouldEnable: false,
		},
		&testJob{
			interval:     time.Second / 2,
			id:           "test_job2",
			shouldEnable: true,
		},
	}

	if err := s.addDefaultJobs(ctx, jobs); err != nil {
		t.Errorf("addDefaultJobs(ctx, %+v) failed unexpectedly with error: %v", jobs, err)
	}

	for _, job := range jobs {
		if _, ok := s.jobs[job.ID()]; job.ShouldEnable(ctx) != ok {
			t.Errorf("job %s had set shouldEnable: %t, entry found: %t", job.ID(), job.ShouldEnable(ctx), ok)
		}
	}
}
