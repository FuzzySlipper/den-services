package resources

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time             { return c.now }
func (c *fakeClock) Advance(step time.Duration) { c.now = c.now.Add(step) }

func utc(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func ptr[T any](value T) *T { return &value }

func newTestService(t *testing.T) (*Service, *fakeClock) {
	t.Helper()
	store, err := OpenStore(context.Background(), filepath.Join(t.TempDir(), "resources.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	clock := &fakeClock{now: utc("2026-10-29T20:00:00Z")}
	service := NewService(store, clock.Now, 90*time.Second)
	ctx := context.Background()
	for i, id := range []string{"den-m5", "den-nimo"} {
		if _, err := service.RegisterHost(ctx, id, RegisterHostRequest{Name: id, Position: i}); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []ResourceRequest{
		{ID: "m5-comfy", Host: ptr("den-m5"), Name: ptr("ComfyUI"), Path: ptr("/home/agent/comfy"), Match: ptr("comfy-main")},
		{ID: "nimo-models", Host: ptr("den-nimo"), Name: ptr("Models"), Path: ptr("/home/comfy/models")},
	} {
		if _, err := service.UpsertResource(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	return service, clock
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var serviceErr *ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Code() != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}

func TestReportWorkRejectsUnknownHostAndForeignResource(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	_, err := service.ReportWork(ctx, WorkReportRequest{Host: "den-m6", Title: "typo"})
	wantCode(t, err, "host_not_found")
	if !strings.Contains(err.Error(), "den-m5, den-nimo") {
		t.Fatalf("error should list known hosts: %v", err)
	}
	_, err = service.ReportWork(ctx, WorkReportRequest{Host: "den-m5", Resources: []string{"nimo-models"}})
	wantCode(t, err, "validation_failed")
	_, err = service.ReportWork(ctx, WorkReportRequest{Host: "den-m5", TTLSeconds: ptr(0.0)})
	wantCode(t, err, "validation_failed")
}

func TestDelayedReportsNeverReviveFinishedWork(t *testing.T) {
	service, clock := newTestService(t)
	ctx := context.Background()
	report := WorkReportRequest{Host: "den-m5", Source: "blender", ExternalID: ptr("render-42"), Title: "Render", State: "running"}
	first, err := service.ReportWork(ctx, report)
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	report.State = "completed"
	done, err := service.ReportWork(ctx, report)
	if err != nil || done.ID != first.ID || done.State != "completed" || done.EndedAt == nil {
		t.Fatalf("completion = %+v, %v", done, err)
	}

	// A late heartbeat for the same job, by external ID or by row ID.
	clock.Advance(time.Minute)
	report.State = "running"
	late, err := service.ReportWork(ctx, report)
	if err != nil || late.State != "completed" {
		t.Fatalf("late report revived the job: %+v, %v", late, err)
	}
	renewed, err := service.UpdateWork(ctx, first.ID, WorkUpdateRequest{})
	if err != nil || renewed.State != "completed" {
		t.Fatalf("late heartbeat revived the job: %+v, %v", renewed, err)
	}

	// A report observed before the row's last update is ignored too.
	second := WorkReportRequest{Host: "den-m5", Source: "blender", ExternalID: ptr("render-43"), State: "running"}
	row, err := service.ReportWork(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	second.State, second.ObservedAt = "failed", ptr(clock.now.Add(-time.Minute))
	stale, err := service.ReportWork(ctx, second)
	if err != nil || stale.State != "running" || stale.ID != row.ID {
		t.Fatalf("older report applied: %+v, %v", stale, err)
	}
}

func TestWorkRecordsUseOnlyForStartedWork(t *testing.T) {
	service, clock := newTestService(t)
	ctx := context.Background()
	queued, err := service.ReportWork(ctx, WorkReportRequest{Host: "den-m5", Owner: "agent-a", Resources: []string{"m5-comfy"}, State: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateWork(ctx, queued.ID, WorkUpdateRequest{State: ptr("cancelled")}); err != nil {
		t.Fatal(err)
	}
	resource, _ := service.Resource(ctx, "m5-comfy")
	if resource.LastUsedAt != nil {
		t.Fatalf("queued-then-cancelled work recorded use: %+v", resource)
	}
	clock.Advance(time.Minute)
	if _, err := service.ReportWork(ctx, WorkReportRequest{Host: "den-m5", Owner: "agent-b", Resources: []string{"m5-comfy", "m5-comfy"}}); err != nil {
		t.Fatal(err)
	}
	resource, _ = service.Resource(ctx, "m5-comfy")
	if resource.LastUsedAt == nil || !resource.LastUsedAt.Equal(clock.now) || resource.LastUsedBy != "agent-b" {
		t.Fatalf("running work did not record use: %+v", resource)
	}
}

func TestReportWorkWarnsAboutOverlap(t *testing.T) {
	service, clock := newTestService(t)
	ctx := context.Background()
	if _, err := service.ReportWork(ctx, WorkReportRequest{Host: "den-m5", Title: "Train", Owner: "a", TTLSeconds: ptr(60.0)}); err != nil {
		t.Fatal(err)
	}
	receipt, err := service.ReportWork(ctx, WorkReportRequest{Host: "den-m5", Title: "Render", Owner: "b"})
	if err != nil || len(receipt.Warnings) != 1 || !strings.Contains(receipt.Warnings[0], "Train (a)") {
		t.Fatalf("receipt = %+v, %v", receipt, err)
	}
	// An expired check-in no longer warns.
	clock.Advance(2 * time.Minute)
	receipt, err = service.ReportWork(ctx, WorkReportRequest{Host: "den-m5", Title: "Bake", Owner: "c"})
	if err != nil || len(receipt.Warnings) != 1 || !strings.Contains(receipt.Warnings[0], "Render") {
		t.Fatalf("receipt = %+v, %v", receipt, err)
	}
}

func TestUpsertResourceMergesAndKeepsUsageDates(t *testing.T) {
	service, clock := newTestService(t)
	ctx := context.Background()
	if _, err := service.UpsertResource(ctx, ResourceRequest{ID: "m5-comfy", Touch: true, Owner: "agent-a"}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Hour)
	updated, err := service.UpsertResource(ctx, ResourceRequest{ID: "m5-comfy", Lifecycle: ptr("experimental"), Notes: ptr("trial"), Confirm: true, Owner: "agent-b"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Path != "/home/agent/comfy" || updated.Match != "comfy-main" || updated.Lifecycle != "experimental" ||
		updated.LastUsedBy != "agent-a" || updated.ConfirmedBy != "agent-b" || !updated.ConfirmedAt.Equal(clock.now) {
		t.Fatalf("merge = %+v", updated)
	}
	_, err = service.UpsertResource(ctx, ResourceRequest{ID: "m5-comfy", Host: ptr("den-nimo")})
	wantCode(t, err, "validation_failed")
	_, err = service.UpsertResource(ctx, ResourceRequest{ID: "m5-comfy", Lifecycle: ptr("abandoned")})
	wantCode(t, err, "validation_failed")
	_, err = service.UpsertResource(ctx, ResourceRequest{ID: "new-one", Host: ptr("den-m5"), Name: ptr("No path")})
	wantCode(t, err, "validation_failed")
	_, err = service.UpsertResource(ctx, ResourceRequest{ID: "new-one", Host: ptr("den-m9"), Name: ptr("x"), Path: ptr("/x")})
	wantCode(t, err, "host_not_found")
}

func TestTelemetryUpdatesPathsActivityAndStatus(t *testing.T) {
	service, clock := newTestService(t)
	ctx := context.Background()
	state, err := service.State(ctx, 10)
	if err != nil || state.Hosts[0].Status != "unknown" || state.Counts.HostsFresh != 0 {
		t.Fatalf("never-sampled host should be unknown: %+v, %v", state.Hosts, err)
	}
	snapshot := `{"sampled_at": ` + jsonNumber(clock.now.Add(-5*time.Second)) + `, "cpu_percent": 12, "cpu_count": 32,
		"memory": {"total_bytes": 100, "available_bytes": 40}, "gpus": [{"name": "8060S", "utilization_percent": 3}],
		"processes": [{"pid": 7, "command": "python main.py", "cwd": "/srv/comfy-main", "cpu_percent": 1, "rss_bytes": 9}],
		"disks": [], "containers": [{"name": "strix-beast"}], "paths": {"/home/agent/comfy": true}, "warnings": []}`
	if err := service.ReportTelemetry(ctx, "den-m5", TelemetryRequest{Telemetry: json.RawMessage(snapshot)}); err != nil {
		t.Fatal(err)
	}
	resource, _ := service.Resource(ctx, "m5-comfy")
	if resource.PathPresent == nil || !*resource.PathPresent || resource.ObservedActivityAt == nil {
		t.Fatalf("probe did not update the resource: %+v", resource)
	}
	state, _ = service.State(ctx, 10)
	if host := state.Hosts[0]; host.Status != "quiet" || host.Stale || host.SampledAt == nil {
		t.Fatalf("fresh idle host = %+v", host)
	}

	// A failed attempt keeps the last values and shows unknown.
	if err := service.ReportTelemetry(ctx, "den-m5", TelemetryRequest{Error: ptr("ssh: connect timed out")}); err != nil {
		t.Fatal(err)
	}
	state, _ = service.State(ctx, 10)
	if host := state.Hosts[0]; host.Status != "unknown" || host.Error == nil || string(host.Telemetry) == "null" {
		t.Fatalf("failed host = %+v", host)
	}

	// A busy GPU is working; malformed telemetry is refused and changes nothing.
	busy := strings.Replace(snapshot, `"utilization_percent": 3`, `"utilization_percent": 90`, 1)
	if err := service.ReportTelemetry(ctx, "den-m5", TelemetryRequest{Telemetry: json.RawMessage(busy)}); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(snapshot, `"cpu_percent": 12`, `"cpu_percent": "12%"`, 1)
	err = service.ReportTelemetry(ctx, "den-m5", TelemetryRequest{Telemetry: json.RawMessage(bad)})
	wantCode(t, err, "validation_failed")
	state, _ = service.State(ctx, 10)
	if host := state.Hosts[0]; host.Status != "working" || host.Error != nil {
		t.Fatalf("busy host = %+v", host)
	}

	// After stale_after the same values are unknown, not quiet.
	clock.Advance(2 * time.Minute)
	state, _ = service.State(ctx, 10)
	if host := state.Hosts[0]; host.Status != "unknown" || !host.Stale {
		t.Fatalf("stale host = %+v", host)
	}

	// An old snapshot pushed again stays stale.
	if err := service.ReportTelemetry(ctx, "den-m5", TelemetryRequest{Telemetry: json.RawMessage(snapshot)}); err != nil {
		t.Fatal(err)
	}
	state, _ = service.State(ctx, 10)
	if host := state.Hosts[0]; !host.Stale {
		t.Fatalf("re-pushed old snapshot looks fresh: %+v", host)
	}
	err = service.ReportTelemetry(ctx, "den-m5", TelemetryRequest{})
	wantCode(t, err, "validation_failed")
}

func TestAppsMarkObservedAndQueueStatus(t *testing.T) {
	service, clock := newTestService(t)
	ctx := context.Background()
	snapshot := `{"sampled_at": ` + jsonNumber(clock.now) + `, "cpu_percent": 1}`
	if err := service.ReportTelemetry(ctx, "den-m5", TelemetryRequest{Telemetry: json.RawMessage(snapshot)}); err != nil {
		t.Fatal(err)
	}
	apps := AppsRequest{Apps: []App{{Name: "ComfyUI", Kind: "comfy", Resources: []string{"m5-comfy"}, Online: true, Running: ptr(0), Queued: ptr(2)}}}
	if err := service.ReportApps(ctx, "den-m5", apps); err != nil {
		t.Fatal(err)
	}
	state, _ := service.State(ctx, 10)
	if state.Hosts[0].Status != "queued" || state.Counts.NativeQueued != 2 {
		t.Fatalf("queued host = %+v counts %+v", state.Hosts[0], state.Counts)
	}
	resource, _ := service.Resource(ctx, "m5-comfy")
	if resource.ObservedActivityAt != nil {
		t.Fatal("an idle queue is not observed activity")
	}
	apps.Apps[0].Running = ptr(1)
	if err := service.ReportApps(ctx, "den-m5", apps); err != nil {
		t.Fatal(err)
	}
	state, _ = service.State(ctx, 10)
	resource, _ = service.Resource(ctx, "m5-comfy")
	if state.Hosts[0].Status != "working" || resource.ObservedActivityAt == nil {
		t.Fatalf("running queue: host %+v resource %+v", state.Hosts[0], resource)
	}
}

func TestStateKeepsActiveWorkBeyondTheLimit(t *testing.T) {
	service, clock := newTestService(t)
	ctx := context.Background()
	active, err := service.ReportWork(ctx, WorkReportRequest{Host: "den-m5", Title: "Long job", TTLSeconds: ptr(86400.0)})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		clock.Advance(time.Second)
		if _, err := service.ReportWork(ctx, WorkReportRequest{Host: "den-nimo", State: "completed"}); err != nil {
			t.Fatal(err)
		}
	}
	state, err := service.State(ctx, 2)
	if err != nil || len(state.Work) != 3 || state.Work[2].ID != active.ID || state.Counts.Running != 1 {
		t.Fatalf("work = %+v, %v", state.Work, err)
	}
	if state.Hosts[0].Status != "unknown" {
		t.Fatal("reported work does not make an unsampled host known")
	}
}

func TestImportBoardCopiesLegacyRowsOnce(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()
	legacyPath := filepath.Join(t.TempDir(), "board.sqlite3")
	legacy, err := sql.Open("sqlite", "file:"+legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`create table resources (id text primary key, data text not null)`,
		`create table work (id text primary key, data text not null)`,
		`insert into resources values ('m5-comfy', '{"id":"m5-comfy","host":"den-m5","name":"Changed","path":"/x"}')`,
		`insert into resources values ('agents-blender', '{"id":"agents-blender","host":"den-agents","name":"Blender","path":"/opt/blender","lifecycle":"maintained","last_used_at":1761700000.5,"last_used_by":"baker","path_present":true}')`,
		`insert into work values ('w1', '{"id":"w1","host":"den-agents","source":"command","external_id":"e1","title":"Bake","owner":"baker","project":"p","resources":["agents-blender"],"state":"completed","note":"","url":"","ttl":180,"created_at":1761700000,"updated_at":1761700100,"expires_at":1761700280,"started_at":1761700000,"ended_at":1761700100}')`,
	}
	for _, statement := range statements {
		if _, err := legacy.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	_ = legacy.Close()
	summary, err := ImportBoard(ctx, service.store, legacyPath, utc("2026-10-29T20:00:00Z"))
	if err != nil || summary != (ImportSummary{Hosts: 1, Resources: 1, ResourcesSkipped: 1, Work: 1}) {
		t.Fatalf("summary = %+v, %v", summary, err)
	}
	again, err := ImportBoard(ctx, service.store, legacyPath, utc("2026-10-29T20:00:00Z"))
	if err != nil || again != (ImportSummary{ResourcesSkipped: 2, WorkSkipped: 1}) {
		t.Fatalf("second import = %+v, %v", again, err)
	}
	kept, _ := service.Resource(ctx, "m5-comfy")
	blender, _ := service.Resource(ctx, "agents-blender")
	work, _ := service.Work(ctx, "w1")
	if kept.Name != "ComfyUI" || blender.LastUsedBy != "baker" || blender.LastUsedAt.Unix() != 1761700000 ||
		*blender.PathPresent != true || work.State != "completed" || *work.ExternalID != "e1" || work.TTLSeconds != 180 {
		t.Fatalf("imported: %+v %+v %+v", kept, blender, work)
	}
}

func jsonNumber(at time.Time) string {
	encoded, _ := json.Marshal(float64(at.UnixNano()) / 1e9)
	return string(encoded)
}
