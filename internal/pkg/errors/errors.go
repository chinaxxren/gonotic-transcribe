// Package errors provides error handling utilities.
package errors

import (
	"fmt"
)

// AppError represents an application error with code and details.
// It implements the error interface and provides structured error information.
type AppError struct {
	Code    ErrorCode              // Error code
	Message string                 // Human-readable error message
	Details map[string]interface{} // Additional error details
	Err     error                  // Underlying error (not exposed to client)
}

// Error implements the error interface.
// Returns the error message.
//
// Returns:
//   - string: Error message
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error.
// This allows errors.Is and errors.As to work with wrapped errors.
//
// Returns:
//   - error: Underlying error
func (e *AppError) Unwrap() error {
	return e.Err
}

// WithDetails adds additional details to the error.
//
// Parameters:
//   - key: Detail key
//   - value: Detail value
//
// Returns:
//   - *AppError: Error with added details
func (e *AppError) WithDetails(key string, value interface{}) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// New creates a new application error.
//
// Parameters:
//   - code: Error code
//   - message: Error message
//
// Returns:
//   - *AppError: New application error
func New(code ErrorCode, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
	}
}

// Wrap wraps an existing error with an application error code.
// This preserves the original error for logging while providing
// a structured error for the API response.
//
// Parameters:
//   - code: Error code
//   - message: Error message
//   - err: Underlying error to wrap
//
// Returns:
//   - *AppError: Wrapped application error
func Wrap(code ErrorCode, message string, err error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// NewValidationError creates a validation error.
//
// Parameters:
//   - message: Validation error message
//
// Returns:
//   - *AppError: Validation error
func NewValidationError(message string) *AppError {
	return New(ErrValidationFailed, message)
}

// NewNotFoundError creates a not found error.
//
// Parameters:
//   - resource: Resource type that was not found
//
// Returns:
//   - *AppError: Not found error
func NewNotFoundError(resource string) *AppError {
	return New(ErrResourceNotFound, fmt.Sprintf("%s not found", resource))
}

// NewUnauthorizedError creates an unauthorized error.
//
// Parameters:
//   - message: Unauthorized error message
//
// Returns:
//   - *AppError: Unauthorized error
func NewUnauthorizedError(message string) *AppError {
	return New(ErrUnauthorized, message)
}

// NewInternalError creates an internal server error.
//
// Parameters:
//   - message: Error message
//   - err: Underlying error
//
// Returns:
//   - *AppError: Internal server error
func NewInternalError(message string, err error) *AppError {
	return Wrap(ErrInternalServer, message, err)
}

// NewDatabaseError creates a database error.
//
// Parameters:
//   - message: Error message
//   - err: Underlying database error
//
// Returns:
//   - *AppError: Database error
func NewDatabaseError(message string, err error) *AppError {
	return Wrap(ErrDatabaseError, message, err)
}

// IsAppError checks if an error is an AppError.
//
// Parameters:
//   - err: Error to check
//
// Returns:
//   - bool: True if error is an AppError
func IsAppError(err error) bool {
	_, ok := err.(*AppError)
	return ok
}

// GetAppError extracts an AppError from an error.
// Returns nil if the error is not an AppError.
//
// Parameters:
//   - err: Error to extract from
//
// Returns:
//   - *AppError: Extracted AppError or nil
func GetAppError(err error) *AppError {
	if appErr, ok := err.(*AppError); ok {
		return appErr
	}
	return nil
}

// Common error instances for reuse
var (
	// ErrEmailRequired is returned when email is not provided
	ErrEmailRequiredError = New(ErrEmailRequired, "Email is required")

	// ErrEmailInvalidError is returned when email format is invalid
	ErrEmailInvalidError = New(ErrEmailInvalid, "Invalid email format")

	// ErrUserNotFoundError is returned when user is not found
	ErrUserNotFoundError = New(ErrUserNotFound, "User not found")

	// ErrMeetingNotFoundError is returned when meeting is not found
	ErrMeetingNotFoundError = New(ErrMeetingNotFound, "Meeting not found")

	// ErrUnauthorizedAccessError is returned when user cannot access resource
	ErrUnauthorizedAccessError = New(ErrResourceUnauthorized, "Unauthorized access to resource")

	// ErrInvalidTokenError is returned when JWT token is invalid
	ErrInvalidTokenError = New(ErrTokenInvalid, "Invalid authentication token")

	// ErrExpiredTokenError is returned when JWT token is expired
	ErrExpiredTokenError = New(ErrTokenExpired, "Authentication token has expired")

	// ErrRateLimitError is returned when rate limit is exceeded
	ErrRateLimitError = New(ErrRateLimitExceeded, "Rate limit exceeded, please try again later")
)
