package orchestration

import "errors"

var (
	ErrInvalidStateTransition   = errors.New("invalid state transition")
	ErrNoPropletAvailable       = errors.New("no proplet available")
	ErrRoundNotFound            = errors.New("round not found")
	ErrRoundTimeout             = errors.New("round timeout")
	ErrInsufficientParticipants = errors.New("insufficient participants")
)
