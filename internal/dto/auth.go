// Package dto 定义数据传输对象
package dto

// SendVerificationCodeRequest 发送验证码请求
type SendVerificationCodeRequest struct {
	Email string `json:"email" binding:"required,email"` // 邮箱地址
}

// SendVerificationCodeRequestV2 发送验证码请求（API2，携带设备信息）
type SendVerificationCodeRequestV2 struct {
	Email      string `json:"email" binding:"required,email"` // 邮箱地址
	DeviceCode string `json:"device_code" binding:"required"` // 设备唯一标识
	DeviceType string `json:"device_type" binding:"required"` // 设备类型（ios/android/osx等）
}

// VerifyCodeRequest 验证码验证请求
type VerifyCodeRequest struct {
	Email           string `json:"email" binding:"required,email"`             // 邮箱地址
	Code            string `json:"verification_code" binding:"required,len=6"` // 6位验证码
	CreateIfMissing *bool  `json:"create_if_missing,omitempty"`                // 如果用户不存在是否创建（可选，默认 true）
	RememberMe      *bool  `json:"remember_me,omitempty"`                      // 是否记住我（可选，默认 false）
}

// RefreshTokenRequest 刷新令牌请求
type RefreshTokenRequest struct {
	Token string `json:"token" binding:"required"` // 旧令牌
}

// AppleLoginRequest 苹果登录请求
type AppleLoginRequest struct {
	IdentityToken string  `json:"identity_token" binding:"required"`
	Email         *string `json:"email,omitempty"`
	RememberMe    *bool   `json:"remember_me,omitempty"`
}

// AuthResponse 认证响应（兼容旧 Python API）
type AuthResponse struct {
	AccessToken  string    `json:"access_token"`           // JWT 令牌
	TokenType    string    `json:"token_type"`             // 令牌类型（固定 "bearer"）
	User         *UserInfo `json:"user"`                   // 用户信息
	NewlyCreated bool      `json:"newlyCreated,omitempty"` // 是否新创建用户（当前实现始终为 false）
}

// UserInfo 用户信息
type UserInfo struct {
	ID        int    `json:"id"`        // 用户 ID
	Email     string `json:"email"`     // 邮箱
	Role      string `json:"role"`      // 角色
	CreatedAt int64  `json:"createdAt"` // 创建时间
}

// VerifyTokenResponse 验证 token 响应
type VerifyTokenResponse struct {
	Valid  bool   `json:"valid"`   // token 是否有效
	UserID int    `json:"user_id"` // 用户 ID
	Role   string `json:"role"`    // 用户角色
}

// LogoutResponse 登出响应
type LogoutResponse struct {
	Success bool   `json:"success"` // 是否成功
	Message string `json:"message"` // 消息
}
