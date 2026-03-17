// Package service 提供业务逻辑实现
package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	json "github.com/bytedance/sonic"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// WebSocketSession WebSocket 会话
type WebSocketSession struct {
	UserID           int // 唯一会话标识，直接使用 userID
	Conn             *websocket.Conn
	RemoteConn       *RemoteConnection
	SessionUUID      string
	MeetingID        *int
	ClientVersion    string
	isTranscribing   atomic.Bool
	isPaused         atomic.Bool
	isStopped        atomic.Bool // 用户是否主动停止了会话
	StartTime        time.Time
	LastActivityTime time.Time // 最后活动时间（用于僵尸会话检测）
	CreatedAt        time.Time
	UpdatedAt        time.Time

	clientDisconnectedOnce sync.Once
	clientDisconnected     chan struct{}

	// 客户端偏好设置
	AudioFormat   string
	LanguageHints []string
	clientPrefs   *ClientPreferences

	// Remote 连接状态
	remoteConnecting   atomic.Bool
	remoteReady        atomic.Bool
	remoteReconnecting atomic.Bool

	audioSendFailSinceUnix int64
	audioSendFailActive    atomic.Bool
	audioPacketStatsMu     sync.Mutex
	audioPacketStatsSecond int64
	audioPacketStatsCount  uint64
	lastActivityUnixNano   atomic.Int64
	pendingAudio           []bufferedAudioPacket
	pendingFinalRecords    []*TranscriptionRecord
	bufferedAudioFlushing  atomic.Bool

	// 统计更新标记，防止重复更新
	StatisticsUpdated bool

	mu                  sync.RWMutex
	writeMu             sync.Mutex
	startedMessageSent  atomic.Bool
	resultGeneration    atomic.Uint64
	resultProcessorOnce sync.Once
	resultQueue         chan *queuedTranscriptionResult
	resultWakeCh        chan struct{}
	overflowResults     []*queuedTranscriptionResult
	ctx                 context.Context
	cancel              context.CancelFunc
	logger              *zap.Logger
	manager             *WebSocketSessionManager
}

type queuedTranscriptionResult struct {
	Text              string
	Temp              string
	Speaker           string
	Language          string
	TranslationStatus string
	Timestamp         int64
	ReceiveTime       int64
	IsFinal           bool
	Generation        uint64
}

type bufferedAudioPacket struct {
	Data       []byte
	BufferedAt time.Time
}

const (
	maxBufferedAudioPackets = 256
	maxPendingFinalRecords  = 128
	resultQueueSize         = 256
	maxOverflowResults      = 256
	maxBufferedAudioAge     = 5 * time.Second
)

var (
	ErrBufferedAudioLimitReached = fmt.Errorf("buffered audio limit reached")
	ErrPendingFinalRecordsFull   = fmt.Errorf("pending final transcription buffer full")
)

func (s *WebSocketSession) notifyUpdated() {
	if s.manager == nil {
		return
	}

	s.mu.Lock()
	s.UpdatedAt = time.Now()
	s.mu.Unlock()

	// 临时禁用会话持久化，避免阻塞
	// s.manager.NotifySessionUpdated(s)
}

func (s *WebSocketSession) GetClientPreferences() *ClientPreferences {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.clientPrefs == nil {
		return nil
	}

	copyPrefs := &ClientPreferences{
		AudioFormat:   s.clientPrefs.AudioFormat,
		LanguageHints: append([]string(nil), s.clientPrefs.LanguageHints...),
	}

	return copyPrefs
}

func (s *WebSocketSession) IsTranscribing() bool {
	return s.isTranscribing.Load()
}

func (s *WebSocketSession) SetTranscribing(v bool) {
	s.isTranscribing.Store(v)
}

func (s *WebSocketSession) IsPaused() bool {
	return s.isPaused.Load()
}

func (s *WebSocketSession) SetPaused(v bool) {
	s.isPaused.Store(v)
}

func (s *WebSocketSession) IsStopped() bool {
	return s.isStopped.Load()
}

func (s *WebSocketSession) SetStopped(v bool) {
	s.isStopped.Store(v)
}

func (s *WebSocketSession) MarkClientDisconnected() {
	s.clientDisconnectedOnce.Do(func() {
		if s.clientDisconnected != nil {
			close(s.clientDisconnected)
		}
	})
}

func (s *WebSocketSession) ClientDisconnected() <-chan struct{} {
	s.mu.RLock()
	ch := s.clientDisconnected
	s.mu.RUnlock()
	return ch
}

func (s *WebSocketSession) RemoteConnecting() bool {
	return s.remoteConnecting.Load()
}

func (s *WebSocketSession) SetRemoteConnecting(v bool) {
	s.remoteConnecting.Store(v)
}

func (s *WebSocketSession) RemoteReady() bool {
	return s.remoteReady.Load()
}

func (s *WebSocketSession) SetRemoteReady(v bool) {
	s.remoteReady.Store(v)
}

func (s *WebSocketSession) BeginRemoteReconnect() bool {
	return s.remoteReconnecting.CompareAndSwap(false, true)
}

func (s *WebSocketSession) EndRemoteReconnect() {
	s.remoteReconnecting.Store(false)
}

func (s *WebSocketSession) BeginBufferedAudioFlush() bool {
	return s.bufferedAudioFlushing.CompareAndSwap(false, true)
}

func (s *WebSocketSession) EndBufferedAudioFlush() {
	s.bufferedAudioFlushing.Store(false)
}

func (s *WebSocketSession) RecordAudioPacket(now time.Time) (uint64, bool) {
	nowSec := now.Unix()
	s.audioPacketStatsMu.Lock()
	defer s.audioPacketStatsMu.Unlock()

	if s.audioPacketStatsSecond == 0 {
		s.audioPacketStatsSecond = nowSec
		s.audioPacketStatsCount = 1
		return 0, false
	}

	if s.audioPacketStatsSecond == nowSec {
		s.audioPacketStatsCount++
		return 0, false
	}

	prevCount := s.audioPacketStatsCount
	s.audioPacketStatsSecond = nowSec
	s.audioPacketStatsCount = 1
	return prevCount, true
}

func (s *WebSocketSession) MarkAudioSendFailure(now time.Time, notifyAfter time.Duration) bool {
	if s.audioSendFailActive.CompareAndSwap(false, true) {
		atomic.StoreInt64(&s.audioSendFailSinceUnix, now.UnixNano())
		return false
	}

	sinceUnix := atomic.LoadInt64(&s.audioSendFailSinceUnix)
	return sinceUnix > 0 && now.UnixNano()-sinceUnix >= notifyAfter.Nanoseconds()
}

func (s *WebSocketSession) ResetAudioSendFailure() {
	s.audioSendFailActive.Store(false)
	atomic.StoreInt64(&s.audioSendFailSinceUnix, 0)
}

func (s *WebSocketSession) MarkStartedMessageSent() bool {
	return s.startedMessageSent.CompareAndSwap(false, true)
}

func (s *WebSocketSession) StartedMessageSent() bool {
	return s.startedMessageSent.Load()
}

func (s *WebSocketSession) ResetStartedMessageSent() {
	s.startedMessageSent.Store(false)
}

func (s *WebSocketSession) BufferAudio(audioData []byte) error {
	if len(audioData) == 0 {
		return nil
	}

	packetCopy := append([]byte(nil), audioData...)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.dropExpiredBufferedAudioLocked(now)
	if len(s.pendingAudio) >= maxBufferedAudioPackets {
		return ErrBufferedAudioLimitReached
	}
	s.pendingAudio = append(s.pendingAudio, bufferedAudioPacket{
		Data:       packetCopy,
		BufferedAt: now,
	})
	return nil
}

func (s *WebSocketSession) HasBufferedAudio() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dropExpiredBufferedAudioLocked(time.Now())
	return len(s.pendingAudio) > 0
}

func (s *WebSocketSession) DrainBufferedAudio() []bufferedAudioPacket {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pendingAudio) == 0 {
		return nil
	}

	now := time.Now()
	s.dropExpiredBufferedAudioLocked(now)
	if len(s.pendingAudio) == 0 {
		return nil
	}

	audio := make([]bufferedAudioPacket, 0, len(s.pendingAudio))
	for _, packet := range s.pendingAudio {
		if len(packet.Data) == 0 {
			continue
		}
		audio = append(audio, packet)
	}
	s.pendingAudio = s.pendingAudio[:0]

	return audio
}

func (s *WebSocketSession) RequeueBufferedAudio(audio []bufferedAudioPacket) error {
	if len(audio) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.dropExpiredBufferedAudioLocked(time.Now())

	merged := make([]bufferedAudioPacket, 0, len(audio)+len(s.pendingAudio))
	for _, packet := range audio {
		if len(packet.Data) == 0 {
			continue
		}
		packetCopy := bufferedAudioPacket{
			Data:       append([]byte(nil), packet.Data...),
			BufferedAt: packet.BufferedAt,
		}
		merged = append(merged, packetCopy)
	}
	merged = append(merged, s.pendingAudio...)
	if len(merged) > maxBufferedAudioPackets {
		return ErrBufferedAudioLimitReached
	}
	s.pendingAudio = merged
	return nil
}

func (s *WebSocketSession) dropExpiredBufferedAudioLocked(now time.Time) {
	if len(s.pendingAudio) == 0 {
		return
	}

	keep := s.pendingAudio[:0]
	for _, packet := range s.pendingAudio {
		if len(packet.Data) == 0 {
			continue
		}
		if !packet.BufferedAt.IsZero() && now.Sub(packet.BufferedAt) > maxBufferedAudioAge {
			continue
		}
		keep = append(keep, packet)
	}
	s.pendingAudio = keep
}

func (s *WebSocketSession) EnqueueResult(result *queuedTranscriptionResult) bool {
	if result == nil || s.resultQueue == nil {
		return false
	}

	select {
	case <-s.ctx.Done():
		return false
	case s.resultQueue <- result:
		return true
	default:
		if !result.IsFinal {
			return false
		}
		return s.enqueueOverflowResult(result)
	}
}

func (s *WebSocketSession) ResultQueue() <-chan *queuedTranscriptionResult {
	return s.resultQueue
}

func (s *WebSocketSession) ResultWake() <-chan struct{} {
	return s.resultWakeCh
}

func (s *WebSocketSession) CurrentResultGeneration() uint64 {
	return s.resultGeneration.Load()
}

func (s *WebSocketSession) AdvanceResultGeneration() uint64 {
	generation := s.resultGeneration.Add(1)
	s.clearResultBuffers()
	return generation
}

func (s *WebSocketSession) DrainOverflowResults() []*queuedTranscriptionResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.overflowResults) == 0 {
		return nil
	}

	results := s.overflowResults
	s.overflowResults = nil
	return results
}

func (s *WebSocketSession) enqueueOverflowResult(result *queuedTranscriptionResult) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.overflowResults) >= maxOverflowResults {
		return false
	}

	s.overflowResults = append(s.overflowResults, result)
	s.signalResultWakeLocked()
	return true
}

func (s *WebSocketSession) clearResultBuffers() {
	if s.resultQueue != nil {
		for {
			select {
			case <-s.resultQueue:
			default:
				goto drainedQueue
			}
		}
	}

drainedQueue:
	s.mu.Lock()
	s.overflowResults = nil
	s.mu.Unlock()

	if s.resultWakeCh != nil {
		for {
			select {
			case <-s.resultWakeCh:
			default:
				return
			}
		}
	}
}

func (s *WebSocketSession) signalResultWakeLocked() {
	if s.resultWakeCh == nil {
		return
	}
	select {
	case s.resultWakeCh <- struct{}{}:
	default:
	}
}

func (s *WebSocketSession) BufferPendingFinalRecord(record *TranscriptionRecord) error {
	if record == nil {
		return nil
	}

	recordCopy := *record

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pendingFinalRecords) >= maxPendingFinalRecords {
		return ErrPendingFinalRecordsFull
	}
	s.pendingFinalRecords = append(s.pendingFinalRecords, &recordCopy)
	return nil
}

func (s *WebSocketSession) DrainPendingFinalRecords() []*TranscriptionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pendingFinalRecords) == 0 {
		return nil
	}

	records := s.pendingFinalRecords
	s.pendingFinalRecords = nil
	return records
}

func (s *WebSocketSession) RequeuePendingFinalRecords(records []*TranscriptionRecord) error {
	if len(records) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	merged := make([]*TranscriptionRecord, 0, len(records)+len(s.pendingFinalRecords))
	for _, record := range records {
		if record == nil {
			continue
		}
		recordCopy := *record
		merged = append(merged, &recordCopy)
	}
	merged = append(merged, s.pendingFinalRecords...)
	if len(merged) > maxPendingFinalRecords {
		return ErrPendingFinalRecordsFull
	}
	s.pendingFinalRecords = merged
	return nil
}

// WebSocketSessionManager WebSocket 会话管理器
type WebSocketSessionManager struct {
	sessions           map[int]*WebSocketSession // userID -> Session (直接使用userID作为键)
	connectionStates   map[int]string            // userID -> state (connecting, connected, disconnecting, disconnected)
	mu                 sync.RWMutex
	timeManager        UnifiedTimeManager
	remoteManager      *RemoteConnectionManager
	recoveryManager    *SessionRecoveryManager
	transcriptionCache TranscriptionCache // 统一的转录缓存（替代sessionStore）
	logger             *zap.Logger
}

// NewWebSocketSessionManager 创建会话管理器
func NewWebSocketSessionManager(
	timeManager UnifiedTimeManager,
	remoteManager *RemoteConnectionManager,
	transcriptionCache TranscriptionCache,
	logger *zap.Logger,
) *WebSocketSessionManager {
	recoveryManager := NewSessionRecoveryManager(logger)

	// 启动定期清理过期状态（每小时清理一次，保留24小时内的状态）
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			recoveryManager.CleanupExpiredStates(24 * time.Hour)
		}
	}()

	return &WebSocketSessionManager{
		sessions:           make(map[int]*WebSocketSession),
		connectionStates:   make(map[int]string),
		timeManager:        timeManager,
		remoteManager:      remoteManager,
		recoveryManager:    recoveryManager,
		transcriptionCache: transcriptionCache,
		logger:             logger,
	}
}

// CreateSession 创建新会话
// 与 Python 版本一致：检查现有会话并恢复 session_uuid 和偏好设置
func (m *WebSocketSessionManager) CreateSession(
	ctx context.Context,
	userID int,
	conn *websocket.Conn,
) (*WebSocketSession, error) {
	m.mu.Lock()

	removedSessionIDs := make([]int, 0, 1)

	// 关闭用户的旧会话（完整清理）
	// 优化：直接使用 userID 作为键
	if oldSession, ok := m.sessions[userID]; ok {
		m.logger.Warn("关闭用户的旧会话",
			zap.Int("user_id", userID))

		// 完整清理旧会话（类似 Python 版本的 _cleanup_existing_connections）
		if oldSession.RemoteConn != nil {
			_ = oldSession.RemoteConn.Close()
		}
		_ = oldSession.Close()
		removedSessionIDs = append(removedSessionIDs, userID)
		delete(m.sessions, userID)
		delete(m.connectionStates, userID)
	}

	// 检查时间管理器中的现有会话（与 Python 版本一致）
	// Python 版本：连接时不生成新UUID，等待start命令时生成
	// 但如果存在现有会话，会恢复 session_uuid 和偏好设置
	var sessionUUID string
	var existingSession *SessionInfo

	// 通过 timeManager 接口检查现有会话
	if existingSessionInfo, exists := m.timeManager.GetSessionInfo(userID); exists {
		existingSession = existingSessionInfo

		// 如果存在转录中或暂停的会话，保持会话状态以支持重连恢复
		if existingSession.IsTranscribing || existingSession.IsPaused {
			m.logger.Info("检测到用户重连，保持现有会话状态以支持恢复",
				zap.Int("user_id", userID),
				zap.String("existing_session_uuid", existingSession.SessionUUID),
				zap.Bool("was_transcribing", existingSession.IsTranscribing),
				zap.Bool("was_paused", existingSession.IsPaused))

			// 保持现有的 session_uuid 以支持会话恢复
			sessionUUID = existingSession.SessionUUID
			m.logger.Info("保持现有会话UUID以支持重连恢复",
				zap.Int("user_id", userID),
				zap.String("session_uuid", sessionUUID))
		} else if existingSession.SessionUUID != "" {
			// 如果存在未转录的会话，恢复 session_uuid
			sessionUUID = existingSession.SessionUUID
			m.logger.Info("发现现有非转录会话，恢复 session_uuid",
				zap.Int("user_id", userID),
				zap.String("session_uuid", sessionUUID))
		} else {
			sessionUUID = ""
		}
	} else {
		// 没有现有会话，等待 start 命令时生成新的 session_uuid
		sessionUUID = ""
	}

	// 创建新会话（优化：直接使用 userID 作为键）
	// 标记连接状态为 connecting（与 Python 版本一致）
	m.connectionStates[userID] = "connecting"
	m.logger.Debug("连接状态：connecting",
		zap.Int("user_id", userID))

	sessionCtx, cancel := context.WithCancel(ctx)

	now := time.Now()
	session := &WebSocketSession{
		UserID:             userID,
		Conn:               conn,
		SessionUUID:        sessionUUID, // 将在 start 命令时生成
		StartTime:          now,
		LastActivityTime:   now, // 初始化最后活动时间
		CreatedAt:          now,
		UpdatedAt:          now,
		clientDisconnected: make(chan struct{}),
		resultQueue:        make(chan *queuedTranscriptionResult, resultQueueSize),
		resultWakeCh:       make(chan struct{}, 1),
		ctx:                sessionCtx,
		cancel:             cancel,
		logger:             m.logger,
		manager:            m,
	}
	session.lastActivityUnixNano.Store(now.UnixNano())

	// 如果存在现有会话，恢复偏好设置（Python 版本会恢复 audio_format_override 和 language_hints_override）
	if existingSession != nil {
		// 恢复音频格式偏好
		if existingSession.AudioFormatOverride != "" {
			session.AudioFormat = existingSession.AudioFormatOverride
			m.logger.Info("恢复会话音频格式偏好",
				zap.Int("user_id", userID),
				zap.String("audio_format", session.AudioFormat))
		}

		// 恢复语言提示偏好
		if len(existingSession.LanguageHintsOverride) > 0 {
			session.LanguageHints = make([]string, len(existingSession.LanguageHintsOverride))
			copy(session.LanguageHints, existingSession.LanguageHintsOverride)
			m.logger.Info("恢复会话语言提示偏好",
				zap.Int("user_id", userID),
				zap.Strings("language_hints", session.LanguageHints))
		}

		if session.AudioFormat != "" || len(session.LanguageHints) > 0 {
			session.clientPrefs = &ClientPreferences{
				AudioFormat:   session.AudioFormat,
				LanguageHints: append([]string(nil), session.LanguageHints...),
			}
			m.logger.Info("恢复会话客户端偏好设置",
				zap.Int("user_id", userID),
				zap.String("audio_format", session.clientPrefs.AudioFormat),
				zap.Strings("language_hints", session.clientPrefs.LanguageHints))
		}
	}

	m.sessions[userID] = session
	// 注意：不再需要userSessions映射，因为直接使用userID作为键

	// 标记连接状态为 connected（与 Python 版本一致）
	m.connectionStates[userID] = "connected"
	m.logger.Debug("连接状态：connected",
		zap.Int("user_id", userID))

	m.logger.Info("WebSocket 会话已创建",
		zap.Int("user_id", userID),
		zap.String("session_uuid", sessionUUID))

	m.mu.Unlock()

	// 尝试从持久化存储恢复会话状态
	storedSessionUserID := m.restoreSessionSnapshot(session)
	if storedSessionUserID != 0 && storedSessionUserID != userID {
		removedSessionIDs = append(removedSessionIDs, storedSessionUserID)
	}

	// 清理被移除的旧会话快照
	for _, removedID := range removedSessionIDs {
		if removedID == 0 || removedID == userID {
			continue
		}
		if err := m.deleteSessionSnapshot(removedID); err != nil {
			m.logger.Debug("旧会话快照删除失败",
				zap.Error(err),
				zap.Int("user_id", removedID))
		}
	}

	session.notifyUpdated()

	return session, nil
}

// GetSession 获取会话（为了兼容性，支持字符串 sessionID）
func (m *WebSocketSessionManager) GetSession(sessionID string) (*WebSocketSession, bool) {
	// 将字符串 sessionID 转换为 userID
	userID, err := strconv.Atoi(sessionID)
	if err != nil {
		return nil, false
	}
	return m.GetSessionByUserID(userID)
}

// GetSessionByUserID 根据用户ID获取会话（优化后的主要方法）
func (m *WebSocketSessionManager) GetSessionByUserID(userID int) (*WebSocketSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 直接使用 userID 作为键
	session, exists := m.sessions[userID]
	return session, exists
}

// IsUserConnectionActive 检查用户连接是否真正活跃（增强版连接检查）
func (m *WebSocketSessionManager) IsUserConnectionActive(userID int) bool {
	session, exists := m.GetSessionByUserID(userID)
	if !exists {
		return false
	}

	session.mu.RLock()
	defer session.mu.RUnlock()

	// 1. 检查WebSocket连接是否存在
	if session.Conn == nil {
		return false
	}

	// 2. 检查连接状态
	state := m.GetConnectionStateByUserID(userID)
	if state == "disconnected" || state == "disconnecting" {
		return false
	}

	// 3. 检查最后活动时间（超过5分钟无活动视为僵尸连接）
	if time.Since(session.LastActivityTime) > 5*time.Minute {
		m.logger.Warn("检测到僵尸连接，最后活动时间过久",
			zap.Int("user_id", userID),
			zap.Duration("inactive_duration", time.Since(session.LastActivityTime)))
		return false
	}

	// 4. 尝试ping连接以验证活跃性（可选，避免频繁ping）
	// 这里可以添加更复杂的连接验证逻辑

	return true
}

// UpdateLastActivity 更新会话的最后活动时间
func (m *WebSocketSessionManager) UpdateLastActivity(userID int) {
	m.mu.RLock()
	session, exists := m.sessions[userID]
	m.mu.RUnlock()

	if exists {
		session.UpdateActivity()
	}
}

// UpdateLastActivityBySessionID 更新会话的最后活动时间（兼容方法）
func (m *WebSocketSessionManager) UpdateLastActivityBySessionID(sessionID string) {
	userID, err := strconv.Atoi(sessionID)
	if err != nil {
		return
	}
	m.UpdateLastActivity(userID)
}

// RemoveSession 移除会话

func (m *WebSocketSessionManager) RemoveSession(userID int) {
	m.mu.Lock()
	if _, exists := m.sessions[userID]; !exists {
		m.mu.Unlock()
		return
	}
	m.connectionStates[userID] = "disconnected"
	// 注意：直接使用 userID 作为键
	delete(m.sessions, userID)
	delete(m.connectionStates, userID)
	m.mu.Unlock()

	m.logger.Debug("连接状态：disconnected",
		zap.Int("user_id", userID))

	if err := m.deleteSessionSnapshot(userID); err != nil {
		m.logger.Warn("删除会话快照失败",
			zap.Error(err),
			zap.Int("user_id", userID))
	}

	m.logger.Info("WebSocket 会话已移除",
		zap.Int("user_id", userID))
}

// GetConnectionStateByUserID 根据用户ID获取连接状态（主要方法）
func (m *WebSocketSessionManager) GetConnectionStateByUserID(userID int) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.connectionStates[userID]
	if !exists {
		return "unknown"
	}
	return state
}

// GetConnectionState 获取连接状态（兼容方法）
func (m *WebSocketSessionManager) GetConnectionState(sessionID string) string {
	userID, err := strconv.Atoi(sessionID)
	if err != nil {
		return "unknown"
	}
	return m.GetConnectionStateByUserID(userID)
}

// SetConnectionStateByUserID 根据用户ID设置连接状态（主要方法）
func (m *WebSocketSessionManager) SetConnectionStateByUserID(userID int, state string) {
	m.mu.Lock()
	m.connectionStates[userID] = state
	session, exists := m.sessions[userID]
	m.mu.Unlock()

	m.logger.Debug("连接状态已更新",
		zap.Int("user_id", userID),
		zap.String("state", state))

	if exists {
		session.notifyUpdated()
	}
}

// SetConnectionState 设置连接状态（兼容方法）
func (m *WebSocketSessionManager) SetConnectionState(sessionID string, state string) {
	userID, err := strconv.Atoi(sessionID)
	if err != nil {
		return
	}
	m.SetConnectionStateByUserID(userID, state)
}

// NotifySessionUpdated persists session state via the unified TranscriptionCache.
func (m *WebSocketSessionManager) NotifySessionUpdated(session *WebSocketSession) {
	if session == nil || m.transcriptionCache == nil {
		return
	}

	// 获取连接状态
	state := "unknown"
	m.mu.RLock()
	if connectionState, ok := m.connectionStates[session.UserID]; ok {
		state = connectionState
	}
	m.mu.RUnlock()

	// 将WebSocketSession转换为SessionInfo
	sessionInfo := m.convertWebSocketSessionToSessionInfo(session, state)

	// 使用TranscriptionCache保存（按UserID）
	ctx := context.Background()
	if err := m.transcriptionCache.Set(ctx, session.UserID, sessionInfo); err != nil {
		m.logger.Warn("保存会话状态失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID))
	}
}

func (m *WebSocketSessionManager) restoreSessionSnapshot(session *WebSocketSession) int {
	sessionInfo := m.fetchSessionInfoForRestore(session)
	if sessionInfo == nil {
		return 0
	}

	session.mu.Lock()
	if sessionInfo.SessionUUID != "" {
		session.SessionUUID = sessionInfo.SessionUUID
	}
	if sessionInfo.MeetingID > 0 {
		session.MeetingID = &sessionInfo.MeetingID
	}
	if !sessionInfo.TranscriptionStartTime.IsZero() {
		session.StartTime = sessionInfo.TranscriptionStartTime
	} else {
		session.StartTime = time.Now()
	}
	if !sessionInfo.LastActivityTime.IsZero() {
		session.LastActivityTime = sessionInfo.LastActivityTime
	}
	if sessionInfo.AudioFormat != "" {
		session.AudioFormat = sessionInfo.AudioFormat
	}
	if sessionInfo.LanguageHints != nil {
		session.LanguageHints = append([]string(nil), sessionInfo.LanguageHints...)
	}
	session.SetRemoteConnecting(sessionInfo.RemoteConnecting)
	session.SetRemoteReady(sessionInfo.RemoteReady)
	if !sessionInfo.CreatedAt.IsZero() {
		session.CreatedAt = sessionInfo.CreatedAt
	}
	if !sessionInfo.LastUpdate.IsZero() {
		session.UpdatedAt = sessionInfo.LastUpdate
	}
	// 新连接恢复时强制进入空闲状态，避免遗留的转录状态阻止 start 命令
	session.SetTranscribing(false)
	session.SetPaused(false)
	session.mu.Unlock()

	if sessionInfo.ConnectionState != "" {
		m.SetConnectionStateByUserID(session.UserID, sessionInfo.ConnectionState)
	}

	return sessionInfo.UserID
}

func (m *WebSocketSessionManager) fetchSessionInfoForRestore(session *WebSocketSession) *SessionInfo {
	if m.transcriptionCache == nil || session == nil {
		return nil
	}

	ctx := context.Background()

	// 直接按UserID查找（简化后的逻辑）
	sessionInfo, err := m.transcriptionCache.Get(ctx, session.UserID)
	if err != nil {
		return nil
	}

	return sessionInfo
}

func (m *WebSocketSessionManager) deleteSessionSnapshot(userID int) error {
	if userID == 0 || m.transcriptionCache == nil {
		return nil
	}

	// 检查会话是否存在
	m.mu.RLock()
	_, exists := m.sessions[userID]
	m.mu.RUnlock()

	if !exists {
		// 如果找不到活跃会话，说明会话已经不存在了
		return nil
	}

	return m.transcriptionCache.Delete(context.Background(), userID)
}

// CloseUserSession 关闭用户的会话
func (m *WebSocketSessionManager) CloseUserSession(userID int) bool {
	m.mu.RLock()
	session, exists := m.sessions[userID]
	m.mu.RUnlock()

	if !exists || session == nil {
		return false
	}

	_ = session.Close()
	m.RemoveSession(userID)

	return true
}

// GetActiveSessionCount 获取活跃会话数
func (m *WebSocketSessionManager) GetActiveSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// RemoveUserSessions 移除用户的所有会话（与 Python 版本的清理用户连接一致）
func (m *WebSocketSessionManager) RemoveUserSessions(userID int) {
	// 由于优化后一个用户只有一个会话，直接移除即可
	m.mu.RLock()
	session, exists := m.sessions[userID]
	m.mu.RUnlock()

	if exists {
		_ = session.Close()
		m.RemoveSession(userID)
		m.logger.Info("已移除用户的会话",
			zap.Int("user_id", userID))
	}
}

// GetAllSessions 获取所有会话（用于健康检查）
func (m *WebSocketSessionManager) GetAllSessions() []*WebSocketSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*WebSocketSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// UpdateActivity 更新最后活动时间
func (s *WebSocketSession) UpdateActivity() {
	now := time.Now()
	nowUnix := now.UnixNano()
	lastUnix := s.lastActivityUnixNano.Load()
	if lastUnix != 0 && nowUnix-lastUnix < int64(time.Second) {
		return
	}
	if !s.lastActivityUnixNano.CompareAndSwap(lastUnix, nowUnix) {
		return
	}

	s.mu.Lock()
	s.LastActivityTime = now
	s.mu.Unlock()

	s.notifyUpdated()
}

// clientWriteTimeout 是客户端 WebSocket 连接写操作的超时时间
const clientWriteTimeout = 10 * time.Second

// SendMessage 发送消息到客户端
// P2 修复: 添加写超时，防止客户端网络卡顿导致 Goroutine 阻塞
func (s *WebSocketSession) SendMessage(message interface{}) error {
	// 提取消息类型（用于日志）
	messageType := "unknown"
	messageForMarshal := message
	switch msg := message.(type) {
	case map[string]interface{}:
		if msgType, ok := msg["type"].(string); ok {
			messageType = msgType
		}
		if _, hasReceiveTime := msg["receive_time"]; hasReceiveTime {
			cloned := make(map[string]interface{}, len(msg)+1)
			for k, v := range msg {
				cloned[k] = v
			}
			cloned["send_time"] = time.Now().UnixMilli()
			messageForMarshal = cloned
		}
	case *FrontendTranscriptionMessage:
		messageType = string(msg.Type)
		if msg.ReceiveTime != 0 {
			cloned := *msg
			cloned.SendTime = time.Now().UnixMilli()
			messageForMarshal = &cloned
		}
	}

	data, err := json.Marshal(messageForMarshal)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	s.mu.RLock()
	conn := s.Conn
	userID := s.UserID
	logger := s.logger
	s.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("websocket 连接不存在")
	}

	if messageType != "transcription" && messageType != "time" && messageType != "audio_packet_stats" {
		// 直接打印 JSON（不进行修改优化，不使用转义）
		logger.Info("发送给客户端的消息",
			zap.Int("user_id", userID),
			zap.ByteString("json", data))
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := conn.SetWriteDeadline(time.Now().Add(clientWriteTimeout)); err != nil {
		return fmt.Errorf("设置写超时失败: %w", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.Error("发送消息失败",
			zap.Error(err),
			zap.Int("user_id", userID))
		return err
	}

	return nil
}

func (s *WebSocketSession) SendPing(timeout time.Duration) error {
	s.mu.RLock()
	conn := s.Conn
	s.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("websocket 连接不存在")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("设置 Ping 写超时失败: %w", err)
	}

	if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
		return err
	}

	return nil
}

// Close 关闭会话
func (s *WebSocketSession) Close() error {
	s.mu.Lock()

	s.clientDisconnectedOnce.Do(func() {
		if s.clientDisconnected != nil {
			close(s.clientDisconnected)
		}
	})

	cancel := s.cancel
	remoteConn := s.RemoteConn
	conn := s.Conn
	s.mu.Unlock()

	// 在会话主锁外执行取消和连接关闭，避免未来 ctx.Done 清理路径引入锁顺序问题。
	if cancel != nil {
		cancel()
	}
	if remoteConn != nil && !remoteConn.IsClosed() {
		_ = remoteConn.Close()
	}

	var err error
	if conn != nil {
		s.writeMu.Lock()
		err = conn.Close()
		s.writeMu.Unlock()
	}
	if err != nil {
		s.logger.Warn("关闭 WebSocket 连接失败",
			zap.Error(err),
			zap.Int("user_id", s.UserID))
	}

	s.logger.Info("WebSocket 会话已关闭",
		zap.Int("user_id", s.UserID))

	return err
}

// GetStats 获取统计信息
func (s *WebSocketSession) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"user_id":         s.UserID,
		"session_uuid":    s.SessionUUID,
		"is_transcribing": s.IsTranscribing(),
		"is_paused":       s.IsPaused(),
		"start_time":      s.StartTime,
	}
}

// ResumeSession 恢复会话
func (m *WebSocketSessionManager) ResumeSession(
	ctx context.Context,
	userID int,
	sessionUUID string,
	conn *websocket.Conn,
) (*WebSocketSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否有该用户的活跃会话
	if oldSession, ok := m.sessions[userID]; ok {
		// 检查 SessionUUID 是否匹配
		if oldSession.SessionUUID == sessionUUID {
			// 恢复会话
			m.logger.Info("恢复现有会话",
				zap.Int("user_id", userID),
				zap.String("session_uuid", sessionUUID))

			// 更新连接
			oldSession.mu.Lock()
			oldSession.Conn = conn
			oldSession.clientDisconnectedOnce = sync.Once{}
			oldSession.clientDisconnected = make(chan struct{})
			oldSession.mu.Unlock()

			return oldSession, nil
		}

		// SessionUUID 不匹配，关闭旧会话
		m.logger.Warn("SessionUUID 不匹配，关闭旧会话",
			zap.Int("user_id", userID),
			zap.String("old_uuid", oldSession.SessionUUID),
			zap.String("new_uuid", sessionUUID))
		_ = oldSession.Close()
		delete(m.sessions, userID)
	}

	// 创建新会话
	return m.CreateSession(ctx, userID, conn)
}

// GetSessionByUUID 根据 UUID 获取会话
func (m *WebSocketSessionManager) GetSessionByUUID(sessionUUID string) (*WebSocketSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, session := range m.sessions {
		if session.SessionUUID == sessionUUID {
			return session, true
		}
	}

	return nil, false
}

// SaveSessionState 保存会话状态（用于恢复）
func (m *WebSocketSessionManager) SaveSessionState(session *WebSocketSession) error {
	return m.recoveryManager.SaveSessionState(session)
}

// RestoreSession 恢复会话
func (m *WebSocketSessionManager) RestoreSession(
	ctx context.Context,
	sessionUUID string,
	newSession *WebSocketSession,
) error {
	return m.recoveryManager.RestoreSession(ctx, sessionUUID, newSession)
}

// GetRecoverableSession 获取可恢复的会话状态
func (m *WebSocketSessionManager) GetRecoverableSession(sessionUUID string) (*SessionState, bool) {
	return m.recoveryManager.GetSessionState(sessionUUID)
}

// ==========================================
// 单一数据源访问器方法
// ==========================================

// GetSessionUUIDFromTimeManager 从timeManager获取SessionUUID (单一数据源)
func (s *WebSocketSession) GetSessionUUIDFromTimeManager() string {
	if s.manager != nil && s.manager.timeManager != nil {
		if sessionInfo, exists := s.manager.timeManager.GetSessionInfo(s.UserID); exists {
			return sessionInfo.SessionUUID
		}
	}
	return s.SessionUUID // 降级到本地缓存
}

// GetMeetingIDFromTimeManager 从timeManager获取MeetingID (单一数据源)
func (s *WebSocketSession) GetMeetingIDFromTimeManager() int {
	if s.manager != nil && s.manager.timeManager != nil {
		if sessionInfo, exists := s.manager.timeManager.GetSessionInfo(s.UserID); exists {
			return sessionInfo.MeetingID
		}
	}
	if s.MeetingID != nil {
		return *s.MeetingID // 降级到本地缓存
	}
	return 0
}

// SyncFromTimeManager 从timeManager同步所有ID到本地缓存
func (s *WebSocketSession) SyncFromTimeManager() {
	if s.manager != nil && s.manager.timeManager != nil {
		if sessionInfo, exists := s.manager.timeManager.GetSessionInfo(s.UserID); exists {
			s.mu.Lock()
			s.SessionUUID = sessionInfo.SessionUUID
			if sessionInfo.MeetingID > 0 {
				s.MeetingID = &sessionInfo.MeetingID
			}
			s.mu.Unlock()

			// TODO: 使用TranscriptionCache更新WebSocketID
			// 暂时跳过WebSocketID更新，保持最小化修改
		}
	}
}

// convertWebSocketSessionToSessionInfo 将WebSocketSession转换为SessionInfo
func (m *WebSocketSessionManager) convertWebSocketSessionToSessionInfo(session *WebSocketSession, connectionState string) *SessionInfo {
	session.mu.RLock()
	defer session.mu.RUnlock()

	meetingID := 0
	if session.MeetingID != nil {
		meetingID = *session.MeetingID
	}

	// 创建SessionInfo
	sessionInfo := NewSessionInfoWithSessionID(strconv.Itoa(session.UserID), session.UserID, session.SessionUUID, meetingID)

	// 复制WebSocketSession的状态
	sessionInfo.IsTranscribing = session.IsTranscribing()
	sessionInfo.IsPaused = session.IsPaused()
	sessionInfo.LastActivityTime = session.LastActivityTime
	sessionInfo.CreatedAt = session.CreatedAt
	sessionInfo.LastUpdate = session.UpdatedAt

	// 复制配置信息
	sessionInfo.AudioFormat = session.AudioFormat
	sessionInfo.LanguageHints = append([]string(nil), session.LanguageHints...)
	sessionInfo.RemoteConnecting = session.RemoteConnecting()
	sessionInfo.RemoteReady = session.RemoteReady()
	sessionInfo.ConnectionState = connectionState

	// 设置状态
	if session.IsTranscribing() {
		sessionInfo.Status = "transcribing"
	} else if session.IsPaused() {
		sessionInfo.Status = "paused"
	} else {
		sessionInfo.Status = "idle"
	}

	return sessionInfo
}

// convertSessionInfoToWebSocketSession 将SessionInfo转换回WebSocketSession（用于恢复）
func (m *WebSocketSessionManager) convertSessionInfoToWebSocketSession(sessionInfo *SessionInfo, conn *websocket.Conn) *WebSocketSession {
	ctx, cancel := context.WithCancel(context.Background())

	session := &WebSocketSession{
		UserID:           sessionInfo.UserID,
		Conn:             conn,
		SessionUUID:      sessionInfo.SessionUUID,
		StartTime:        time.Now(), // 使用当前时间作为开始时间
		LastActivityTime: sessionInfo.LastActivityTime,
		CreatedAt:        sessionInfo.CreatedAt,
		UpdatedAt:        sessionInfo.LastUpdate,
		AudioFormat:      sessionInfo.AudioFormat,
		LanguageHints:    append([]string(nil), sessionInfo.LanguageHints...),
		ctx:              ctx,
		cancel:           cancel,
		logger:           m.logger,
		manager:          m,
	}
	if !sessionInfo.LastActivityTime.IsZero() {
		session.lastActivityUnixNano.Store(sessionInfo.LastActivityTime.UnixNano())
	}
	session.SetTranscribing(sessionInfo.IsTranscribing)
	session.SetPaused(sessionInfo.IsPaused)
	session.SetRemoteConnecting(sessionInfo.RemoteConnecting)
	session.SetRemoteReady(sessionInfo.RemoteReady)

	// 设置MeetingID
	if sessionInfo.MeetingID > 0 {
		session.MeetingID = &sessionInfo.MeetingID
	}

	return session
}
