package model

type VerificationRequest struct {
	Email            string  `db:"email" json:"email"`
	VerificationCode string  `db:"verification_code" json:"-"`
	CodeExpiry       int64   `db:"code_expiry" json:"-"`
	CodeAttempts     int     `db:"code_attempts" json:"-"`
	DeviceCode       *string `db:"device_code" json:"deviceCode,omitempty"`
	DeviceType       *string `db:"device_type" json:"deviceType,omitempty"`
	CreatedAt        int64   `db:"created_at" json:"createdAt"`
	UpdatedAt        int64   `db:"updated_at" json:"updatedAt"`
}
