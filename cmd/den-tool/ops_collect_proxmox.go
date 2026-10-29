package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Proxmox logs local times in the node's zone.
)

// Proxmox runs vzdump on its own schedule. This source reads what Proxmox
// already records: the backup jobs, the guests, each vzdump task's log and the
// backup storage inventory. Every guest becomes a host in Ops; a guest in an
// enabled backup job gets a job of its own, so a guest left out of every job
// shows as "not configured" instead of disappearing.

const (
	proxmoxDefaultTaskLimit = 30
	proxmoxDoneMarker       = "proxmox-task "
	proxmoxLogLineLimit     = 20000
	proxmoxHeartbeatStale   = "30m"
	proxmoxScheduleGrace    = "2h"
)

var (
	vzdumpGuestStart   = regexp.MustCompile(`^INFO: Starting Backup of VM (\d+) \((qemu|lxc)\)`)
	vzdumpGuestEnd     = regexp.MustCompile(`^INFO: Finished Backup of VM (\d+) \((\d+):(\d\d):(\d\d)\)`)
	vzdumpGuestError   = regexp.MustCompile(`^ERROR: Backup of VM (\d+) failed - (.*)`)
	vzdumpStartedAt    = regexp.MustCompile(`^INFO: Backup started at (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)`)
	vzdumpFinishedAt   = regexp.MustCompile(`^INFO: Backup finished at (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d)`)
	vzdumpArchive      = regexp.MustCompile(`^INFO: creating (?:vzdump )?archive '([^']+)'`)
	vzdumpBytesWritten = regexp.MustCompile(`^INFO: Total bytes written: (\d+)`)
	vzdumpTransferred  = regexp.MustCompile(`^INFO: transferred ([\d.]+) (B|KiB|MiB|GiB|TiB) in`)
	proxmoxTime        = regexp.MustCompile(`^(\d{1,2})((?:,\d{1,2})*):(\d\d)$`)
	hostSlugInvalid    = regexp.MustCompile(`[^a-z0-9._-]+`)
)

type proxmoxBackupJob struct {
	ID           string `json:"id"`
	Schedule     string `json:"schedule"`
	Enabled      any    `json:"enabled"`
	VMID         string `json:"vmid"`
	All          any    `json:"all"`
	Exclude      string `json:"exclude"`
	Pool         string `json:"pool"`
	Node         string `json:"node"`
	Storage      string `json:"storage"`
	Mode         string `json:"mode"`
	PruneBackups any    `json:"prune-backups"`
	RepeatMissed any    `json:"repeat-missed"`
}

type proxmoxGuest struct {
	VMID     int    `json:"vmid"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Node     string `json:"node"`
	Status   string `json:"status"`
	Template any    `json:"template"`
}

type proxmoxTask struct {
	UPID      string `json:"upid"`
	Status    string `json:"status"`
	StartTime int64  `json:"starttime"`
	EndTime   int64  `json:"endtime"`
	ID        string `json:"id"`
}

type proxmoxStorage struct {
	Storage      string `json:"storage"`
	Type         string `json:"type"`
	Server       string `json:"server"`
	Export       string `json:"export"`
	Path         string `json:"path"`
	PruneBackups string `json:"prune-backups"`
}

type proxmoxContent struct {
	VolID string `json:"volid"`
	Size  int64  `json:"size"`
	CTime int64  `json:"ctime"`
	VMID  int    `json:"vmid"`
}

type proxmoxInventory struct {
	Jobs     []proxmoxBackupJob `json:"jobs"`
	Guests   []proxmoxGuest     `json:"guests"`
	Tasks    []proxmoxTask      `json:"tasks"`
	Storages []proxmoxStorage   `json:"storages"`
	Timezone string             `json:"timezone"`
}

type proxmoxLogLine struct {
	Text string `json:"t"`
}

func collectProxmox(ctx context.Context, source opsCollectSource, env opsCollectEnv, staticHosts map[string]json.RawMessage) ([]opsCollectWrite, error) {
	limit := source.TaskLimit
	if limit <= 0 {
		limit = proxmoxDefaultTaskLimit
	}
	node := shellQuote(source.Node)
	output, err := env.remote(ctx, source.SSH, pvesh(map[string]string{
		"jobs":     "pvesh get /cluster/backup --output-format json",
		"guests":   "pvesh get /cluster/resources --type vm --output-format json",
		"tasks":    fmt.Sprintf("pvesh get /nodes/%s/tasks --typefilter vzdump --limit %d --output-format json", node, limit),
		"storages": "pvesh get /storage --output-format json",
		// The node's time API reports no zone name, so ask systemd.
		"timezone": `timedatectl show -p Timezone --value | sed 's/.*/"&"/'`,
	}))
	if err != nil {
		return nil, err
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(output, &members); err != nil {
		return nil, fmt.Errorf("parse proxmox inventory: %w", err)
	}
	// Coverage is only judged from a complete inventory: a failed read must
	// never look like "no backup jobs" or "no guests".
	for _, key := range []string{"jobs", "guests", "tasks", "storages"} {
		if raw := members[key]; len(raw) == 0 || string(raw) == "null" {
			return nil, fmt.Errorf("proxmox inventory incomplete: %s could not be read; coverage left unchanged", key)
		}
	}
	var inventory proxmoxInventory
	if err := json.Unmarshal(output, &inventory); err != nil {
		return nil, fmt.Errorf("parse proxmox inventory: %w", err)
	}
	location, err := time.LoadLocation(firstNonEmpty(inventory.Timezone, "UTC"))
	if err != nil {
		return nil, fmt.Errorf("proxmox timezone: %w", err)
	}
	guests := map[int]proxmoxGuest{}
	for _, guest := range inventory.Guests {
		if guest.Node == source.Node && !truthy(guest.Template) {
			guests[guest.VMID] = guest
		}
	}
	jobs := enabledProxmoxJobs(inventory.Jobs, source.Node)
	details := map[string]string{}
	for _, vmid := range sortedVMIDs(guests) {
		guest := guests[vmid]
		details["config-"+strconv.Itoa(vmid)] = fmt.Sprintf("pvesh get /nodes/%s/%s/%d/config --output-format json", node, guest.Type, vmid)
	}
	var pendingTasks []proxmoxTask
	for _, task := range inventory.Tasks {
		if task.EndTime > 0 && env.cache.isDone(proxmoxDoneMarker+task.UPID) {
			continue
		}
		if env.now.Sub(time.Unix(task.StartTime, 0)) > env.lookback {
			continue
		}
		pendingTasks = append(pendingTasks, task)
		details["log-"+strconv.Itoa(len(pendingTasks)-1)] = fmt.Sprintf("pvesh get /nodes/%s/tasks/%s/log --limit %d --output-format json", node, shellQuote(task.UPID), proxmoxLogLineLimit)
	}
	for _, storage := range jobStorages(jobs) {
		details["content-"+storage] = fmt.Sprintf("pvesh get /nodes/%s/storage/%s/content --content backup --output-format json", node, shellQuote(storage))
	}
	output, err = env.remote(ctx, source.SSH, pvesh(details))
	if err != nil {
		return nil, err
	}
	var detail map[string]json.RawMessage
	if err := json.Unmarshal(output, &detail); err != nil {
		return nil, fmt.Errorf("parse proxmox details: %w", err)
	}
	var problems []string
	configs := map[int]map[string]any{}
	for _, vmid := range sortedVMIDs(guests) {
		var config map[string]any
		if json.Unmarshal(detail["config-"+strconv.Itoa(vmid)], &config) != nil || config == nil {
			problems = append(problems, fmt.Sprintf("config of guest %d could not be read; its disk exclusions are reported as unknown", vmid))
			continue
		}
		configs[vmid] = config
	}
	archives := map[string]proxmoxContent{}
	for _, storage := range jobStorages(jobs) {
		var content []proxmoxContent
		_ = json.Unmarshal(detail["content-"+storage], &content)
		for _, item := range content {
			archives[path.Base(item.VolID)] = item
		}
	}
	storages := map[string]proxmoxStorage{}
	for _, storage := range inventory.Storages {
		storages[storage.Storage] = storage
	}

	plan := planProxmoxCoverage(source, guests, configs, jobs, storages, location.String())
	var writes []opsCollectWrite
	for _, hostID := range sortedKeys(plan.hosts) {
		if _, static := staticHosts[hostID]; !static {
			writes = append(writes, opsCollectWrite{Path: opsPath("hosts", hostID), Body: mustJSON(plan.hosts[hostID])})
		}
	}
	for _, jobID := range sortedKeys(plan.jobs) {
		writes = append(writes, opsCollectWrite{Path: opsPath("jobs", jobID), Body: mustJSON(plan.jobs[jobID])})
	}
	retired, err := retireProxmoxJobs(ctx, source, env, plan)
	if err != nil {
		problems = append(problems, err.Error())
	}
	writes = append(writes, retired...)
	for index, task := range pendingTasks {
		var lines []proxmoxLogLine
		if err := json.Unmarshal(detail["log-"+strconv.Itoa(index)], &lines); err != nil || len(lines) == 0 {
			problems = append(problems, "no log for task "+task.UPID)
			continue
		}
		runs := parseVzdumpTask(source, task, lines, location, guests, archives, env.now)
		for _, run := range runs {
			vmid := run["vmid"].(int)
			jobID := proxmoxJobID(source.HostID, vmid)
			marker := ""
			if task.EndTime > 0 {
				marker = proxmoxDoneMarker + task.UPID
			}
			writes = append(writes, opsCollectWrite{Path: opsPath("jobs", jobID, "runs", run["run_id"].(string)), Body: mustJSON(run), Done: marker})
		}
		if len(runs) == 0 && task.EndTime > 0 {
			env.cache.markDone(proxmoxDoneMarker+task.UPID, env.now)
		}
	}
	return writes, joinProblems(problems)
}

// pvesh builds one remote script printing a JSON object whose members are the
// output of each command, or null when a command fails.
func pvesh(commands map[string]string) string {
	var script strings.Builder
	script.WriteString("printf '{'")
	for index, key := range sortedKeys(commands) {
		separator := ""
		if index > 0 {
			separator = ","
		}
		fmt.Fprintf(&script, "; printf '%s\"%s\":'; out=$(%s 2>/dev/null) && [ -n \"$out\" ] && printf '%%s' \"$out\" || printf null", separator, key, commands[key])
	}
	script.WriteString("; printf '}'")
	return script.String()
}

type proxmoxPlan struct {
	hosts map[string]map[string]any
	jobs  map[string]map[string]any
}

func planProxmoxCoverage(source opsCollectSource, guests map[int]proxmoxGuest, configs map[int]map[string]any, jobs []proxmoxBackupJob, storages map[string]proxmoxStorage, timezone string) proxmoxPlan {
	plan := proxmoxPlan{hosts: map[string]map[string]any{}, jobs: map[string]map[string]any{}}
	covered := map[int][]string{}
	for _, job := range jobs {
		for _, vmid := range jobMembers(job, guests) {
			guest, known := guests[vmid]
			if !known {
				continue
			}
			covered[vmid] = append(covered[vmid], job.ID)
			kind := map[string]string{"qemu": "VM", "lxc": "CT"}[guest.Type]
			storage := storages[job.Storage]
			body := map[string]any{
				"host_id":               hostSlug(guest.Name),
				"display_name":          fmt.Sprintf("%s Proxmox backup", guest.Name),
				"engine":                "vzdump",
				"kind":                  "backup",
				"source":                fmt.Sprintf("%s (node %s) %s %d", source.HostID, source.Node, kind, vmid),
				"scope":                 guestScope(guest, configs[vmid], firstNonEmpty(job.Mode, "snapshot")),
				"target":                storageTarget(job.Storage, storage),
				"heartbeat_stale_after": proxmoxHeartbeatStale,
				"retention":             retentionText(job, storage),
				"recovery_pointer":      fmt.Sprintf("%s dump archives; restore to a new ID with %s", storageTarget(job.Storage, storage), map[string]string{"qemu": "qmrestore", "lxc": "pct restore"}[guest.Type]),
				"notes":                 fmt.Sprintf("Proxmox backup job %s on %s; schedule %q.", job.ID, source.HostID, job.Schedule),
				"enabled":               true,
			}
			if schedule, ok := proxmoxSchedule(job.Schedule); ok {
				schedule["timezone"] = timezone
				body["schedule"] = schedule
			} else {
				body["notes"] = fmt.Sprintf("%v Ops cannot read this schedule, so freshness is not judged.", body["notes"])
			}
			if !truthy(job.RepeatMissed) {
				body["notes"] = fmt.Sprintf("%v A missed slot is not caught up (repeat-missed 0).", body["notes"])
			}
			plan.jobs[proxmoxJobID(source.HostID, vmid)] = body
		}
	}
	for _, vmid := range sortedVMIDs(guests) {
		guest := guests[vmid]
		kind := map[string]string{"qemu": "VM", "lxc": "CT"}[guest.Type]
		notes := fmt.Sprintf("Proxmox %s %d on %s, %s.", kind, vmid, source.HostID, guest.Status)
		if jobsFor := covered[vmid]; len(jobsFor) > 0 {
			notes += " Guest backup job: " + strings.Join(jobsFor, ", ") + "."
		} else {
			notes += " Not in any enabled Proxmox backup job."
		}
		if configs[vmid] == nil {
			notes += " Disk exclusions unknown: the guest config could not be read."
		} else if excluded := excludedVolumes(guest, configs[vmid]); len(excluded) > 0 {
			notes += " Not in guest backups: " + strings.Join(excluded, "; ") + "."
		}
		plan.hosts[hostSlug(guest.Name)] = map[string]any{
			"display_name": guest.Name,
			"role":         fmt.Sprintf("Proxmox %s %d on %s", kind, vmid, source.HostID),
			"notes":        notes,
		}
	}
	return plan
}

// parseVzdumpTask splits one vzdump task log into a run per guest.
func parseVzdumpTask(source opsCollectSource, task proxmoxTask, lines []proxmoxLogLine, location *time.Location, guests map[int]proxmoxGuest, archives map[string]proxmoxContent, now time.Time) []map[string]any {
	var runs []map[string]any
	var current map[string]any
	var warnings []string
	taskStart := time.Unix(task.StartTime, 0).UTC()
	finish := func(state string, finishedAt time.Time) {
		if current == nil {
			return
		}
		if state == "success" && len(warnings) > 0 {
			state = "warning"
		}
		if _, ok := current["error"]; state == "failed" && !ok {
			current["error"] = "the task log has no result for this guest"
		}
		current["state"] = state
		current["warnings"] = warnings
		if state != "running" {
			current["finished_at"] = finishedAt
			current["updated_at"] = finishedAt
		}
		runs = append(runs, current)
		current, warnings = nil, nil
	}
	lastAt := taskStart
	for _, line := range lines {
		text := line.Text
		if match := vzdumpGuestStart.FindStringSubmatch(text); match != nil {
			finish("failed", lastAt)
			vmid, _ := strconv.Atoi(match[1])
			current = map[string]any{
				"vmid": vmid, "run_id": strings.TrimSuffix(task.UPID, ":") + ":" + match[1],
				"host": hostSlug(guests[vmid].Name), "engine": "vzdump", "runner": "proxmox", "kind": "backup",
				"started_at": lastAt, "upid": task.UPID, "proxmox_host": source.HostID,
			}
			warnings = []string{}
			continue
		}
		// "Backup finished at" follows the guest's result line, so it sets
		// the finish time of the run just closed.
		if match := vzdumpFinishedAt.FindStringSubmatch(text); match != nil {
			if at, ok := proxmoxLocalTime(match[1], location); ok {
				lastAt = at
				if current == nil && len(runs) > 0 && runs[len(runs)-1]["state"] != "running" {
					runs[len(runs)-1]["finished_at"], runs[len(runs)-1]["updated_at"] = at, at
				}
			}
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case vzdumpStartedAt.MatchString(text):
			if at, ok := proxmoxLocalTime(vzdumpStartedAt.FindStringSubmatch(text)[1], location); ok {
				current["started_at"], lastAt = at, at
			}
		case vzdumpArchive.MatchString(text):
			file := path.Base(vzdumpArchive.FindStringSubmatch(text)[1])
			archive := map[string]any{"name": file, "id": file}
			if content, ok := archives[file]; ok {
				archive["id"] = content.VolID
				current["stats"] = map[string]any{"compressed_size": content.Size}
			}
			current["archive"] = archive
			current["recovery_pointer"] = archive["id"]
		case vzdumpBytesWritten.MatchString(text):
			setOriginalSize(current, vzdumpBytesWritten.FindStringSubmatch(text)[1], "B")
		case vzdumpTransferred.MatchString(text):
			match := vzdumpTransferred.FindStringSubmatch(text)
			setOriginalSize(current, match[1], match[2])
		case strings.HasPrefix(text, "WARN: "):
			warnings = append(warnings, strings.TrimPrefix(text, "WARN: "))
		case vzdumpGuestError.MatchString(text):
			current["error"] = vzdumpGuestError.FindStringSubmatch(text)[2]
			finish("failed", lastAt)
		case vzdumpGuestEnd.MatchString(text):
			match := vzdumpGuestEnd.FindStringSubmatch(text)
			hours, _ := strconv.Atoi(match[2])
			minutes, _ := strconv.Atoi(match[3])
			seconds, _ := strconv.Atoi(match[4])
			started, _ := current["started_at"].(time.Time)
			finish("success", started.Add(time.Duration(hours*3600+minutes*60+seconds)*time.Second))
		}
	}
	if current != nil {
		if task.EndTime == 0 {
			current["updated_at"] = now
			finish("running", now)
		} else {
			current["error"] = "vzdump task ended before this guest finished: " + task.Status
			finish("failed", time.Unix(task.EndTime, 0).UTC())
		}
	}
	return runs
}

func setOriginalSize(run map[string]any, amount string, unit string) {
	value, err := strconv.ParseFloat(amount, 64)
	if err != nil {
		return
	}
	scale := map[string]float64{"B": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40}[unit]
	stats, _ := run["stats"].(map[string]any)
	if stats == nil {
		stats = map[string]any{}
	}
	stats["original_size"] = int64(value * scale)
	run["stats"] = stats
}

func proxmoxLocalTime(value string, location *time.Location) (time.Time, bool) {
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, location)
	return parsed.UTC(), err == nil
}

// proxmoxSchedule reads the simple calendar events Proxmox jobs normally use:
// "6:00", "daily", "sat 02:30", "mon..fri 21:00" or "2,14:30". Anything else
// is left unscheduled rather than guessed.
func proxmoxSchedule(event string) (map[string]any, bool) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(event)))
	weekdays := []string{}
	var clock string
	switch len(fields) {
	case 1:
		clock = fields[0]
	case 2:
		days, ok := proxmoxWeekdays(fields[0])
		if !ok {
			return nil, false
		}
		weekdays, clock = days, fields[1]
	default:
		return nil, false
	}
	if clock == "daily" {
		clock = "0:00"
	}
	match := proxmoxTime.FindStringSubmatch(clock)
	if match == nil {
		return nil, false
	}
	var times []string
	for _, hour := range strings.Split(match[1]+match[2], ",") {
		value, err := strconv.Atoi(hour)
		if err != nil || value > 23 {
			return nil, false
		}
		times = append(times, fmt.Sprintf("%02d:%s", value, match[3]))
	}
	return map[string]any{"times": times, "weekdays": weekdays, "grace": proxmoxScheduleGrace}, true
}

func proxmoxWeekdays(spec string) ([]string, bool) {
	order := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	var days []string
	for _, part := range strings.Split(spec, ",") {
		from, to, isRange := strings.Cut(part, "..")
		start, end := slices.Index(order, from), slices.Index(order, to)
		if !isRange {
			end = start
		}
		if start < 0 || end < start {
			return nil, false
		}
		days = append(days, order[start:end+1]...)
	}
	return days, true
}

func enabledProxmoxJobs(jobs []proxmoxBackupJob, node string) []proxmoxBackupJob {
	var enabled []proxmoxBackupJob
	for _, job := range jobs {
		if job.Enabled != nil && !truthy(job.Enabled) {
			continue
		}
		if job.Node != "" && job.Node != node {
			continue
		}
		enabled = append(enabled, job)
	}
	return enabled
}

func jobMembers(job proxmoxBackupJob, guests map[int]proxmoxGuest) []int {
	var members []int
	if truthy(job.All) {
		excluded := map[int]bool{}
		for _, id := range splitIDs(job.Exclude) {
			excluded[id] = true
		}
		for _, vmid := range sortedVMIDs(guests) {
			if !excluded[vmid] {
				members = append(members, vmid)
			}
		}
		return members
	}
	return splitIDs(job.VMID)
}

func jobStorages(jobs []proxmoxBackupJob) []string {
	var storages []string
	for _, job := range jobs {
		if job.Storage != "" && !slices.Contains(storages, job.Storage) {
			storages = append(storages, job.Storage)
		}
	}
	return storages
}

func guestScope(guest proxmoxGuest, config map[string]any, mode string) string {
	scope := fmt.Sprintf("whole guest, vzdump %s mode", mode)
	if config == nil {
		return scope + "; disk exclusions unknown (the guest config could not be read)"
	}
	if excluded := excludedVolumes(guest, config); len(excluded) > 0 {
		scope += "; excludes " + strings.Join(excluded, ", ")
	}
	return scope
}

// excludedVolumes lists guest disks that vzdump leaves out: VM disks marked
// backup=0, and container mount points that are not marked backup=1 (Proxmox
// skips those by default) or are bind mounts.
func excludedVolumes(guest proxmoxGuest, config map[string]any) []string {
	var excluded []string
	diskKey := regexp.MustCompile(`^(scsi|virtio|sata|ide|efidisk|tpmstate)\d+$`)
	mountKey := regexp.MustCompile(`^mp\d+$`)
	for _, key := range sortedKeys(config) {
		value, _ := config[key].(string)
		switch {
		case guest.Type == "qemu" && diskKey.MatchString(key) && strings.Contains(value, "backup=0"):
			excluded = append(excluded, fmt.Sprintf("%s (backup=0)", key))
		case guest.Type == "lxc" && mountKey.MatchString(key):
			volume, options, _ := strings.Cut(value, ",")
			mountPoint := ""
			for _, option := range strings.Split(options, ",") {
				if after, ok := strings.CutPrefix(option, "mp="); ok {
					mountPoint = after
				}
			}
			if strings.HasPrefix(volume, "/") || !strings.Contains(value, "backup=1") {
				excluded = append(excluded, fmt.Sprintf("%s %s", key, firstNonEmpty(mountPoint, volume)))
			}
		}
	}
	return excluded
}

func storageTarget(name string, storage proxmoxStorage) string {
	switch {
	case storage.Server != "" && storage.Export != "":
		return fmt.Sprintf("%s (%s %s:%s)", name, storage.Type, storage.Server, storage.Export)
	case storage.Path != "":
		return fmt.Sprintf("%s (%s %s)", name, storage.Type, storage.Path)
	default:
		return name
	}
}

func retentionText(job proxmoxBackupJob, storage proxmoxStorage) string {
	policy := storage.PruneBackups
	if text, ok := job.PruneBackups.(string); ok && text != "" {
		policy = text
	}
	switch {
	case policy == "":
		return "Proxmox default (keep-all unless the storage sets prune-backups)"
	case strings.Contains(policy, "keep-all=1"):
		return policy + " (nothing is pruned)"
	default:
		return policy
	}
}

func proxmoxJobID(hostID string, vmid int) string {
	return fmt.Sprintf("%s-vzdump-%d", hostID, vmid)
}

func hostSlug(name string) string {
	return strings.Trim(hostSlugInvalid.ReplaceAllString(strings.ToLower(name), "-"), "-.")
}

func splitIDs(list string) []int {
	var ids []int
	for _, part := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' }) {
		if id, err := strconv.Atoi(part); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func sortedVMIDs(guests map[int]proxmoxGuest) []int {
	ids := make([]int, 0, len(guests))
	for id := range guests {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case string:
		return typed == "1" || typed == "true"
	default:
		return false
	}
}

// opsRegisteredJob is the part of an Ops job a collector needs to re-register
// it unchanged except for being disabled.
type opsRegisteredJob struct {
	JobID           string `json:"job_id"`
	HostID          string `json:"host_id"`
	DisplayName     string `json:"display_name"`
	Engine          string `json:"engine"`
	Kind            string `json:"kind"`
	Source          string `json:"source"`
	Scope           string `json:"scope"`
	Target          string `json:"target"`
	Retention       string `json:"retention"`
	RecoveryPointer string `json:"recovery_pointer"`
	Notes           string `json:"notes"`
	Registered      bool   `json:"registered"`
	Enabled         bool   `json:"enabled"`
	Schedule        *struct {
		Timezone     string   `json:"timezone"`
		Times        []string `json:"times"`
		Weekdays     []string `json:"weekdays"`
		GraceSeconds int64    `json:"grace_seconds"`
	} `json:"schedule"`
	HeartbeatStaleAfterSeconds int64 `json:"heartbeat_stale_after_seconds"`
}

// retireProxmoxJobs disables guest jobs this source registered earlier that
// the current (complete) inventory no longer has: the guest left every
// enabled backup job, or its job was disabled. The job and its runs stay in
// Ops; it just stops counting as coverage.
func retireProxmoxJobs(ctx context.Context, source opsCollectSource, env opsCollectEnv, plan proxmoxPlan) ([]opsCollectWrite, error) {
	if env.registeredJobs == nil {
		return nil, nil
	}
	registered, err := env.registeredJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not list Ops jobs, so removed Proxmox coverage was not retired this time: %w", err)
	}
	prefix := source.HostID + "-vzdump-"
	var writes []opsCollectWrite
	for _, job := range registered {
		if !strings.HasPrefix(job.JobID, prefix) || !job.Registered || !job.Enabled {
			continue
		}
		if _, current := plan.jobs[job.JobID]; current {
			continue
		}
		body := map[string]any{
			"host_id": job.HostID, "display_name": job.DisplayName, "engine": job.Engine, "kind": job.Kind,
			"source": job.Source, "scope": job.Scope, "target": job.Target, "retention": job.Retention,
			"recovery_pointer": job.RecoveryPointer, "enabled": false,
			"notes": strings.TrimSpace(fmt.Sprintf("%s Disabled %s: no enabled Proxmox backup job on %s includes this guest any more.",
				job.Notes, env.now.UTC().Format("2006-01-02"), source.HostID)),
		}
		if job.Schedule != nil {
			body["schedule"] = map[string]any{
				"timezone": job.Schedule.Timezone, "times": job.Schedule.Times, "weekdays": job.Schedule.Weekdays,
				"grace": fmt.Sprintf("%ds", job.Schedule.GraceSeconds),
			}
		}
		if job.HeartbeatStaleAfterSeconds > 0 {
			body["heartbeat_stale_after"] = fmt.Sprintf("%ds", job.HeartbeatStaleAfterSeconds)
		}
		writes = append(writes, opsCollectWrite{Path: opsPath("jobs", job.JobID), Body: mustJSON(body)})
	}
	return writes, nil
}
