package errors

import "errors"

var (
	ErrNotFound     = errors.New("not found")
	ErrEmptyKey     = errors.New("empty key")
	ErrInvalidData  = errors.New("invalid data type")
	ErrEntityExists = errors.New("entity already exists")
)
