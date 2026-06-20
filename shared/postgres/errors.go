package postgres

import "errors"

var ErrMissingDatabaseURL = errors.New("database url is required") //nolint:gochecknoglobals
