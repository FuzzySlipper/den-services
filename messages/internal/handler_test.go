package messages

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandlerSendMessageAndLatestPacket(t *testing.T) {
	store := newMemoryStore()
	service := NewService(store, NoopProjectValidator{}, NoopTaskReader{}, time.Now)
	mux := http.NewServeMux()
	NewHandler(service).RegisterRoutes(mux)

	sendBody := bytes.NewBufferString(`{"task_id":7,"sender":"pi","content":"hello","intent":"question"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/den-services/messages", sendBody)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("send status = %d body=%s", rec.Code, rec.Body.String())
	}
	var sent MessageResponse
	if err := json.NewDecoder(rec.Body).Decode(&sent); err != nil {
		t.Fatalf("decode send response: %v", err)
	}
	if sent.ProjectID != "den-services" || sent.TaskID == nil || *sent.TaskID != 7 {
		t.Fatalf("sent response = %#v", sent)
	}

	packetBody := bytes.NewBufferString(`{"packet_type":"coder_context_packet","sender":"pi"}`)
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/den-services/tasks/7/packets/context", packetBody)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("packet status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/den-services/tasks/7/packets/latest?packet_type=coder_context_packet&role=coder", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("latest packet status = %d body=%s", rec.Code, rec.Body.String())
	}
	var latest MessageResponse
	if err := json.NewDecoder(rec.Body).Decode(&latest); err != nil {
		t.Fatalf("decode latest response: %v", err)
	}
	if latest.Metadata["schema"] != PacketSchema {
		t.Fatalf("latest metadata = %#v", latest.Metadata)
	}
}
