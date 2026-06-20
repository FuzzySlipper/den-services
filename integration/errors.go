package integration

import "errors"

var ErrMissingAdminDatabaseURL = errors.New("admin database url is required") //nolint:gochecknoglobals
