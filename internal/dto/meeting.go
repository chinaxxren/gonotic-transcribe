// Package dto 定义数据传输对象
package dto

// CreateMeetingRequest 创建会议请求
type CreateMeetingRequest struct {
	Title string `json:"title"` // 会议标题（可选）
}

// MeetingResponse 会议响应
type MeetingResponse struct {
	ID          string `json:"id"`          // 会议 ID（字符串格式）
	UUID        string `json:"uuid"`        // 会话 UUID
	SessionUUID string `json:"sessionUuid"` // 会话 UUID（camelCase）
	Title       string `json:"title"`       // 会议标题
	Description string `json:"description"` // 会议描述
	Duration    int    `json:"duration"`    // 会议时长（秒）
	Status      string `json:"status"`      // 会议状态
	StartTime   int64  `json:"startTime"`   // 会议开始时间（Unix时间戳）
	EndTime     int64  `json:"endTime"`     // 会议结束时间（Unix时间戳）
	CreatedAt   int64  `json:"createdAt"`   // 创建时间（camelCase，Unix时间戳）
	UpdatedAt   int64  `json:"updatedAt"`   // 更新时间（camelCase，Unix时间戳）
}

// MeetingDownloadResponse 会议下载响应
type MeetingDownloadResponse struct {
	Meeting       MeetingResponse `json:"meeting"`       // 会议信息
	Transcription string          `json:"transcription"` // 转录内容
	DownloadedAt  int64           `json:"downloaded_at"` // 下载时间
}

// TranscriptionMessage 转录消息
type TranscriptionMessage struct {
	Text              string                 `json:"text"`                         // 转录文本
	Timestamp         int64                  `json:"timestamp,omitempty"`          // 时间戳
	Speaker           string                 `json:"speaker,omitempty"`            // 说话人
	Language          string                 `json:"language,omitempty"`           // 语言
	TranslationStatus string                 `json:"translation_status,omitempty"` // 翻译状态
	Metadata          map[string]interface{} `json:"metadata,omitempty"`           // 额外信息
}
