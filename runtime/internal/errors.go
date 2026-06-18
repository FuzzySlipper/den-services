package runtime

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrInvalidRuntimeInstance = errors.New("invalid runtime instance")   //nolint:gochecknoglobals
	ErrInvalidRuntimeState    = errors.New("invalid runtime state")      //nolint:gochecknoglobals
	ErrInstanceNotFound       = errors.New("runtime instance not found") //nolint:gochecknoglobals
	ErrSubscriptionNotFound   = errors.New("subscription not found")     //nolint:gochecknoglobals
	ErrInvalidSubscription    = errors.New("invalid subscription")       //nolint:gochecknoglobals
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

func notFound(err error, id string) error {
	return NewServiceError(fmt.Errorf("%w: %s", err, id), "not_found", http.StatusNotFound)
}

func badRequest(err error) error {
	return NewServiceError(err, "bad_request", http.StatusBadRequest)
}
