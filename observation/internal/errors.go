package observation

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrInvalidActivityEvent = errors.New("invalid activity event")    //nolint:gochecknoglobals
	ErrInvalidSourceDomain  = errors.New("invalid source domain")     //nolint:gochecknoglobals
	ErrInvalidQuery         = errors.New("invalid observation query") //nolint:gochecknoglobals
)

type ServiceError struct {
	err    error
	code   string
	status int
}

func NewServiceError(err error, code string, status int) *ServiceError {
	return &ServiceError{err: err, code: code, status: status}
}

func (e *ServiceError) Error() string {
	return e.err.Error()
}

func (e *ServiceError) Unwrap() error {
	return e.err
}

func (e *ServiceError) Code() string {
	return e.code
}

func (e *ServiceError) HTTPStatus() int {
	return e.status
}

func badRequest(err error) error {
	return NewServiceError(err, "bad_request", http.StatusBadRequest)
}

func notFound(kind string, id string) error {
	return NewServiceError(fmt.Errorf("%s not found: %s", kind, id), "not_found", http.StatusNotFound)
}
