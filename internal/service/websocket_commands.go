package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	json "github.com/bytedance/sonic"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	applogger "github.com/chinaxxren/gonotic/internal/pkg/logger"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/repository"
)

var remoteResultPool = sync.Pool{
	New: func() interface{} {
		return &remoteResultPayload{}
	},
}

type remoteToken struct {
	Text              string `json:"text"`
	TranslationStatus string `json:"translation_status"`
	Language          string `json:"language"`
	Speaker           string `json:"speaker"`
	IsFinal           bool   `json:"is_final"`
}

type remoteTimestamp struct {
	Value int64
	Valid bool
}

func (t *remoteTimestamp) UnmarshalJSON(data []byte) error {
	t.Value = 0
	t.Valid = false

	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		return nil
	}

	if raw[0] == '"' {
		unquoted, err := strconv.Unquote(raw)
		if err != nil {
			return err
		}
		raw = strings.TrimSpace(unquoted)
		if raw == "" {
			return nil
		}
	}

	if tsInt, err := strconv.ParseInt(raw, 10, 64); err == nil {
		t.Value = tsInt
		t.Valid = tsInt != 0
		return nil
	}

	tsFloat, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return err
	}
	t.Value = int64(tsFloat)
	t.Valid = t.Value != 0
	return nil
}

type remoteResultPayload struct {
	Tokens            []remoteToken          `json:"tokens"`
	Text              string                 `json:"text"`
	Temp              string                 `json:"temp"`
	IsFinal           bool                   `json:"is_final"`
	Words             int                    `json:"words"`
	TokensCount       int                    `json:"tokens_count"`
	Speaker           string                 `json:"speaker"`
	Language          string                 `json:"language"`
	TranslationStatus string                 `json:"translation_status"`
	Timestamp         remoteTimestamp        `json:"timestamp"`
	Extra             map[string]interface{} `json:"-"`
}

func (p *remoteResultPayload) reset() {
	if len(p.Tokens) > 0 {
		p.Tokens = p.Tokens[:0]
	}
	p.Text = ""
	p.Temp = ""
	p.IsFinal = false
	p.Words = 0
	p.TokensCount = 0
	p.Speaker = ""
	p.Language = ""
	p.TranslationStatus = ""
	p.Timestamp = remoteTimestamp{}
	if p.Extra != nil {
		for k := range p.Extra {
			delete(p.Extra, k)
		}
	}
}

// handleStart 处理 start 命令
func (h *WebSocketHandler) handleStart(session *WebSocketSession, msg WebSocketMessage) error {
	h.logger.Info("处理 start 命令",
		zap.Int("user_id", session.UserID))

	// 更新最后活动时间和命令
	h.sessionManager.UpdateLastActivity(session.UserID)

	// 防止重复 start：如果 timeManager 已处于转录中且 Remote 正在连接/已就绪，拒绝新的 start
	if h.timeManager != nil {
		if existingSession, ok := h.timeManager.GetSessionInfo(session.UserID); ok && existingSession != nil {
			if existingSession.IsTranscribing && (existingSession.RemoteConnecting || existingSession.RemoteReady) {
				h.logger.Warn("拒绝重复 start：已有转录会话正在进行",
					zap.Int("user_id", session.UserID),
					zap.String("existing_session_uuid", existingSession.SessionUUID),
					zap.Bool("remote_connecting", existingSession.RemoteConnecting),
					zap.Bool("remote_ready", existingSession.RemoteReady))
				return session.SendMessage(NewErrorMessage("4009", "Transcription already in progress", "Remote is connecting/ready; reject duplicate start"))
			}
		}
	}

	h.resetSessionForNewStart(session)
	resultGeneration := session.CurrentResultGeneration()

	// 0. 解析客户端偏好设置
	// 现在 msg.Data 已经在 handleTextMessage 中从顶层字段填充了
	prefs := ParseClientPreferences(msg.Data, h.logger)
	ApplyClientPreferences(session, prefs)

	h.logger.Info("应用客户端偏好设置",
		zap.Int("user_id", session.UserID),
		zap.String("audio_format", prefs.AudioFormat),
		zap.Strings("language_hints", prefs.LanguageHints))

	// 调试：确认客户端配置已正确解析
	h.logger.Debug("客户端配置详情",
		zap.String("audio_format", prefs.AudioFormat),
		zap.Strings("language_hints", prefs.LanguageHints),
		zap.Bool("audio_format_empty", prefs.AudioFormat == ""),
		zap.Int("language_hints_count", len(prefs.LanguageHints)))

	// 1. 生成SessionUUID并设置会话标识
	session.mu.Lock()
	if session.SessionUUID == "" {
		session.SessionUUID = uuid.New().String()
		h.logger.Info("生成新的SessionUUID",
			zap.Int("user_id", session.UserID),
			zap.String("session_uuid", session.SessionUUID))
	}
	sessionUUID := session.SessionUUID
	session.mu.Unlock()

	// 使用SessionUUID作为会话标识
	sessionIdentifier := sessionUUID
	h.logger.Info("使用SessionUUID作为会话标识",
		zap.Int("user_id", session.UserID),
		zap.String("session_identifier", sessionIdentifier))

	// 2. 启动时间管理器
	h.logger.Debug("准备调用handleTimeManagerOperation",
		zap.Int("user_id", session.UserID))

	var result map[string]interface{}
	err := h.handleTimeManagerOperation("start", session,
		func() (map[string]interface{}, error) {
			// 这里暂时使用占位符，后续需要集成 MeetingRepository
			meetingID := 0 // 占位符
			// P1 修复: 使用 session.ctx，支持取消
			res, err := h.timeManager.StartTranscription(
				session.ctx,
				session.UserID,
				sessionIdentifier,
				meetingID,
			)
			result = res // 保存结果供后续使用
			return res, err
		},
		func(res map[string]interface{}) error {
			h.logger.Debug("执行onSuccess回调",
				zap.Int("user_id", session.UserID))

			// 2.1. 保存客户端偏好设置到 SessionInfo（与 Python 版本一致）
			session.mu.RLock()
			audioFormat := session.AudioFormat
			languageHints := make([]string, len(session.LanguageHints))
			copy(languageHints, session.LanguageHints)
			session.mu.RUnlock()

			h.logger.Debug("准备更新会话偏好设置",
				zap.Int("user_id", session.UserID),
				zap.String("audio_format", audioFormat),
				zap.Strings("language_hints", languageHints))

			if audioFormat != "" || len(languageHints) > 0 {
				h.timeManager.UpdateSessionPreferences(session.UserID, audioFormat, languageHints)
			}

			h.logger.Debug("onSuccess回调完成",
				zap.Int("user_id", session.UserID))
			return nil
		},
	)

	h.logger.Debug("handleTimeManagerOperation 调用完成",
		zap.Int("user_id", session.UserID))

	h.logger.Debug("handleTimeManagerOperation 返回结果",
		zap.Int("user_id", session.UserID),
		zap.Error(err),
		zap.Any("result", result))

	if err != nil {
		// 检查是否是余额不足的错误
		if result != nil {
			if message, ok := result["message"].(string); ok &&
				(strings.Contains(message, "Insufficient time") || strings.Contains(message, "Insufficient time balance")) {
				h.logger.Warn("时间余额不足",
					zap.Int("user_id", session.UserID),
					zap.String("message", message))

				planID := ""
				if data, ok := result["data"].(map[string]interface{}); ok {
					if value, ok := data["plan_id"].(string); ok {
						planID = value
					}
				}

				// 发送时间耗尽消息
				exhausted := NewTimeExhaustedMessage()
				if planID != "" {
					exhausted["plan_id"] = planID
					title := "No minutes left this month."
					if planID == "YEAR_PRO" || planID == "YEAR_PRO_MINI" {
						title = "No minutes left in your plan."
					}
					exhausted["plan_title"] = title
				}
				if err := session.SendMessage(exhausted); err != nil {
					h.logger.Error("发送时间耗尽消息失败",
						zap.Error(err),
						zap.Int("user_id", session.UserID))
				}

				// 等待一下再关闭连接，确保消息发送完成
				time.Sleep(100 * time.Millisecond)

				// 正常返回，不返回错误（避免主循环发送额外错误消息）
				return nil
			}
		}
		// 其他错误情况
		return session.SendMessage(NewErrorMessage("5001", "Failed to start transcription", err.Error()))
	}

	if _, err := h.ensureSessionMeetingInitialized(session.ctx, session); err != nil {
		h.logger.Warn("start 后预初始化会议失败，首条结果时将重试",
			zap.Error(err),
			zap.Int("user_id", session.UserID),
			zap.String("session_uuid", sessionUUID))
	}

	// 2. 连接 Remote 服务
	session.SetRemoteConnecting(true)

	session.notifyUpdated()

	// P1 修复: 使用 session.ctx，支持取消
	remoteConn, err := h.remoteManager.Connect(
		session.ctx,
		session.UserID,
		sessionIdentifier,
	)
	if err != nil {
		h.logger.Error("连接 Remote 服务失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID))

		session.SetRemoteConnecting(false)

		session.notifyUpdated()

		// 停止时间管理器（清理操作保留 context.Background）
		_, _ = h.timeManager.StopTranscription(
			context.Background(),
			session.UserID,
			nil,
			nil,
		)

		return session.SendMessage(NewServiceUnavailableMessage("Unable to connect to transcription service", "Remote connection failed"))
	}

	// 3. 发送 Remote 配置并等待响应
	// 构建完整的 STT 配置（使用客户端配置和服务器配置）
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

	// 始终提供翻译配置作为默认值，由客户端决定是否使用翻译
	translationConfig := &ServerTranslationConfig{
		Enabled:         true, // 始终启用，让客户端控制
		Type:            h.config.TranslationType,
		TargetLanguages: h.config.TranslationTargetLanguages,
	}

	config := BuildRemoteStartPayload(
		remoteConn.apiKey,
		prefs.AudioFormat,   // 客户端音频格式（优先）
		prefs.LanguageHints, // 客户端语言提示（优先）
		sttConfig,
		audioConfig,
		translationConfig,
	)

	response, err := remoteConn.SendConfigAndWaitResponse(config, 5*time.Second)
	if err != nil {
		h.logger.Error("发送 Remote 配置失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID),
			zap.String("remote_url", h.remoteManager.remoteURL),
			zap.String("session_identifier", sessionIdentifier),
			zap.Any("remote_response", response))

		session.SetRemoteConnecting(false)

		// 关闭 Remote 连接
		_ = remoteConn.Close()

		// 停止时间管理器
		_, _ = h.timeManager.StopTranscription(
			context.Background(),
			session.UserID,
			nil,
			nil,
		)

		return session.SendMessage(NewServiceUnavailableMessage("Remote configuration failed", "Unable to initialize transcription service"))
	}

	h.logger.Info("Remote 配置成功",
		zap.Int("user_id", session.UserID),
		zap.Any("response", response))

	// 4. 启动 Remote 消息接收循环
	go h.receiveFromRemote(session, remoteConn, resultGeneration)

	// 5. 更新会话状态
	session.mu.Lock()
	session.RemoteConn = remoteConn
	session.mu.Unlock()
	session.SetTranscribing(true)
	session.SetPaused(false)
	session.SetRemoteConnecting(false)
	session.SetRemoteReady(true)
	session.notifyUpdated()
	h.scheduleBufferedAudioFlush(session, remoteConn)

	h.logger.Info("转录已启动，等待第一条转录结果",
		zap.Int("user_id", session.UserID),
		zap.String("session_uuid", session.SessionUUID))

	// 注意：不在这里发送 started 消息
	// started 消息将在收到第一条转录结果时发送

	return nil
}

// resetSessionForNewStart 清理旧的会话状态，确保新的 start 从干净环境开始
func (h *WebSocketHandler) resetSessionForNewStart(session *WebSocketSession) {
	session.mu.Lock()
	wasTranscribing := session.IsTranscribing()
	remoteConn := session.RemoteConn
	meetingID := session.MeetingID
	session.SessionUUID = ""
	session.MeetingID = nil
	session.AudioFormat = ""
	session.LanguageHints = nil
	session.clientPrefs = nil
	session.StartTime = time.Now() // 重置为当前时间而不是零值
	session.RemoteConn = nil
	session.pendingAudio = nil
	session.pendingFinalRecords = nil
	session.StatisticsUpdated = false // 重置统计更新标记
	session.mu.Unlock()
	session.SetTranscribing(false)
	session.SetPaused(false)
	session.SetRemoteConnecting(false)
	session.SetRemoteReady(false)
	session.ResetStartedMessageSent()
	session.AdvanceResultGeneration()

	if remoteConn != nil {
		if err := remoteConn.Close(); err != nil {
			h.logger.Warn("关闭旧 Remote 连接失败",
				zap.Error(err),
				zap.Int("user_id", session.UserID))
		}
	}

	if wasTranscribing {
		if _, err := h.timeManager.StopTranscription(
			context.Background(),
			session.UserID,
			meetingID,
			nil,
		); err != nil {
			h.logger.Warn("重置会话时停止转录失败",
				zap.Error(err),
				zap.Int("user_id", session.UserID))
		}
	}

	session.notifyUpdated()
	h.sessionManager.SetConnectionStateByUserID(session.UserID, "connected")
}

// handlePause 处理 pause 命令
func (h *WebSocketHandler) handlePause(session *WebSocketSession, msg WebSocketMessage) error {
	h.logger.Info("处理 pause 命令",
		zap.Int("user_id", session.UserID))

	// 更新最后活动时间和命令
	h.sessionManager.UpdateLastActivity(session.UserID)

	// 检查是否正在转录
	session.mu.RLock()
	isTranscribing := session.IsTranscribing()
	isPaused := session.IsPaused()
	session.mu.RUnlock()
	if !isTranscribing || isPaused {
		return session.SendMessage(NewErrorMessage("4002", "Not currently transcribing", ""))
	}

	// 暂停时间管理器
	// P1 修复: 使用 session.ctx，支持取消
	err := h.handleTimeManagerOperation("pause", session,
		func() (map[string]interface{}, error) {
			return h.timeManager.PauseTranscription(session.ctx, session.UserID)
		},
		nil, // 暂停操作不需要特殊的成功回调
	)
	if err != nil {
		return session.SendMessage(NewErrorMessage("5003", "Failed to pause transcription", err.Error()))
	}

	// 获取 Remote 统计信息
	if session.RemoteConn != nil {
		stats := session.RemoteConn.GetStats()
		h.logger.Info("Remote 流统计（暂停时）",
			zap.Int("user_id", session.UserID),
			zap.Int64("chunk_count", stats.ChunkCount),
			zap.Int64("byte_count", stats.ByteCount))

		// 关闭 Remote 连接
		_ = session.RemoteConn.Close()
	}

	// 更新会话状态
	session.SetPaused(true)
	session.SetTranscribing(false)
	session.SetRemoteConnecting(false)
	session.SetRemoteReady(false)

	session.mu.Lock()
	meetingID := session.MeetingID
	session.mu.Unlock()

	session.notifyUpdated()

	// 更新连接状态（与 Python 版本一致）
	h.sessionManager.SetConnectionStateByUserID(session.UserID, "paused")

	// 更新会议状态到数据库（如果有 meeting_id）
	if meetingID != nil && h.meetingRepo != nil {
		// 暂停时不更新 end_time，但也不设置 StatisticsUpdated
		// 让结束时的更新能够正常执行
		h.updateMeetingStatistics(*meetingID, session, "paused", false, nil)
	}

	h.logger.Info("转录已暂停",
		zap.Int("user_id", session.UserID))

	// 暂停时断开客户端 WebSocket 连接（与用户要求一致）
	// 关闭客户端连接，让客户端重新连接以恢复
	if err := session.Close(); err != nil {
		h.logger.Warn("关闭客户端 WebSocket 连接失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID))
	} else {
		h.logger.Info("已断开客户端 WebSocket 连接（暂停）",
			zap.Int("user_id", session.UserID))
	}

	// 不发送 paused 确认消息（不需要，因为连接已断开）
	return nil
}

// handleResume 处理 resume 命令
// Resume消息只支持简单格式：{"type": "resume"}
// 不支持在resume消息中携带audio_format、language_hints等配置参数
// 配置参数只能通过start消息设置，resume时使用缓存的偏好设置
func (h *WebSocketHandler) handleResume(session *WebSocketSession, msg WebSocketMessage) error {
	h.logger.Info("处理 resume 命令",
		zap.Int("user_id", session.UserID))

	// 更新最后活动时间
	h.sessionManager.UpdateLastActivity(session.UserID)

	// Resume消息只支持 {"type": "resume"} 格式，不解析额外配置
	// 直接使用会话缓存的客户端偏好设置（如果不存在，后续会从 timeManager 或 server 默认值回退）
	prefs := GetSessionClientPreferences(session)

	// 1. 检查时间管理器中是否有可恢复的会话（优化重连逻辑）
	session.mu.RLock()
	meetingID := session.MeetingID
	sessionUUID := session.SessionUUID
	session.mu.RUnlock()

	var existingSession *SessionInfo
	var canResumeSession bool
	if sessionInfo, exists := h.timeManager.GetSessionInfo(session.UserID); exists {
		existingSession = sessionInfo

		// 优化：支持恢复暂停会话或断线前的转录会话
		canResumeSession = existingSession.IsPaused ||
			(existingSession.IsTranscribing && existingSession.SessionUUID != "")

		if canResumeSession {
			// 使用单一数据源同步所有ID
			session.SyncFromTimeManager()
			sessionUUID = session.SessionUUID
			meetingID = session.MeetingID

			if existingSession.IsPaused {
				h.logger.Info("从timeManager恢复暂停会话 (单一数据源)",
					zap.Int("user_id", session.UserID),
					zap.String("session_uuid", sessionUUID),
					zap.Int("meeting_id", existingSession.MeetingID),
					zap.Int("user_id", session.UserID))
			} else {
				h.logger.Info("从timeManager恢复断线会话 (单一数据源)",
					zap.Int("user_id", session.UserID),
					zap.String("session_uuid", sessionUUID),
					zap.Int("meeting_id", existingSession.MeetingID),
					zap.Bool("was_transcribing", existingSession.IsTranscribing),
					zap.Int("user_id", session.UserID))
			}
		} else {
			// 会话存在但无法恢复：对于resume消息，仍尝试使用现有SessionUUID
			if existingSession.SessionUUID != "" {
				// 使用单一数据源同步ID，保持连续性
				session.SyncFromTimeManager()
				sessionUUID = session.SessionUUID
				meetingID = session.MeetingID

				h.logger.Info("resume消息保持现有ID（即使无法恢复状态，单一数据源）",
					zap.Int("user_id", session.UserID),
					zap.String("session_uuid", sessionUUID),
					zap.Int("meeting_id", existingSession.MeetingID),
					zap.String("existing_status", existingSession.Status),
					zap.Int("user_id", session.UserID))
			} else {
				// 只有在完全没有SessionUUID时才生成新的；否则保留当前会话UUID避免创建新会话导致无法继续转录
				if sessionUUID == "" {
					h.logger.Warn("无法恢复会话：timeManager有会话但当前会话无UUID",
						zap.Int("user_id", session.UserID),
						zap.String("existing_session_uuid", existingSession.SessionUUID))
					return session.SendMessage(NewErrorMessage("4008", "No session to resume", "Session data inconsistent. Please start a new transcription session."))
				} else {
					h.logger.Info("resume 保持当前会话UUID",
						zap.Int("user_id", session.UserID),
						zap.String("session_uuid", sessionUUID))
				}
			}
		}
	} else {
		// 时间管理器中没有会话：检查是否有可恢复的会话数据
		if sessionUUID == "" {
			h.logger.Warn("无法恢复会话：没有可恢复的会话数据",
				zap.Int("user_id", session.UserID))
			return session.SendMessage(NewErrorMessage("4008", "No session to resume", "No active session found. Please start a new transcription session."))
		} else {
			h.logger.Info("timeManager无会话，但当前会话有UUID，尝试恢复",
				zap.Int("user_id", session.UserID),
				zap.String("session_uuid", sessionUUID))
		}
	}

	// 兜底：resume 过程中必须保证 prefs 非空，否则会导致 Remote 配置阶段失败（无法继续转录）
	if prefs == nil {
		if existingSession != nil && (existingSession.AudioFormat != "" || len(existingSession.LanguageHints) > 0) {
			prefs = &ClientPreferences{
				AudioFormat:   existingSession.AudioFormat,
				LanguageHints: append([]string(nil), existingSession.LanguageHints...),
			}
			ApplyClientPreferences(session, prefs)
			h.logger.Info("resume 使用 timeManager 缓存的客户端偏好设置",
				zap.Int("user_id", session.UserID),
				zap.String("audio_format", prefs.AudioFormat),
				zap.Strings("language_hints", prefs.LanguageHints))
		} else {
			prefs = &ClientPreferences{
				AudioFormat:   h.config.STTAudioFormat,
				LanguageHints: append([]string(nil), h.config.STTLanguageHints...),
			}
			ApplyClientPreferences(session, prefs)
			h.logger.Warn("resume 无缓存偏好设置，回退到服务器默认配置",
				zap.Int("user_id", session.UserID),
				zap.String("audio_format", prefs.AudioFormat),
				zap.Strings("language_hints", prefs.LanguageHints))
		}
	} else {
		h.logger.Info("resume 使用会话缓存的客户端偏好设置",
			zap.Int("user_id", session.UserID),
			zap.String("audio_format", prefs.AudioFormat),
			zap.Strings("language_hints", prefs.LanguageHints))
	}

	// 2. 通过 session_uuid 查找会议记录（仅在没有MeetingID时查找）
	// 优先保持从timeManager恢复的MeetingID，避免被数据库查找结果覆盖
	if meetingID == nil && sessionUUID != "" && h.meetingRepo != nil {
		meeting, err := h.meetingRepo.GetByUUID(context.Background(), sessionUUID)
		if err == nil && meeting != nil {
			// 检查权限（确保是用户的会议）
			if meeting.UserID == session.UserID {
				session.mu.Lock()
				session.MeetingID = &meeting.ID
				meetingID = &meeting.ID
				session.mu.Unlock()
				h.logger.Info("通过 session_uuid 找到现有会议记录",
					zap.Int("user_id", session.UserID),
					zap.String("session_uuid", sessionUUID),
					zap.Int("meeting_id", meeting.ID))
			} else {
				h.logger.Warn("通过 session_uuid 找到的会议记录不属于当前用户",
					zap.Int("user_id", session.UserID),
					zap.String("session_uuid", sessionUUID),
					zap.Int("meeting_user_id", meeting.UserID))
			}
		} else if err != nil {
			h.logger.Debug("通过 session_uuid 未找到会议记录",
				zap.String("session_uuid", sessionUUID),
				zap.Error(err))
		}
	} else if meetingID != nil {
		h.logger.Info("保持从timeManager恢复的MeetingID",
			zap.Int("user_id", session.UserID),
			zap.String("session_uuid", sessionUUID),
			zap.Int("meeting_id", *meetingID))
	}

	// 如果通过 session_uuid 找不到，尝试通过 meeting_id 恢复
	if meetingID == nil && session.MeetingID != nil {
		meeting, err := h.meetingRepo.GetByID(context.Background(), *session.MeetingID)
		if err != nil {
			h.logger.Warn("无法恢复会议记录",
				zap.Error(err),
				zap.Int("meeting_id", *session.MeetingID),
				zap.Int("user_id", session.UserID))
		} else if meeting != nil {
			h.logger.Info("会议记录已恢复（通过 meeting_id）",
				zap.Int("meeting_id", meeting.ID),
				zap.String("title", meeting.Title),
				zap.String("language", meeting.Language),
				zap.Int("duration", meeting.Duration))
		}
	}

	// 3. 恢复或开始时间管理器（与 Python 版本一致）
	var result map[string]interface{}
	var err error

	if canResumeSession && existingSession != nil {
		// 恢复现有会话（不区分暂停还是断线重连）
		h.logger.Info("恢复现有会话，继续之前的会议",
			zap.Int("user_id", session.UserID),
			zap.String("session_uuid", sessionUUID),
			zap.Bool("was_paused", existingSession.IsPaused),
			zap.Bool("was_transcribing", existingSession.IsTranscribing))

		// 统一使用 ResumeTranscription，如果会话不是暂停状态，
		// TimeManager 内部会处理状态转换
		// P1 修复: 使用 session.ctx，支持取消
		result, err = h.timeManager.ResumeTranscription(
			session.ctx,
			session.UserID,
		)
	} else {
		// 无法恢复会话，降级为start逻辑
		h.logger.Info("无法恢复会话，降级为start逻辑",
			zap.Int("user_id", session.UserID),
			zap.String("session_uuid", sessionUUID))

		// 确保有会议ID
		if meetingID == nil {
			// 创建新会议记录（降级逻辑）
			h.logger.Info("创建新会议记录（resume降级）",
				zap.Int("user_id", session.UserID))
			h.logger.Info("resume 降级为 start（调用 handleStart）",
				zap.Int("user_id", session.UserID),
				zap.String("session_uuid", sessionUUID))
			return h.handleStart(session, msg)
		}

		// P1 修复: 使用 session.ctx，支持取消
		result, err = h.timeManager.StartTranscription(
			session.ctx,
			session.UserID,
			sessionUUID,
			*meetingID,
		)
	}

	if err != nil {
		h.logger.Error("恢复/开始时间管理器失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID))
		return session.SendMessage(NewErrorMessage("5004", "Failed to resume transcription", err.Error()))
	}

	// 检查结果
	if success, ok := result["success"].(bool); !ok || !success {
		message, _ := result["message"].(string)
		return session.SendMessage(NewErrorMessage("5004", "Failed to resume transcription", message))
	}

	// 3. 重新连接 Remote 服务
	session.SetRemoteConnecting(true)

	// P1 修复: 使用 session.ctx，支持取消
	remoteConn, err := h.remoteManager.Connect(
		session.ctx,
		session.UserID,
		sessionUUID,
	)
	if err != nil {
		h.logger.Error("重新连接 Remote 服务失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID))

		session.SetRemoteConnecting(false)

		// 清理时间管理状态（与 Python 版本一致）
		_, _ = h.timeManager.StopTranscription(
			context.Background(),
			session.UserID,
			meetingID,
			nil,
		)
		h.logger.Info("已清理时间管理状态（恢复失败）",
			zap.Int("user_id", session.UserID))

		// 发送 stop_and_clear 错误（与 Python 版本一致）
		return session.SendMessage(NewStopAndClearMessage("Failed to reconnect to transcription service. Session state is inconsistent."))
	}

	// 5. 发送 Remote 配置并等待响应（与 Python 版本一致）
	// 构建完整的 STT 配置（使用客户端配置和服务器配置）
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

	// 始终提供翻译配置作为默认值，由客户端决定是否使用翻译
	translationConfig := &ServerTranslationConfig{
		Enabled:         true, // 始终启用，让客户端控制
		Type:            h.config.TranslationType,
		TargetLanguages: h.config.TranslationTargetLanguages,
	}

	config := BuildRemoteStartPayload(
		remoteConn.apiKey,
		prefs.AudioFormat,   // 客户端音频格式（优先）
		prefs.LanguageHints, // 客户端语言提示（优先）
		sttConfig,
		audioConfig,
		translationConfig,
	)

	response, err := remoteConn.SendConfigAndWaitResponse(config, 5*time.Second)
	if err != nil {
		h.logger.Error("发送 Remote 配置失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID))

		session.SetRemoteConnecting(false)

		// 关闭 Remote 连接
		_ = remoteConn.Close()

		// 清理时间管理状态（与 Python 版本一致）
		_, _ = h.timeManager.StopTranscription(
			context.Background(),
			session.UserID,
			meetingID,
			nil,
		)
		h.logger.Info("已清理时间管理状态（配置失败）",
			zap.Int("user_id", session.UserID))

		// 发送 stop_and_clear 错误（与 Python 版本一致）
		return session.SendMessage(NewStopAndClearMessage("Remote configuration failed during resume"))
	}

	h.logger.Info("Remote 配置成功（resume）",
		zap.Int("user_id", session.UserID),
		zap.Any("response", response))

	resultGeneration := session.AdvanceResultGeneration()

	// 6. 启动 Remote 消息接收循环
	go h.receiveFromRemote(session, remoteConn, resultGeneration)

	// 7. 更新会话状态
	session.mu.Lock()
	session.RemoteConn = remoteConn
	meetingID = session.MeetingID
	session.mu.Unlock()
	session.SetTranscribing(true)
	session.SetPaused(false)
	session.SetRemoteConnecting(false)
	session.SetRemoteReady(true)

	h.sessionManager.SetConnectionStateByUserID(session.UserID, "connected")
	h.scheduleBufferedAudioFlush(session, remoteConn)

	// 8. 更新会议状态为 active
	if meetingID != nil && h.meetingRepo != nil {
		// 重置统计更新标记，让结束时的更新能够正常执行
		session.mu.Lock()
		session.StatisticsUpdated = false
		session.mu.Unlock()

		// 使用统一的会议统计更新方法，确保状态一致性
		h.updateMeetingStatistics(*meetingID, session, "active", false, nil)
	}

	// 记录恢复成功的详细信息
	resumeType := "unknown"
	if canResumeSession && existingSession != nil {
		if existingSession.IsPaused {
			resumeType = "paused_session"
		} else if existingSession.IsTranscribing {
			resumeType = "disconnected_session"
		}
	} else {
		resumeType = "fallback_to_start"
	}

	h.logger.Info("WebSocket重连恢复成功",
		zap.Int("user_id", session.UserID),
		zap.String("session_uuid", sessionUUID),
		zap.String("resume_type", resumeType),
		zap.Int("meeting_id", func() int {
			if meetingID != nil {
				return *meetingID
			}
			return 0
		}()),
		zap.Bool("has_remote_conn", session.RemoteConn != nil))

	// 不发送 resumed 确认消息（不需要）
	// 与 Python 版本一致：首条转录到达时再发送 started
	return nil
}

// handleStop 处理 stop 命令
func (h *WebSocketHandler) handleStop(session *WebSocketSession, msg WebSocketMessage) error {
	// 更新停止状态
	if msg.Type == "stop" {
		session.SetStopped(true)
	}
	h.logger.Info("处理 stop 命令",
		zap.Int("user_id", session.UserID))

	// 更新最后活动时间
	h.sessionManager.UpdateLastActivity(session.UserID)

	// 停止时间管理器
	var result map[string]interface{}
	err := h.handleTimeManagerOperation("stop", session,
		func() (map[string]interface{}, error) {
			res, err := h.timeManager.StopTranscription(
				context.Background(),
				session.UserID,
				session.MeetingID,
				nil,
			)
			result = res // 保存结果供后续使用
			return res, err
		},
		nil, // 成功回调在后面单独处理
	)
	if err != nil {
		return session.SendMessage(NewErrorMessage("5004", "Failed to stop transcription", err.Error()))
	}

	// 获取剩余时间
	var remainingSeconds *int
	if data, ok := result["data"].(map[string]interface{}); ok {
		if remaining, ok := data["remaining_time"].(int); ok {
			remainingSeconds = &remaining
		}
	}

	// 记录 Remote 流统计并关闭连接
	if session.RemoteConn != nil {
		// 记录统计信息
		stats := session.RemoteConn.GetStats()
		h.logger.Info("Remote 流统计",
			zap.Int("user_id", session.UserID),
			zap.String("session_uuid", session.SessionUUID),
			zap.Int64("chunk_count", stats.ChunkCount),
			zap.Int64("byte_count", stats.ByteCount),
			zap.Time("last_audio_time", stats.LastAudioTime),
			zap.Duration("duration", time.Since(stats.StartTime)))

		_ = session.RemoteConn.Close()
	}

	// 更新会话状态
	session.SetTranscribing(false)
	session.SetPaused(false)
	session.SetRemoteConnecting(false)
	session.SetRemoteReady(false)

	// 更新连接状态（与 Python 版本一致）
	h.sessionManager.SetConnectionStateByUserID(session.UserID, "disconnecting")

	// stop 前主动补刷一轮待持久化的 final 结果，降低队列抖动时的历史缺失窗口。
	if err := h.flushPendingTranscriptionRecordsForSession(session); err != nil {
		h.logger.Error("stop 前补刷待持久化 final 结果失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID),
			zap.String("session_uuid", session.SessionUUID))
	}

	// 更新会议状态和时长（与 Python 版本一致）
	if session.MeetingID != nil && h.meetingRepo != nil && session.StartedMessageSent() {
		// 用户点击 stop 时，强制更新状态/时长（不论之前是否已更新）
		session.mu.Lock()
		session.StatisticsUpdated = true
		session.mu.Unlock()

		// 检查时间管理器是否返回了实际转录时长
		var actualDuration *int
		if data, ok := result["data"].(map[string]interface{}); ok {
			if totalDuration, ok := data["total_duration"].(int); ok {
				actualDuration = &totalDuration
			}
		}

		// 使用实际转录时长更新会议状态/时长
		h.updateMeetingStatistics(*session.MeetingID, session, string(model.StatusCompleted), true, actualDuration)

		h.logger.Info("用户主动停止，强制更新会议状态",
			zap.Int("user_id", session.UserID),
			zap.Int("meeting_id", *session.MeetingID),
			zap.Bool("was_previously_updated", session.StatisticsUpdated),
			zap.Bool("has_actual_duration", actualDuration != nil))
	} else if session.MeetingID != nil && h.meetingRepo != nil {
		h.logger.Debug("跳过空会议状态更新，等待 cleanupEmptyMeeting 清理",
			zap.Int("user_id", session.UserID),
			zap.Int("meeting_id", *session.MeetingID))
	}

	h.logger.Info("转录已停止",
		zap.Int("user_id", session.UserID))

	// 刷新用户缓存（确保下次获取最新余额）
	// 注意：时间管理器在 StopTranscription 中已经清除了缓存
	// 这里不需要额外操作

	// 检查并清理空会议
	if err := h.cleanupEmptyMeeting(session); err != nil {
		h.logger.Warn("清理空会议失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID))
	}

	// 发送 stopped 消息
	stoppedMsg := NewStoppedMessage(session.SessionUUID, session.MeetingID, "user_stop", remainingSeconds)
	if err := session.SendMessage(stoppedMsg); err != nil {
		h.logger.Warn("发送 stopped 消息失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID))
	}

	// 等待客户端主动断开连接（最多3秒）
	// 这样可以确保客户端收到 stopped 消息；若客户端未断开，服务端再主动关闭客户端 WebSocket。
	h.logger.Debug("等待客户端断开连接",
		zap.Int("user_id", session.UserID))

	select {
	case <-session.ClientDisconnected():
		h.logger.Debug("客户端已断开连接",
			zap.Int("user_id", session.UserID))
	case <-time.After(3 * time.Second):
		// 客户端未按预期断开，服务端主动关闭客户端 WebSocket。
		if err := session.Conn.Close(); err != nil {
			h.logger.Debug("关闭连接时连接已断开（正常）",
				zap.Int("user_id", session.UserID),
				zap.Error(err))
		}
	}

	return nil
}

// cleanupEmptyMeeting 清理空会议（与 Python 版本的 _cleanup_empty_meeting_record 一致）
func (h *WebSocketHandler) cleanupEmptyMeeting(session *WebSocketSession) error {
	// 获取会议记录ID（与 Python 版本一致）
	var meetingID *int
	session.mu.RLock()
	meetingID = session.MeetingID
	sessionUUID := session.SessionUUID
	session.mu.RUnlock()

	// 如果没有会议记录ID，尝试通过 sessionUUID 查找（与 Python 版本一致）
	if meetingID == nil && sessionUUID != "" && h.meetingRepo != nil {
		if existingMeeting, err := h.meetingRepo.GetByUUID(context.Background(), sessionUUID); err == nil && existingMeeting != nil {
			// 检查权限（确保是用户的会议）
			if existingMeeting.UserID == session.UserID {
				meetingID = &existingMeeting.ID
				h.logger.Debug("通过 sessionUUID 找到会议记录",
					zap.Int("meeting_id", existingMeeting.ID),
					zap.String("session_uuid", sessionUUID))
			}
		}
	}

	if meetingID == nil {
		h.logger.Debug("未找到会议记录，跳过清理",
			zap.Int("user_id", session.UserID),
			zap.String("session_uuid", sessionUUID))
		return nil
	}

	if h.meetingRepo != nil {
		meeting, err := h.meetingRepo.GetByID(context.Background(), *meetingID)
		if err != nil {
			return err
		}
		if meeting == nil {
			return nil
		}
		if meeting.Duration > 0 || (meeting.Status != "" && meeting.Status != "active" && meeting.Status != "in_progress") {
			return nil
		}
	}

	// 所有检查都通过，删除空会议记录（与 Python 版本一致）
	h.logger.Info("检测到空会议，准备清理",
		zap.Int("user_id", session.UserID),
		zap.String("session_uuid", sessionUUID),
		zap.Int("meeting_id", *meetingID))

	if h.meetingRepo != nil {
		if err := h.meetingRepo.Delete(context.Background(), *meetingID); err != nil {
			h.logger.Error("删除空会议失败",
				zap.Error(err),
				zap.Int("meeting_id", *meetingID))
			return err
		}

		h.logger.Info("空会议已删除",
			zap.Int("meeting_id", *meetingID),
			zap.Int("user_id", session.UserID),
			zap.String("session_uuid", sessionUUID))
	}

	return nil
}

// receiveFromRemote 从 Remote 接收转录结果
func (h *WebSocketHandler) receiveFromRemote(session *WebSocketSession, remoteConn *RemoteConnection, resultGeneration uint64) {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Error("Remote 接收循环 panic",
				zap.Any("panic", r),
				zap.Int("user_id", session.UserID))
		}
	}()

	isFirstTranscription := true

	for {
		select {
		case <-session.ctx.Done():
			return
		default:
			messageType, message, err := remoteConn.ReadMessage()
			if err != nil {
				// 只有 Remote/Soniox 的异常才需要重连。
				// 客户端/本地导致会话结束，或 Remote 正常 close，都不重连。
				expectedClose := false
				if session.ctx.Err() != nil || session.IsStopped() {
					expectedClose = true
				} else if errors.Is(err, net.ErrClosed) {
					expectedClose = true
				} else if websocket.IsCloseError(err,
					websocket.CloseNormalClosure,
					websocket.CloseGoingAway,
					websocket.CloseNoStatusReceived,
				) {
					expectedClose = true
				}

				session.mu.RLock()
				sessionUUID := session.SessionUUID
				session.mu.RUnlock()
				if expectedClose {
					h.logger.Warn("Remote 读取循环结束（连接已关闭）",
						zap.Error(err),
						zap.Int("user_id", session.UserID),
						zap.String("session_uuid", sessionUUID))
				} else {
					h.logger.Error("从 Remote 读取消息失败",
						zap.Error(err),
						zap.Int("user_id", session.UserID),
						zap.String("session_uuid", sessionUUID))
				}

				if !expectedClose {
					// 非预期断开：尝试自动重连，成功则继续同一 session/meeting，不中断 timeManager。
					// 若已有其他 goroutine 在重连，当前循环直接退出，避免误 Stop/误通知客户端。
					if !session.BeginRemoteReconnect() {
						return
					}
					reconnectErr := h.reconnectRemote(session)
					session.EndRemoteReconnect()
					if reconnectErr == nil {
						_ = remoteConn.Close()
						return
					}
					h.logger.Error("Remote 自动重连失败",
						zap.Error(reconnectErr),
						zap.Int("user_id", session.UserID))
				}

				session.mu.Lock()
				if session.RemoteConn == remoteConn {
					session.RemoteConn = nil
				}
				session.mu.Unlock()

				session.SetRemoteReady(false)
				session.SetRemoteConnecting(false)
				session.SetTranscribing(false)
				if !expectedClose {
					if h.timeManager != nil {
						_, _ = h.timeManager.StopTranscription(
							context.Background(),
							session.UserID,
							nil,
							nil,
						)
					}
					_ = session.SendMessage(NewServiceUnavailableMessage("Transcription service unavailable", "Remote disconnected"))
				}
				_ = remoteConn.Close()
				return
			}

			if messageType == 1 { // TextMessage
				receiveTime := time.Now().UnixMilli()

				// 直接打印 JSON（不进行修改优化，不使用转义）
				// h.logger.Info("收到 Remote 的转录结果",
				// 	zap.Int("user_id", session.UserID),
				// 	zap.ByteString("json", message))

				payload := remoteResultPool.Get().(*remoteResultPayload)
				payload.reset()
				if err := json.Unmarshal(message, payload); err != nil {
					h.logger.Error("解析转录结果失败",
						zap.Error(err),
						zap.Int("user_id", session.UserID))
					remoteResultPool.Put(payload)
					continue
				}

				var text, temp string
				var speaker, language, translationStatus string
				isFinal := payload.IsFinal

				if len(payload.Tokens) > 0 {
					var textBuilder, tempBuilder strings.Builder
					isTranslation := false
					speakerCandidates := make([]string, 0, 4)

					for _, token := range payload.Tokens {
						if token.Text == "" || token.Text == "<end>" {
							continue
						}
						if token.IsFinal {
							textBuilder.WriteString(token.Text)
						} else {
							tempBuilder.WriteString(token.Text)
						}
						if token.TranslationStatus == "translation" {
							isTranslation = true
						}
						if language == "" && token.Language != "" {
							language = token.Language
						}
						if token.Speaker != "" {
							speakerCandidates = append(speakerCandidates, token.Speaker)
						}
					}

					if textBuilder.Len() > 0 {
						text = strings.TrimSpace(strings.Join(strings.Fields(textBuilder.String()), " "))
						isFinal = true
					}
					if tempBuilder.Len() > 0 {
						temp = strings.TrimSpace(strings.Join(strings.Fields(tempBuilder.String()), " "))
					}

					if text == "" && temp == "" {
						remoteResultPool.Put(payload)
						continue
					}

					if isTranslation {
						translationStatus = "translation"
					} else if payload.TranslationStatus != "" {
						translationStatus = payload.TranslationStatus
					} else {
						translationStatus = "original"
					}

					if language == "" {
						if translationStatus == "translation" {
							language = "zh"
						} else {
							language = "en"
						}
					}

					if len(speakerCandidates) > 0 {
						speaker = speakerCandidates[0]
					}
					speaker = strings.TrimSpace(speaker)
					if speaker != "" && !strings.HasPrefix(speaker, "Speaker ") {
						speaker = "Speaker " + speaker
					}

					// tokens 计数已不再使用
				} else {
					// 兼容旧格式
					text = payload.Text
					temp = payload.Temp

					// 如果 text 和 temp 都为空，跳过处理
					if text == "" && temp == "" {
						remoteResultPool.Put(payload)
						continue
					}

					isFinal = payload.IsFinal
					// tokens 计数已不再使用
					speaker = strings.TrimSpace(payload.Speaker)
					if speaker != "" && !strings.HasPrefix(speaker, "Speaker ") {
						speaker = "Speaker " + speaker
					}
					language = payload.Language
					translationStatus = payload.TranslationStatus
				}

				// 提取 timestamp（从 Soniox 结果中，如果没有则使用当前时间）
				timestamp := time.Now().Unix()
				if payload.Timestamp.Valid {
					timestamp = payload.Timestamp.Value
				}

				if isFirstTranscription && isFinal && text != "" {
					isFirstTranscription = false
				}

				result := &queuedTranscriptionResult{
					Text:              text,
					Temp:              temp,
					Speaker:           speaker,
					Language:          language,
					TranslationStatus: translationStatus,
					Timestamp:         timestamp,
					ReceiveTime:       receiveTime,
					IsFinal:           isFinal,
					Generation:        resultGeneration,
				}

				remoteResultPool.Put(payload)

				if !session.EnqueueResult(result) {
					if result.IsFinal {
						h.logger.Error("最终转录结果缓冲已满，停止会话避免静默丢失",
							zap.Int("user_id", session.UserID),
							zap.String("session_uuid", session.SessionUUID))
						_ = session.SendMessage(NewStopAndClearMessage("Transcription pipeline overloaded"))
						_ = session.Close()
						return
					}
				}
			}
		}
	}
}

func (h *WebSocketHandler) processRemoteResults(session *WebSocketSession) {
	for {
		for _, result := range session.DrainOverflowResults() {
			if !h.processQueuedRemoteResult(session, result) {
				return
			}
		}

		select {
		case <-session.ctx.Done():
			return
		case <-session.ResultWake():
			continue
		case result := <-session.ResultQueue():
			if !h.processQueuedRemoteResult(session, result) {
				return
			}
		}
	}
}

func (h *WebSocketHandler) processQueuedRemoteResult(session *WebSocketSession, result *queuedTranscriptionResult) bool {
	if result == nil {
		return true
	}
	if result.Generation != session.CurrentResultGeneration() {
		return true
	}

	if result.IsFinal && result.Text != "" {
		meetingID, err := h.ensureMeetingForTranscription(session)
		if err != nil {
			h.logger.Error("初始化会议记录失败，停止转录会话",
				zap.Error(err),
				zap.Int("user_id", session.UserID),
				zap.String("session_uuid", session.SessionUUID))
			_ = session.SendMessage(NewStopAndClearMessage("Failed to initialize meeting for transcription"))
			_ = session.Close()
			return false
		}

		if meetingID > 0 && session.MarkStartedMessageSent() {
			meetingIDCopy := meetingID
			if err := session.SendMessage(NewStartedMessage(session.SessionUUID, &meetingIDCopy, 0)); err != nil {
				h.logger.Error("发送 started 消息失败",
					zap.Error(err),
					zap.Int("user_id", session.UserID),
					zap.String("session_uuid", session.SessionUUID))
			}
		}
	}

	transcriptionMsg := NewFrontendTranscriptionMessage(
		result.Text,
		result.Temp,
		result.Speaker,
		result.Timestamp,
		result.Language,
		result.TranslationStatus,
	)
	transcriptionMsg.ReceiveTime = result.ReceiveTime

	if err := session.SendMessage(transcriptionMsg); err != nil {
		h.logger.Error("发送转录结果失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID))
	}

	if result.IsFinal && result.Text != "" {
		if err := h.persistQueuedTranscription(session, result); err != nil {
			if errors.Is(err, ErrPendingFinalRecordsFull) {
				h.logger.Error("最终转录结果待持久化缓冲已满，停止会话避免静默丢失",
					zap.Error(err),
					zap.Int("user_id", session.UserID),
					zap.String("session_uuid", session.SessionUUID))
				_ = session.SendMessage(NewStopAndClearMessage("Transcription persistence backlog overflow"))
				_ = session.Close()
				return false
			}
			h.logger.Error("保存转录结果失败",
				zap.Error(err),
				zap.Int("user_id", session.UserID),
				zap.String("session_uuid", session.SessionUUID))
		}
	}

	return true
}

func (h *WebSocketHandler) ensureMeetingForTranscription(session *WebSocketSession) (int, error) {
	session.mu.RLock()
	if session.MeetingID != nil {
		meetingID := *session.MeetingID
		session.mu.RUnlock()
		return meetingID, nil
	}
	session.mu.RUnlock()

	if h.meetingRepo == nil {
		return 0, nil
	}

	meetingID, err := h.ensureSessionMeetingInitialized(context.Background(), session)
	if err != nil {
		return 0, err
	}
	if meetingID <= 0 {
		return 0, fmt.Errorf("meeting id is still unavailable")
	}
	return meetingID, nil
}

func (h *WebSocketHandler) persistQueuedTranscription(session *WebSocketSession, result *queuedTranscriptionResult) error {
	if h.storage == nil || result == nil {
		return nil
	}

	session.mu.RLock()
	meetingID := 0
	if session.MeetingID != nil {
		meetingID = *session.MeetingID
	}
	session.mu.RUnlock()

	record := &TranscriptionRecord{
		Text:              result.Text,
		Speaker:           result.Speaker,
		Timestamp:         result.Timestamp,
		Language:          result.Language,
		TranslationStatus: result.TranslationStatus,
		UID:               session.UserID,
		MeetingID:         meetingID,
	}

	if meetingID == 0 {
		if h.meetingRepo == nil {
			return nil
		}
		return session.BufferPendingFinalRecord(record)
	}

	if err := h.flushPendingTranscriptionRecords(session, meetingID); err != nil {
		if bufferErr := session.BufferPendingFinalRecord(record); bufferErr != nil {
			return bufferErr
		}
		return err
	}
	if err := h.persistTranscriptionRecord(record); err != nil {
		if errors.Is(err, repository.ErrTranscriptionAsyncQueueFull) {
			if bufferErr := session.BufferPendingFinalRecord(record); bufferErr != nil {
				return bufferErr
			}
		}
		return err
	}
	return nil
}

func (h *WebSocketHandler) flushPendingTranscriptionRecords(session *WebSocketSession, meetingID int) error {
	if meetingID == 0 {
		return nil
	}

	records := session.DrainPendingFinalRecords()
	if len(records) == 0 {
		return nil
	}

	batch := make([]*TranscriptionRecord, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		record.MeetingID = meetingID
		batch = append(batch, record)
	}

	if len(batch) == 0 {
		return nil
	}

	if err := h.persistTranscriptionRecords(batch); err != nil {
		h.logger.Error("补写待保存转录记录失败",
			zap.Error(err),
			zap.Int("user_id", session.UserID),
			zap.Int("meeting_id", meetingID),
			zap.Int("record_count", len(batch)))
		if requeueErr := session.RequeuePendingFinalRecords(batch); requeueErr != nil {
			return requeueErr
		}
		return err
	}
	return nil
}

func (h *WebSocketHandler) flushPendingTranscriptionRecordsForSession(session *WebSocketSession) error {
	if session == nil {
		return nil
	}

	session.mu.RLock()
	meetingID := 0
	if session.MeetingID != nil {
		meetingID = *session.MeetingID
	}
	session.mu.RUnlock()

	if meetingID <= 0 {
		return nil
	}
	return h.flushPendingTranscriptionRecords(session, meetingID)
}

func (h *WebSocketHandler) persistTranscriptionRecord(record *TranscriptionRecord) error {
	if record == nil {
		return nil
	}

	return h.persistTranscriptionRecords([]*TranscriptionRecord{record})
}

func (h *WebSocketHandler) persistTranscriptionRecords(records []*TranscriptionRecord) error {
	if len(records) == 0 || h.storage == nil {
		return nil
	}

	batch := make([]*TranscriptionRecord, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		recordCopy := *record
		batch = append(batch, &recordCopy)
	}

	if len(batch) == 0 {
		return nil
	}

	if err := h.storage.BatchSaveTranscription(context.Background(), batch); err != nil {
		h.logger.Warn("转录批量入队失败",
			zap.Error(err),
			zap.Int("record_count", len(batch)))
		return err
	}

	return nil
}

// SendWarningToSession 发送警告到会话
func (h *WebSocketHandler) SendWarningToSession(userID int, warningType string, remainingSeconds int) error {
	session, exists := h.sessionManager.GetSessionByUserID(userID)
	if !exists {
		return fmt.Errorf("会话不存在")
	}

	var msg interface{}
	if warningType == "critical" {
		msg = NewCriticalWarningMessage(remainingSeconds)
	} else {
		msg = NewTimeWarningMessage(warningType, remainingSeconds)
	}

	return session.SendMessage(msg)
}

// ForceStopSession 强制停止会话
func (h *WebSocketHandler) ForceStopSession(userID int, reason string) error {
	session, exists := h.sessionManager.GetSessionByUserID(userID)
	if !exists {
		return fmt.Errorf("会话不存在")
	}

	// 发送强制停止消息
	_ = session.SendMessage(map[string]interface{}{
		"type":    MessageTypeStopAndClear,
		"reason":  reason,
		"message": "时间已耗尽，转录已停止",
	})

	// 关闭会话
	return session.Close()
}

// handleKeepalive 处理 keepalive/ping 命令
func (h *WebSocketHandler) handleKeepalive(session *WebSocketSession, msg WebSocketMessage) error {
	applogger.DebugNoise(h.logger, h.config.Environment, "收到 keepalive 消息",
		zap.Int("user_id", session.UserID))

	// 检查是否有 Remote 连接
	session.mu.RLock()
	remoteConn := session.RemoteConn
	remoteReady := session.RemoteReady()
	conn := session.Conn
	session.mu.RUnlock()

	// 如果有 Remote 连接且已就绪，转发 keepalive 到 Remote
	if remoteConn != nil && remoteReady && !remoteConn.IsClosed() {
		keepaliveMsg := map[string]interface{}{
			"type": "keepalive",
		}

		if err := remoteConn.SendJSON(keepaliveMsg); err != nil {
			h.logger.Warn("转发 keepalive 到 Remote 失败",
				zap.Error(err),
				zap.Int("user_id", session.UserID))
			// 不返回错误，keepalive 失败不应该中断连接
		} else {
			applogger.DebugNoise(h.logger, h.config.Environment, "已转发 keepalive 到 Remote",
				zap.Int("user_id", session.UserID))
		}
	}

	// keepalive 消息也视为一次心跳，延长读超时时间以避免触发 i/o timeout
	if conn != nil {
		if err := conn.SetReadDeadline(time.Now().Add(h.config.PongWait)); err != nil {
			h.logger.Warn("刷新 WebSocket 读超时失败",
				zap.Error(err),
				zap.Int("user_id", session.UserID))
		}
	}

	// 可选：发送 pong 响应给客户端
	// 这取决于客户端是否需要响应
	// return session.SendMessage(NewPongMessage())

	return nil
}
