package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type recordedRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// fakeResources records requests and answers from a per-route table.
type fakeResources struct {
	mu        sync.Mutex
	requests  []recordedRequest
	responses map[string]string
}

func newFakeResources(t *testing.T, responses map[string]string) (*fakeResources, *ResourcesClient) {
	t.Helper()
	fake := &fakeResources{responses: responses}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		fake.mu.Lock()
		fake.requests = append(fake.requests, recordedRequest{Method: r.Method, Path: r.URL.RequestURI(), Body: decoded})
		fake.mu.Unlock()
		if answer, ok := fake.responses[r.Method+" "+r.URL.Path]; ok {
			if strings.HasPrefix(answer, "!") {
				http.Error(w, `{"error":{"code":"host_not_found","message":"host not found: \"den-x\""}}`, http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(w, answer)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)
	client, err := NewResourcesClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEN_RESOURCES_URL", server.URL)
	return fake, client
}

func (f *fakeResources) find(method, prefix string) []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found []recordedRequest
	for _, request := range f.requests {
		if request.Method == method && strings.HasPrefix(request.Path, prefix) {
			found = append(found, request)
		}
	}
	return found
}

func TestParseResourcesFlagsInterleavesAndStopsAtDoubleDash(t *testing.T) {
	flags, err := parseResourcesFlags(
		[]string{"m5-comfy", "--owner", "me", "--resource=a", "--resource", "b", "--", "echo", "--owner", "x"},
		map[string]bool{"owner": true, "resource": true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if flags.get("owner", "") != "me" || strings.Join(flags.values["resource"], ",") != "a,b" ||
		strings.Join(flags.positional, ",") != "m5-comfy" || strings.Join(flags.command, " ") != "echo --owner x" {
		t.Fatalf("flags = %+v", flags)
	}
	if _, err := parseResourcesFlags([]string{"--bogus"}, nil, nil); err == nil {
		t.Fatal("unknown flag accepted")
	}
	if _, err := parseResourcesFlags([]string{"--owner"}, map[string]bool{"owner": true}, nil); err == nil {
		t.Fatal("missing value accepted")
	}
}

func TestResourcesCommandsSendTheOldBoardRequests(t *testing.T) {
	fake, _ := newFakeResources(t, map[string]string{"POST /v1/resources/work": `{"id":"w1","warnings":[]}`})
	var stdout, stderr bytes.Buffer
	code := handleResources([]string{
		"check-in", "--host", "den-m5", "--purpose", "Generate", "--resource", "m5-comfy",
		"--owner", "agent", "--ttl", "60", "--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("check-in = %d: %s", code, stderr.String())
	}
	checkIn := fake.find("POST", "/v1/resources/work")[0].Body
	if checkIn["host"] != "den-m5" || checkIn["title"] != "Generate" || checkIn["ttl_seconds"] != 60.0 ||
		checkIn["source"] != "check-in" || checkIn["state"] != "running" {
		t.Fatalf("check-in body = %+v", checkIn)
	}

	code = handleResources([]string{
		"report", "--source", "blender", "--job-id", "render-42", "--host", "den-agents",
		"--purpose", "Render", "--state", "completed",
	}, &stdout, &stderr)
	report := fake.find("POST", "/v1/resources/work")[1].Body
	if code != 0 || report["source"] != "blender" || report["external_id"] != "render-42" || report["state"] != "completed" {
		t.Fatalf("report = %d %+v", code, report)
	}

	if code := handleResources([]string{"touch", "m5-comfy", "--owner", "me", "--note", "trial"}, &stdout, &stderr); code != 0 {
		t.Fatalf("touch = %d: %s", code, stderr.String())
	}
	touch := fake.find("POST", "/v1/resources/directory")[0].Body
	if touch["touch"] != true || touch["owner"] != "me" || touch["notes"] != "trial" {
		t.Fatalf("touch = %+v", touch)
	}
	if code := handleResources([]string{"maintain", "m5-comfy", "--lifecycle", "retired"}, &stdout, &stderr); code != 2 {
		t.Fatalf("maintain without --note = %d", code)
	}
	if code := handleResources([]string{"check-out", "w1", "--state", "running"}, &stdout, &stderr); code != 2 {
		t.Fatalf("check-out --state running = %d", code)
	}
	if code := handleResources([]string{"check-out", "w1", "--state", "failed"}, &stdout, &stderr); code != 0 {
		t.Fatalf("check-out = %d", code)
	}
	if body := fake.find("POST", "/v1/resources/work/w1")[0].Body; body["state"] != "failed" {
		t.Fatalf("check-out body = %+v", body)
	}
	if code := handleResources([]string{"check-in", "--host", "den-m5"}, &stdout, &stderr); code != 2 {
		t.Fatal("check-in without --purpose accepted")
	}
}

func TestResourcesCommandShowsTheServiceError(t *testing.T) {
	_, _ = newFakeResources(t, map[string]string{"POST /v1/resources/work": "!"})
	var stdout, stderr bytes.Buffer
	code := handleResources([]string{"check-in", "--host", "den-x", "--purpose", "p"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), `host not found: "den-x"`) {
		t.Fatalf("code %d stderr %q", code, stderr.String())
	}
}

func TestResourcesRunChecksInAndOutAroundTheCommand(t *testing.T) {
	fake, client := newFakeResources(t, map[string]string{"POST /v1/resources/work": `{"id":"w9","warnings":["Also reported on den-m5: Train (a)"]}`})
	var stderr bytes.Buffer
	code := runResourcesRun([]string{"--host", "den-m5", "--purpose", "Exit three", "--ttl", "3", "--", "sh", "-c", "sleep 1.2; exit 3"}, client, &stderr)
	if code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
	checkIn := fake.find("POST", "/v1/resources/work")
	if len(checkIn) < 2 || checkIn[0].Body["source"] != "command" || checkIn[0].Body["external_id"] == "" {
		t.Fatalf("requests = %+v", fake.requests)
	}
	last := checkIn[len(checkIn)-1]
	if last.Path != "/v1/resources/work/w9" || last.Body["state"] != "failed" || !strings.Contains(last.Body["note"].(string), "status 3") {
		t.Fatalf("check-out = %+v", last)
	}
	if len(fake.find("POST", "/v1/resources/work/w9")) < 2 {
		t.Fatalf("no heartbeat in a 1.2s command with ttl 3s: %+v", fake.requests)
	}
	if !strings.Contains(stderr.String(), "Also reported on den-m5") {
		t.Fatalf("overlap warning not shown: %q", stderr.String())
	}
}

func TestResourcesRunStillRunsWhenReportingIsDown(t *testing.T) {
	client, err := NewResourcesClient("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := runResourcesRun([]string{"--host", "den-m5", "--purpose", "p", "--", "true"}, client, &stderr); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if strings.Count(stderr.String(), "reporting unavailable") != 1 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestProbeCommandForEachTransport(t *testing.T) {
	options := []byte(`{"paths":["/a b"]}`)
	argv, stdin, err := probeCommand(resourcesCollectHost{Transport: "ssh", SSH: []string{"-F", "~/server-access/gpu-lab/config", "target"}}, options)
	home, _ := os.UserHomeDir()
	if err != nil || argv[0] != "ssh" || !strings.Contains(strings.Join(argv, " "), filepath.Join(home, "server-access/gpu-lab/config")) ||
		argv[len(argv)-1] != `python3 - '{"paths":["/a b"]}'` || !bytes.Contains(stdin, []byte("def collect")) {
		t.Fatalf("ssh argv = %q", argv)
	}
	argv, stdin, _ = probeCommand(resourcesCollectHost{Transport: "windows", SSH: []string{"den-win11"}, ProbeScript: "C:/p.ps1"}, options)
	if stdin != nil || !strings.HasSuffix(argv[len(argv)-1], "-File C:/p.ps1 -OptionsBase64 eyJwYXRocyI6WyIvYSBiIl19") {
		t.Fatalf("windows argv = %q", argv)
	}
	argv, _, _ = probeCommand(resourcesCollectHost{Transport: "local"}, options)
	if strings.Join(argv[:2], " ") != "python3 -" {
		t.Fatalf("local argv = %q", argv)
	}
}

func TestComfyQueueListsJobsAndNodeTypes(t *testing.T) {
	row := map[string]any{}
	body := `{"queue_running":[[1,"p-1",{"10":{"class_type":"SaveImage"},"2":{"class_type":"KSampler"},"3":{"class_type":"KSampler"}}]],
		"queue_pending":[[2,"p-2",{}],[3,"p-3"]]}`
	if err := comfyQueue([]byte(body), row); err != nil {
		t.Fatal(err)
	}
	jobs := row["jobs"].([]map[string]any)
	if row["running"] != 1 || row["queued"] != 2 || jobs[0]["id"] != "p-1" ||
		strings.Join(jobs[0]["node_types"].([]string), ",") != "KSampler,SaveImage" || jobs[2]["state"] != "queued" {
		t.Fatalf("row = %+v", row)
	}
	if err := comfyQueue([]byte(`{"queue_running":[["x"]]}`), map[string]any{}); err == nil {
		t.Fatal("malformed entry accepted")
	}
}

func TestCollectRoundReportsHostsAppsAndWorkbench(t *testing.T) {
	fake, client := newFakeResources(t, map[string]string{
		"GET /v1/resources/directory": `{"resources":[{"host":"den-m5","path":"/home/agent/comfy","probe_path":""},{"host":"den-m5","path":"/x","probe_path":"/x/probe"}]}`,
		"GET /v1/resources/work":      `{"work":[{"id":"row-7","external_id":"wb-done"}]}`,
	})
	root := t.TempDir()
	writeStatus := func(id string, status int) {
		dir := filepath.Join(root, id)
		_ = os.MkdirAll(dir, 0o750)
		body, _ := json.Marshal(map[string]any{"Id": id, "Status": status, "Operation": "bake", "Resources": []string{"gpu:den-nimo"}})
		_ = os.WriteFile(filepath.Join(dir, "status.json"), body, 0o600)
	}
	writeStatus("wb-run", 1)
	writeStatus("wb-done", 2)
	writeStatus("wb-old", 3) // terminal and never seen active: left alone
	var probed []string
	var mu sync.Mutex
	collector := &resourcesCollector{
		client: client, stderr: io.Discard,
		config: resourcesCollectConfig{
			PollSeconds: 20,
			Hosts: []resourcesCollectHost{
				{ID: "den-m5", Transport: "ssh", SSH: []string{"den-m5"}, Apps: []resourcesCollectApp{{Name: "ComfyUI", Kind: "comfy", URL: "http://m5:8188", Resources: []string{"m5-comfy"}}}},
				{ID: "den-nimo", Transport: "ssh", SSH: []string{"den-nimo"}, Apps: []resourcesCollectApp{{Name: "TRELLIS", Kind: "health", URL: "http://nimo:8189"}}},
				{ID: "lab", Transport: "push"},
			},
			WorkbenchRoots:     []string{root},
			WorkbenchResources: map[string]resourcesWorkbenchLink{"gpu:den-nimo": {Host: "den-nimo", Resources: []string{"nimo-comfy"}}},
		},
		runProbe: func(_ context.Context, argv []string, _ []byte) ([]byte, error) {
			mu.Lock()
			probed = append(probed, argv[len(argv)-1])
			mu.Unlock()
			if strings.Contains(strings.Join(argv, " "), "den-nimo") {
				return nil, errors.New("ssh: connect to host den-nimo: No route to host")
			}
			return []byte("\xef\xbb\xbf{\"sampled_at\": 1, \"cpu_percent\": 3}\n"), nil
		},
		httpGet: func(_ context.Context, url string) ([]byte, error) {
			if strings.HasSuffix(url, "/queue") {
				return []byte(`{"queue_running":[],"queue_pending":[[1,"p",{}]]}`), nil
			}
			return nil, errors.New("connection refused")
		},
	}
	collector.round(context.Background())

	if len(fake.find("PUT", "/v1/resources/hosts/")) != 3 || len(fake.find("PUT", "/v1/resources/settings")) != 1 {
		t.Fatalf("registrations = %+v", fake.requests)
	}
	if len(probed) != 2 {
		t.Fatalf("push host was probed: %q", probed)
	}
	if !strings.Contains(strings.Join(probed, "\n"), `"paths":["/home/agent/comfy","/x/probe"]`) {
		t.Fatalf("den-m5 probe did not check its resource paths: %q", probed)
	}
	m5 := fake.find("POST", "/v1/resources/hosts/den-m5/telemetry")[0].Body
	nimo := fake.find("POST", "/v1/resources/hosts/den-nimo/telemetry")[0].Body
	if m5["telemetry"] == nil || !strings.Contains(nimo["error"].(string), "No route to host") {
		t.Fatalf("telemetry m5 %+v nimo %+v", m5, nimo)
	}
	apps := fake.find("POST", "/v1/resources/hosts/den-m5/apps")[0].Body["apps"].([]any)[0].(map[string]any)
	health := fake.find("POST", "/v1/resources/hosts/den-nimo/apps")[0].Body["apps"].([]any)[0].(map[string]any)
	if apps["queued"] != 1.0 || apps["online"] != true || health["online"] != false || health["running"] != nil {
		t.Fatalf("apps m5 %+v nimo %+v", apps, health)
	}
	running := fake.find("POST", "/v1/resources/work")
	var workbench []recordedRequest
	for _, request := range running {
		if request.Path == "/v1/resources/work" && request.Body["source"] == "workbench" {
			workbench = append(workbench, request)
		}
	}
	if len(workbench) != 1 || workbench[0].Body["host"] != "den-nimo" || workbench[0].Body["external_id"] != "wb-run" ||
		workbench[0].Body["ttl_seconds"] != 90.0 || workbench[0].Body["observed_at"] == nil {
		t.Fatalf("workbench reports = %+v", workbench)
	}
	closed := fake.find("POST", "/v1/resources/work/")
	if len(closed) != 1 || closed[0].Path != "/v1/resources/work/row-7" || closed[0].Body["state"] != "completed" {
		t.Fatalf("workbench check-outs = %+v", closed)
	}
}

func TestLoadResourcesCollectConfigChecksTransports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(path, []byte(`{"hosts":[{"id":"den-win11","transport":"windows","ssh":["den-win11"]}]}`), 0o600)
	if _, err := loadResourcesCollectConfig(path); err == nil || !strings.Contains(err.Error(), "probe_script") {
		t.Fatalf("err = %v", err)
	}
	_ = os.WriteFile(path, []byte(`{"hosts":[{"id":"x","transport":"local"}]}`), 0o600)
	config, err := loadResourcesCollectConfig(path)
	if err != nil || config.PollSeconds != 20 {
		t.Fatalf("config = %+v, %v", config, err)
	}
}
