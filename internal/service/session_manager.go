// Package service 提供业务逻辑实现
package service

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// TranscriptionSession 转录会话
type TranscriptionSession struct {
	ID         string
	UserID     int
	MeetingID  int
	Status     SessionStatus
	StartTime  time.Time
	EndTime    *time.Time
	STTClient  *STTClient
	ClientConn *websocket.Conn
	Config     *STTConfig

	// Channels for communication
	AudioBuffer chan []byte
	ResultChan  chan *TranscriptionResult
	StopChan    chan struct{}

	// 存储最终转录结果
	FinalResults []*TranscriptionResult

	// Mutex for concurrent access
	mu sync.RWMutex
}

// SessionManager 管理所有转录会话
type SessionManager struct {
	sessions     map[string]*TranscriptionSession // sessionID -> session
	userSessions map[int]string                   // userID -> sessionID
	mu           sync.RWMutex
}

// NewSessionManager 创建新的会话管理器
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions:     make(map[string]*TranscriptionSession),
		userSessions: make(map[int]string),
	}
}

var sessionCounter uint64

// generateSessionID 生成唯一的会话 ID
func generateSessionID() string {
	counter := atomic.AddUint64(&sessionCounter, 1)
	return fmt.Sprintf("session_%d_%d", time.Now().UnixNano(), counter)
}

// CreateSession 创建新的转录会话
//
// 参数:
//   - userID: 用户 ID
//   - meetingID: 会议 ID
//   - conn: WebSocket 连接
//   - config: STT 配置
//
// 返回:
//   - *TranscriptionSession: 创建的会话
//   - error: 如果创建失败返回错误
func (sm *SessionManager) CreateSession(
	userID int,
	meetingID int,
	conn *websocket.Conn,
	config *STTConfig,
) (*TranscriptionSession, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 如果用户已有会话，先关闭旧会话
	sm.closeUserSessionLocked(userID)

	// 生成唯一的会话 ID
	sessionID := generateSessionID()

	// 创建会话
	session := &TranscriptionSession{
		ID:           sessionID,
		UserID:       userID,
		MeetingID:    meetingID,
		Status:       StatusStarting,
		StartTime:    time.Now(),
		ClientConn:   conn,
		Config:       config,
		AudioBuffer:  make(chan []byte, 100),
		ResultChan:   make(chan *TranscriptionResult, 50),
		StopChan:     make(chan struct{}),
		FinalResults: make([]*TranscriptionResult, 0),
	}

	// 保存会话
	sm.sessions[sessionID] = session
	sm.userSessions[userID] = sessionID

	return session, nil
}

// GetSession 获取指定的会话
//
// 参数:
//   - sessionID: 会话 ID
//
// 返回:
//   - *TranscriptionSession: 会话对象
//   - bool: 是否存在
func (sm *SessionManager) GetSession(sessionID string) (*TranscriptionSession, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[sessionID]
	return session, exists
}

// GetUserSession 获取用户的活跃会话
//
// 参数:
//   - userID: 用户 ID
//
// 返回:
//   - *TranscriptionSession: 会话对象
//   - bool: 是否存在
func (sm *SessionManager) GetUserSession(userID int) (*TranscriptionSession, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessionID, exists := sm.userSessions[userID]
	if !exists {
		return nil, false
	}

	session, exists := sm.sessions[sessionID]
	return session, exists
}

// DeleteSession 删除会话
//
// 参数:
//   - sessionID: 会话 ID
func (sm *SessionManager) DeleteSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 获取会话
	session, exists := sm.sessions[sessionID]
	if !exists {
		return
	}

	// 从用户会话映射中删除
	delete(sm.userSessions, session.UserID)

	// 删除会话
	delete(sm.sessions, sessionID)
}

// CloseUserSession 关闭用户的旧会话
//
// 参数:
//   - userID: 用户 ID
//
// 返回:
//   - bool: 是否关闭了旧会话
func (sm *SessionManager) CloseUserSession(userID int) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	return sm.closeUserSessionLocked(userID)
}

// closeUserSessionLocked 在持有写锁的前提下关闭用户旧会话
func (sm *SessionManager) closeUserSessionLocked(userID int) bool {
	oldSessionID, exists := sm.userSessions[userID]
	if !exists {
		return false
	}

	oldSession, exists := sm.sessions[oldSessionID]
	if exists {
		if oldSession.ClientConn != nil {
			oldSession.ClientConn.Close()
		}
		select {
		case oldSession.StopChan <- struct{}{}:
		default:
		}
		delete(sm.sessions, oldSessionID)
	} else {
		// 数据不同步时直接删除映射
		delete(sm.userSessions, userID)
		return false
	}

	delete(sm.userSessions, userID)
	return true
}

// GetActiveSessionCount 获取活跃会话数
//
// 返回:
//   - int: 活跃会话数
func (sm *SessionManager) GetActiveSessionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return len(sm.sessions)
}

// GetAllSessions 获取所有会话（用于监控）
//
// 返回:
//   - []*TranscriptionSession: 所有会话的副本
func (sm *SessionManager) GetAllSessions() []*TranscriptionSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessions := make([]*TranscriptionSession, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		sessions = append(sessions, session)
	}

	return sessions
}

// TranscriptionSession 方法

// SetStatus 设置会话状态
func (s *TranscriptionSession) SetStatus(status SessionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
}

// GetStatus 获取会话状态
func (s *TranscriptionSession) GetStatus() SessionStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}

// SetEndTime 设置结束时间
func (s *TranscriptionSession) SetEndTime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EndTime = &t
}

// GetDuration 获取会话持续时间
func (s *TranscriptionSession) GetDuration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.EndTime != nil {
		return s.EndTime.Sub(s.StartTime)
	}
	return time.Since(s.StartTime)
}

// SetSTTClient 设置 STT 客户端
func (s *TranscriptionSession) SetSTTClient(client *STTClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.STTClient = client
}

// GetSTTClient 获取 STT 客户端
func (s *TranscriptionSession) GetSTTClient() *STTClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.STTClient
}

// Close 关闭会话并清理资源
func (s *TranscriptionSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 关闭所有 channels
	select {
	case <-s.StopChan:
		// 已经关闭
	default:
		close(s.StopChan)
	}

	// 关闭 STT 客户端
	if s.STTClient != nil {
		s.STTClient.Close()
	}

	// 关闭 WebSocket 连接
	if s.ClientConn != nil {
		s.ClientConn.Close()
	}

	// 设置结束时间
	now := time.Now()
	s.EndTime = &now
	s.Status = StatusCompleted
}

// AddFinalResult 添加最终转录结果
func (s *TranscriptionSession) AddFinalResult(result *TranscriptionResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if result.IsFinal {
		s.FinalResults = append(s.FinalResults, result)
	}
}

// GetFinalResults 获取所有最终转录结果
func (s *TranscriptionSession) GetFinalResults() []*TranscriptionResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 返回副本
	results := make([]*TranscriptionResult, len(s.FinalResults))
	copy(results, s.FinalResults)
	return results
}

// GetFullTranscript 获取完整转录文本
func (s *TranscriptionSession) GetFullTranscript() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var transcript string
	for _, result := range s.FinalResults {
		if result.IsFinal {
			transcript += result.Text + " "
		}
	}
	return transcript
}
