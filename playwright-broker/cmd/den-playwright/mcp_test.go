package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	broker "den-services/playwright-broker/internal"
)

func TestPlaytestMCPToolsAdvertisePermissiveEscapeHatches(t *testing.T) {
	tools := playtestTools()
	data, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := string(data)
	for _, expected := range []string{"playtest_start", "playtest_observe", "playtest_act", "playtest_inspect", "playtest_finish", "eval", "raw CDP", `"additionalProperties":true`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("tools do not contain %q: %s", expected, text)
		}
	}
	for _, forbidden := range []string{"allowlist", "permission denied", "forbidden operation", "unauthorized"} {
		if strings.Contains(strings.ToLower(text), forbidden) {
			t.Fatalf("tools contain restrictive phrase %q: %s", forbidden, text)
		}
	}
}

func TestPlaytestListMCPToolAdvertisesBoundedPaginationAndFilters(t *testing.T) {
	var listTool map[string]any
	for _, tool := range playtestTools() {
		if tool["name"] == "playtest_list" {
			listTool = tool
			break
		}
	}
	if listTool == nil {
		t.Fatal("playtest_list tool not found")
	}
	schema := listTool["inputSchema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	limit := properties["limit"].(map[string]any)
	if limit["default"] != broker.DefaultPlaytestListPageSize || limit["maximum"] != broker.MaxPlaytestListPageSize {
		t.Fatalf("limit schema = %#v", limit)
	}
	for _, name := range []string{"offset", "session_id", "project", "status", "owner", "scenario"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("missing playtest_list property %q: %#v", name, properties)
		}
	}
}

func TestOptionalIntegerArgumentAcceptsDecodedJSONAndDirectInts(t *testing.T) {
	for _, arguments := range []map[string]any{{"limit": float64(20)}, {"limit": 20}} {
		value, err := optionalIntegerArgument(arguments, "limit")
		if err != nil || value != 20 {
			t.Fatalf("optionalIntegerArgument(%#v) = %d, %v", arguments, value, err)
		}
	}
	for _, value := range []any{20.5, "20", float64(1 << 54)} {
		if _, err := optionalIntegerArgument(map[string]any{"limit": value}, "limit"); err == nil {
			t.Fatalf("optionalIntegerArgument(%#v) error = nil", value)
		}
	}
}

func TestPlaytestListMCPDispatchBoundsDiscoveryAndKeepsGetDetail(t *testing.T) {
	stateDir := t.TempDir()
	sessionDir := filepath.Join(stateDir, "playtest-sessions")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	for index := 0; index < broker.DefaultPlaytestListPageSize+1; index++ {
		session := broker.PlaytestSession{
			SessionID:     fmt.Sprintf("session-%02d", index),
			Project:       "fixture",
			Status:        "pass",
			StartedAt:     base.Add(time.Duration(index) * time.Minute),
			ExitInterview: map[string]any{"confidence": "high"},
		}
		data, err := json.Marshal(session)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sessionDir, session.SessionID+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manager := broker.NewPlaytestManager(&broker.Config{StateDir: stateDir})

	value, err := callMCPTool(context.Background(), manager, "playtest_list", map[string]any{})
	if err != nil {
		t.Fatalf("callMCPTool(playtest_list) error = %v", err)
	}
	page := value.(broker.PlaytestListPage)
	if page.Returned != broker.DefaultPlaytestListPageSize || page.TotalMatched != broker.DefaultPlaytestListPageSize+1 || page.NextOffset == nil {
		t.Fatalf("MCP list page = %#v", page)
	}

	value, err = callMCPTool(context.Background(), manager, "playtest_get", map[string]any{"session_id": "session-20"})
	if err != nil {
		t.Fatalf("callMCPTool(playtest_get) error = %v", err)
	}
	detail := value.(broker.PlaytestSession)
	if detail.ExitInterview.(map[string]any)["confidence"] != "high" {
		t.Fatalf("MCP get detail = %#v", detail)
	}
}

func TestToolResultWithImagesKeepsTextWhenObservationHasNoImages(t *testing.T) {
	value := map[string]any{"ok": true, "result": map[string]any{"state": map[string]any{"focused": true}}}
	result := toolResultWithImages(value, false, t.TempDir(), playtestObservationImagePaths(value))
	content := result["content"].([]map[string]any)
	if len(content) != 1 || content[0]["type"] != "text" {
		t.Fatalf("content = %#v", content)
	}
}

func TestToolResultWithImagesAttachesScreenshotWithDetectedMIMEAndBase64(t *testing.T) {
	root := t.TempDir()
	writeTestPNG(t, root, "screenshots/one.png", []byte("one"))
	value := map[string]any{"ok": true, "result": map[string]any{"screenshot": "screenshots/one.png"}}
	result := toolResultWithImages(value, false, root, playtestObservationImagePaths(value))
	content := result["content"].([]map[string]any)
	if len(content) != 2 {
		t.Fatalf("content = %#v", content)
	}
	assertImageContent(t, content[1], append(testPNGHeader(), []byte("one")...))
}

func TestToolResultWithImagesAttachesFrameBurstInOrder(t *testing.T) {
	root := t.TempDir()
	writeTestPNG(t, root, "screenshots/frame-1.png", []byte("first"))
	writeTestPNG(t, root, "screenshots/frame-2.png", []byte("second"))
	value := map[string]any{"ok": true, "result": map[string]any{
		"frames": []any{"screenshots/frame-1.png", "screenshots/frame-2.png"},
	}}
	result := toolResultWithImages(value, false, root, playtestObservationImagePaths(value))
	content := result["content"].([]map[string]any)
	if len(content) != 3 {
		t.Fatalf("content = %#v", content)
	}
	assertImageContent(t, content[1], append(testPNGHeader(), []byte("first")...))
	assertImageContent(t, content[2], append(testPNGHeader(), []byte("second")...))
}

func TestToolResultWithImagesRetainsStructuredResultOnAttachmentFailure(t *testing.T) {
	value := map[string]any{"ok": true, "result": map[string]any{"screenshot": "../outside.png"}}
	result := toolResultWithImages(value, false, t.TempDir(), playtestObservationImagePaths(value))
	content := result["content"].([]map[string]any)
	if !reflect.DeepEqual(result["structuredContent"], value) || len(content) != 2 || content[1]["type"] != "text" {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(content[1]["text"].(string), "outside the session artifact root") {
		t.Fatalf("warning = %q", content[1]["text"])
	}
}

func TestToolResultWithImagesRejectsSymlinkOutsideArtifactRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeTestPNG(t, outside, "outside.png", []byte("outside"))
	if err := os.MkdirAll(filepath.Join(root, "screenshots"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.png"), filepath.Join(root, "screenshots", "linked.png")); err != nil {
		t.Fatal(err)
	}
	assertRejectedImageRetainsStructuredResult(t, root, "screenshots/linked.png")
}

func TestToolResultWithImagesRejectsSymlinkedDirectoryOutsideArtifactRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeTestPNG(t, outside, "outside.png", []byte("outside"))
	if err := os.Symlink(outside, filepath.Join(root, "screenshots")); err != nil {
		t.Fatal(err)
	}
	assertRejectedImageRetainsStructuredResult(t, root, "screenshots/outside.png")
}

func assertRejectedImageRetainsStructuredResult(t *testing.T, root string, relativePath string) {
	t.Helper()
	value := map[string]any{"ok": true, "result": map[string]any{"screenshot": relativePath}}
	result := toolResultWithImages(value, false, root, playtestObservationImagePaths(value))
	content := result["content"].([]map[string]any)
	if !reflect.DeepEqual(result["structuredContent"], value) || len(content) != 2 || content[1]["type"] != "text" {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(content[1]["text"].(string), "playtest image attachment warning") {
		t.Fatalf("warning = %q", content[1]["text"])
	}
}

func writeTestPNG(t *testing.T, root string, relativePath string, suffix []byte) {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(testPNGHeader(), suffix...), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testPNGHeader() []byte {
	return []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
}

func assertImageContent(t *testing.T, content map[string]any, expected []byte) {
	t.Helper()
	if content["type"] != "image" || content["mimeType"] != "image/png" {
		t.Fatalf("content = %#v", content)
	}
	decoded, err := base64.StdEncoding.DecodeString(content["data"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(expected) {
		t.Fatalf("decoded = %x, want %x", decoded, expected)
	}
}
