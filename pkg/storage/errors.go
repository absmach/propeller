package storage

import "errors"

var (
	ErrDBConnection    = errors.New("database connection error")
	ErrDBQuery         = errors.New("database query error")
	ErrDBScan          = errors.New("database scan error")
	ErrMigration       = errors.New("database migration error")
	ErrCreate          = errors.New("create error")
	ErrUpdate          = errors.New("update error")
	ErrDelete          = errors.New("delete error")
	ErrInvalidID       = errors.New("invalid ID")
	ErrTaskNotFound    = errors.New("task not found")
	ErrPropletNotFound = errors.New("proplet not found")
	ErrNotFound        = errors.New("not found")
)
