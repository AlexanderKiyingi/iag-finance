package models

import "errors"

var (
	ErrNotFound     = errors.New("not found")
	ErrValidation   = errors.New("validation error")
	ErrConflict     = errors.New("conflict")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)
