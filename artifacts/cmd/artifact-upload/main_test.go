package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUploadPostsMultipartAndPrintsMetadataOnly(t *testing.T) {
	imagePath := writePNG(t)
	var sawAuth bool
	var sawFields bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer secret-token" {
			sawAuth = true
		}
		if err := r.ParseMultipartForm(1024 * 1024); err != nil {
			t.Fatalf("ParseMultipartForm() error = %v", err)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("FormFile() error = %v", err)
		}
		defer file.Close()
		if r.FormValue("project_id") == "den-services" &&
			r.FormValue("task_id") == "3478" &&
			r.FormValue("logical_name") == "overview.png" &&
			r.FormValue("sensitive") == "true" {
			sawFields = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artifact_id":"art_123","artifact_ref":"den-artifact://art_123","mime_type":"image/png","sha256":"abc","byte_count":70}`))
	}))
	defer server.Close()

	var output bytes.Buffer
	err := upload(t.Context(), uploadConfig{
		filePath:    imagePath,
		baseURL:     server.URL,
		token:       "secret-token",
		projectID:   "den-services",
		taskID:      3478,
		logicalName: "overview.png",
		sensitive:   true,
		timeout:     time.Second,
	}, &output)
	if err != nil {
		t.Fatalf("upload() error = %v", err)
	}
	if !sawAuth {
		t.Fatal("Authorization header was not sent")
	}
	if !sawFields {
		t.Fatal("expected multipart fields were not sent")
	}
	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not json: %v", err)
	}
	if decoded["artifact_ref"] != "den-artifact://art_123" {
		t.Fatalf("artifact_ref = %v", decoded["artifact_ref"])
	}
	if bytes.Contains(output.Bytes(), readFile(t, imagePath)) {
		t.Fatal("output contained raw image bytes")
	}
}

func TestParseFlagsUsesEnvAndDefaultsLogicalName(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "screen.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cfg, err := parseFlags([]string{"-file", imagePath}, func(key string) string {
		switch key {
		case baseURLEnv:
			return "http://127.0.0.1:8090/"
		case tokenEnv:
			return "token"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}
	if cfg.baseURL != "http://127.0.0.1:8090" {
		t.Fatalf("baseURL = %q", cfg.baseURL)
	}
	if cfg.filePath != imagePath {
		t.Fatalf("filePath = %q", cfg.filePath)
	}
}

func TestAddFileDoesNotRequireMimeTypeOverride(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := addFile(writer, uploadConfig{filePath: writePNG(t)}); err != nil {
		t.Fatalf("addFile() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !bytes.Contains(body.Bytes(), []byte(`name="file"`)) {
		t.Fatal("multipart body missing file field")
	}
}

func writePNG(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer file.Close()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	return path
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return data
}
