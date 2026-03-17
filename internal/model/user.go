// Package model contains domain models for the application.
package model

import (
	"strings"
	"time"
)

// User represents a user account in the system.
// It contains authentication information, subscription details, and activity tracking.
type User struct {
	ID               int     `db:"id" json:"id"`
	Email            string  `db:"email" json:"email"`
	AppleSub         *string `db:"apple_sub" json:"-"`
	Role             string  `db:"role" json:"role"` // "free", "pro", "enterprise"
	LastActiveAt     *int64  `db:"last_active_at" json:"lastActiveAt,omitempty"`
	ActiveSessionID  *string `db:"active_session_id" json:"activeSessionId,omitempty"`
	DeviceCode       *string `db:"device_code" json:"deviceCode,omitempty"`
	DeviceType       *string `db:"device_type" json:"deviceType,omitempty"`
	CreatedAt        int64   `db:"created_at" json:"createdAt"`
	UpdatedAt        int64   `db:"updated_at" json:"updatedAt"`
	IsActive         bool    `db:"is_active" json:"isActive"`
	VerificationCode *string `db:"verification_code" json:"-"` // Not exposed in JSON
	CodeExpiry       *int64  `db:"code_expiry" json:"-"`       // Not exposed in JSON
	CodeAttempts     int     `db:"code_attempts" json:"-"`     // Not exposed in JSON

	// 时间余额由 AccountStateService 统一管理，不在 User 表中存储
}

// UserRole represents user subscription tiers.
type UserRole string

const (
	// RoleFree represents a free tier user (免费用户)
	RoleFree UserRole = "free"
	// RoleVip represents a VIP user (付费用户)
	RoleVip UserRole = "vip"
)

// GetRoleLevel returns the numeric level of the role for comparison.
// Higher numbers indicate higher privilege levels.
//
// Returns:
//   - int: Role level (vip=2 > free=1)
func (r UserRole) GetRoleLevel() int {
	switch r {
	case RoleVip:
		return 2
	case RoleFree:
		return 1
	default:
		return 0
	}
}

// IsHigherThan checks if this role is higher than another role.
//
// Parameters:
//   - other: Role to compare against
//
// Returns:
//   - bool: True if this role is higher
func (r UserRole) IsHigherThan(other UserRole) bool {
	return r.GetRoleLevel() > other.GetRoleLevel()
}

// IsValid checks if the user role is valid.
//
// Returns:
//   - bool: True if role is valid
func (r UserRole) IsValid() bool {
	switch r {
	case RoleFree, RoleVip:
		return true
	default:
		return false
	}
}

// String returns the string representation of the role.
//
// Returns:
//   - string: Role as string
func (r UserRole) String() string {
	return string(r)
}

// NewUser creates a new user with default values.
//
// Parameters:
//   - email: User's email address
//
// Returns:
//   - *User: New user instance
func NewUser(email string) *User {
	now := time.Now().Unix()
	return &User{
		Email:     strings.ToLower(strings.TrimSpace(email)),
		Role:      string(RoleFree),
		CreatedAt: now,
		UpdatedAt: now,
		IsActive:  true,
	}
}

// IsUserActive checks if the user account is active.
//
// Returns:
//   - bool: True if user is active
func (u *User) IsUserActive() bool {
	return u.IsActive
}

// GetDisplayName returns the user's display name.
// Currently uses the email prefix (part before @).
//
// Returns:
//   - string: Display name
func (u *User) GetDisplayName() string {
	parts := strings.Split(u.Email, "@")
	if len(parts) > 0 {
		return parts[0]
	}
	return u.Email
}

// GetRole returns the user's role as a UserRole type.
//
// Returns:
//   - UserRole: User's subscription role
func (u *User) GetRole() UserRole {
	return UserRole(u.Role)
}

// IsFree checks if the user is on the free tier.
//
// Returns:
//   - bool: True if user is on free tier
func (u *User) IsFree() bool {
	return u.Role == string(RoleFree)
}

// IsVip checks if the user is a VIP user.
//
// Returns:
//   - bool: True if user is VIP
func (u *User) IsVip() bool {
	return u.Role == string(RoleVip)
}

// HasPaidRole checks if the user has any paid role (not free).
//
// Returns:
//   - bool: True if user has paid role
func (u *User) HasPaidRole() bool {
	return u.IsVip()
}

// CanUpgradeTo checks if the user can upgrade to a target role.
//
// Parameters:
//   - targetRole: Target role to upgrade to
//
// Returns:
//   - bool: True if upgrade is allowed
func (u *User) CanUpgradeTo(targetRole UserRole) bool {
	currentRole := UserRole(u.Role)
	return targetRole.IsHigherThan(currentRole)
}

// HasVerificationCode checks if the user has a pending verification code.
//
// Returns:
//   - bool: True if verification code exists
func (u *User) HasVerificationCode() bool {
	return u.VerificationCode != nil && u.CodeExpiry != nil
}

// IsVerificationCodeExpired checks if the verification code has expired.
//
// Returns:
//   - bool: True if code is expired
func (u *User) IsVerificationCodeExpired() bool {
	if u.CodeExpiry == nil {
		return true
	}
	return time.Now().Unix() > *u.CodeExpiry
}

// CanAttemptVerification checks if the user can attempt verification.
// Returns false if too many attempts have been made.
//
// Parameters:
//   - maxAttempts: Maximum allowed attempts
//
// Returns:
//   - bool: True if user can attempt verification
func (u *User) CanAttemptVerification(maxAttempts int) bool {
	return u.CodeAttempts < maxAttempts
}

// UpdateLastActive updates the user's last active timestamp.
func (u *User) UpdateLastActive() {
	now := time.Now().Unix()
	u.LastActiveAt = &now
	u.UpdatedAt = now
}

// SetVerificationCode sets a new verification code for the user.
//
// Parameters:
//   - code: Verification code
//   - expiryMinutes: Code expiry duration in minutes
func (u *User) SetVerificationCode(code string, expiryMinutes int) {
	expiry := time.Now().Add(time.Duration(expiryMinutes) * time.Minute).Unix()
	u.VerificationCode = &code
	u.CodeExpiry = &expiry
	u.CodeAttempts = 0
	u.UpdatedAt = time.Now().Unix()
}

// ClearVerificationCode clears the verification code and resets attempts.
func (u *User) ClearVerificationCode() {
	u.VerificationCode = nil
	u.CodeExpiry = nil
	u.CodeAttempts = 0
	u.UpdatedAt = time.Now().Unix()
}

// IncrementCodeAttempts increments the verification code attempt counter.
func (u *User) IncrementCodeAttempts() {
	u.CodeAttempts++
	u.UpdatedAt = time.Now().Unix()
}

// SetRole updates the user's subscription role.
//
// Parameters:
//   - role: New user role
func (u *User) SetRole(role UserRole) {
	u.Role = string(role)
	u.UpdatedAt = time.Now().Unix()
}

// Validate validates the user model.
//
// Returns:
//   - error: Validation error if any field is invalid
func (u *User) Validate() error {
	if u.Email == "" {
		return ErrEmailRequired
	}

	if !isValidEmail(u.Email) {
		return ErrEmailInvalid
	}

	if !UserRole(u.Role).IsValid() {
		return ErrInvalidRole
	}

	return nil
}

// Common validation errors
var (
	ErrEmailRequired = &ValidationError{Field: "email", Message: "Email is required"}
	ErrEmailInvalid  = &ValidationError{Field: "email", Message: "Invalid email format"}
	ErrInvalidRole   = &ValidationError{Field: "role", Message: "Invalid user role"}
)

// ValidationError represents a field validation error.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return e.Message
}

// isValidEmail performs basic email validation.
//
// Parameters:
//   - email: Email address to validate
//
// Returns:
//   - bool: True if email format is valid
func isValidEmail(email string) bool {
	// Basic email validation
	if len(email) < 3 || len(email) > 100 {
		return false
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}

	if len(parts[0]) == 0 || len(parts[1]) == 0 {
		return false
	}

	if !strings.Contains(parts[1], ".") {
		return false
	}

	return true
}
