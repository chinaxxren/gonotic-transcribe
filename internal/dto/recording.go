// Package dto 定义数据传输对象
package dto

// RecordingTokenResponse 录音 token 响应
type RecordingTokenResponse struct {
	Token            string   `json:"token"`            // Remote API 密钥
	ExpiresAt        *int64   `json:"expires_at"`       // 过期时间（null 表示不过期）
	Scopes           []string `json:"scopes"`           // 权限范围
	TTLSeconds       *int     `json:"ttl_seconds"`      // 有效期（秒）
	RemainingSeconds int      `json:"remainingSeconds"` // 用户剩余时间（秒）
	UserRole         string   `json:"userRole"`         // 用户角色
	TimeChecked      bool     `json:"timeChecked"`      // 是否检查了时间
	ExpiresIn        *int     `json:"expiresIn"`        // 过期时间（秒）
	Note             string   `json:"note"`             // 备注
}

// ValidateTokenRequest 验证 token 请求
type ValidateTokenRequest struct {
	Token string `json:"token" binding:"required"` // Token
}

// ValidateTokenResponse 验证 token 响应
type ValidateTokenResponse struct {
	Valid            bool   `json:"valid"`            // Token 是否有效
	RemainingSeconds int    `json:"remainingSeconds"` // 用户剩余时间（秒）
	UserRole         string `json:"userRole"`         // 用户角色
	TimeChecked      bool   `json:"timeChecked"`      // 是否检查了时间
}

// RecordUsageRequest 记录使用时长请求
type RecordUsageRequest struct {
	DurationSeconds int `json:"duration_seconds" binding:"required"` // 使用时长（秒）
	MeetingID       int `json:"meeting_id"`                          // 会议 ID（可选）
}

// RecordUsageResponse 记录使用时长响应
type RecordUsageResponse struct {
	TotalSeconds     int `json:"totalSeconds"`     // 总时间（秒）
	RemainingSeconds int `json:"remainingSeconds"` // 剩余时间（秒）
	UsedSeconds      int `json:"usedSeconds"`      // 已使用时间（秒）
}

// CreateRecordingSessionRequest 创建转录会话请求
type CreateRecordingSessionRequest struct {
	Language          string `json:"language,omitempty"`           // 会话语言
	EnableTranslation bool   `json:"enable_translation,omitempty"` // 是否启用翻译
}

// CreateRecordingSessionResponse 创建转录会话响应
type CreateRecordingSessionResponse struct {
	SessionID string                 `json:"session_id"` // 会话 ID
	APIKey    string                 `json:"api_key"`    // Remote API Key
	Config    map[string]interface{} `json:"config"`     // 会话配置
}

// RecordingHealthResponse 录音健康检查响应
type RecordingHealthResponse struct {
	Status    string `json:"status"`    // 服务状态
	Service   string `json:"service"`   // 服务名称
	Timestamp int64  `json:"timestamp"` // 时间戳
}

// RecordingUsageStatsResponse 录音使用统计响应
type RecordingUsageStatsResponse struct {
	TotalRequests   int `json:"total_requests"`   // 总请求数
	TotalDuration   int `json:"total_duration"`   // 总时长（秒）
	AverageDuration int `json:"average_duration"` // 平均时长（秒）
}
