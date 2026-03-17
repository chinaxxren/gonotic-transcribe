// Package service 提供业务逻辑实现
package service

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// MessageType 定义消息类型
type MessageType string

const (
	// 客户端消息类型
	MessageTypeAuth      MessageType = "auth"
	MessageTypeStart     MessageType = "start"
	MessageTypeAudio     MessageType = "audio"
	MessageTypePause     MessageType = "pause"
	MessageTypeResume    MessageType = "resume"
	MessageTypeStop      MessageType = "stop"
	MessageTypePing      MessageType = "ping"
	MessageTypeKeepalive MessageType = "keepalive"

	// 服务器消息类型
	MessageTypeConnected          MessageType = "connected"
	MessageTypeStarted            MessageType = "started"
	MessageTypeTranscription      MessageType = "transcription"
	MessageTypeStopped            MessageType = "stopped"
	MessageTypePong               MessageType = "pong"
	MessageTypeWarning            MessageType = "warning"
	MessageTypeTimeWarning        MessageType = "time_warning"
	MessageTypeCriticalWarning    MessageType = "critical_warning"
	MessageTypeTimeExhausted      MessageType = "time_exhausted"
	MessageTypeTimeExpired        MessageType = "time_expired"
	MessageTypeStopAndClear       MessageType = "stop_and_clear"
	MessageTypeServiceUnavailable MessageType = "service_unavailable"
	MessageTypeSessionEnd         MessageType = "session_end"
	MessageTypeError              MessageType = "error"
)

// SessionStatus 定义会话状态
type SessionStatus string

const (
	StatusStarting  SessionStatus = "starting"
	StatusActive    SessionStatus = "active"
	StatusStopping  SessionStatus = "stopping"
	StatusCompleted SessionStatus = "completed"
	StatusError     SessionStatus = "error"
)

// ClientMessage 客户端发送的消息
type ClientMessage struct {
	Type      MessageType     `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// ServerMessage 服务器发送的消息
type ServerMessage struct {
	Type      MessageType `json:"type"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp int64       `json:"timestamp"` // Unix 时间戳（秒）
}

// AuthMessage 认证消息
type AuthMessage struct {
	Token string `json:"token"`
}

// StartMessage 开始转录消息
type StartMessage struct {
	MeetingID int        `json:"meeting_id"`
	Config    *STTConfig `json:"config,omitempty"`
}

// AudioMessage 音频数据消息（JSON 格式）
type AudioMessage struct {
	Data   string `json:"data"`             // Base64 编码的音频数据
	Format string `json:"format,omitempty"` // 音频格式（可选）
}

// StopMessage 停止转录消息
type StopMessage struct {
	// 可以为空，或包含额外信息
}

// PingMessage 心跳消息
type PingMessage struct {
	// 可以为空
}

// ConnectedData 连接确认数据
type ConnectedData struct {
	Message   string `json:"message"`
	UserID    int    `json:"user_id"`
	MeetingID int    `json:"meeting_id,omitempty"`
}

// StartedData 转录开始确认数据
type StartedData struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// TranscriptionResult 转录结果
type TranscriptionResult struct {
	Text       string  `json:"text"`
	IsFinal    bool    `json:"is_final"`
	Confidence float64 `json:"confidence,omitempty"`
	StartTime  float64 `json:"start_time,omitempty"`
	EndTime    float64 `json:"end_time,omitempty"`
	Speaker    string  `json:"speaker,omitempty"`
	Timestamp  int64   `json:"timestamp"`
}

// StoppedData 转录停止确认数据
type StoppedData struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	Duration  int    `json:"duration,omitempty"` // 秒
}

// PongData 心跳响应数据
type PongData struct {
	Timestamp int64 `json:"timestamp"` // Unix 时间戳（秒）
}

// WarningData 警告消息数据
type WarningData struct {
	Message          string `json:"message"`
	RemainingSeconds int    `json:"remaining_seconds,omitempty"`
	UsagePercent     int    `json:"usage_percent,omitempty"`
}

// SessionEndData 会话结束数据
type SessionEndData struct {
	Reason        string `json:"reason"`
	TotalDuration int    `json:"total_duration"` // 秒
}

// ErrorData 错误消息数据
type ErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// STTConfig STT 服务配置
type STTConfig struct {
	Language   string `json:"language,omitempty"`    // 语言代码，如 "zh", "en"
	SampleRate int    `json:"sample_rate,omitempty"` // 采样率，默认 16000
	Channels   int    `json:"channels,omitempty"`    // 声道数，默认 1
	Format     string `json:"format,omitempty"`      // 音频格式，如 "pcm", "wav"
}

// TranscriptionError 转录错误类型
type TranscriptionError struct {
	Code    string
	Message string
	Cause   error
}

// Error 实现 error 接口
func (e *TranscriptionError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// Unwrap 支持错误链
func (e *TranscriptionError) Unwrap() error {
	return e.Cause
}

// 错误码常量
const (
	ErrCodeUnauthorized     = "UNAUTHORIZED"
	ErrCodeQuotaExceeded    = "QUOTA_EXCEEDED"
	ErrCodeSessionNotFound  = "SESSION_NOT_FOUND"
	ErrCodeSessionExists    = "SESSION_EXISTS"
	ErrCodeInvalidMessage   = "INVALID_MESSAGE"
	ErrCodeAudioFormat      = "AUDIO_FORMAT_ERROR"
	ErrCodeSTTUnavailable   = "STT_UNAVAILABLE"
	ErrCodeSTTConnection    = "STT_CONNECTION_ERROR"
	ErrCodeInternalError    = "INTERNAL_ERROR"
	ErrCodePermissionDenied = "PERMISSION_DENIED"
	ErrCodeInvalidSession   = "INVALID_SESSION"
	ErrCodeTimeout          = "TIMEOUT"
)

// NewTranscriptionError 创建新的转录错误
func NewTranscriptionError(code, message string, cause error) *TranscriptionError {
	return &TranscriptionError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// ValidateClientMessage 验证客户端消息
func ValidateClientMessage(msg *ClientMessage) error {
	if msg.Type == "" {
		return NewTranscriptionError(ErrCodeInvalidMessage, "消息类型不能为空", nil)
	}

	// 验证消息类型是否有效
	validTypes := map[MessageType]bool{
		MessageTypeAuth:  true,
		MessageTypeStart: true,
		MessageTypeAudio: true,
		MessageTypeStop:  true,
		MessageTypePing:  true,
	}

	if !validTypes[msg.Type] {
		return NewTranscriptionError(ErrCodeInvalidMessage, "无效的消息类型: "+string(msg.Type), nil)
	}

	return nil
}

// ParseStartMessage 解析开始转录消息
func ParseStartMessage(data json.RawMessage) (*StartMessage, error) {
	// 检查数据是否为空
	if len(data) == 0 {
		return nil, NewTranscriptionError(ErrCodeInvalidMessage, "Start message data is empty", nil)
	}

	// 检查是否是 null 或空对象
	dataStr := string(data)
	if dataStr == "null" || dataStr == "{}" || dataStr == "" {
		return nil, NewTranscriptionError(ErrCodeInvalidMessage, "Start message data is null or empty", nil)
	}

	var msg StartMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, NewTranscriptionError(ErrCodeInvalidMessage, "Failed to parse start message: "+err.Error(), err)
	}

	if msg.MeetingID <= 0 {
		return nil, NewTranscriptionError(ErrCodeInvalidMessage, "meeting_id must be greater than 0", nil)
	}

	// 设置默认配置
	if msg.Config == nil {
		msg.Config = &STTConfig{
			Language:   "en",
			SampleRate: 16000,
			Channels:   1,
			Format:     "aac",
		}
	} else {
		// 填充默认值
		if msg.Config.Language == "" {
			msg.Config.Language = "en"
		}
		if msg.Config.SampleRate == 0 {
			msg.Config.SampleRate = 16000
		}
		if msg.Config.Channels == 0 {
			msg.Config.Channels = 1
		}
		if msg.Config.Format == "" {
			msg.Config.Format = "aac"
		}
	}

	return &msg, nil
}

// ParseAudioMessage 解析音频消息
func ParseAudioMessage(data json.RawMessage) (*AudioMessage, error) {
	var msg AudioMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, NewTranscriptionError(ErrCodeInvalidMessage, "解析 audio 消息失败", err)
	}

	if msg.Data == "" {
		return nil, NewTranscriptionError(ErrCodeInvalidMessage, "音频数据不能为空", nil)
	}

	return &msg, nil
}

// CreateServerMessage 创建服务器消息
func CreateServerMessage(msgType MessageType, data interface{}) *ServerMessage {
	return &ServerMessage{
		Type:      msgType,
		Data:      data,
		Timestamp: time.Now().Unix(),
	}
}

// CreateErrorMessage 创建错误消息
func CreateErrorMessage(code, message string) *ServerMessage {
	return &ServerMessage{
		Type:  MessageTypeError,
		Error: message,
		Data: ErrorData{
			Code:    code,
			Message: message,
		},
		Timestamp: time.Now().Unix(),
	}
}

// TranscriptionStats 转录统计信息
type TranscriptionStats struct {
	TotalConnections int64
	TotalSessions    int64
	TotalAudioBytes  int64
	TotalResults     int64
	TotalErrors      int64
	STTErrors        int64
	mu               sync.RWMutex
}

// NewTranscriptionStats 创建新的统计实例
func NewTranscriptionStats() *TranscriptionStats {
	return &TranscriptionStats{}
}

// IncrementConnections 增加连接计数
func (s *TranscriptionStats) IncrementConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalConnections++
}

// IncrementSessions 增加会话计数
func (s *TranscriptionStats) IncrementSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalSessions++
}

// AddAudioBytes 添加音频字节数
func (s *TranscriptionStats) AddAudioBytes(bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalAudioBytes += bytes
}

// IncrementResults 增加结果计数
func (s *TranscriptionStats) IncrementResults() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalResults++
}

// IncrementErrors 增加错误计数
func (s *TranscriptionStats) IncrementErrors() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalErrors++
}

// IncrementSTTErrors 增加 STT 错误计数
func (s *TranscriptionStats) IncrementSTTErrors() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.STTErrors++
}

// GetStats 获取统计信息
func (s *TranscriptionStats) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"total_connections": s.TotalConnections,
		"total_sessions":    s.TotalSessions,
		"total_audio_bytes": s.TotalAudioBytes,
		"total_results":     s.TotalResults,
		"total_errors":      s.TotalErrors,
		"stt_errors":        s.STTErrors,
	}
}

// NewStartedMessage 创建 started 消息
// 格式：{"type": "started", "session_uuid": "123", "meeting_id": "242"}
func NewStartedMessage(sessionUUID string, meetingID *int, remainingSeconds int) map[string]interface{} {
	msg := map[string]interface{}{
		"type":         string(MessageTypeStarted),
		"session_uuid": sessionUUID,
	}
	if meetingID != nil {
		// meeting_id 应该是字符串格式
		msg["meeting_id"] = fmt.Sprintf("%d", *meetingID)
	}
	return msg
}

// NewTranscriptionMessage 创建转录结果消息（旧格式，保留以兼容）
// 格式：{"type": "transcription", "text": "...", "language": "...", "translation_status": "...", "is_final": false, "timestamp": 1761198021}
func NewTranscriptionMessage(text string, isFinal bool, segment int) map[string]interface{} {
	// timestamp 使用整数（Unix 时间戳秒）
	return map[string]interface{}{
		"type":      string(MessageTypeTranscription),
		"text":      text,
		"is_final":  isFinal,
		"timestamp": time.Now().Unix(),
		"segment":   segment,
	}
}

// NewFrontendTranscriptionMessage 创建前端转录消息格式
// 格式：{"type": "transcription", "text": "...", "temp": "...", "language": "...", "speaker": "...", "translation_status": "...", "timestamp": 1761480411}
// 此格式用于发送给客户端，与 Python 版本保持一致
// text: 只包含 is_final=True 的 tokens
// temp: 只包含 is_final=False 的 tokens
type FrontendTranscriptionMessage struct {
	Type              MessageType `json:"type"`
	Text              string      `json:"text"`
	Temp              string      `json:"temp"`
	Timestamp         int64       `json:"timestamp"`
	TranslationStatus string      `json:"translation_status"`
	Speaker           string      `json:"speaker,omitempty"`
	Language          string      `json:"language,omitempty"`
	ReceiveTime       int64       `json:"receive_time,omitempty"`
	SendTime          int64       `json:"send_time,omitempty"`
}

func NewFrontendTranscriptionMessage(
	text string,
	temp string,
	speaker string,
	timestamp int64,
	language string,
	translationStatus string,
) *FrontendTranscriptionMessage {
	return &FrontendTranscriptionMessage{
		Type:              MessageTypeTranscription,
		Text:              text,
		Temp:              temp,
		Timestamp:         timestamp,
		TranslationStatus: translationStatus,
		Speaker:           speaker,
		Language:          language,
	}
}

// NewUnifiedTranscriptionMessage 创建统一格式的转录消息（用于数据库存储）
// 格式：{"text": "...", "speaker": "Speaker 1", "timestamp": 1761480411, "language": "en", "translation_status": "original", "uid": 1, "meeting_id": 1002}
// 此格式用于存储到数据库，包含 uid 和 meeting_id
func NewUnifiedTranscriptionMessage(
	text string,
	speaker string,
	timestamp int64,
	language string,
	translationStatus string,
	uid int,
	meetingID int,
) map[string]interface{} {
	msg := map[string]interface{}{
		"text":               text,
		"timestamp":          timestamp,
		"speaker":            speaker,
		"language":           language,
		"translation_status": translationStatus,
		"uid":                uid,
		"meeting_id":         meetingID,
	}

	return msg
}

// NewTimeWarningMessage 创建时间警告消息
// 格式：{"type": "time_warning", "warning_type": "10 minutes", "remaining_minutes": 8.5, "remaining_seconds": 510, "timestamp": 1761198021, "message": "..."}
func NewTimeWarningMessage(warningType string, remainingSeconds int) map[string]interface{} {
	// timestamp 使用整数（Unix 时间戳秒）
	remainingMinutes := float64(remainingSeconds) / 60.0
	message := fmt.Sprintf("Time warning: %.1f minutes remaining", remainingMinutes)
	return map[string]interface{}{
		"type":              string(MessageTypeTimeWarning),
		"warning_type":      warningType,
		"remaining_minutes": remainingMinutes,
		"remaining_seconds": remainingSeconds,
		"timestamp":         time.Now().Unix(),
		"message":           message,
	}
}

// NewCriticalWarningMessage 创建临界警告消息
func NewCriticalWarningMessage(remainingSeconds int) map[string]interface{} {
	message := fmt.Sprintf("Time almost exhausted! Only %d seconds remaining", remainingSeconds)
	return map[string]interface{}{
		"type":              string(MessageTypeCriticalWarning),
		"code":              "1103",
		"remaining_seconds": remainingSeconds,
		"message":           message,
	}
}

// NewTimeExhaustedMessage 创建时间耗尽消息
func NewTimeExhaustedMessage() map[string]interface{} {
	return map[string]interface{}{
		"type":    string(MessageTypeTimeExhausted),
		"code":    "1101",
		"message": "Minutes used for this period. Upgrade in Settings for more time.",
	}
}

// NewStoppedMessage 创建停止确认消息
// 格式：{"type": "stopped", "session_uuid": "...", "meeting_id": "242", "timestamp": 1761198021000, "reason": "server_stop", "remaining_seconds": 180}
func NewStoppedMessage(sessionUUID string, meetingID *int, reason string, remainingSeconds *int) map[string]interface{} {
	msg := map[string]interface{}{
		"type":         string(MessageTypeStopped),
		"session_uuid": sessionUUID,
		"timestamp":    time.Now().UnixMilli(), // 毫秒整数
		"reason":       reason,
	}
	if meetingID != nil {
		// meeting_id 应该是字符串格式
		msg["meeting_id"] = fmt.Sprintf("%d", *meetingID)
	}
	if remainingSeconds != nil {
		msg["remaining_seconds"] = *remainingSeconds
	}
	return msg
}

// NewErrorMessage 创建错误消息
// 格式：{"type": "error", "code": "1204", "message": "...", "details": "..."}
func NewErrorMessage(code string, message string, details string) map[string]interface{} {
	msg := map[string]interface{}{
		"type":    string(MessageTypeError),
		"code":    code,
		"message": message,
	}
	if details != "" {
		msg["details"] = details
	}
	return msg
}

// NewStopAndClearMessage 创建 stop_and_clear 消息（与 Python 版本一致）
// 格式：{"type": "stop_and_clear", "code": "1205", "message": "..."}
// 用于通知客户端停止并清除状态（通常在恢复失败或状态不一致时使用）
func NewStopAndClearMessage(message string) map[string]interface{} {
	return map[string]interface{}{
		"type":    string(MessageTypeStopAndClear),
		"code":    "1205", // TRANSCRIPTION_RESUME_FAILED 或类似错误码
		"message": message,
	}
}

// NewServiceUnavailableMessage 创建服务不可用消息
// 格式：{"type": "service_unavailable", "code": "1205", "message": "...", "retry": false, "action": "close_meeting"}
func NewServiceUnavailableMessage(reason string, details string) map[string]interface{} {
	message := reason
	if details != "" {
		message = details
	}
	return map[string]interface{}{
		"type":    string(MessageTypeServiceUnavailable),
		"code":    "1205",
		"message": message,
		"retry":   false,
		"action":  "close_meeting",
	}
}

// WebSocketMessage WebSocket 消息结构
type WebSocketMessage struct {
	Type       MessageType            `json:"type"`
	Version    string                 `json:"version,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	ClientTime *float64               `json:"clientTime,omitempty"`
	Success    *bool                  `json:"success,omitempty"`
	Message    string                 `json:"message,omitempty"`
	Code       *int                   `json:"code,omitempty"`
}
