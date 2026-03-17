// Package service 提供业务逻辑实现
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	applogger "github.com/chinaxxren/gonotic/internal/pkg/logger"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/repository"
)

// WebSocketHandler WebSocket 处理器
type WebSocketHandler struct {
	sessionManager *WebSocketSessionManager
	timeManager    UnifiedTimeManager
	remoteManager  *RemoteConnectionManager
	storage        TranscriptionStorage
	meetingRepo    repository.MeetingRepository
	config         *WebSocketHandlerConfig
	logger         *zap.Logger
}

const (
	audioSendFailureReconnectAfter = 1500 * time.Millisecond
	bufferedAudioReplayRetryDelay  = 50 * time.Millisecond
)

func (h *WebSocketHandler) injectClientPreferences(session *WebSocketSession, msg *WebSocketMessage, raw map[string]interface{}) {
	if msg == nil || raw == nil {
		return
	}

	if msg.Data == nil {
		msg.Data = make(map[string]interface{})
	}

	if nested, ok := raw["data"].(map[string]interface{}); ok {
		for k, v := range nested {
			if _, exists := msg.Data[k]; !exists {
				msg.Data[k] = v
			}
		}
	}

	extracted := false
	if v, ok := raw["audio_format"]; ok {
		msg.Data["audio_format"] = v
		extracted = true
	}
	if v, ok := raw["language_hints"]; ok {
		msg.Data["language_hints"] = v
		extracted = true
	}

	if extracted {
		applogger.DebugNoise(h.logger, h.config.Environment, "已合并客户端偏好字段到消息数据",
			zap.Int("user_id", session.UserID),
			zap.Any("data", msg.Data))
	}
}

func parseWebSocketMessage(raw map[string]interface{}) (WebSocketMessage, error) {
	var msg WebSocketMessage
	if raw == nil {
		return msg, fmt.Errorf("消息为空")
	}

	msgType, ok := raw["type"].(string)
	if !ok || msgType == "" {
		return msg, fmt.Errorf("消息类型缺失")
	}
	msg.Type = MessageType(msgType)

	if version, ok := raw["version"].(string); ok {
		msg.Version = version
	}
	if data, ok := raw["data"].(map[string]interface{}); ok {
		msg.Data = data
	}
	if clientTime, ok := float64FromValue(raw["clientTime"]); ok {
		msg.ClientTime = &clientTime
	}
	if success, ok := raw["success"].(bool); ok {
		msg.Success = &success
	}
	if code, ok := intFromValue(raw["code"]); ok {
		msg.Code = &code
	}
	if message, ok := raw["message"].(string); ok {
		msg.Message = message
	}

	return msg, nil
}

func float64FromValue(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	default:
		return 0, false
	}
}

func intFromValue(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

func updateSessionClientVersionForStart(logger *zap.Logger, session *WebSocketSession, msg WebSocketMessage) {
	if session == nil {
		return
	}
	if msg.Type != MessageTypeStart {
		return
	}
	if msg.Version == "" {
		return
	}

	session.mu.Lock()
	prev := session.ClientVersion
	session.ClientVersion = msg.Version
	session.mu.Unlock()

	if prev != msg.Version && logger != nil {
		logger.Info("客户端版本已更新",
			zap.Int("user_id", session.UserID),
			zap.String("prev", prev),
			zap.String("version", msg.Version))
	}
}

// 直接使用 repository.MeetingRepository，不需要额外的接口定义

// TranscriptionStorage 转录存储接口（避免循环依赖）
type TranscriptionStorage interface {
	SaveTranscription(ctx context.Context, record *TranscriptionRecord) error
	BatchSaveTranscription(ctx context.Context, records []*TranscriptionRecord) error
}

// TranscriptionRecord 转录记录（DynamoDB 存储结构）
type TranscriptionRecord struct {
	Text              string `json:"text"`                         // 转录文本
	Speaker           string `json:"speaker,omitempty"`            // 说话人标识
	Timestamp         int64  `json:"timestamp"`                    // Unix 时间戳
	Language          string `json:"language,omitempty"`           // 语言
	TranslationStatus string `json:"translation_status,omitempty"` // 翻译状态
	UID               int    `json:"uid"`                          // 用户 ID
	MeetingID         int    `json:"meeting_id"`                   // 会议 ID
}

// WebSocketHandlerConfig WebSocket 处理器配置
type WebSocketHandlerConfig struct {
	Environment       string
	RemoteURL         string
	EnterpriseManager *EnterpriseManager // 企业级API Key管理器
	PingInterval      time.Duration
	PongWait          time.Duration
	WriteWait         time.Duration
	MaxMessageSize    int64
	ReadBufferSize    int
	WriteBufferSize   int
	// STT 配置
	STTModel                             string
	STTAudioFormat                       string
	STTLanguageHints                     []string
	STTEnableProfanityFilter             bool
	STTEnableSpeakerDiarization          bool
	STTEnableGlobalSpeakerIdentification bool
	STTEnableSpeakerChangeDetection      bool
	// Audio 配置
	AudioSampleRate int
	AudioChannels   int
	// Translation 配置
	TranslationEnabled         bool
	TranslationType            string
	TranslationTargetLanguages []string
}

// DefaultWebSocketHandlerConfig 默认配置
func DefaultWebSocketHandlerConfig() *WebSocketHandlerConfig {
	return &WebSocketHandlerConfig{
		Environment:     "development",
		RemoteURL:       "ws://localhost:8001/transcribe",
		PingInterval:    30 * time.Second,
		PongWait:        60 * time.Second,
		WriteWait:       10 * time.Second,
		MaxMessageSize:  512 * 1024, // 512KB
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
}

// NewWebSocketHandler 创建 WebSocket 处理器
func NewWebSocketHandler(
	timeManager UnifiedTimeManager,
	storage TranscriptionStorage,
	meetingRepo repository.MeetingRepository,
	transcriptionCache TranscriptionCache,
	config *WebSocketHandlerConfig,
	logger *zap.Logger,
) *WebSocketHandler {
	if config == nil {
		config = DefaultWebSocketHandlerConfig()
	}

	// 创建Remote连接管理器（企业级管理器模式）
	if config.EnterpriseManager == nil {
		logger.Fatal("企业级管理器未配置")
	}
	remoteManager := NewRemoteConnectionManager(config.RemoteURL, config.EnterpriseManager, logger)
	if transcriptionCache == nil {
		transcriptionCache = NewMemoryTranscriptionCache(logger)
	}
	sessionManager := NewWebSocketSessionManager(timeManager, remoteManager, transcriptionCache, logger)

	handler := &WebSocketHandler{
		sessionManager: sessionManager,
		timeManager:    timeManager,
		remoteManager:  remoteManager,
		storage:        storage,
		meetingRepo:    meetingRepo,
		config:         config,
		logger:         logger,
	}

	// 注册时间管理器回调
	handler.registerTimeManagerCallbacks()

	return handler
}

// pingLoop P3 修复: 定期发送 Ping 帧，检测僵尸连接
// 如果 Ping 发送失败或超时未收到 Pong，关闭连接
func (h *WebSocketHandler) pingLoop(session *WebSocketSession) {
	ticker := time.NewTicker(h.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-session.ctx.Done():
			return
		case <-ticker.C:
			if err := session.SendPing(h.config.WriteWait); err != nil {
				h.logger.Debug("发送 Ping 失败，关闭连接",
					zap.Error(err),
					zap.Int("user_id", session.UserID))
				_ = session.Close()
				return
			}

			h.logger.Debug("Ping 已发送",
				zap.Int("user_id", session.UserID))
		}
	}
}

// HandleConnection 处理 WebSocket 连接
func (h *WebSocketHandler) HandleConnection(ctx context.Context, conn *websocket.Conn, userID int) error {
	// 创建会话
	session, err := h.sessionManager.CreateSession(ctx, userID, conn)
	if err != nil {
		h.logger.Error("创建会话失败",
			zap.Error(err),
			zap.Int("user_id", userID))
		return err
	}

	// 设置连接参数
	conn.SetReadLimit(h.config.MaxMessageSize)
	conn.SetReadDeadline(time.Now().Add(h.config.PongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.config.PongWait))
		// P3 修复: 收到 Pong 时更新最后活动时间
		session.UpdateActivity()
		return nil
	})

	// P3 修复: 启动 Ping 心跳 goroutine，检测僵尸连接
	go h.pingLoop(session)
	session.resultProcessorOnce.Do(func() {
		go h.processRemoteResults(session)
	})

	// 处理消息
	// 与 Python 版本一致：在断开连接时执行完整的清理流程
	defer func() {
		// 确保标记客户端连接已断开，供 stop 流程判断使用
		session.MarkClientDisconnected()

		// 区分正常断开和异常断开
		isAbnormalClose := false
		if r := recover(); r != nil {
			isAbnormalClose = true
			h.logger.Error("WebSocket 连接异常断开（panic）",
				zap.Any("panic", r),
				zap.Int("user_id", userID))
		}

		// 执行完整的断开连接清理流程（与 Python 版本的 disconnect_client 一致）
		h.handleClientDisconnect(session, isAbnormalClose)
	}()

	for {
		select {
		case <-session.ctx.Done():
			// 正常断开（上下文取消）
			h.logger.Info("WebSocket 连接正常断开（上下文取消）",
				zap.Int("user_id", userID))
			return nil
		default:
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				session.MarkClientDisconnected()
				// 检查是否为异常关闭
				isAbnormalClose := websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure)
				if isAbnormalClose {
					h.logger.Error("WebSocket 连接异常关闭",
						zap.Error(err),
						zap.Int("user_id", userID))
				} else {
					h.logger.Info("WebSocket 连接正常关闭",
						zap.Error(err),
						zap.Int("user_id", userID))
				}
				// 返回错误，触发 defer 清理
				return err
			}

			// 更新最后活动时间
			session.UpdateActivity()
			if err := h.handleMessage(session, messageType, message); err != nil {
				h.logger.Error("处理消息失败",
					zap.Error(err),
					zap.Int("user_id", userID))
				// 发送错误消息
				_ = session.SendMessage(NewErrorMessage("5000", "Failed to process message", err.Error()))
			}
		}
	}
}

// handleMessage 处理消息
func (h *WebSocketHandler) handleMessage(session *WebSocketSession, messageType int, message []byte) error {
	switch messageType {
	case websocket.TextMessage:
		return h.handleTextMessage(session, message)
	case websocket.BinaryMessage:
		return h.handleBinaryMessage(session, message)
	default:
		return fmt.Errorf("不支持的消息类型: %d", messageType)
	}
}

// handleTextMessage 处理文本消息（命令）
func (h *WebSocketHandler) handleTextMessage(session *WebSocketSession, message []byte) error {
	var rawMsg map[string]interface{}
	if err := json.Unmarshal(message, &rawMsg); err != nil {
		return fmt.Errorf("解析消息失败: %w", err)
	}

	messageType, _ := rawMsg["type"].(string)
	if messageType == "time" {
		return session.SendMessage(rawMsg)
	}

	msg, err := parseWebSocketMessage(rawMsg)
	if err != nil {
		return fmt.Errorf("解析消息失败: %w", err)
	}

	// 只对关键类型打印 Info，避免高频噪音日志。
	if msg.Type == MessageTypeStart || msg.Type == MessageTypeStop || msg.Type == MessageTypeResume {
		h.logger.Info("收到客户端的消息",
			zap.Int("user_id", session.UserID),
			zap.ByteString("json", message))
	}

	switch msg.Type {
	case MessageTypeStart:
		updateSessionClientVersionForStart(h.logger, session, msg)
		h.injectClientPreferences(session, &msg, rawMsg)
		return h.handleStart(session, msg)
	case MessageTypePause:
		return h.handlePause(session, msg)
	case MessageTypeResume:
		return h.handleResume(session, msg)
	case MessageTypeStop:
		return h.handleStop(session, msg)
	case MessageTypeKeepalive:
		return h.handleKeepalive(session, msg)
	default:
		return fmt.Errorf("未知的消息类型: %s", msg.Type)
	}
}

// handleBinaryMessage 处理二进制消息（音频数据）
func (h *WebSocketHandler) handleBinaryMessage(session *WebSocketSession, audioData []byte) error {
	now := time.Now()
	if h.config == nil || h.config.Environment != "production" {
		if prevCount, shouldSendStats := session.RecordAudioPacket(now); shouldSendStats {
			_ = session.SendMessage(map[string]interface{}{
				"type":      "audio_packet_stats",
				"packets":   prevCount,
				"timestamp": now.UnixMilli(),
			})
		}
	}

	applogger.DebugNoise(h.logger, h.config.Environment, "收到客户端音频包",
		zap.Int("user_id", session.UserID),
		zap.Int("audio_bytes", len(audioData)))

	isTranscribing := session.IsTranscribing()
	isPaused := session.IsPaused()
	remoteConnecting := session.RemoteConnecting()
	remoteReady := session.RemoteReady()
	session.mu.RLock()
	remoteConn := session.RemoteConn
	sessionUUID := session.SessionUUID
	session.mu.RUnlock()

	// 检查是否正在转录
	if !isTranscribing || isPaused {
		return nil
	}

	// Remote 连接未就绪时直接丢弃音频
	if remoteConnecting || !remoteReady {
		if err := session.BufferAudio(audioData); err != nil {
			h.failSessionForAudioBufferOverflow(session, sessionUUID, err)
		}
		return nil
	}

	// 检查 Remote 连接是否可用
	if remoteConn == nil || remoteConn.IsClosed() {
		if err := session.BufferAudio(audioData); err != nil {
			h.failSessionForAudioBufferOverflow(session, sessionUUID, err)
		}
		return nil
	}

	// 直接转发音频块，不做额外处理
	if err := remoteConn.SendAudio(audioData); err != nil {
		if errors.Is(err, ErrRemoteAudioQueueFull) {
			if bufferErr := session.BufferAudio(audioData); bufferErr != nil {
				h.failSessionForAudioBufferOverflow(session, sessionUUID, bufferErr)
				return nil
			}
			if !session.MarkAudioSendFailure(now, audioSendFailureReconnectAfter) {
				return nil
			}
			h.logger.Warn("Remote 音频发送队列持续满载，准备重连",
				zap.Int("user_id", session.UserID),
				zap.String("session_uuid", sessionUUID))
		} else {
			h.logger.Error("转发音频到 Remote 失败",
				zap.Error(err),
				zap.Int("user_id", session.UserID),
				zap.String("session_uuid", sessionUUID))

			if !session.MarkAudioSendFailure(now, audioSendFailureReconnectAfter) {
				return nil
			}
		}

		// 非预期失败持续：尝试自动重连，成功则继续同一 session/meeting，不中断 timeManager。
		// 若已有其他 goroutine 在重连，直接返回，避免误 Stop/误通知客户端。
		// **P0 修复**: 将重连逻辑异步化，避免阻塞 WebSocket 主读取循环。
		if !session.BeginRemoteReconnect() {
			// 已有重连任务在执行，直接返回（音频会被丢弃，这是预期行为）
			return nil
		}

		// 异步执行重连，不阻塞主循环
		go func(oldRemoteConn *RemoteConnection) {
			defer session.EndRemoteReconnect()

			reconnectErr := h.reconnectRemote(session)
			if reconnectErr == nil {
				_ = oldRemoteConn.Close()
				session.ResetAudioSendFailure()
				h.logger.Info("Remote 异步重连成功（音频发送失败触发）",
					zap.Int("user_id", session.UserID))
				return
			}

			h.logger.Error("Remote 自动重连失败（音频发送失败触发）",
				zap.Error(reconnectErr),
				zap.Int("user_id", session.UserID))

			session.SetRemoteReady(false)
			session.SetRemoteConnecting(false)
			session.SetTranscribing(false)
			session.mu.Lock()
			if session.RemoteConn == oldRemoteConn {
				session.RemoteConn = nil
			}
			session.mu.Unlock()
			session.ResetAudioSendFailure()
			_ = oldRemoteConn.Close()
			if h.timeManager != nil {
				_, _ = h.timeManager.StopTranscription(
					context.Background(),
					session.UserID,
					nil,
					nil,
				)
			}
			_ = session.SendMessage(NewServiceUnavailableMessage("Transcription service unavailable", "Remote disconnected"))
		}(remoteConn)

		return nil
	}

	session.ResetAudioSendFailure()
	if session.HasBufferedAudio() {
		h.scheduleBufferedAudioFlush(session, remoteConn)
	}
	return nil
}

func (h *WebSocketHandler) failSessionForAudioBufferOverflow(session *WebSocketSession, sessionUUID string, err error) {
	if session == nil {
		return
	}

	h.logger.Error("音频缓冲区已满，停止会话避免静默丢音频",
		zap.Error(err),
		zap.Int("user_id", session.UserID),
		zap.String("session_uuid", sessionUUID))
	_ = session.SendMessage(NewStopAndClearMessage("Buffered audio overflow"))
	_ = session.Close()
}

func (h *WebSocketHandler) reconnectRemote(session *WebSocketSession) error {
	session.mu.Lock()
	oldConn := session.RemoteConn
	lastConfig := (*RemoteConfig)(nil)
	if oldConn != nil {
		lastConfig = oldConn.GetLastConfig()
	}
	session.SetRemoteReady(false)
	session.SetRemoteConnecting(true)
	session.mu.Unlock()

	if oldConn != nil {
		_ = oldConn.Close()
	}

	defer func() {
		session.SetRemoteConnecting(false)
	}()

	session.mu.RLock()
	sessionID := session.SessionUUID
	session.mu.RUnlock()
	if sessionID == "" {
		sessionID = strconv.Itoa(session.UserID)
	}

	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt-1) * 200 * time.Millisecond)
		}
		h.logger.Info("尝试重连 Remote",
			zap.Int("user_id", session.UserID),
			zap.Int("attempt", attempt))
		newConn, err := h.remoteManager.Connect(session.ctx, session.UserID, sessionID)
		if err != nil {
			lastErr = err
			continue
		}

		cfg := lastConfig
		if cfg != nil {
			copyCfg := *cfg
			copyCfg.APIKey = newConn.apiKey
			cfg = &copyCfg
		} else {
			cfg = h.buildRemoteConfigForSession(session, newConn.apiKey)
		}

		if cfg == nil {
			lastErr = fmt.Errorf("无法构建 Remote 配置")
			_ = newConn.Close()
			continue
		}

		if _, err := newConn.SendConfigAndWaitResponse(cfg, 5*time.Second); err != nil {
			lastErr = err
			_ = newConn.Close()
			continue
		}

		session.mu.Lock()
		session.RemoteConn = newConn
		session.mu.Unlock()
		session.SetRemoteReady(true)
		session.SetRemoteConnecting(false)

		resultGeneration := session.AdvanceResultGeneration()
		go h.receiveFromRemote(session, newConn, resultGeneration)
		h.scheduleBufferedAudioFlush(session, newConn)
		h.logger.Info("Remote 重连成功",
			zap.Int("user_id", session.UserID))
		return nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("remote 重连失败")
	}
	return lastErr
}

func (h *WebSocketHandler) scheduleBufferedAudioFlush(session *WebSocketSession, remoteConn *RemoteConnection) {
	if session == nil || remoteConn == nil {
		return
	}
	if !session.HasBufferedAudio() {
		return
	}
	if !session.BeginBufferedAudioFlush() {
		return
	}

	go func() {
		defer session.EndBufferedAudioFlush()
		h.flushBufferedAudio(session, remoteConn)
	}()
}

func (h *WebSocketHandler) flushBufferedAudio(session *WebSocketSession, remoteConn *RemoteConnection) {
	for {
		if session.ctx.Err() != nil {
			return
		}

		packets := session.DrainBufferedAudio()
		if len(packets) == 0 {
			return
		}

		retryLater := false
		for idx, packet := range packets {
			session.mu.RLock()
			currentRemote := session.RemoteConn
			remoteReady := session.RemoteReady()
			session.mu.RUnlock()

			if currentRemote != remoteConn || !remoteReady {
				if err := session.RequeueBufferedAudio(packets[idx:]); err != nil {
					h.failSessionForAudioBufferOverflow(session, session.SessionUUID, err)
					return
				}
				if currentRemote != nil && remoteReady {
					remoteConn = currentRemote
					retryLater = true
				} else {
					return
				}
				break
			}

			if err := remoteConn.SendAudio(packet.Data); err != nil {
				fields := []zap.Field{
					zap.Int("user_id", session.UserID),
					zap.String("session_uuid", session.SessionUUID),
					zap.Int("remaining_packets", len(packets)-idx),
				}
				if !errors.Is(err, ErrRemoteAudioQueueFull) {
					fields = append(fields, zap.Error(err))
				}
				h.logger.Warn("回放缓冲音频失败，重新入队等待后续发送", fields...)
				if requeueErr := session.RequeueBufferedAudio(packets[idx:]); requeueErr != nil {
					h.failSessionForAudioBufferOverflow(session, session.SessionUUID, requeueErr)
					return
				}
				if errors.Is(err, ErrRemoteAudioQueueFull) {
					retryLater = true
				} else {
					return
				}
				break
			}
		}

		if !retryLater {
			if !session.HasBufferedAudio() {
				return
			}
			continue
		}

		select {
		case <-session.ctx.Done():
			return
		case <-time.After(bufferedAudioReplayRetryDelay):
		}
	}
}

func (h *WebSocketHandler) buildRemoteConfigForSession(session *WebSocketSession, apiKey string) *RemoteConfig {
	session.mu.RLock()
	audioFormat := session.AudioFormat
	languageHints := append([]string(nil), session.LanguageHints...)
	session.mu.RUnlock()

	sttConfig := ServerSTTConfig{
		Model:                             h.config.STTModel,
		AudioFormat:                       h.config.STTAudioFormat,
		LanguageHints:                     h.config.STTLanguageHints,
		EnableProfanityFilter:             h.config.STTEnableProfanityFilter,
		EnableSpeakerDiarization:          h.config.STTEnableSpeakerDiarization,
		EnableGlobalSpeakerIdentification: h.config.STTEnableGlobalSpeakerIdentification,
		EnableSpeakerChangeDetection:      h.config.STTEnableSpeakerChangeDetection,
	}

	audioConfig := ServerAudioConfig{
		SampleRate: h.config.AudioSampleRate,
		Channels:   h.config.AudioChannels,
	}

	translationConfig := &ServerTranslationConfig{
		Enabled:         true,
		Type:            h.config.TranslationType,
		TargetLanguages: h.config.TranslationTargetLanguages,
	}

	return BuildRemoteStartPayload(
		apiKey,
		audioFormat,
		languageHints,
		sttConfig,
		audioConfig,
		translationConfig,
	)
}

// GetStats 获取统计信息
func (h *WebSocketHandler) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"active_sessions": h.sessionManager.GetActiveSessionCount(),
	}
}

// GetSessionInfo 获取会话信息（简化版：直接使用userID）
func (h *WebSocketHandler) GetSessionInfo(userID int) (map[string]interface{}, error) {
	session, exists := h.sessionManager.GetSessionByUserID(userID)
	if !exists {
		return nil, fmt.Errorf("会话不存在")
	}

	return session.GetStats(), nil
}

// GetConnectionStatus 获取连接状态信息（与 Python 版本的 get_connection_status 一致）
func (h *WebSocketHandler) GetConnectionStatus(userID *int) map[string]interface{} {
	if userID != nil {
		// 返回特定用户的连接状态
		userConnections := make(map[string]interface{})
		sessions := h.sessionManager.GetAllSessions()

		for _, session := range sessions {
			if session.UserID == *userID {
				session.mu.RLock()
				userConnections[strconv.Itoa(session.UserID)] = map[string]interface{}{
					"state":        h.sessionManager.GetConnectionStateByUserID(session.UserID),
					"connected_at": session.StartTime.Unix(),
					"session_uuid": session.SessionUUID,
					"meeting_id":   session.MeetingID,
				}
				session.mu.RUnlock()
			}
		}

		return map[string]interface{}{
			"user_id":            *userID,
			"active_connections": userConnections,
		}
	} else {
		// 返回所有连接状态
		allConnections := make(map[string]interface{})
		sessions := h.sessionManager.GetAllSessions()

		for _, session := range sessions {
			session.mu.RLock()
			allConnections[strconv.Itoa(session.UserID)] = map[string]interface{}{
				"user_id":      session.UserID,
				"state":        h.sessionManager.GetConnectionStateByUserID(session.UserID),
				"connected_at": session.StartTime.Unix(),
				"session_uuid": session.SessionUUID,
				"meeting_id":   session.MeetingID,
			}
			session.mu.RUnlock()
		}

		// 统计唯一用户数
		uniqueUsers := make(map[int]bool)
		for _, session := range sessions {
			uniqueUsers[session.UserID] = true
		}

		return map[string]interface{}{
			"total_connections": len(sessions),
			"total_users":       len(uniqueUsers),
			"connections":       allConnections,
		}
	}
}

// GetConnectionHealthReport 获取连接健康报告（与 Python 版本的 get_connection_health_report 一致）
func (h *WebSocketHandler) GetConnectionHealthReport() map[string]interface{} {
	now := time.Now()
	sessions := h.sessionManager.GetAllSessions()

	healthReport := map[string]interface{}{
		"timestamp":                 now.Unix(),
		"total_active_connections":  len(sessions),
		"connection_states_summary": make(map[string]int),
		"long_running_connections":  []map[string]interface{}{},
		"potential_issues":          []string{},
	}

	// 统计连接状态
	stateSummary := make(map[string]int)
	stateSummary["connecting"] = 0
	stateSummary["connected"] = 0
	stateSummary["disconnecting"] = 0
	stateSummary["disconnected"] = 0

	longRunningConnections := []map[string]interface{}{}
	uniqueUsers := make(map[int]bool)

	for _, session := range sessions {
		state := h.sessionManager.GetConnectionStateByUserID(session.UserID)
		stateSummary[state]++
		uniqueUsers[session.UserID] = true

		// 检查长时间运行的连接（超过1小时）
		session.mu.RLock()
		duration := now.Sub(session.StartTime)
		sessionUUID := session.SessionUUID
		session.mu.RUnlock()

		if duration > time.Hour {
			longRunningConnections = append(longRunningConnections, map[string]interface{}{
				"user_id":        session.UserID,
				"duration_hours": duration.Hours(),
				"session_uuid":   sessionUUID,
			})
		}
	}

	healthReport["connection_states_summary"] = stateSummary
	healthReport["total_users"] = len(uniqueUsers)
	healthReport["long_running_connections"] = longRunningConnections

	// 检查潜在问题
	potentialIssues := []string{}
	if stateSummary["disconnecting"] > 0 {
		potentialIssues = append(potentialIssues, "有连接正在断开中，可能存在清理延迟")
	}
	if stateSummary["connecting"] > 2 {
		potentialIssues = append(potentialIssues, "多个连接正在建立中，可能存在竞态条件")
	}
	if len(longRunningConnections) > 0 {
		potentialIssues = append(potentialIssues, fmt.Sprintf("发现%d个长时间运行的连接", len(longRunningConnections)))
	}

	healthReport["potential_issues"] = potentialIssues

	return healthReport
}

// CleanupStaleConnections 清理过期连接（与 Python 版本的 cleanup_stale_connections 一致）
func (h *WebSocketHandler) CleanupStaleConnections(maxAgeSeconds int) int {
	now := time.Now()
	maxAge := time.Duration(maxAgeSeconds) * time.Second
	staleCount := 0

	sessions := h.sessionManager.GetAllSessions()
	for _, session := range sessions {
		session.mu.RLock()
		age := now.Sub(session.StartTime)
		// sessionID := session.ID // 不再需要
		userID := session.UserID
		session.mu.RUnlock()

		if age > maxAge {
			h.logger.Warn("发现过期连接，准备清理",
				zap.Int("user_id", userID),
				zap.Duration("age", age))

			// 执行断开连接清理
			h.handleClientDisconnect(session, false)
			staleCount++
		}
	}

	if staleCount > 0 {
		h.logger.Info("已清理过期连接",
			zap.Int("count", staleCount),
			zap.Int("max_age_seconds", maxAgeSeconds))
	}

	return staleCount
}

// SendMessageToClient 发送消息到指定客户端（与 Python 版本的 send_message_to_client 一致）
// 带连接状态检查和日志记录
func (h *WebSocketHandler) SendMessageToClient(sessionID string, message map[string]interface{}) bool {
	session, exists := h.sessionManager.GetSession(sessionID)
	if !exists {
		h.logger.Warn("客户端连接不存在",
			zap.String("session_id", sessionID))
		return false
	}

	// 检查连接状态
	session.mu.RLock()
	conn := session.Conn
	session.mu.RUnlock()

	if conn == nil {
		h.logger.Warn("连接已断开，无法发送消息",
			zap.String("session_id", sessionID))
		return false
	}

	// 发送消息
	if err := session.SendMessage(message); err != nil {
		h.logger.Error("发送消息失败",
			zap.Error(err),
			zap.String("session_id", sessionID))
		return false
	}

	return true
}

// SendErrorAndDisconnect 发送错误消息并断开客户端连接（与 Python 版本的 _send_error_and_disconnect 一致）
func (h *WebSocketHandler) SendErrorAndDisconnect(sessionID string, errorMessage string) {
	session, exists := h.sessionManager.GetSession(sessionID)
	if !exists {
		h.logger.Warn("客户端连接不存在，无法发送错误",
			zap.String("session_id", sessionID))
		return
	}

	// 发送错误消息
	errorMsg := NewErrorMessage("1205", errorMessage, "")
	if err := session.SendMessage(errorMsg); err != nil {
		h.logger.Error("发送错误消息失败",
			zap.Error(err),
			zap.String("session_id", sessionID))
	}

	// 断开连接
	h.handleClientDisconnect(session, false)
	h.logger.Info("已发送错误消息并断开客户端",
		zap.String("session_id", sessionID),
		zap.String("error_message", errorMessage))
}

// SendServiceUnavailableError 发送服务不可用错误（与 Python 版本的 _send_service_unavailable_error 一致）
func (h *WebSocketHandler) SendServiceUnavailableError(sessionID string) {
	session, exists := h.sessionManager.GetSession(sessionID)
	if !exists {
		h.logger.Warn("客户端连接不存在，无法发送服务不可用错误",
			zap.String("session_id", sessionID))
		return
	}

	errorMsg := NewServiceUnavailableMessage(
		"Transcription service unavailable",
		"Please try again later",
	)
	if err := session.SendMessage(errorMsg); err != nil {
		h.logger.Error("发送服务不可用错误失败",
			zap.Error(err),
			zap.String("session_id", sessionID))
	} else {
		h.logger.Info("已发送服务不可用错误",
			zap.String("session_id", sessionID))
	}
}

// ForceCleanupSessionOnError 强制清理错误会话（与 Python 版本的 _force_cleanup_session_on_error 一致）
func (h *WebSocketHandler) ForceCleanupSessionOnError(userID int, errorMessage string) {
	h.logger.Warn("开始强制清理错误会话",
		zap.Int("user_id", userID),
		zap.String("error_message", errorMessage))

	// 1. 强制停止时间管理器中的会话
	if h.timeManager != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					h.logger.Error("强制停止时间管理器异常",
						zap.Any("panic", r),
						zap.Int("user_id", userID))
				}
			}()

			// 尝试停止时间管理器会话
			_, err := h.timeManager.StopTranscription(
				context.Background(),
				userID,
				nil,
				nil,
			)
			if err != nil {
				h.logger.Error("强制停止时间管理器失败",
					zap.Error(err),
					zap.Int("user_id", userID))
			} else {
				h.logger.Info("时间管理器会话已强制停止",
					zap.Int("user_id", userID))
			}
		}()
	}

	// 2. 清理该用户的所有 WebSocket 连接
	sessions := h.sessionManager.GetAllSessions()
	for _, session := range sessions {
		if session.UserID == userID {
			h.logger.Info("清理用户的 WebSocket 连接",
				zap.Int("user_id", userID))
			h.handleClientDisconnect(session, true)
		}
	}

	h.logger.Info("强制清理错误会话完成",
		zap.Int("user_id", userID))
}

// CleanupWebSocketConnectionsForUser 清理用户的所有 WebSocket 连接（与 Python 版本的 _cleanup_websocket_connections_for_user 一致）
func (h *WebSocketHandler) CleanupWebSocketConnectionsForUser(userID int) {
	h.logger.Info("清理用户的所有 WebSocket 连接",
		zap.Int("user_id", userID))

	sessions := h.sessionManager.GetAllSessions()
	cleanedCount := 0

	for _, session := range sessions {
		if session.UserID == userID {
			h.logger.Info("关闭用户的 WebSocket 连接",
				zap.Int("user_id", userID))

			// 关闭连接
			func() {
				defer func() {
					if r := recover(); r != nil {
						h.logger.Error("关闭连接异常",
							zap.Any("panic", r),
							zap.Int("user_id", userID))
					}
				}()

				if err := session.Close(); err != nil {
					h.logger.Warn("关闭连接失败",
						zap.Error(err),
						zap.Int("user_id", userID))
				}
			}()

			// 执行断开连接清理
			h.handleClientDisconnect(session, false)
			cleanedCount++
		}
	}

	// 从会话管理器中移除
	h.sessionManager.RemoveUserSessions(userID)

	h.logger.Info("已清理用户的所有 WebSocket 连接",
		zap.Int("user_id", userID),
		zap.Int("cleaned_count", cleanedCount))
}

// handleClientDisconnect 处理客户端断开连接（与 Python 版本的 disconnect_client 一致）
// 执行完整的清理流程：更新会议统计、停止时间管理器、关闭 Remote 连接、清理空会议等
func (h *WebSocketHandler) handleClientDisconnect(session *WebSocketSession, isAbnormalClose bool) {
	h.logger.Info("开始断开连接清理流程",
		zap.Int("user_id", session.UserID),
		zap.Bool("is_abnormal_close", isAbnormalClose))

	// 更新连接状态为 "disconnecting"（与 Python 版本一致）
	h.sessionManager.SetConnectionStateByUserID(session.UserID, "disconnecting")

	// 获取会话状态，判断断开原因
	session.mu.RLock()
	isPaused := session.IsPaused()
	isTranscribing := session.IsTranscribing()
	isStopped := session.IsStopped()
	meetingID := session.MeetingID
	session.mu.RUnlock()

	// 判断是否为正常断开：只有用户主动停止了会话才是正常的
	isNormalDisconnect := isStopped

	// 如果用户没有主动停止，都保持会话状态等待重连
	if !isNormalDisconnect {
		if isTranscribing {
			h.logger.Info("检测到异常断开，保持转录会话状态等待重连",
				zap.Int("user_id", session.UserID),
				zap.Bool("is_transcribing", isTranscribing))
		} else if isPaused {
			h.logger.Info("检测到异常断开，保持暂停会话状态等待重连",
				zap.Int("user_id", session.UserID),
				zap.Bool("is_paused", isPaused))
		} else {
			h.logger.Info("检测到异常断开，保持会话状态等待重连",
				zap.Int("user_id", session.UserID))
		}

		// 断网/异常断开也需要写入 meeting 状态，但要避免与 pause/stop 重复写入。
		// 规则：pause/stop 必写；如果 pause/stop 已写入（StatisticsUpdated=true），断网不再写。
		if meetingID != nil && h.meetingRepo != nil && session.StartedMessageSent() {
			session.mu.RLock()
			statisticsUpdated := session.StatisticsUpdated
			session.mu.RUnlock()

			if !statisticsUpdated {
				// 标记已写入，避免后续重复触发
				session.mu.Lock()
				session.StatisticsUpdated = true
				session.mu.Unlock()

				h.updateMeetingStatistics(*meetingID, session, string(model.StatusFailed), true, nil)
			} else {
				h.logger.Debug("断网断开时统计信息已写入，跳过重复写入",
					zap.Int("user_id", session.UserID),
					zap.Int("meeting_id", *meetingID))
			}
		} else if meetingID != nil && h.meetingRepo != nil {
			h.logger.Debug("异常断开前未产生有效 final，跳过会议状态写入",
				zap.Int("user_id", session.UserID),
				zap.Int("meeting_id", *meetingID))
		}

		// 异常断开前也尝试补刷一轮待持久化 final 结果，减少仅前端可见的历史缺口。
		if err := h.flushPendingTranscriptionRecordsForSession(session); err != nil {
			h.logger.Error("异常断开前补刷待持久化 final 结果失败",
				zap.Error(err),
				zap.Int("user_id", session.UserID),
				zap.String("session_uuid", session.SessionUUID))
		}

		// 只更新连接状态为断开，不执行完整清理
		h.sessionManager.SetConnectionStateByUserID(session.UserID, "disconnected")
		return
	}

	h.logger.Info("执行正常断开清理流程（用户主动停止）",
		zap.Int("user_id", session.UserID),
		zap.Bool("is_stopped", isStopped))

	// 1. 更新会议状态到数据库（与 Python 版本一致）
	if meetingID != nil && h.meetingRepo != nil && session.StartedMessageSent() {
		// 断开连接时更新统计信息，但要防止重复更新
		session.mu.Lock()
		if !session.StatisticsUpdated {
			session.StatisticsUpdated = true
			session.mu.Unlock()
			h.updateMeetingStatistics(*meetingID, session, "completed", true, nil)
		} else {
			session.mu.Unlock()
			h.logger.Debug("统计信息已更新，跳过重复更新",
				zap.Int("user_id", session.UserID),
				zap.Int("meeting_id", *meetingID))
		}
	} else if meetingID != nil && h.meetingRepo != nil {
		h.logger.Debug("未产生有效 final，断开时跳过 completed 状态写入",
			zap.Int("user_id", session.UserID),
			zap.Int("meeting_id", *meetingID))
	} else {
		h.logger.Debug("会议记录ID缺失（用户未进行转录）",
			zap.Int("user_id", session.UserID))
	}

	// 2. 停止或暂停时间管理器（与 Python 版本一致）
	if h.timeManager != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					h.logger.Error("时间管理器操作异常",
						zap.Any("panic", r),
						zap.Int("user_id", session.UserID))
				}
			}()

			if isPaused {
				// 如果已经暂停，不需要再次暂停
				h.logger.Debug("会话已暂停，跳过时间管理器操作",
					zap.Int("user_id", session.UserID))
			} else if isTranscribing {
				// 如果正在转录，停止时间管理器
				h.logger.Info("停止时间管理器会话",
					zap.Int("user_id", session.UserID))
				_, err := h.timeManager.StopTranscription(
					context.Background(),
					session.UserID,
					meetingID,
					nil,
				)
				if err != nil {
					h.logger.Error("停止时间管理器失败",
						zap.Error(err),
						zap.Int("user_id", session.UserID))
				} else {
					h.logger.Info("时间管理器已停止",
						zap.Int("user_id", session.UserID))
				}
			}
		}()
	}

	// 3. 关闭 Remote 连接（与 Python 版本一致）
	func() {
		defer func() {
			if r := recover(); r != nil {
				h.logger.Error("关闭 Remote 连接异常",
					zap.Any("panic", r),
					zap.Int("user_id", session.UserID))
			}
		}()

		session.mu.RLock()
		remoteConn := session.RemoteConn
		session.mu.RUnlock()

		if remoteConn != nil {
			if err := remoteConn.Close(); err != nil {
				h.logger.Warn("关闭 Remote 连接失败",
					zap.Error(err),
					zap.Int("user_id", session.UserID))
			} else {
				h.logger.Info("Remote 连接已关闭",
					zap.Int("user_id", session.UserID))
			}
		}
	}()

	// 关闭 Remote 后主动补刷一次待持久化 final 结果。
	if err := h.flushPendingTranscriptionRecordsForSession(session); err != nil {
		h.logger.Error("关闭 Remote 后补刷待持久化 final 结果失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID),
			zap.String("session_uuid", session.SessionUUID))
	}

	// 4. 清理空会议记录（与 Python 版本一致，仅在停止时清理）
	if !isPaused {
		func() {
			defer func() {
				if r := recover(); r != nil {
					h.logger.Error("清理空会议异常",
						zap.Any("panic", r),
						zap.Int("user_id", session.UserID))
				}
			}()

			if err := h.cleanupEmptyMeeting(session); err != nil {
				h.logger.Warn("清理空会议失败",
					zap.Error(err),
					zap.Int("user_id", session.UserID))
			}
		}()
	}

	// 5. 从会话管理器中移除会话（与 Python 版本的从活跃连接中移除一致）
	h.sessionManager.RemoveSession(session.UserID)

	// 6. 关闭会话（关闭 WebSocket 连接）
	func() {
		defer func() {
			if r := recover(); r != nil {
				h.logger.Error("关闭会话异常",
					zap.Any("panic", r),
					zap.Int("user_id", session.UserID))
			}
		}()

		if err := session.Close(); err != nil {
			h.logger.Warn("关闭会话失败",
				zap.Error(err),
				zap.Int("user_id", session.UserID))
		}
	}()

	// 更新连接状态为 "disconnected"（与 Python 版本一致）
	h.sessionManager.SetConnectionStateByUserID(session.UserID, "disconnected")

	h.logger.Info("断开连接清理流程完成",
		zap.Int("user_id", session.UserID))
}

// registerTimeManagerCallbacks 注册时间管理器回调
func (h *WebSocketHandler) registerTimeManagerCallbacks() {
	// 注册警告回调
	h.timeManager.SetWarningCallback(func(userID int, warningType string, remainingSeconds int, planID string) {
		h.sendWarningToUser(userID, warningType, remainingSeconds, planID)
	})

	// 注册强制停止回调
	h.timeManager.SetForceStopCallback(func(userID int, reason string) {
		h.forceStopUserSession(userID, reason)
	})

}

// verifyStateConsistency 验证时间管理器状态与WebSocket状态的一致性
func (h *WebSocketHandler) verifyStateConsistency(operation string, session *WebSocketSession) error {
	// 获取时间管理器中的会话状态
	sessionInfo, exists := h.timeManager.GetSessionInfo(session.UserID)
	if !exists {
		// 如果是停止操作，时间管理器中没有会话是正常的
		if operation == "stop" {
			return nil
		}
		return fmt.Errorf("时间管理器中未找到会话，但WebSocket会话存在")
	}

	// 获取WebSocket会话状态
	wsIsTranscribing := session.IsTranscribing()
	wsIsPaused := session.IsPaused()

	// 验证状态一致性
	var inconsistencies []string

	// 检查转录状态
	if sessionInfo.IsTranscribing != wsIsTranscribing {
		inconsistencies = append(inconsistencies,
			fmt.Sprintf("转录状态不一致: TimeManager=%v, WebSocket=%v",
				sessionInfo.IsTranscribing, wsIsTranscribing))
	}

	// 检查暂停状态
	if sessionInfo.IsPaused != wsIsPaused {
		inconsistencies = append(inconsistencies,
			fmt.Sprintf("暂停状态不一致: TimeManager=%v, WebSocket=%v",
				sessionInfo.IsPaused, wsIsPaused))
	}

	// 检查逻辑冲突
	if sessionInfo.IsTranscribing && sessionInfo.IsPaused {
		inconsistencies = append(inconsistencies, "时间管理器状态异常：同时处于转录和暂停状态")
	}

	if wsIsTranscribing && wsIsPaused {
		inconsistencies = append(inconsistencies, "WebSocket状态异常：同时处于转录和暂停状态")
	}

	if len(inconsistencies) > 0 {
		h.logger.Warn("检测到状态不一致",
			zap.String("operation", operation),
			zap.Int("user_id", session.UserID),
			zap.Strings("inconsistencies", inconsistencies),
			zap.Bool("tm_is_transcribing", sessionInfo.IsTranscribing),
			zap.Bool("tm_is_paused", sessionInfo.IsPaused),
			zap.Bool("ws_is_transcribing", wsIsTranscribing),
			zap.Bool("ws_is_paused", wsIsPaused))

		// 尝试状态对齐（以时间管理器状态为准）
		h.alignWebSocketState(session, sessionInfo)

		// 临时不返回错误，只记录警告
		// return fmt.Errorf("状态不一致: %v", inconsistencies)
	}

	return nil
}

// alignWebSocketState 将WebSocket状态与时间管理器状态对齐
func (h *WebSocketHandler) alignWebSocketState(session *WebSocketSession, sessionInfo *SessionInfo) {
	oldTranscribing := session.IsTranscribing()
	oldPaused := session.IsPaused()

	// 以时间管理器状态为准进行对齐
	session.SetTranscribing(sessionInfo.IsTranscribing)
	session.SetPaused(sessionInfo.IsPaused)

	h.logger.Info("状态已对齐",
		zap.Int("user_id", session.UserID),
		zap.Bool("old_transcribing", oldTranscribing),
		zap.Bool("new_transcribing", session.IsTranscribing()),
		zap.Bool("old_paused", oldPaused),
		zap.Bool("new_paused", session.IsPaused()))

	// 通知会话状态更新
	// 临时禁用，避免阻塞
	// session.notifyUpdated()
}

// sendWarningToUser 向用户发送警告消息
func (h *WebSocketHandler) sendWarningToUser(userID int, warningType string, remainingSeconds int, planID string) {
	// 查找用户的活跃会话
	session, exists := h.sessionManager.GetSessionByUserID(userID)
	if !exists {
		h.logger.Warn("未找到用户会话，无法发送警告",
			zap.Int("user_id", userID),
			zap.String("warning_type", warningType))
		return
	}

	// 构建警告消息
	var messageType string
	var message string
	remainingMinutes := remainingSeconds / 60
	planTitle := ""

	switch warningType {
	case "info":
		messageType = "time_warning"
		message = fmt.Sprintf("Time remaining: %d minutes", remainingMinutes)
	case "warning":
		messageType = "time_warning"
		message = "Only 10 minutes left this month."
		if strings.TrimSpace(planID) != "" {
			planTitle = "Only 10 minutes left this month."
			if planID == "YEAR_PRO" || planID == "YEAR_PRO_MINI" {
				planTitle = "Only 10 minutes left in your plan."
			}
			message = planTitle
		}
	case "critical":
		messageType = "critical_warning"
		message = fmt.Sprintf("Urgent warning: Only %d seconds remaining", remainingSeconds)
	default:
		messageType = "time_warning"
		message = fmt.Sprintf("Time remaining: %d seconds", remainingSeconds)
	}

	// 发送警告消息
	warningMsg := map[string]interface{}{
		"type":              messageType,
		"warning_type":      warningType,
		"remaining_seconds": remainingSeconds,
		"remaining_minutes": remainingMinutes,
		"message":           message,
		"timestamp":         time.Now().Unix(),
	}
	if strings.TrimSpace(planID) != "" {
		warningMsg["plan_id"] = planID
		if strings.TrimSpace(planTitle) != "" {
			warningMsg["plan_title"] = planTitle
		}
	}

	if err := session.SendMessage(warningMsg); err != nil {
		h.logger.Error("发送警告消息失败",
			zap.Error(err),
			zap.Int("user_id", userID),
			zap.String("warning_type", warningType))
	} else {
		h.logger.Info("警告消息已发送",
			zap.Int("user_id", userID),
			zap.String("warning_type", warningType),
			zap.Int("remaining_seconds", remainingSeconds))
	}
}

// forceStopUserSession 强制停止用户会话
func (h *WebSocketHandler) forceStopUserSession(userID int, reason string) {
	// 查找用户的活跃会话
	session, exists := h.sessionManager.GetSessionByUserID(userID)
	if !exists {
		h.logger.Warn("未找到用户会话，无法强制停止",
			zap.Int("user_id", userID),
			zap.String("reason", reason))
		return
	}

	h.logger.Info("强制停止用户会话",
		zap.Int("user_id", userID),
		zap.String("reason", reason),
		zap.String("session_uuid", session.SessionUUID))

	// 发送时间耗尽消息
	exhaustedMsg := map[string]interface{}{
		"type":      "time_exhausted",
		"reason":    reason,
		"message":   "You've run out of transcription minutes. Upgrade to continue.",
		"timestamp": time.Now().Unix(),
	}

	if err := session.SendMessage(exhaustedMsg); err != nil {
		h.logger.Error("发送时间耗尽消息失败",
			zap.Error(err),
			zap.Int("user_id", userID))
	}

	// 停止转录
	if err := h.handleStop(session, WebSocketMessage{}); err != nil {
		h.logger.Error("强制停止转录失败",
			zap.Error(err),
			zap.Int("user_id", userID))
	}

	// 发送停止确认消息
	stoppedMsg := map[string]interface{}{
		"type":      "stopped",
		"reason":    "time_exhausted",
		"message":   "转录已停止",
		"forced":    true,
		"timestamp": time.Now().Unix(),
	}

	if err := session.SendMessage(stoppedMsg); err != nil {
		h.logger.Error("发送停止确认消息失败",
			zap.Error(err),
			zap.Int("user_id", userID))
	}
}

// handleTimeManagerOperation 处理时间管理器操作的公共方法
func (h *WebSocketHandler) handleTimeManagerOperation(
	operation string,
	session *WebSocketSession,
	operationFunc func() (map[string]interface{}, error),
	onSuccess func(result map[string]interface{}) error,
) error {
	h.logger.Info("执行时间管理器操作",
		zap.String("operation", operation),
		zap.Int("user_id", session.UserID))

	// 执行时间管理器操作
	result, err := operationFunc()
	if err != nil {
		h.logger.Error("时间管理器操作失败",
			zap.String("operation", operation),
			zap.Error(err),
			zap.Int("user_id", session.UserID))
		return err
	}

	// 检查操作结果
	if success, ok := result["success"].(bool); !ok || !success {
		message, _ := result["message"].(string)
		h.logger.Warn("时间管理器操作未成功",
			zap.String("operation", operation),
			zap.Int("user_id", session.UserID),
			zap.String("message", message))
		return fmt.Errorf("时间管理器操作失败: %s", message)
	}

	// 状态二次确认：验证时间管理器状态与WebSocket状态的一致性
	if err := h.verifyStateConsistency(operation, session); err != nil {
		h.logger.Error("状态一致性验证失败",
			zap.String("operation", operation),
			zap.Error(err),
			zap.Int("user_id", session.UserID))
		// 不返回错误，只记录告警，避免影响正常流程
	}

	h.logger.Debug("状态验证完成，继续执行",
		zap.String("operation", operation),
		zap.Int("user_id", session.UserID))

	// 执行成功回调
	if onSuccess != nil {
		if err := onSuccess(result); err != nil {
			h.logger.Error("时间管理器操作成功回调失败",
				zap.String("operation", operation),
				zap.Error(err),
				zap.Int("user_id", session.UserID))
			return err
		}
	}

	h.logger.Info("时间管理器操作成功",
		zap.String("operation", operation),
		zap.Int("user_id", session.UserID))

	return nil
}

// updateMeetingStatisticsWithDuration 更新会议统计信息的公共方法（仅更新 status/duration/end_time）
func (h *WebSocketHandler) updateMeetingStatistics(meetingID int, session *WebSocketSession, status string, includeEndTime bool, actualDuration *int) {
	var duration int

	if actualDuration != nil {
		// 使用提供的实际转录时长
		duration = *actualDuration
		h.logger.Debug("使用实际转录时长",
			zap.Int("meeting_id", meetingID),
			zap.Int("actual_duration", duration))
	} else {
		// 回退到计算从开始时间到现在的时间（用于兼容性）
		session.mu.RLock()
		startTime := session.StartTime
		session.mu.RUnlock()
		duration = int(time.Since(startTime).Seconds())
		h.logger.Debug("使用计算的会话时长",
			zap.Int("meeting_id", meetingID),
			zap.Int("calculated_duration", duration))
	}

	// 检查duration值是否在合理范围内
	if duration < 0 || duration > 2147483647 { // INT最大值
		session.mu.RLock()
		startTime := session.StartTime
		session.mu.RUnlock()

		h.logger.Error("Duration值超出范围",
			zap.Int("duration", duration),
			zap.Time("start_time", startTime),
			zap.Time("current_time", time.Now()),
			zap.Int64("start_time_unix", startTime.Unix()),
			zap.Int("meeting_id", meetingID),
			zap.Int("user_id", session.UserID),
			zap.Bool("using_actual_duration", actualDuration != nil))

		// 使用安全的默认值
		if duration < 0 {
			duration = 0
		} else {
			duration = 86400 // 24小时作为最大合理值
		}

		h.logger.Warn("使用安全的duration值",
			zap.Int("safe_duration", duration),
			zap.Int("meeting_id", meetingID))
	}

	updates := map[string]interface{}{
		"status": status,
	}

	// 只在会议结束时更新持续时间和结束时间
	if includeEndTime {
		updates["duration_seconds"] = duration
		updates["end_time"] = time.Now().Unix()
	}

	if err := h.meetingRepo.Update(context.Background(), meetingID, updates); err != nil {
		h.logger.Error("更新会议统计失败",
			zap.Error(err),
			zap.Int("meeting_id", meetingID),
			zap.Int("user_id", session.UserID),
			zap.String("status", status))
	} else {
		logFields := []zap.Field{
			zap.Int("meeting_id", meetingID),
			zap.Int("duration_seconds", duration),
			zap.String("status", status),
		}

		if includeEndTime {
			logFields = append(logFields, zap.Int64("end_time", time.Now().Unix()))
		}

		h.logger.Info("会议统计信息已更新", logFields...)
	}
}

func (h *WebSocketHandler) buildMeetingForSession(session *WebSocketSession) *model.Meeting {
	session.mu.RLock()
	defer session.mu.RUnlock()

	meeting := model.NewMeeting(session.UserID, "New Note")
	meeting.SessionUUID = session.SessionUUID
	if len(session.LanguageHints) > 0 {
		meeting.Language = session.LanguageHints[0]
	} else {
		meeting.Language = "en"
	}
	meeting.Status = string(model.StatusActive)
	return meeting
}

func (h *WebSocketHandler) attachMeetingToSession(ctx context.Context, session *WebSocketSession, meetingID int) (int, error) {
	if meetingID <= 0 {
		return 0, nil
	}

	if err := h.createInitialUsageLedgerRecord(ctx, session.UserID, meetingID, session); err != nil {
		return 0, err
	}

	session.mu.Lock()
	session.MeetingID = &meetingID
	session.mu.Unlock()

	if err := h.timeManager.UpdateSessionMeetingID(session.UserID, meetingID); err != nil {
		h.logger.Warn("更新时间管理器中的会议ID失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID),
			zap.Int("meeting_id", meetingID))
	}

	return meetingID, nil
}

func (h *WebSocketHandler) ensureSessionMeetingInitialized(ctx context.Context, session *WebSocketSession) (int, error) {
	session.mu.RLock()
	if session.MeetingID != nil && *session.MeetingID > 0 {
		meetingID := *session.MeetingID
		session.mu.RUnlock()
		return meetingID, nil
	}
	sessionUUID := session.SessionUUID
	session.mu.RUnlock()

	if h.meetingRepo == nil {
		return 0, nil
	}

	if sessionUUID != "" {
		existingMeeting, err := h.meetingRepo.GetByUUID(ctx, sessionUUID)
		if err != nil {
			h.logger.Warn("通过 sessionUUID 查询会议失败",
				zap.Error(err),
				zap.Int("user_id", session.UserID),
				zap.String("session_uuid", sessionUUID))
		} else if existingMeeting != nil && existingMeeting.UserID == session.UserID {
			meetingID, err := h.attachMeetingToSession(ctx, session, existingMeeting.ID)
			if err != nil {
				return 0, err
			}
			h.logger.Info("复用已有会议记录",
				zap.Int("user_id", session.UserID),
				zap.Int("meeting_id", meetingID),
				zap.String("session_uuid", sessionUUID))
			return meetingID, nil
		}
	}

	meeting := h.buildMeetingForSession(session)
	if err := h.meetingRepo.Create(ctx, meeting); err != nil {
		h.logger.Error("创建会议记录失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID),
			zap.String("session_uuid", sessionUUID))
		return 0, fmt.Errorf("创建会议记录失败: %w", err)
	}

	meetingID, err := h.attachMeetingToSession(ctx, session, meeting.ID)
	if err != nil {
		return 0, err
	}
	h.logger.Info("会话预初始化会议记录成功",
		zap.Int("user_id", session.UserID),
		zap.Int("meeting_id", meetingID),
		zap.String("session_uuid", sessionUUID))
	return meetingID, nil
}

// handleFirstTranscription 处理第一条转录，确保会议记录存在
func (h *WebSocketHandler) handleFirstTranscription(session *WebSocketSession) error {
	_, err := h.ensureSessionMeetingInitialized(context.Background(), session)
	return err
}

// createInitialUsageLedgerRecord 创建初始的 UsageLedger 记录
func (h *WebSocketHandler) createInitialUsageLedgerRecord(ctx context.Context, userID int, meetingID int, session *WebSocketSession) error {
	if meetingID <= 0 {
		return nil
	}

	if sessionInfo, exists := h.timeManager.GetSessionInfo(userID); exists && sessionInfo != nil {
		_, _, accumulatedBaseSeconds := sessionInfo.CalculateFinalTimes()
		if accumulatedBaseSeconds <= 0 {
			h.logger.Debug("跳过初始 UsageLedger 记录创建：暂无累积基础秒数",
				zap.Int("user_id", userID),
				zap.Int("meeting_id", meetingID))
			return nil
		}
	}

	h.logger.Info("创建初始 UsageLedger 记录",
		zap.Int("user_id", userID),
		zap.Int("meeting_id", meetingID))

	type initialUsageLedgerCoordinator interface {
		CreateAndMarkInitialUsageLedger(ctx context.Context, userID int, meetingID int) error
	}
	if coordinator, ok := h.timeManager.(initialUsageLedgerCoordinator); ok {
		return coordinator.CreateAndMarkInitialUsageLedger(ctx, userID, meetingID)
	}

	// 获取当前会话的累积时间（而不是使用0秒）
	sessionStats, err := h.timeManager.GetSessionStats(ctx, userID)
	if err != nil {
		h.logger.Warn("获取会话统计失败，使用0秒初始化",
			zap.Error(err),
			zap.Int("user_id", userID))
		// 如果获取失败，使用0秒作为fallback
		err = h.timeManager.CreateInitialUsageLedger(ctx, userID, meetingID, nil)
	} else {
		// 使用实际的累积时间创建初始记录
		currentSessionTime := 0
		if val, ok := sessionStats["current_session_time"].(float64); ok {
			currentSessionTime = int(val)
		}

		h.logger.Info("使用实际累积时间创建初始 UsageLedger 记录",
			zap.Int("user_id", userID),
			zap.Int("meeting_id", meetingID),
			zap.Int("accumulated_seconds", currentSessionTime))

		// 通过 TimeManager 创建初始的 UsageLedger 记录，使用实际累积时间
		err = h.timeManager.CreateInitialUsageLedgerWithTime(ctx, userID, meetingID, currentSessionTime)
	}

	// 通过 TimeManager 创建初始的 UsageLedger 记录
	if err != nil {
		h.logger.Error("通过 TimeManager 创建初始 UsageLedger 记录失败",
			zap.Error(err),
			zap.Int("user_id", userID),
			zap.Int("meeting_id", meetingID))
		return fmt.Errorf("创建初始 UsageLedger 记录失败: %w", err)
	}

	h.logger.Info("初始 UsageLedger 记录创建成功",
		zap.Int("user_id", userID),
		zap.Int("meeting_id", meetingID))

	return nil
}
