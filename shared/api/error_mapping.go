package api

import (
	"errors"
	"net/http"
)

type CodedStatusError interface {
	error
	Code() string
	HTTPStatus() int
}

type ErrorStatus struct {
	err    error
	status int
	code   string
}

func statusMap() []ErrorStatus {
	return []ErrorStatus{
		{err: ErrBadRequest, status: http.StatusBadRequest, code: "bad_request"},
		{err: ErrUnauthorized, status: http.StatusUnauthorized, code: "unauthorized"},
		{err: ErrForbidden, status: http.StatusForbidden, code: "forbidden"},
		{err: ErrNotFound, status: http.StatusNotFound, code: "not_found"},
		{err: ErrConflict, status: http.StatusConflict, code: "conflict"},
		{err: ErrValidation, status: http.StatusBadRequest, code: "validation_failed"},
		{err: ErrUnavailable, status: http.StatusServiceUnavailable, code: "service_unavailable"},
	}
}

func statusForError(err error) (int, string) {
	var coded CodedStatusError
	if errors.As(err, &coded) {
		return coded.HTTPStatus(), coded.Code()
	}

	for _, entry := range statusMap() {
		if errors.Is(err, entry.err) {
			return entry.status, entry.code
		}
	}

	return http.StatusInternalServerError, "internal_error"
}
