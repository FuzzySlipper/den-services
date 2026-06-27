package artifacts

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPUploadMetadataContentAndDelete(t *testing.T) {
	store := newMemoryArtifactStore()
	blobs := newMemoryBlobStore()
	cfg := testServerConfig()
	cfg.Limits.MaxBytesPerArtifact = 1024 * 1024
	cfg.Limits.MaxPixelsPerImage = 16
	service := NewArtifactService(store, blobs, cfg, fixedClock)
	server, err := NewHTTPServer(cfg, testBuildInfo(t), service)
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v", err)
	}

	createResponse := uploadTinyPNG(t, server.Handler)
	if createResponse.ArtifactID == "" {
		t.Fatal("ArtifactID is empty")
	}
	if createResponse.ArtifactRef != "den-artifact://"+createResponse.ArtifactID {
		t.Fatalf("ArtifactRef = %s", createResponse.ArtifactRef)
	}

	metadataRequest := authedRequest(http.MethodGet, "/v1/artifacts/"+createResponse.ArtifactID+"/metadata", nil)
	metadataResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(metadataResponse, metadataRequest)
	if metadataResponse.Code != http.StatusOK {
		t.Fatalf("metadata status = %d, body = %s", metadataResponse.Code, metadataResponse.Body.String())
	}

	contentRequest := authedRequest(http.MethodGet, "/v1/artifacts/"+createResponse.ArtifactID+"/content", nil)
	contentResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(contentResponse, contentRequest)
	if contentResponse.Code != http.StatusOK {
		t.Fatalf("content status = %d", contentResponse.Code)
	}
	if contentResponse.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("content type = %s", contentResponse.Header().Get("Content-Type"))
	}
	if !bytes.Equal(contentResponse.Body.Bytes(), tinyPNG(t)) {
		t.Fatal("content body did not match upload")
	}

	deleteRequest := authedRequest(http.MethodDelete, "/v1/artifacts/"+createResponse.ArtifactID, nil)
	deleteResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d", deleteResponse.Code)
	}

	deletedMetadataResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(deletedMetadataResponse, metadataRequest)
	if deletedMetadataResponse.Code != http.StatusNotFound {
		t.Fatalf("deleted metadata status = %d", deletedMetadataResponse.Code)
	}
}

func uploadTinyPNG(t *testing.T, handler http.Handler) CreateArtifactResponse {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	mustWriteField(t, writer, "project_id", "den-services")
	mustWriteField(t, writer, "task_id", "3476")
	mustWriteField(t, writer, "logical_name", "pixel.png")
	mustWriteField(t, writer, "created_by", "codex")
	part, err := writer.CreateFormFile("file", "pixel.png")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write(tinyPNG(t)); err != nil {
		t.Fatalf("writing multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}

	request := authedRequest(http.MethodPost, "/v1/artifacts", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body = %s", response.Code, response.Body.String())
	}
	var decoded CreateArtifactResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return decoded
}

func mustWriteField(t *testing.T, writer *multipart.Writer, key string, value string) {
	t.Helper()
	if err := writer.WriteField(key, value); err != nil {
		t.Fatalf("WriteField(%s) error = %v", key, err)
	}
}

func authedRequest(method string, path string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("Authorization", "Bearer test-token")
	return request
}
