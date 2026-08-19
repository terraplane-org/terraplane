package services

import "errors"

var (
	ErrJobWrongAgent    = errors.New("job is not owned by agent")
	ErrJobInvalidStatus = errors.New("job is not in a valid status for this operation")
)
