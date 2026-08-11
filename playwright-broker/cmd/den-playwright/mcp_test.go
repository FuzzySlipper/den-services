package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
