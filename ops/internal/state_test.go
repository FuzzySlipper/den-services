package ops

import (
	"testing"
	"time"
)

func mustSchedule(t *testing.T, times []string, weekdays []string, grace time.Duration) *Schedule {
	t.Helper()
	schedule, err := NewSchedule("America/Los_Angeles", times, weekdays, grace)
	if err != nil {
		t.Fatalf("NewSchedule: %v", err)
	}
	return schedule
}

func utc(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestScheduleSlotsFollowDaylightSaving(t *testing.T) {
	schedule := mustSchedule(t, []string{"07:00"}, nil, 3*time.Hour)
	tests := []struct {
		name     string
		at       string
		previous string
		next     string
	}{
		{"pdt morning", "2026-10-29T15:00:00Z", "2026-10-29T14:00:00Z", "2026-10-30T14:00:00Z"},
		{"before slot uses yesterday", "2026-10-29T13:59:00Z", "2026-10-28T14:00:00Z", "2026-10-29T14:00:00Z"},
		{"exact slot is previous", "2026-10-29T14:00:00Z", "2026-10-29T14:00:00Z", "2026-10-30T14:00:00Z"},
		{"pst after the change", "2026-11-02T16:00:00Z", "2026-11-02T15:00:00Z", "2026-11-03T15:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous, ok := schedule.PreviousSlot(utc(tt.at))
			if !ok || !previous.Equal(utc(tt.previous)) {
				t.Fatalf("PreviousSlot = %v, %v; want %s", previous, ok, tt.previous)
			}
			next, ok := schedule.NextSlot(utc(tt.at))
			if !ok || !next.Equal(utc(tt.next)) {
				t.Fatalf("NextSlot = %v, %v; want %s", next, ok, tt.next)
			}
		})
	}
}

func TestScheduleWeekdays(t *testing.T) {
	schedule := mustSchedule(t, []string{"09:00"}, []string{"sat"}, 6*time.Hour)
	// Thursday 2026-10-29: the previous Saturday was 2026-10-24.
	previous, _ := schedule.PreviousSlot(utc("2026-10-29T20:00:00Z"))
	if !previous.Equal(utc("2026-10-24T16:00:00Z")) {
		t.Fatalf("PreviousSlot = %v", previous)
	}
	next, _ := schedule.NextSlot(utc("2026-10-29T20:00:00Z"))
	if !next.Equal(utc("2026-10-31T16:00:00Z")) {
		t.Fatalf("NextSlot = %v", next)
	}
}

func TestNewScheduleRejectsBadInput(t *testing.T) {
	cases := []struct {
		timezone string
		times    []string
		weekdays []string
		grace    time.Duration
	}{
		{"Mars/Base", []string{"07:00"}, nil, time.Hour},
		{"", []string{"07:00"}, nil, time.Hour},
		{"UTC", nil, nil, time.Hour},
		{"UTC", []string{"7am"}, nil, time.Hour},
		{"UTC", []string{"07:00"}, []string{"funday"}, time.Hour},
		{"UTC", []string{"07:00"}, nil, 0},
	}
	for _, tc := range cases {
		if _, err := NewSchedule(tc.timezone, tc.times, tc.weekdays, tc.grace); err == nil {
			t.Fatalf("NewSchedule(%+v) succeeded", tc)
		}
	}
}

func finishedRun(id string, state RunState, started string, complete bool) *Run {
	start := utc(started)
	finish := start.Add(10 * time.Minute)
	return &Run{RunID: id, State: state, StartedAt: start, UpdatedAt: finish, FinishedAt: &finish, Complete: complete}
}

func runningRun(id string, started string, heartbeat string) *Run {
	return &Run{RunID: id, State: RunRunning, StartedAt: utc(started), UpdatedAt: utc(heartbeat)}
}

func TestEvaluateJob(t *testing.T) {
	job := &Job{
		JobID: "den-agents-git", Enabled: true, HeartbeatStaleAfter: 15 * time.Minute,
		Schedule: mustSchedule(t, []string{"07:00"}, nil, 3*time.Hour),
	}
	// 2026-10-29 07:00 PDT is 14:00Z; its window closes at 17:00Z.
	todayOK := finishedRun("today", RunSuccess, "2026-10-29T14:00:05Z", true)
	yesterdayOK := finishedRun("yesterday", RunSuccess, "2026-10-28T14:00:05Z", true)
	failedToday := finishedRun("failed", RunFailed, "2026-10-29T14:00:05Z", false)
	warningToday := finishedRun("warn", RunWarning, "2026-10-29T14:00:05Z", false)
	skippedToday := finishedRun("skip", RunSkipped, "2026-10-29T14:00:05Z", false)
	tests := []struct {
		name      string
		job       *Job
		runs      JobRuns
		now       string
		status    JobStatus
		freshness Freshness
	}{
		{"never reported", job, JobRuns{}, "2026-10-29T18:00:00Z", StatusNeverReported, FreshnessOverdue},
		{"fresh success", job, JobRuns{LastAttempt: todayOK, LastStarted: todayOK, LastFinished: todayOK, LastSuccess: todayOK}, "2026-10-29T18:00:00Z", StatusOK, FreshnessFresh},
		{"yesterday still fresh inside today's window", job, JobRuns{LastAttempt: yesterdayOK, LastStarted: yesterdayOK, LastFinished: yesterdayOK, LastSuccess: yesterdayOK}, "2026-10-29T16:00:00Z", StatusOK, FreshnessFresh},
		{"yesterday overdue after window", job, JobRuns{LastAttempt: yesterdayOK, LastStarted: yesterdayOK, LastFinished: yesterdayOK, LastSuccess: yesterdayOK}, "2026-10-29T17:30:00Z", StatusOverdue, FreshnessOverdue},
		{"failure keeps prior success", job, JobRuns{LastAttempt: failedToday, LastStarted: failedToday, LastFinished: failedToday, LastSuccess: yesterdayOK}, "2026-10-29T18:00:00Z", StatusFailed, FreshnessFresh},
		{"warning is not success", job, JobRuns{LastAttempt: warningToday, LastStarted: warningToday, LastFinished: warningToday, LastSuccess: yesterdayOK}, "2026-10-29T18:00:00Z", StatusWarning, FreshnessFresh},
		{"skipped morning stays visible and overdue", job, JobRuns{LastAttempt: skippedToday, LastStarted: yesterdayOK, LastFinished: yesterdayOK, LastSuccess: yesterdayOK}, "2026-10-29T18:00:00Z", StatusOverdue, FreshnessOverdue},
		{"running with fresh heartbeat", job, JobRuns{LastAttempt: runningRun("now", "2026-10-29T14:00:05Z", "2026-10-29T14:20:00Z"), LastStarted: runningRun("now", "2026-10-29T14:00:05Z", "2026-10-29T14:20:00Z"), LastFinished: yesterdayOK, LastSuccess: yesterdayOK}, "2026-10-29T14:25:00Z", StatusRunning, FreshnessFresh},
		{"silent running run is unknown", job, JobRuns{LastAttempt: runningRun("now", "2026-10-29T14:00:05Z", "2026-10-29T14:20:00Z"), LastStarted: runningRun("now", "2026-10-29T14:00:05Z", "2026-10-29T14:20:00Z"), LastFinished: yesterdayOK, LastSuccess: yesterdayOK}, "2026-10-29T15:00:00Z", StatusUnknown, FreshnessFresh},
		{"disabled", &Job{JobID: "off", Enabled: false, HeartbeatStaleAfter: time.Minute}, JobRuns{}, "2026-10-29T18:00:00Z", StatusDisabled, FreshnessUnscheduled},
		{"unscheduled only skipped", &Job{JobID: "manual", Enabled: true, HeartbeatStaleAfter: time.Minute}, JobRuns{LastAttempt: skippedToday}, "2026-10-29T18:00:00Z", StatusUnknown, FreshnessUnscheduled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := EvaluateJob(tt.job, tt.runs, utc(tt.now))
			if view.Status != tt.status || view.Freshness != tt.freshness {
				t.Fatalf("status/freshness = %s/%s (%s); want %s/%s", view.Status, view.Freshness, view.StatusReason, tt.status, tt.freshness)
			}
			if tt.runs.LastSuccess != nil && view.Runs.LastSuccess != tt.runs.LastSuccess {
				t.Fatalf("last success was replaced")
			}
		})
	}
}

func TestEarlyManualRunCoversNextSlot(t *testing.T) {
	job := &Job{JobID: "j", Enabled: true, HeartbeatStaleAfter: time.Minute, Schedule: mustSchedule(t, []string{"07:00"}, nil, 3*time.Hour)}
	// A manual run at 21:20 PDT the evening before (04:20Z) counts for the next morning.
	manual := finishedRun("manual", RunSuccess, "2026-10-29T04:20:37Z", true)
	view := EvaluateJob(job, JobRuns{LastAttempt: manual, LastStarted: manual, LastFinished: manual, LastSuccess: manual}, utc("2026-10-29T18:00:00Z"))
	if view.Status != StatusOK {
		t.Fatalf("status = %s (%s)", view.Status, view.StatusReason)
	}
	if view.NextDueAt == nil || !view.NextDueAt.Equal(utc("2026-10-30T14:00:00Z")) {
		t.Fatalf("next due = %v", view.NextDueAt)
	}
}

func TestDecideReport(t *testing.T) {
	heartbeat := runningRun("r", "2026-10-29T14:00:00Z", "2026-10-29T14:05:00Z")
	laterHeartbeat := runningRun("r", "2026-10-29T14:00:00Z", "2026-10-29T14:06:00Z")
	earlierHeartbeat := runningRun("r", "2026-10-29T14:00:00Z", "2026-10-29T14:04:00Z")
	finished := finishedRun("r", RunSuccess, "2026-10-29T14:00:00Z", true)
	correction := finishedRun("r", RunWarning, "2026-10-29T14:00:00Z", true)
	correction.UpdatedAt = correction.UpdatedAt.Add(time.Minute)
	tests := []struct {
		name     string
		existing *Run
		incoming *Run
		want     ReportOutcome
	}{
		{"first report", nil, heartbeat, OutcomeCreated},
		{"newer heartbeat", heartbeat, laterHeartbeat, OutcomeUpdated},
		{"replayed older heartbeat", heartbeat, earlierHeartbeat, OutcomeIgnoredStale},
		{"same heartbeat twice", heartbeat, heartbeat, OutcomeDuplicate},
		{"terminal closes a running run", laterHeartbeat, finished, OutcomeUpdated},
		{"late heartbeat cannot revive", finished, laterHeartbeat, OutcomeIgnoredTerminal},
		{"duplicate terminal delivery", finished, finished, OutcomeDuplicate},
		{"newer terminal correction", finished, correction, OutcomeUpdated},
		{"older terminal replay", correction, finished, OutcomeIgnoredStale},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecideReport(tt.existing, tt.incoming); got != tt.want {
				t.Fatalf("DecideReport = %s; want %s", got, tt.want)
			}
		})
	}
}
