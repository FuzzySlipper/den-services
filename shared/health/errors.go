package health

import "errors"

var (
	ErrMissingServiceName = errors.New("service name is required") //nolint:gochecknoglobals
	ErrMissingVersion     = errors.New("version is required")      //nolint:gochecknoglobals
	ErrMissingCommit      = errors.New("commit is required")       //nolint:gochecknoglobals
	ErrMissingBuiltAt     = errors.New("built_at is required")     //nolint:gochecknoglobals
)
