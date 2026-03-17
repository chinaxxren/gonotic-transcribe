// Package errors provides API response structures.
package errors

import "github.com/gin-gonic/gin"

// APIResponse represents a standard API response.
// All API endpoints should return responses in this format for consistency.
//
// Response format:
// - Success: {"success": true, "code": 1, "data": {...}, "message": "..."}
// - Error:   {"success": false, "code": 4001, "message": "..."}
type APIResponse struct {
	Success bool        `json:"success"`        // Indicates if the request was successful
	Code    int         `json:"code"`           // Numeric response code
	Data    interface{} `json:"data,omitempty"` // Response data (present on success, omitted on error)
	Message string      `json:"message"`        // Response message (always present)
}

// SuccessResponse creates a successful API response.
//
// Parameters:
//   - data: Response data to include
//
// Returns:
//   - APIResponse: Success response with data
//
// Example:
//
//	response := SuccessResponse(map[string]string{"message": "User created"})
func SuccessResponse(data interface{}) APIResponse {
	// Extract message from data if it's a map and remove it from data
	message := "Success"
	var cleanData interface{} = data

	if dataMap, ok := data.(map[string]interface{}); ok {
		if msg, exists := dataMap["message"]; exists {
			if msgStr, ok := msg.(string); ok {
				message = msgStr
				// Create a new map without the message field
				newMap := make(map[string]interface{})
				for k, v := range dataMap {
					if k != "message" {
						newMap[k] = v
					}
				}
				cleanData = newMap
			}
		}
	} else if dataMap, ok := data.(gin.H); ok {
		if msg, exists := dataMap["message"]; exists {
			if msgStr, ok := msg.(string); ok {
				message = msgStr
				// Create a new map without the message field
				newMap := gin.H{}
				for k, v := range dataMap {
					if k != "message" {
						newMap[k] = v
					}
				}
				cleanData = newMap
			}
		}
	}

	resp := APIResponse{
		Success: true,
		Code:    responseCodeFor(SuccessCode),
		Data:    cleanData,
		Message: message,
	}

	return resp
}

// ErrorResponse creates an error API response from an AppError.
//
// Parameters:
//   - err: Application error
//
// Returns:
//   - APIResponse: Error response
//
// Example:
//
//	response := ErrorResponse(errors.New(errors.ErrUserNotFound, "User not found"))
func ErrorResponse(err *AppError) APIResponse {
	return APIResponse{
		Success: false,
		Code:    responseCodeFor(err.Code),
		Message: err.Message,
	}
}

// ErrorResponseWithCode creates an error API response with a code and message.
//
// Parameters:
//   - code: Error code
//   - message: Error message
//
// Returns:
//   - APIResponse: Error response
//
// Example:
//
//	response := ErrorResponseWithCode(errors.ErrValidationFailed, "Invalid input")
func ErrorResponseWithCode(code ErrorCode, message string) APIResponse {
	return APIResponse{
		Success: false,
		Code:    responseCodeFor(code),
		Message: message,
	}
}

// InternalErrorResponse creates a generic internal error response.
// This should be used when we don't want to expose internal error details to clients.
//
// Returns:
//   - APIResponse: Internal error response
func InternalErrorResponse() APIResponse {
	msg := "An internal server error occurred"
	return APIResponse{
		Success: false,
		Code:    responseCodeFor(ErrInternalServer),
		Message: msg,
	}
}

// ValidationErrorResponse creates a validation error response.
//
// Parameters:
//   - message: Validation error message
//
// Returns:
//   - APIResponse: Validation error response
func ValidationErrorResponse(message string) APIResponse {
	return APIResponse{
		Success: false,
		Code:    responseCodeFor(ErrValidationFailed),
		Message: message,
	}
}

// UnauthorizedErrorResponse creates an unauthorized error response.
//
// Returns:
//   - APIResponse: Unauthorized error response
func UnauthorizedErrorResponse() APIResponse {
	msg := "Authentication required"
	return APIResponse{
		Success: false,
		Code:    responseCodeFor(ErrUnauthorized),
		Message: msg,
	}
}

// NotFoundErrorResponse creates a not found error response.
//
// Parameters:
//   - resource: Resource type that was not found
//
// Returns:
//   - APIResponse: Not found error response
func NotFoundErrorResponse(resource string) APIResponse {
	msg := resource + " not found"
	return APIResponse{
		Success: false,
		Code:    responseCodeFor(ErrResourceNotFound),
		Message: msg,
	}
}

// RateLimitErrorResponse creates a rate limit error response.
//
// Returns:
//   - APIResponse: Rate limit error response
func RateLimitErrorResponse() APIResponse {
	msg := "Rate limit exceeded, please try again later"
	return APIResponse{
		Success: false,
		Code:    responseCodeFor(ErrRateLimitExceeded),
		Message: msg,
	}
}

// ForbiddenErrorResponse creates a forbidden error response.
//
// Parameters:
//   - message: Forbidden error message
//
// Returns:
//   - APIResponse: Forbidden error response
func ForbiddenErrorResponse(message string) APIResponse {
	return APIResponse{
		Success: false,
		Code:    responseCodeFor(ErrResourceUnauthorized),
		Message: message,
	}
}

// BadRequestErrorResponse creates a bad request error response.
//
// Parameters:
//   - message: Bad request error message
//
// Returns:
//   - APIResponse: Bad request error response
func BadRequestErrorResponse(message string) APIResponse {
	return APIResponse{
		Success: false,
		Code:    responseCodeFor(ErrValidationFailed),
		Message: message,
	}
}

const defaultErrorCode = 0

var errorCodeIntMap = map[ErrorCode]int{
	SuccessCode: 1,

	ErrInvalidCredentials:      1001,
	ErrTokenExpired:            1002,
	ErrTokenInvalid:            1003,
	ErrVerificationFailed:      1004,
	ErrVerificationCodeExpired:  1005,
	ErrVerificationCodeInvalid:  1006,
	ErrTooManyAttempts:         1007,
	ErrUnauthorized:            1008,
	ErrSessionExpired:          1009,

	ErrValidationFailed: 2000,
	ErrEmailInvalid:     2001,
	ErrEmailRequired:    2002,
	ErrPasswordInvalid:  2003,
	ErrFieldRequired:    2004,
	ErrFieldInvalid:     2005,
	ErrFileTooLarge:     2006,
	ErrFileTypeInvalid:  2007,

	ErrResourceNotFound:      3000,
	ErrResourceUnauthorized:  3001,
	ErrResourceAlreadyExists: 3002,
	ErrResourceConflict:      3003,
	ErrUserNotFound:          3004,
	ErrMeetingNotFound:       3005,
	ErrSubscriptionNotFound:  3006,

	ErrRateLimitExceeded:         4000,
	ErrQuotaExceeded:             4001,
	ErrFeatureNotAvailable:       4002,
	ErrTimeConsumptionFailed:     4003,
	ErrTranscriptionLimitReached: 4004,
	ErrPaymentRequired:           4005,
	ErrSubscriptionInactive:      4006,

	ErrInternalServer:     5000,
	ErrDatabaseError:      5001,
	ErrExternalService:    5002,
	ErrServiceUnavailable: 5003,
	ErrTimeout:            5004,
	ErrConfigurationError:  5005,
	ErrEmailSendFailed:    5006,
	ErrStorageError:       5007,
	ErrWebSocketError:     5008,

	ErrPaymentFailed:        6000,
	ErrPaymentCanceled:      6001,
	ErrInvalidPaymentMethod: 6002,
}

func responseCodeFor(code ErrorCode) int {
	if v, ok := errorCodeIntMap[code]; ok {
		return v
	}
	return defaultErrorCode
}
