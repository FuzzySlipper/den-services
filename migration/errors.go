package migration

import "errors"

var ErrMissingPool = errors.New("postgres pool is required") //nolint:gochecknoglobals
