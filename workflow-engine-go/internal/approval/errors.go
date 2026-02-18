package approval

import (
	"errors"
	"strconv"
)

var (
	ErrInvalidExpression         = errors.New("invalid expression")
	ErrExpressionTypeMismatch    = errors.New("expression type mismatch")
	ErrInvalidApprovalTransition = errors.New("invalid approval transition")
	ErrUnauthorizedApprovalActor = errors.New("unauthorized approval actor")
)

type SyntaxError struct {
	Position int
	Message  string
}

func (e *SyntaxError) Error() string {
	return "syntax error at position " + strconv.Itoa(e.Position) + ": " + e.Message
}

type TypeError struct {
	Message string
}

func (e *TypeError) Error() string {
	return "type error: " + e.Message
}
