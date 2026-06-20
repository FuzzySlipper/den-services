package conversation

import "errors"

var (
	ErrMissingServiceToken = errors.New("DEN_CONVERSATION_SERVICE_TOKEN is required") //nolint:gochecknoglobals
	ErrInvalidChannel      = errors.New("invalid channel")                            //nolint:gochecknoglobals
	ErrChannelNotFound     = errors.New("channel not found")                          //nolint:gochecknoglobals
	ErrInvalidMessage      = errors.New("invalid message")                            //nolint:gochecknoglobals
	ErrMessageNotFound     = errors.New("message not found")                          //nolint:gochecknoglobals
	ErrInvalidMembership   = errors.New("invalid membership")                         //nolint:gochecknoglobals
	ErrInvalidReaction     = errors.New("invalid reaction")                           //nolint:gochecknoglobals
	ErrInvalidReadCursor   = errors.New("invalid read cursor")                        //nolint:gochecknoglobals
	ErrInvalidLimit        = errors.New("invalid limit")                              //nolint:gochecknoglobals
	ErrMissingDedupeKey    = errors.New("dedupe_key or Idempotency-Key is required")  //nolint:gochecknoglobals
)
