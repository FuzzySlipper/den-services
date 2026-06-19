package delivery

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrInvalidIntent          = errors.New("invalid delivery intent")           //nolint:gochecknoglobals
	ErrInvalidIntentState     = errors.New("invalid delivery intent state")     //nolint:gochecknoglobals
	ErrIntentNotFound         = errors.New("delivery intent not found")         //nolint:gochecknoglobals
	ErrIntentAlreadyClaimed   = errors.New("intent already claimed")            //nolint:gochecknoglobals
	ErrIntentAlreadyCompleted = errors.New("intent already completed")          //nolint:gochecknoglobals
	ErrIntentExpired          = errors.New("intent expired")                    //nolint:gochecknoglobals
	ErrIntentTargetMismatch   = errors.New("intent target mismatch")            //nolint:gochecknoglobals
	ErrRuntimeNotAlive        = errors.New("runtime is not alive")              //nolint:gochecknoglobals
	ErrMissingRuntimeAuth     = errors.New("runtime service token is required") //nolint:gochecknoglobals
	ErrInvalidLifecycleEvent  = errors.New("invalid lifecycle event")           //nolint:gochecknoglobals
	ErrInvalidClaimToken      = errors.New("invalid claim token")               //nolint:gochecknoglobals
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

func conflict(err error) error {
	return NewServiceError(err, errorCode(err), http.StatusConflict)
}

func notFound(id int64) error {
	return NewServiceError(fmt.Errorf("%w: %d", ErrIntentNotFound, id), "intent_not_found", http.StatusNotFound)
}

func badRequest(err error) error {
	return NewServiceError(err, "bad_request", http.StatusBadRequest)
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrIntentAlreadyClaimed):
		return "intent_already_claimed"
	case errors.Is(err, ErrIntentAlreadyCompleted):
		return "intent_already_completed"
	case errors.Is(err, ErrIntentExpired):
		return "intent_expired"
	case errors.Is(err, ErrIntentTargetMismatch):
		return "intent_target_mismatch"
	case errors.Is(err, ErrRuntimeNotAlive):
		return "runtime_not_alive"
	case errors.Is(err, ErrInvalidClaimToken):
		return "invalid_claim_token"
	default:
		return "conflict"
	}
}
