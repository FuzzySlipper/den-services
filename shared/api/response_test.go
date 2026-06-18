package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testPayload struct {
	Name string `json:"name"`
}

type testCodedError struct{}

func (e testCodedError) Error() string {
	return "coded failure"
}

func (e testCodedError) Code() string {
	return "coded_failure"
}

func (e testCodedError) HTTPStatus() int {
	return http.StatusTeapot
}

func TestWriteJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteJSON(recorder, http.StatusCreated, testPayload{Name: "den"})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	if body := strings.TrimSpace(recorder.Body.String()); body != `{"name":"den"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestWriteServiceErrorMapsSentinelErrors(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteServiceError(recorder, fmtWrapped(ErrConflict))

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"conflict"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestWriteServiceErrorUsesCodedError(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteServiceError(recorder, testCodedError{})

	if recorder.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTeapot)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"coded_failure"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestDecodeJSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"den"}`))
	var got testPayload

	if err := DecodeJSON(request, &got); err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}
	if got.Name != "den" {
		t.Fatalf("DecodeJSON() name = %q, want den", got.Name)
	}
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"den","extra":true}`))
	var got testPayload

	err := DecodeJSON(request, &got)
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("DecodeJSON() error = %v, want %v", err, ErrBadRequest)
	}
}

func fmtWrapped(err error) error {
	return &wrappedError{err: err}
}

type wrappedError struct {
	err error
}

func (e *wrappedError) Error() string {
	return "wrapped: " + e.err.Error()
}

func (e *wrappedError) Unwrap() error {
	return e.err
}
