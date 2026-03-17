// Package dto 定义数据传输对象
package dto

// UpdateProfileRequest 更新个人资料请求
type UpdateProfileRequest struct {
	Email *string `json:"email,omitempty"` // 邮箱地址（可选）
}

// UserProfileResponse 用户个人资料响应
type UserProfileResponse struct {
	ID               int    `json:"id"`               // 用户 ID
	Email            string `json:"email"`            // 邮箱地址
	Role             string `json:"role"`             // 用户角色
	IsActive         bool   `json:"is_active"`        // 是否激活（下划线格式）
	IsActiveCompat   bool   `json:"isActive"`         // 是否激活（驼峰格式，移动端兼容）
	IsUnlimited      bool   `json:"isUnlimited"`      // 是否无限制（兼容移动端）
	CreatedAt        int64  `json:"created_at"`       // 创建时间（下划线格式）
	CreatedAtCompat  int64  `json:"createdAt"`        // 创建时间（驼峰格式，移动端兼容）
	UpdatedAt        int64  `json:"updated_at"`       // 更新时间（下划线格式）
	UpdatedAtCompat  int64  `json:"updatedAt"`        // 更新时间（驼峰格式，移动端兼容）
	LastActiveAt     int64  `json:"last_active"`      // 最后活跃时间（下划线格式）
	LastActiveCompat int64  `json:"lastActive"`       // 最后活跃时间（驼峰格式，移动端兼容）
	RemainingSeconds int    `json:"remainingSeconds"` // 剩余时间（秒）
	TotalSeconds     int    `json:"totalSeconds"`     // 总时间（秒）- 移动端必需
	UsedSeconds      int    `json:"usedSeconds"`      // 已使用时间（秒）- 移动端必需
}

// SubscriptionResponse 订阅信息响应
type SubscriptionResponse struct {
	Role              string `json:"role"`               // 用户角色
	IsSubscribed      bool   `json:"is_subscribed"`      // 是否订阅
	SubscriptionLevel string `json:"subscription_level"` // 订阅级别
}

type SubscriptionV2Response struct {
	SubscriptionType string `json:"subscription_type"`
	ExpiresAt        *int64 `json:"expires_at,omitempty"`
}

// UserTimeResponse 用户时间信息响应
type UserTimeResponse struct {
	TotalSeconds     int    `json:"totalSeconds"`          // 总时间（秒）
	RemainingSeconds int    `json:"remainingSeconds"`      // 剩余时间（秒）
	UsedSeconds      int    `json:"usedSeconds"`           // 已使用时间（秒）
	IsUnlimited      bool   `json:"isUnlimited"`           // 是否无限制
	LastUsedAt       *int64 `json:"lastUsedAt,omitempty"`  // 最后使用时间
	LastAddedAt      *int64 `json:"lastAddedAt,omitempty"` // 最后添加时间
}

// UserTimeBalanceResponse 用户时间余额详情响应
type UserTimeBalanceResponse struct {
	TotalSeconds     int    `json:"totalSeconds"`          // 总时间（秒）
	RemainingSeconds int    `json:"remainingSeconds"`      // 剩余时间（秒）
	UsedSeconds      int    `json:"usedSeconds"`           // 已使用时间（秒）
	IsUnlimited      bool   `json:"isUnlimited"`           // 是否无限制
	LastUsedAt       *int64 `json:"lastUsedAt,omitempty"`  // 最后使用时间
	LastAddedAt      *int64 `json:"lastAddedAt,omitempty"` // 最后添加时间
	Role             string `json:"role"`                  // 用户角色
}

// UpdateTimeUsageRequest 用户侧时间扣减请求
type UpdateTimeUsageRequest struct {
	SecondsUsed *int `json:"seconds_used,omitempty"` // 使用的秒数
	MinutesUsed *int `json:"minutes_used,omitempty"` // 使用的分钟数
}

// UserStatsResponse 用户统计响应
type UserStatsResponse struct {
	TotalMeetings       int `json:"total_meetings"`       // 会议总数
	TotalDuration       int `json:"total_duration"`       // 总时长（秒）
	TotalTranscriptions int `json:"total_transcriptions"` // 转录总数
}

// RenewSubscriptionRequest 续费订阅请求
type RenewSubscriptionRequest struct {
	TimeType  string `json:"time_type" binding:"required"`  // 时间类型
	Seconds   int    `json:"seconds" binding:"required"`    // 续费时间（秒）
	ExpiresAt int64  `json:"expires_at" binding:"required"` // 过期时间戳
	SourceID  *int   `json:"source_id,omitempty"`           // 来源 ID（可选）
}

// RenewSubscriptionResponse 续费订阅响应
type RenewSubscriptionResponse struct {
	TimeType       string `json:"time_type"`       // 时间类型
	BalanceSeconds int    `json:"balance_seconds"` // 余额（秒）
	ExpiresAt      int64  `json:"expires_at"`      // 过期时间戳
}
