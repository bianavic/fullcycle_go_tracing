package domain

import "errors"

var (
	ErrInvalidZipcode  = errors.New("invalid zipcode")
	ErrZipcodeNotFound = errors.New("can not find zipcode")
	ErrUpstream        = errors.New("upstream service unavailable")
)
