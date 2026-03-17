// Package service 提供业务逻辑实现
package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SessionState 会话状态（用于恢复）
type SessionState struct {
	SessionUUID    string    `json:"session_uuid"`
	UserID         int       `json:"user_id"`
	MeetingID      *int      `json:"meeting_id,omitempty"`
	IsTranscribing bool      `json:"is_transcribing"`
	IsPaused       bool      `json:"is_paused"`
	StartTime      time.Time `json:"start_time"`
	LastUpdate     time.Time `json:"last_update"`
}

// SessionRecoveryManager 会话恢复管理器
type SessionRecoveryManager struct {
	states map[string]*SessionState // sessionUUID -> SessionState
	mu     sync.RWMutex
	logger *zap.Logger
}

// NewSessionRecoveryManager 创建会话恢复管理器
func NewSessionRecoveryManager(logger *zap.Logger) *SessionRecoveryManager {
	return &SessionRecoveryManager{
		states: make(map[string]*SessionState),
		logger: logger,
	}
}

// SaveSessionState 保存会话状态
func (m *SessionRecoveryManager) SaveSessionState(session *WebSocketSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session.mu.RLock()
	state := &SessionState{
		SessionUUID:    session.SessionUUID,
		UserID:         session.UserID,
		MeetingID:      session.MeetingID,
		IsTranscribing: session.IsTranscribing(),
		IsPaused:       session.IsPaused(),
		StartTime:      session.StartTime,
		LastUpdate:     time.Now(),
	}
	session.mu.RUnlock()

	m.states[session.SessionUUID] = state

	m.logger.Debug("会话状态已保存",
		zap.String("session_uuid", session.SessionUUID),
		zap.Int("user_id", session.UserID))

	return nil
}

// GetSessionState 获取会话状态
func (m *SessionRecoveryManager) GetSessionState(sessionUUID string) (*SessionState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.states[sessionUUID]
	return state, exists
}

// DeleteSessionState 删除会话状态
func (m *SessionRecoveryManager) DeleteSessionState(sessionUUID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.states, sessionUUID)

	m.logger.Debug("会话状态已删除",
		zap.String("session_uuid", sessionUUID))
}

// CleanupExpiredStates 清理过期状态
func (m *SessionRecoveryManager) CleanupExpiredStates(maxAge time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for uuid, state := range m.states {
		if now.Sub(state.LastUpdate) > maxAge {
			delete(m.states, uuid)
			m.logger.Info("清理过期会话状态",
				zap.String("session_uuid", uuid),
				zap.Duration("age", now.Sub(state.LastUpdate)))
		}
	}
}

// GetStateCount 获取状态数量
func (m *SessionRecoveryManager) GetStateCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.states)
}

// RestoreSession 恢复会话
func (m *SessionRecoveryManager) RestoreSession(
	ctx context.Context,
	sessionUUID string,
	newConn *WebSocketSession,
) error {
	state, exists := m.GetSessionState(sessionUUID)
	if !exists {
		return ErrSessionNotFound
	}

	// 验证用户匹配
	if state.UserID != newConn.UserID {
		return ErrUserMismatch
	}

	// 恢复会话状态
	newConn.mu.Lock()
	newConn.SessionUUID = state.SessionUUID
	newConn.MeetingID = state.MeetingID
	newConn.SetTranscribing(state.IsTranscribing)
	newConn.SetPaused(state.IsPaused)
	newConn.StartTime = state.StartTime
	newConn.mu.Unlock()

	m.logger.Info("会话已恢复",
		zap.String("session_uuid", sessionUUID),
		zap.Int("user_id", state.UserID),
		zap.Bool("is_transcribing", state.IsTranscribing),
		zap.Bool("is_paused", state.IsPaused))

	return nil
}

// SerializeState 序列化状态（用于持久化存储）
func (m *SessionRecoveryManager) SerializeState(sessionUUID string) ([]byte, error) {
	state, exists := m.GetSessionState(sessionUUID)
	if !exists {
		return nil, ErrSessionNotFound
	}

	return json.Marshal(state)
}

// DeserializeState 反序列化状态（从持久化存储恢复）
func (m *SessionRecoveryManager) DeserializeState(data []byte) (*SessionState, error) {
	var state SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// 错误定义
var (
	ErrSessionNotFound = &SessionError{Code: "SESSION_NOT_FOUND", Message: "会话不存在"}
	ErrUserMismatch    = &SessionError{Code: "USER_MISMATCH", Message: "用户不匹配"}
)

// SessionError 会话错误
type SessionError struct {
	Code    string
	Message string
}

func (e *SessionError) Error() string {
	return e.Message
}
