package ops

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// maxScheduleLookbackDays bounds slot searches; a weekly schedule needs eight.
const maxScheduleLookbackDays = 400

// Schedule is when a machine-local timer is expected to start a job. Ops only
// uses it to judge freshness; it never starts anything.
type Schedule struct {
	location *time.Location
	times    []clockTime
	weekdays []time.Weekday
	grace    time.Duration
}

type clockTime struct{ hour, minute int }

func (c clockTime) String() string { return fmt.Sprintf("%02d:%02d", c.hour, c.minute) }

// NewSchedule builds a schedule from local HH:MM start times, optional
// lowercase three-letter weekdays (empty means daily) and the time after a
// start within which a run must have begun before the job is overdue.
func NewSchedule(timezone string, times []string, weekdays []string, grace time.Duration) (*Schedule, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil || timezone == "" {
		return nil, validationFailed("schedule.timezone %q is not a known IANA timezone", timezone)
	}
	if len(times) == 0 {
		return nil, validationFailed("schedule.times needs at least one HH:MM start time")
	}
	if grace <= 0 {
		return nil, validationFailed("schedule.grace must be positive")
	}
	schedule := &Schedule{location: location, grace: grace}
	for _, value := range times {
		parsed, err := time.Parse("15:04", value)
		if err != nil {
			return nil, validationFailed("schedule.times entry %q must be HH:MM", value)
		}
		schedule.times = append(schedule.times, clockTime{hour: parsed.Hour(), minute: parsed.Minute()})
	}
	slices.SortFunc(schedule.times, func(a, b clockTime) int { return (a.hour*60 + a.minute) - (b.hour*60 + b.minute) })
	for _, value := range weekdays {
		day, ok := parseWeekday(value)
		if !ok {
			return nil, validationFailed("schedule.weekdays entry %q must be one of sun mon tue wed thu fri sat", value)
		}
		if !slices.Contains(schedule.weekdays, day) {
			schedule.weekdays = append(schedule.weekdays, day)
		}
	}
	slices.Sort(schedule.weekdays)
	return schedule, nil
}

func parseWeekday(value string) (time.Weekday, bool) {
	for day := time.Sunday; day <= time.Saturday; day++ {
		if strings.EqualFold(value, day.String()[:3]) {
			return day, true
		}
	}
	return time.Sunday, false
}

func (s *Schedule) Timezone() string     { return s.location.String() }
func (s *Schedule) Grace() time.Duration { return s.grace }

func (s *Schedule) Times() []string {
	values := make([]string, 0, len(s.times))
	for _, value := range s.times {
		values = append(values, value.String())
	}
	return values
}

func (s *Schedule) Weekdays() []string {
	values := make([]string, 0, len(s.weekdays))
	for _, day := range s.weekdays {
		values = append(values, strings.ToLower(day.String()[:3]))
	}
	return values
}

func (s *Schedule) runsOn(day time.Weekday) bool {
	return len(s.weekdays) == 0 || slices.Contains(s.weekdays, day)
}

// PreviousSlot returns the latest scheduled start at or before at.
func (s *Schedule) PreviousSlot(at time.Time) (time.Time, bool) {
	local := at.In(s.location)
	for offset := 0; offset <= maxScheduleLookbackDays; offset++ {
		day := time.Date(local.Year(), local.Month(), local.Day()-offset, 0, 0, 0, 0, s.location)
		if !s.runsOn(day.Weekday()) {
			continue
		}
		for index := len(s.times) - 1; index >= 0; index-- {
			slot := time.Date(day.Year(), day.Month(), day.Day(), s.times[index].hour, s.times[index].minute, 0, 0, s.location)
			if !slot.After(at) {
				return slot.UTC(), true
			}
		}
	}
	return time.Time{}, false
}

// NextSlot returns the earliest scheduled start strictly after at.
func (s *Schedule) NextSlot(at time.Time) (time.Time, bool) {
	local := at.In(s.location)
	for offset := 0; offset <= maxScheduleLookbackDays; offset++ {
		day := time.Date(local.Year(), local.Month(), local.Day()+offset, 0, 0, 0, 0, s.location)
		if !s.runsOn(day.Weekday()) {
			continue
		}
		for _, value := range s.times {
			slot := time.Date(day.Year(), day.Month(), day.Day(), value.hour, value.minute, 0, 0, s.location)
			if slot.After(at) {
				return slot.UTC(), true
			}
		}
	}
	return time.Time{}, false
}

// JobRuns is the run history needed to judge one job.
type JobRuns struct {
	// LastAttempt is the newest run of any state, including skipped and running.
	LastAttempt *Run
	// LastStarted is the newest run that was not skipped.
	LastStarted *Run
	// LastFinished is the newest terminal run that was not skipped.
	LastFinished *Run
	// LastSuccess is the newest complete run: the last good restore point.
	LastSuccess *Run
	// LastArchive is the newest run that reported an archive, whatever its state.
	LastArchive      *Run
	LastVerification *Verification
	LastRestoreTest  *Verification
}

// JobView is a job with its computed condition at one instant.
type JobView struct {
	Job          *Job
	Runs         JobRuns
	Status       JobStatus
	StatusReason string
	Freshness    Freshness
	CurrentRun   *Run
	Heartbeat    *HeartbeatState
	ExpectedBy   *time.Time
	NextDueAt    *time.Time
	EvaluatedAt  time.Time
}

// EvaluateJob decides a job's status. Lack of evidence is never success: an
// unreported, overdue or silent job is shown as such, and a failure keeps the
// earlier restore point visible in Runs.LastSuccess.
func EvaluateJob(job *Job, runs JobRuns, now time.Time) JobView {
	view := JobView{Job: job, Runs: runs, Freshness: FreshnessUnscheduled, EvaluatedAt: now}
	if runs.LastAttempt != nil && !runs.LastAttempt.IsTerminal() {
		view.CurrentRun = runs.LastAttempt
		heartbeat := HeartbeatFresh
		if now.Sub(runs.LastAttempt.UpdatedAt) > job.HeartbeatStaleAfter {
			heartbeat = HeartbeatStale
		}
		view.Heartbeat = &heartbeat
	}
	if job.Schedule != nil {
		view.Freshness = FreshnessFresh
		if next, ok := job.Schedule.NextSlot(now); ok {
			view.NextDueAt = &next
		}
		if expected, ok := job.Schedule.PreviousSlot(now.Add(-job.Schedule.Grace())); ok {
			deadline := expected.Add(job.Schedule.Grace())
			view.ExpectedBy = &deadline
			if !coversSlot(job.Schedule, expected, runs.LastStarted) {
				view.Freshness = FreshnessOverdue
			}
		}
	}
	view.Status, view.StatusReason = rollUp(job, &view, now)
	return view
}

// coversSlot reports whether a run began after the previous slot's window
// closed, so one early or manual run still counts for the slot it precedes.
func coversSlot(schedule *Schedule, slot time.Time, run *Run) bool {
	if run == nil {
		return false
	}
	windowOpen := time.Time{}
	if prior, ok := schedule.PreviousSlot(slot.Add(-time.Nanosecond)); ok {
		windowOpen = prior.Add(schedule.Grace())
		if windowOpen.After(slot) {
			windowOpen = slot
		}
	}
	return !run.StartedAt.Before(windowOpen)
}

func rollUp(job *Job, view *JobView, now time.Time) (JobStatus, string) {
	runs := view.Runs
	switch {
	case !job.Enabled:
		return StatusDisabled, "job is disabled; freshness is not judged"
	case runs.LastAttempt == nil:
		return StatusNeverReported, "no run has been reported for this job"
	case view.Heartbeat != nil && *view.Heartbeat == HeartbeatStale:
		age := now.Sub(view.CurrentRun.UpdatedAt).Round(time.Second)
		return StatusUnknown, fmt.Sprintf("run %s stopped reporting %s ago without finishing", view.CurrentRun.RunID, age)
	case runs.LastFinished != nil && runs.LastFinished.State == RunFailed:
		return StatusFailed, fmt.Sprintf("latest finished run %s failed", runs.LastFinished.RunID)
	case view.Freshness == FreshnessOverdue:
		return StatusOverdue, fmt.Sprintf("no run started for the slot due by %s", view.ExpectedBy.Format(time.RFC3339))
	case runs.LastFinished != nil && runs.LastFinished.State == RunWarning:
		return StatusWarning, fmt.Sprintf("latest finished run %s finished with warnings", runs.LastFinished.RunID)
	case view.CurrentRun != nil:
		return StatusRunning, fmt.Sprintf("run %s is in progress", view.CurrentRun.RunID)
	case runs.LastFinished == nil:
		return StatusUnknown, "only skipped runs have been reported"
	}
	return StatusOK, fmt.Sprintf("latest finished run %s succeeded", runs.LastFinished.RunID)
}

// ReportOutcome says what a run report did to the stored run.
type ReportOutcome string

const (
	OutcomeCreated         ReportOutcome = "created"
	OutcomeUpdated         ReportOutcome = "updated"
	OutcomeDuplicate       ReportOutcome = "duplicate"
	OutcomeIgnoredTerminal ReportOutcome = "ignored_terminal"
	OutcomeIgnoredStale    ReportOutcome = "ignored_stale"
)

// DecideReport applies a run report to the stored run. Reports are idempotent
// by job and run ID: a finished run is never reopened by a late heartbeat, and
// an older copy (a spool replay) never overwrites a newer one.
func DecideReport(existing *Run, incoming *Run) ReportOutcome {
	switch {
	case existing == nil:
		return OutcomeCreated
	case existing.IsTerminal() && !incoming.IsTerminal():
		return OutcomeIgnoredTerminal
	case !existing.IsTerminal() && incoming.IsTerminal():
		return OutcomeUpdated
	case incoming.UpdatedAt.Before(existing.UpdatedAt):
		return OutcomeIgnoredStale
	case incoming.UpdatedAt.Equal(existing.UpdatedAt) && incoming.IsTerminal() == existing.IsTerminal():
		return OutcomeDuplicate
	}
	return OutcomeUpdated
}
