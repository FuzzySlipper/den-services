package schema

import (
	"fmt"
	"net/http"
)

type RequestError struct {
	status  int
	code    string
	message string
}

func BadRequest(format string, args ...any) *RequestError {
	return &RequestError{
		status:  http.StatusBadRequest,
		code:    "invalid_visual_inspect_request",
		message: fmt.Sprintf(format, args...),
	}
}

func PayloadTooLarge(format string, args ...any) *RequestError {
	return &RequestError{
		status:  http.StatusRequestEntityTooLarge,
		code:    "visual_inspect_payload_too_large",
		message: fmt.Sprintf(format, args...),
	}
}

func UnsupportedArtifact(format string, args ...any) *RequestError {
	return &RequestError{
		status:  http.StatusBadRequest,
		code:    "unsupported_artifact_ref",
		message: fmt.Sprintf(format, args...),
	}
}

func (e *RequestError) Error() string {
	return e.message
}

func (e *RequestError) Code() string {
	return e.code
}

func (e *RequestError) HTTPStatus() int {
	return e.status
}
