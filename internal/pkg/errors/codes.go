// Package errors provides error handling utilities and error code definitions.
// It maintains compatibility with the Python backend error codes.
package errors

// ErrorCode represents an application error code.
// Error codes are used to provide consistent error identification across the API.
type ErrorCode string

// Success code
const (
	// SuccessCode indicates a successful operation
	SuccessCode ErrorCode = "SUCCESS"
)

// Authentication errors (1xxx range)
const (
	// ErrInvalidCredentials indicates invalid login credentials
	ErrInvalidCredentials ErrorCode = "AUTH_INVALID_CREDENTIALS"

	// ErrTokenExpired indicates the JWT token has expired
	ErrTokenExpired ErrorCode = "AUTH_TOKEN_EXPIRED"

	// ErrTokenInvalid indicates the JWT token is invalid or malformed
	ErrTokenInvalid ErrorCode = "AUTH_TOKEN_INVALID"

	// ErrVerificationFailed indicates verification code validation failed
	ErrVerificationFailed ErrorCode = "AUTH_VERIFICATION_FAILED"

	// ErrVerificationCodeExpired indicates the verification code has expired
	ErrVerificationCodeExpired ErrorCode = "AUTH_VERIFICATION_CODE_EXPIRED"

	// ErrVerificationCodeInvalid indicates the verification code is incorrect
	ErrVerificationCodeInvalid ErrorCode = "AUTH_VERIFICATION_CODE_INVALID"

	// ErrTooManyAttempts indicates too many verification attempts
	ErrTooManyAttempts ErrorCode = "AUTH_TOO_MANY_ATTEMPTS"

	// ErrUnauthorized indicates the user is not authenticated
	ErrUnauthorized ErrorCode = "AUTH_UNAUTHORIZED"

	// ErrSessionExpired indicates the user session has expired
	ErrSessionExpired ErrorCode = "AUTH_SESSION_EXPIRED"
)

// Validation errors (2xxx range)
const (
	// ErrValidationFailed indicates general validation failure
	ErrValidationFailed ErrorCode = "VALIDATION_FAILED"

	// ErrEmailInvalid indicates invalid email format
	ErrEmailInvalid ErrorCode = "VALIDATION_EMAIL_INVALID"

	// ErrEmailRequired indicates email is required but not provided
	ErrEmailRequired ErrorCode = "VALIDATION_EMAIL_REQUIRED"

	// ErrPasswordInvalid indicates invalid password format
	ErrPasswordInvalid ErrorCode = "VALIDATION_PASSWORD_INVALID"

	// ErrFieldRequired indicates a required field is missing
	ErrFieldRequired ErrorCode = "VALIDATION_FIELD_REQUIRED"

	// ErrFieldInvalid indicates a field has an invalid value
	ErrFieldInvalid ErrorCode = "VALIDATION_FIELD_INVALID"

	// ErrFileTooLarge indicates uploaded file exceeds size limit
	ErrFileTooLarge ErrorCode = "VALIDATION_FILE_TOO_LARGE"

	// ErrFileTypeInvalid indicates uploaded file type is not allowed
	ErrFileTypeInvalid ErrorCode = "VALIDATION_FILE_TYPE_INVALID"
)

// Resource errors (3xxx range)
const (
	// ErrResourceNotFound indicates the requested resource does not exist
	ErrResourceNotFound ErrorCode = "RESOURCE_NOT_FOUND"

	// ErrResourceUnauthorized indicates the user cannot access the resource
	ErrResourceUnauthorized ErrorCode = "RESOURCE_UNAUTHORIZED"

	// ErrResourceAlreadyExists indicates the resource already exists
	ErrResourceAlreadyExists ErrorCode = "RESOURCE_ALREADY_EXISTS"

	// ErrResourceConflict indicates a conflict with existing resource
	ErrResourceConflict ErrorCode = "RESOURCE_CONFLICT"

	// ErrUserNotFound indicates the user does not exist
	ErrUserNotFound ErrorCode = "RESOURCE_USER_NOT_FOUND"

	// ErrMeetingNotFound indicates the meeting does not exist
	ErrMeetingNotFound ErrorCode = "RESOURCE_MEETING_NOT_FOUND"

	// ErrSubscriptionNotFound indicates the subscription does not exist
	ErrSubscriptionNotFound ErrorCode = "RESOURCE_SUBSCRIPTION_NOT_FOUND"
)

// Business logic errors (4xxx range)
const (
	// ErrRateLimitExceeded indicates rate limit has been exceeded
	ErrRateLimitExceeded ErrorCode = "BUSINESS_RATE_LIMIT_EXCEEDED"

	// ErrQuotaExceeded indicates user quota has been exceeded
	ErrQuotaExceeded ErrorCode = "BUSINESS_QUOTA_EXCEEDED"

	// ErrFeatureNotAvailable indicates feature is not available for user's plan
	ErrFeatureNotAvailable ErrorCode = "BUSINESS_FEATURE_NOT_AVAILABLE"

	// ErrTimeConsumptionFailed indicates user time deduction failed
	ErrTimeConsumptionFailed ErrorCode = "TIME_CONSUMPTION_FAILED"

	// ErrTranscriptionLimitReached indicates transcription time limit reached
	ErrTranscriptionLimitReached ErrorCode = "BUSINESS_TRANSCRIPTION_LIMIT_REACHED"

	// ErrPaymentRequired indicates payment is required to proceed
	ErrPaymentRequired ErrorCode = "BUSINESS_PAYMENT_REQUIRED"

	// ErrSubscriptionInactive indicates user subscription is not active
	ErrSubscriptionInactive ErrorCode = "BUSINESS_SUBSCRIPTION_INACTIVE"
)

// System errors (5xxx range)
const (
	// ErrInternalServer indicates an internal server error
	ErrInternalServer ErrorCode = "SYSTEM_INTERNAL_ERROR"

	// ErrDatabaseError indicates a database operation error
	ErrDatabaseError ErrorCode = "SYSTEM_DATABASE_ERROR"

	// ErrExternalService indicates an external service error
	ErrExternalService ErrorCode = "SYSTEM_EXTERNAL_SERVICE_ERROR"

	// ErrServiceUnavailable indicates a service is temporarily unavailable
	ErrServiceUnavailable ErrorCode = "SYSTEM_SERVICE_UNAVAILABLE"

	// ErrTimeout indicates an operation timeout
	ErrTimeout ErrorCode = "SYSTEM_TIMEOUT"

	// ErrConfigurationError indicates a configuration error
	ErrConfigurationError ErrorCode = "SYSTEM_CONFIGURATION_ERROR"

	// ErrEmailSendFailed indicates email sending failed
	ErrEmailSendFailed ErrorCode = "SYSTEM_EMAIL_SEND_FAILED"

	// ErrStorageError indicates a storage operation error
	ErrStorageError ErrorCode = "SYSTEM_STORAGE_ERROR"

	// ErrWebSocketError indicates a WebSocket operation error
	ErrWebSocketError ErrorCode = "SYSTEM_WEBSOCKET_ERROR"
)

// Payment errors (6xxx range)
const (
	// ErrPaymentFailed indicates payment processing failed
	ErrPaymentFailed ErrorCode = "PAYMENT_FAILED"

	// ErrPaymentCanceled indicates payment was canceled
	ErrPaymentCanceled ErrorCode = "PAYMENT_CANCELED"

	// ErrInvalidPaymentMethod indicates invalid payment method
	ErrInvalidPaymentMethod ErrorCode = "PAYMENT_INVALID_METHOD"
)

// String returns the string representation of the error code.
//
// Returns:
//   - string: Error code as string
func (e ErrorCode) String() string {
	return string(e)
}

// HTTPStatus returns the recommended HTTP status code for this error code.
// This provides a mapping from application error codes to HTTP status codes.
//
// Returns:
//   - int: HTTP status code
func (e ErrorCode) HTTPStatus() int {
	switch e {
	// Success
	case SuccessCode:
		return 200

	// Authentication errors -> 401 Unauthorized
	case ErrInvalidCredentials, ErrTokenExpired, ErrTokenInvalid,
		ErrVerificationFailed, ErrVerificationCodeExpired,
		ErrVerificationCodeInvalid, ErrUnauthorized, ErrSessionExpired:
		return 401

	// Too many attempts -> 429 Too Many Requests
	case ErrTooManyAttempts, ErrRateLimitExceeded:
		return 429

	// Validation errors -> 400 Bad Request
	case ErrValidationFailed, ErrEmailInvalid, ErrEmailRequired,
		ErrPasswordInvalid, ErrFieldRequired, ErrFieldInvalid,
		ErrFileTooLarge, ErrFileTypeInvalid:
		return 400

	// Resource not found -> 404 Not Found
	case ErrResourceNotFound, ErrUserNotFound, ErrMeetingNotFound,
		ErrSubscriptionNotFound:
		return 404

	// Resource unauthorized -> 403 Forbidden
	case ErrResourceUnauthorized:
		return 403

	// Resource conflicts -> 409 Conflict
	case ErrResourceAlreadyExists, ErrResourceConflict:
		return 409

	// Business logic errors -> 402 Payment Required or 403 Forbidden
	case ErrPaymentRequired:
		return 402
	case ErrQuotaExceeded, ErrFeatureNotAvailable,
		ErrTranscriptionLimitReached, ErrSubscriptionInactive:
		return 403

	// Time consumption failure -> 400 Bad Request
	case ErrTimeConsumptionFailed:
		return 400

	// Payment errors -> 402 Payment Required
	case ErrPaymentFailed, ErrPaymentCanceled, ErrInvalidPaymentMethod:
		return 402

	// Service unavailable -> 503 Service Unavailable
	case ErrServiceUnavailable, ErrTimeout:
		return 503

	// System errors -> 500 Internal Server Error
	default:
		return 500
	}
}
