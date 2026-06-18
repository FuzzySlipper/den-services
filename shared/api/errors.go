package api

import "errors"

var (
	ErrBadRequest   = errors.New("bad request")         //nolint:gochecknoglobals
	ErrUnauthorized = errors.New("unauthorized")        //nolint:gochecknoglobals
	ErrForbidden    = errors.New("forbidden")           //nolint:gochecknoglobals
	ErrNotFound     = errors.New("not found")           //nolint:gochecknoglobals
	ErrConflict     = errors.New("conflict")            //nolint:gochecknoglobals
	ErrValidation   = errors.New("validation failed")   //nolint:gochecknoglobals
	ErrUnavailable  = errors.New("service unavailable") //nolint:gochecknoglobals
)
