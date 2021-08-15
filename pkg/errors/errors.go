package errors

import (
	_ "github.com/pkg/errors"
)

type ErrorType int

const (
	InternalErrorType ErrorType = iota // Internal Error, log but do not display to user
	ValidateErrorType                  // Validation Error, log and display to user if applicable
)

type Error struct {
	T   ErrorType
	Err error
}

func New(t ErrorType, msg error) Error {
	return Error{t, msg}
}

func (e Error) Error() string {
	return e.Err.Error()
}
