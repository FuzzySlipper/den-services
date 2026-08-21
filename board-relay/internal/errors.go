package boardrelay

import "net/http"

type ServiceError struct {
	err    error
	code   string
	status int
}

func (e *ServiceError) Error() string   { return e.err.Error() }
func (e *ServiceError) Unwrap() error   { return e.err }
func (e *ServiceError) Code() string    { return e.code }
func (e *ServiceError) HTTPStatus() int { return e.status }

func validationFailed(err error) error {
	return &ServiceError{err: err, code: "validation_failed", status: http.StatusBadRequest}
}
