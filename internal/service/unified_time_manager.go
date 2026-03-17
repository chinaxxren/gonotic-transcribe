// Package service 提供业务逻辑实现
package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chinaxxren/gonotic/internal/config"
	"github.com/chinaxxren/gonotic/internal/dto"
	"github.com/chinaxxren/gonotic/internal/model"
	"go.uber.org/zap"
)

// UnifiedTimeManager 统一时间管理器接口
type UnifiedTimeManager interface {
	// Start 启动时间管理器
	Start(ctx context.Context) error

	// Stop 停止时间管理器
	Stop() error

	// StartTranscription 开始转录
	StartTranscription(ctx context.Context, userID int, sessionUUID string, meetingID int) (map[string]interface{}, error)

	// PauseTranscription 暂停转录
	PauseTranscription(ctx context.Context, userID int) (map[string]interface{}, error)

	// ResumeTranscription 恢复转录
	ResumeTranscription(ctx context.Context, userID int) (map[string]interface{}, error)

	// StopTranscription 停止转录
	StopTranscription(ctx context.Context, userID int, meetingDBID *int, meetingStats map[string]interface{}) (map[string]interface{}, error)

	// GetSessionStatus 获取会话状态
	GetSessionStatus(ctx context.Context, userID int) (map[string]interface{}, error)

	// GetSessionStats 获取会话统计信息
	GetSessionStats(ctx context.Context, userID int) (map[string]interface{}, error)

	// UpdateSessionMeetingID 更新会话的会议ID
	UpdateSessionMeetingID(userID int, meetingID int) error

	// GetRemainingTimeDirectQuery 直接查询用户剩余时间（不使用缓存）
	GetRemainingTimeDirectQuery(ctx context.Context, userID int) (int, error)

	// CreateInitialUsageLedger 创建初始的 UsageLedger 记录
	CreateInitialUsageLedger(ctx context.Context, userID int, meetingID int, metadata map[string]interface{}) error

	// CreateInitialUsageLedgerWithTime 创建初始的 UsageLedger 记录，使用指定的累积时间
	CreateInitialUsageLedgerWithTime(ctx context.Context, userID int, meetingID int, accumulatedSeconds int) error

	// GetSessionInfo 获取会话信息（用于恢复会话）
	GetSessionInfo(userID int) (*SessionInfo, bool)

	// UpdateSessionPreferences 更新会话偏好设置
	UpdateSessionPreferences(userID int, audioFormat string, languageHints []string)

	// GetRemainingTime 获取用户剩余时间
	GetRemainingTime(ctx context.Context, userID int) (int, error)

	// SetWarningCallback 设置警告回调
	SetWarningCallback(callback func(userID int, warningType string, remainingSeconds int, planID string))

	// SetForceStopCallback 设置强制停止回调
	SetForceStopCallback(callback func(userID int, reason string))

	// GetStats 获取统计信息
	GetStats() map[string]int64
}

func resolvePlanIDFromRole(role string) string {
	planID := "FREE_MONTHLY"
	switch model.AccountRole(role) {
	case model.AccountRolePro:
		planID = "YEAR_PRO"
	case model.AccountRoleProMini:
		planID = "YEAR_PRO_MINI"
	case model.AccountRolePremium:
		planID = "YEAR_SUB"
	case model.AccountRolePayg:
		planID = "HOUR_PACK"
	case model.AccountRoleSpecialOffer:
		planID = "SPECIAL_OFFER"
	case model.AccountRoleFree:
		planID = "FREE_MONTHLY"
	}
	return planID
}

func (tm *unifiedTimeManager) getUserRemainingTimeDirectThrottled(ctx context.Context, userID int) (int, error) {
	const minDirectBalanceQueryInterval = 5 * time.Second

	now := time.Now()
	if tm != nil && tm.now != nil {
		now = tm.now()
	}
	tm.balanceCheckMu.Lock()
	if last, ok := tm.lastBalanceChk[userID]; ok {
		if now.Sub(last.at) < minDirectBalanceQueryInterval {
			value := last.value
			tm.balanceCheckMu.Unlock()
			return value, nil
		}
	}
	tm.balanceCheckMu.Unlock()

	value, err := tm.getUserRemainingTime(ctx, userID)
	if err != nil {
		return 0, err
	}

	tm.balanceCheckMu.Lock()
	tm.lastBalanceChk[userID] = struct {
		at    time.Time
		value int
	}{at: now, value: value}
	tm.balanceCheckMu.Unlock()

	return value, nil
}

// efficientMonitoringLoop 高效监控循环
func (tm *unifiedTimeManager) efficientMonitoringLoop(ctx context.Context) {
	ticker := time.NewTicker(tm.config.MonitoringInterval)
	defer ticker.Stop()

	lastBillingTime := time.Now()
	lastSessionCheckTime := time.Now()

	tm.logger.Info("监控循环已启动",
		zap.Duration("monitoring_interval", tm.config.MonitoringInterval),
		zap.Duration("billing_interval", tm.config.BillingInterval))

	// 初始化统计计数器
	tm.statsMu.Lock()
	tm.stats["monitoring_cycles"] = 0
	tm.stats["billing_cycles"] = 0
	tm.stats["session_checks"] = 0
	tm.statsMu.Unlock()

	for {
		select {
		case <-ctx.Done():
			tm.logger.Info("监控循环已停止")
			return
		case <-ticker.C:
			now := time.Now()

			// 统计监控周期
			tm.statsMu.Lock()
			tm.stats["monitoring_cycles"]++
			tm.statsMu.Unlock()

			if now.Sub(lastBillingTime) >= tm.config.BillingInterval {
				tm.processRealtimeBilling(ctx)
				lastBillingTime = now

				tm.statsMu.Lock()
				tm.stats["billing_cycles"]++
				tm.statsMu.Unlock()
			}

			// 会话状态检查（每个监控周期都执行，但频率较低）
			if now.Sub(lastSessionCheckTime) >= tm.config.MonitoringInterval {
				tm.checkAllActiveSessions(ctx)
				lastSessionCheckTime = now

				tm.statsMu.Lock()
				tm.stats["session_checks"]++
				tm.statsMu.Unlock()
			}
		}
	}
}

// unifiedTimeManager 实现 UnifiedTimeManager 接口
type unifiedTimeManager struct {
	config        *TimeManagerConfig
	accountFacade AccountStateFacade

	// 会话管理
	sessionCache TranscriptionCache // 转录缓存管理器
	mu           sync.RWMutex       // 基本并发控制锁

	// 回调函数
	warningCallback   func(userID int, warningType string, remainingSeconds int, planID string)
	forceStopCallback func(userID int, reason string)

	// 监控任务
	monitoringTask context.CancelFunc
	isRunning      bool

	// 统计信息
	statsMu sync.Mutex
	stats   map[string]int64

	balanceCheckMu sync.Mutex
	lastBalanceChk map[int]struct {
		at    time.Time
		value int
	}

	now func() time.Time

	// 高级服务
	healthService   *HealthCheckService
	batchProcessor  *BatchProcessor
	errorLogger     *ErrorLogger
	servicesStarted bool

	logger *zap.Logger
}

// NewUnifiedTimeManager 创建新的统一时间管理器
func NewUnifiedTimeManager(
	accountFacade AccountStateFacade,
	logger *zap.Logger,
) UnifiedTimeManager {
	// 使用默认内存缓存（向后兼容）
	sessionCache := InitTranscriptionCache("memory", logger)

	return &unifiedTimeManager{
		config:        DefaultTimeManagerConfig(),
		accountFacade: accountFacade,
		sessionCache:  sessionCache, // 使用单例缓存
		stats: map[string]int64{
			"sessions_started":   0,
			"sessions_completed": 0,
			"cache_hits":         0,
			"database_queries":   0,
			"billing_cycles":     0,
		},
		lastBalanceChk: make(map[int]struct {
			at    time.Time
			value int
		}),
		now:    time.Now,
		logger: logger,
	}
}

// NewUnifiedTimeManagerWithConfig 使用配置创建统一时间管理器
func NewUnifiedTimeManagerWithConfig(
	accountFacade AccountStateFacade,
	redisConfig config.RedisConfig,
	logger *zap.Logger,
) UnifiedTimeManager {
	// 根据配置初始化TranscriptionCache
	sessionCache := InitTranscriptionCacheWithConfig(redisConfig, logger)

	return &unifiedTimeManager{
		config:        DefaultTimeManagerConfig(),
		accountFacade: accountFacade,
		sessionCache:  sessionCache, // 使用配置的缓存
		stats: map[string]int64{
			"sessions_started":   0,
			"sessions_completed": 0,
			"cache_hits":         0,
			"database_queries":   0,
			"billing_cycles":     0,
		},
		lastBalanceChk: make(map[int]struct {
			at    time.Time
			value int
		}),
		now:    time.Now,
		logger: logger,
	}
}

const (
	balanceSourceRealtime    = "time_manager_realtime"
	balanceSourceStop        = "time_manager_stop"
	balanceSourceStopPartial = "time_manager_stop_partial"
)

// Start 启动时间管理器
func (tm *unifiedTimeManager) Start(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.isRunning {
		return fmt.Errorf("时间管理器已在运行")
	}

	tm.isRunning = true

	// 创建可取消的上下文
	monitorCtx, cancel := context.WithCancel(ctx)
	tm.monitoringTask = cancel

	// 启动监控循环
	go tm.efficientMonitoringLoop(monitorCtx)

	tm.logger.Info("统一时间管理器已启动",
		zap.Duration("billing_interval", tm.config.BillingInterval),
		zap.Duration("monitoring_interval", tm.config.MonitoringInterval))

	return nil
}

// Stop 停止时间管理器
func (tm *unifiedTimeManager) Stop() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.isRunning {
		return fmt.Errorf("时间管理器未运行")
	}

	tm.isRunning = false

	// 取消监控任务
	if tm.monitoringTask != nil {
		tm.monitoringTask()
	}

	tm.logger.Info("统一时间管理器已停止")

	return nil
}

// StartTranscription 开始转录
func (tm *unifiedTimeManager) StartTranscription(ctx context.Context, userID int, sessionUUID string, meetingID int) (map[string]interface{}, error) {
	tm.logger.Info("开始转录",
		zap.Int("user_id", userID),
		zap.String("session_uuid", sessionUUID),
		zap.Int("meeting_id", meetingID))

	// 检查最小时长要求（30秒）
	const minRequiredSeconds = 30

	// 1. 清理现有会话（如果存在）- start命令要求全新会话
	existingSession, err := tm.sessionCache.Get(ctx, userID)
	if err == nil {
		// 发现现有会话，直接清理
		tm.logger.Info("start命令要求全新会话，清理现有会话",
			zap.Int("user_id", userID),
			zap.String("existing_session_uuid", existingSession.SessionUUID),
			zap.Bool("is_transcribing", existingSession.IsTranscribing),
			zap.Bool("is_paused", existingSession.IsPaused))
		tm.silentCleanupSession(ctx, userID, string(CleanupReasonSessionCleanupBeforeNewStart))
	}

	// 2. 刷新用户剩余时间（一次直接查询，并写入缓存）
	remainingTime, err := tm.refreshRemainingTime(ctx, userID)
	if err != nil {
		tm.logger.Error("刷新余额失败",
			zap.Int("user_id", userID),
			zap.Error(err))
		return nil, err
	}

	if remainingTime <= 0 {
		tm.logger.Warn("用户余额不足",
			zap.Int("user_id", userID),
			zap.Int("remaining_time", remainingTime))
		planID := ""
		summary, err := tm.accountFacade.GetAccountSummary(ctx, userID)
		if err == nil && summary != nil {
			planID = resolvePlanIDFromRole(summary.Role)
		}
		return map[string]interface{}{
			"success": false,
			"message": tm.config.Messages["insufficient_time"],
			"data": map[string]interface{}{
				"plan_id": planID,
			},
		}, nil
	}

	if remainingTime < minRequiredSeconds {
		tm.logger.Warn("用户剩余时长不足最小转录要求（刷新后检查）",
			zap.Int("user_id", userID),
			zap.Int("remaining_time", remainingTime),
			zap.Int("min_required", minRequiredSeconds))
		planID := ""
		summary, err := tm.accountFacade.GetAccountSummary(ctx, userID)
		if err == nil && summary != nil {
			planID = resolvePlanIDFromRole(summary.Role)
		}
		return map[string]interface{}{
			"success": false,
			"message": "Insufficient time: minimum 30 seconds required for transcription",
			"data": map[string]interface{}{
				"plan_id": planID,
			},
		}, nil
	}

	// 5. 创建新会话
	tm.mu.Lock()
	session := NewSessionInfo(userID, sessionUUID, meetingID)
	session.IsTranscribing = true
	session.Status = tm.config.StatusTranscribing
	session.RemainingTime = remainingTime
	session.SafeUpdateBillingState(true) // 启用实时扣费
	session.LastBillingTime = time.Now()
	session.TranscriptionStartTime = time.Now()

	tm.mu.Unlock()

	// 使用复合操作创建或更新会话（一次锁操作完成）
	if err := tm.sessionCache.CreateOrUpdateSession(ctx, userID, session); err != nil {
		tm.logger.Error("保存会话到缓存失败", zap.Error(err), zap.Int("user_id", userID))
		return tm.buildErrorResponse("Failed to save session"), fmt.Errorf("failed to save session: %w", err)
	}

	// 6. 更新统计
	tm.statsMu.Lock()
	tm.stats["sessions_started"]++
	tm.statsMu.Unlock()

	tm.logger.Info("转录会话已创建",
		zap.Int("user_id", userID),
		zap.String("session_uuid", sessionUUID),
		zap.Int("remaining_time", remainingTime))

	return tm.buildResponseData(session, "转录已开始", nil), nil
}

// PauseTranscription 暂停转录
func (tm *unifiedTimeManager) PauseTranscription(ctx context.Context, userID int) (map[string]interface{}, error) {
	session, err := tm.sessionCache.Get(ctx, userID)
	if err != nil {
		return tm.buildErrorResponse(tm.config.Messages["session_not_found"]), nil
	}

	if !session.IsTranscribing {
		return tm.buildErrorResponse(tm.config.Messages["not_transcribing"]), nil
	}

	// 计算当前会话时间（整秒）
	currentSessionTime := int64(time.Since(session.TranscriptionStartTime) / time.Second)
	if currentSessionTime < 0 {
		currentSessionTime = 0
	}
	session.PausedSessionTime += currentSessionTime
	session.TotalDuration = session.PausedSessionTime
	session.CurrentSessionTime = 0
	session.PauseCount++

	// 更新状态
	session.IsPaused = true
	session.IsTranscribing = false
	session.Status = tm.config.StatusPaused
	session.LastUpdate = time.Now()
	session.SafeUpdateBillingState(false) // 暂停时停止实时扣费

	// 更新剩余时间
	if _, err := tm.refreshRemainingTimeLocked(ctx, session); err != nil {
		tm.logger.Error("刷新余额失败",
			zap.Int("user_id", userID),
			zap.Error(err))
	}

	tm.logger.Info("转录已暂停",
		zap.Int("user_id", userID),
		zap.Int64("total_duration", session.TotalDuration))

	// 保存更新后的session到缓存
	if err := tm.sessionCache.Set(ctx, userID, session); err != nil {
		tm.logger.Error("保存暂停会话失败", zap.Error(err), zap.Int("user_id", userID))
	}

	return tm.buildResponseData(session, tm.config.Messages["recording_paused"], nil), nil
}

// ResumeTranscription 恢复转录
func (tm *unifiedTimeManager) ResumeTranscription(ctx context.Context, userID int) (map[string]interface{}, error) {
	session, err := tm.sessionCache.Get(ctx, userID)
	if err != nil {
		return tm.buildErrorResponse(tm.config.Messages["session_not_found"]), nil
	}

	// 支持恢复暂停状态或断线重连状态的会话
	if !session.IsPaused && !session.IsTranscribing {
		return tm.buildErrorResponse(tm.config.Messages["not_paused"]), nil
	}

	// 如果已经在转录中，直接返回成功（断线重连场景）
	if session.IsTranscribing && !session.IsPaused {
		tm.logger.Info("会话已在转录中，直接返回成功（断线重连）",
			zap.Int("user_id", userID))
		return tm.buildResponseData(session, tm.config.Messages["recording_resumed"], nil), nil
	}

	now := time.Now()

	// 恢复录制
	session.IsPaused = false
	session.IsTranscribing = true
	session.TranscriptionStartTime = now
	session.Status = tm.config.StatusTranscribing
	session.LastUpdate = now
	session.SafeUpdateBillingState(true) // 恢复时重新启用实时扣费
	session.LastBillingTime = now        // 重置扣费时间

	// 更新总时长（确保使用暂停前的时间）
	session.TotalDuration = session.PausedSessionTime

	// 更新剩余时间
	if _, err := tm.refreshRemainingTimeLocked(ctx, session); err != nil {
		tm.logger.Error("刷新余额失败",
			zap.Int("user_id", userID),
			zap.Error(err))
	}

	tm.logger.Info("转录已恢复",
		zap.Int("user_id", userID))

	// 保存更新后的session到缓存
	if err := tm.sessionCache.Set(ctx, userID, session); err != nil {
		tm.logger.Error("保存恢复会话失败", zap.Error(err), zap.Int("user_id", userID))
	}

	return tm.buildResponseData(session, tm.config.Messages["recording_resumed"], nil), nil
}

// StopTranscription 停止转录
func (tm *unifiedTimeManager) StopTranscription(ctx context.Context, userID int, meetingDBID *int, meetingStats map[string]interface{}) (map[string]interface{}, error) {
	session, err := tm.sessionCache.Get(ctx, userID)
	if err != nil {
		return tm.buildErrorResponse(tm.config.Messages["session_not_found"]), nil
	}

	// 尝试开始结算，防止与实时扣费竞争
	if !session.TryStartSettlement() {
		tm.logger.Warn("会话已在结算中，跳过重复停止",
			zap.Int("user_id", userID))
		return tm.buildErrorResponse("Session is already being settled"), nil
	}

	// 确保在函数结束时释放结算锁
	defer session.FinishSettlement()

	totalSessionTime, _, finalConsumedSeconds := session.CalculateFinalTimes()

	session.IsTranscribing = false
	session.IsPaused = false
	session.Status = tm.config.StatusCompleted
	session.LastUpdate = time.Now()
	// 注意：IsBillingActive 已在 TryStartSettlement 中设置为 false

	persistedBeforeStop := session.PersistedSeconds
	persistedBaseBeforeStop := session.PersistedBaseSeconds
	consumedBeforeStop := session.ConsumedSeconds
	multiplier := session.getBillingMultiplier()
	if multiplier <= 0 {
		multiplier = 1
	}

	tm.logger.Debug("停止扣费",
		zap.Int("user_id", userID),
		zap.Int64("persisted_total", persistedBeforeStop),
		zap.Int64("persisted_base", persistedBaseBeforeStop),
		zap.Int64("consumed", consumedBeforeStop),
		zap.Int64("final_consumed", finalConsumedSeconds),
		zap.Int("multiplier", multiplier))

	outstandingBaseSeconds := finalConsumedSeconds - persistedBaseBeforeStop
	if outstandingBaseSeconds < 0 {
		outstandingBaseSeconds = 0
	}

	meetingIDForBilling := session.MeetingID
	if meetingIDForBilling <= 0 && meetingDBID != nil {
		meetingIDForBilling = *meetingDBID
		// 使用复合操作更新MeetingID（一次锁操作）
		if err := tm.sessionCache.UpdateSessionWithMeetingID(ctx, userID, meetingIDForBilling); err != nil {
			tm.logger.Error("更新MeetingID失败", zap.Error(err), zap.Int("user_id", userID))
		}
	}

	if outstandingBaseSeconds > 0 {
		if meetingIDForBilling <= 0 {
			tm.logger.Warn("停止扣费跳过：缺少会议ID",
				zap.Int("user_id", userID),
				zap.Int64("outstanding_base", outstandingBaseSeconds))
		} else {
			req, transcriptionSeconds, translationSeconds := buildUsageRequest(userID, meetingIDForBilling, outstandingBaseSeconds, multiplier, balanceSourceStop)

			if err := tm.updateUserTimeBalance(ctx, req); err != nil {
				tm.logger.Warn("停止时扣费失败，但允许停止操作继续",
					zap.Int("user_id", userID),
					zap.Int64("outstanding_base", outstandingBaseSeconds),
					zap.Int("transcription_seconds", transcriptionSeconds),
					zap.Int("translation_seconds", translationSeconds),
					zap.Error(err))

				remainingTime, balanceErr := tm.refreshRemainingTime(ctx, userID)
				if balanceErr == nil && remainingTime > 0 {
					partialBaseSeconds := int64(remainingTime)
					if partialBaseSeconds > outstandingBaseSeconds {
						partialBaseSeconds = outstandingBaseSeconds
					}

					if partialBaseSeconds > 0 {
						partialReq, partialTranscription, partialTranslation := buildUsageRequest(userID, meetingIDForBilling, partialBaseSeconds, multiplier, balanceSourceStopPartial)
						if partialErr := tm.updateUserTimeBalance(ctx, partialReq); partialErr == nil {
							tm.mu.Lock()
							session.PersistedBaseSeconds += partialBaseSeconds
							session.PersistedSeconds += int64(partialTranscription + partialTranslation)
							tm.mu.Unlock()

							addMeetingStat(meetingStats, "transcription_seconds", partialTranscription)
							addMeetingStat(meetingStats, "translation_seconds", partialTranslation)

							tm.logger.Info("部分扣费成功",
								zap.Int("user_id", userID),
								zap.Int64("partial_base_seconds", partialBaseSeconds),
								zap.Int("partial_transcription", partialTranscription),
								zap.Int("partial_translation", partialTranslation))
						} else {
							tm.logger.Warn("部分扣费也失败",
								zap.Int("user_id", userID),
								zap.Int64("partial_base_seconds", partialBaseSeconds),
								zap.Error(partialErr))
						}
					}
				}
			} else {
				tm.mu.Lock()
				session.PersistedBaseSeconds += outstandingBaseSeconds
				session.PersistedSeconds += int64(transcriptionSeconds + translationSeconds)
				tm.mu.Unlock()

				addMeetingStat(meetingStats, "transcription_seconds", transcriptionSeconds)
				addMeetingStat(meetingStats, "translation_seconds", translationSeconds)

				tm.logger.Debug("停止时扣费成功",
					zap.Int("user_id", userID),
					zap.Int64("outstanding_base", outstandingBaseSeconds),
					zap.Int("transcription_seconds", transcriptionSeconds),
					zap.Int("translation_seconds", translationSeconds),
					zap.Int64("persisted_total", session.PersistedSeconds),
					zap.Int64("persisted_base", session.PersistedBaseSeconds))
			}
			tm.logger.Debug("停止时扣费成功",
				zap.Int("user_id", userID),
				zap.Int64("outstanding_base", outstandingBaseSeconds),
				zap.Int("transcription_seconds", transcriptionSeconds),
				zap.Int("translation_seconds", translationSeconds),
				zap.Int64("persisted_total", session.PersistedSeconds),
				zap.Int64("persisted_base", session.PersistedBaseSeconds))
		}
	} else {
		tm.logger.Debug("无需扣费：所有时间已扣费",
			zap.Int("user_id", userID),
			zap.Int64("final_consumed", finalConsumedSeconds),
			zap.Int64("persisted_base", session.PersistedBaseSeconds))
	}

	// 从sessionCache删除会话
	_ = tm.sessionCache.Delete(ctx, userID)

	tm.statsMu.Lock()
	tm.stats["sessions_completed"]++
	tm.statsMu.Unlock()

	tm.logger.Info("转录已停止",
		zap.Int("user_id", userID),
		zap.Int64("consumed_time", totalSessionTime))

	// 只返回必要的信息：meeting_id是字符串
	meetingIDStr := "0"
	if meetingDBID != nil {
		meetingIDStr = fmt.Sprintf("%d", *meetingDBID)
	} else if session.MeetingID > 0 {
		meetingIDStr = fmt.Sprintf("%d", session.MeetingID)
	}

	return map[string]interface{}{
		"type":       "stopped",
		"success":    true,
		"message":    tm.config.Messages["recording_stopped"],
		"meeting_id": meetingIDStr,
	}, nil
}

// GetSessionStatus 获取会话状态
func (tm *unifiedTimeManager) GetSessionStatus(ctx context.Context, userID int) (map[string]interface{}, error) {
	session, err := tm.sessionCache.Get(ctx, userID)
	if err != nil {
		return map[string]interface{}{
			"success": true,
			"message": "no_session",
			"data": map[string]interface{}{
				"is_transcribing": false,
				"is_paused":       false,
				"status":          "no_session",
			},
		}, nil
	}

	session.UpdateCurrentTime()

	// P3 修复: 通过 sessionCache.UpdateRemainingTime 更新，已在写锁内完成
	// 移除冗余的无锁修改，避免数据竞争
	if remaining, err := tm.getUserRemainingTimeCached(ctx, userID); err == nil {
		_ = tm.sessionCache.UpdateRemainingTime(ctx, userID, remaining)
	}

	response := tm.buildResponseData(session, "会话状态", nil)
	return response, nil
}

// UpdateSessionPreferences 更新会话偏好设置
// P1 修复: 使用带超时的 context 代替 context.Background()
func (tm *unifiedTimeManager) UpdateSessionPreferences(userID int, audioFormat string, languageHints []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 使用TranscriptionCache更新偏好设置（集中锁管理）
	if err := tm.sessionCache.UpdatePreferences(ctx, userID, audioFormat, languageHints); err != nil {
		tm.logger.Error("更新会话偏好设置失败", zap.Error(err), zap.Int("user_id", userID))
		return
	}
}

// buildResponseData 构建响应数据
func (tm *unifiedTimeManager) buildResponseData(session *SessionInfo, message string, extraData map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{
		"is_transcribing":      session.IsTranscribing,
		"is_paused":            session.IsPaused,
		"total_duration":       session.TotalDuration,
		"remaining_time":       session.RemainingTime,
		"current_session_time": session.CurrentSessionTime,
		"paused_session_time":  session.PausedSessionTime,
		"pause_count":          session.PauseCount,
		"consumed_seconds":     session.ConsumedSeconds,
		"persisted_seconds":    session.PersistedSeconds,
		"persisted_base":       session.PersistedBaseSeconds,
		"status":               session.Status,
	}

	for k, v := range extraData {
		data[k] = v
	}

	return map[string]interface{}{
		"success": true,
		"message": message,
		"data":    data,
	}
}

// buildErrorResponse 构建错误响应
func (tm *unifiedTimeManager) buildErrorResponse(message string) map[string]interface{} {
	return map[string]interface{}{
		"success": false,
		"message": message,
		"data":    nil,
	}
}

// getUserRemainingTime 获取用户剩余时间（通过 AccountStateFacade 查询）
func (tm *unifiedTimeManager) getUserRemainingTime(ctx context.Context, userID int) (int, error) {
	summary, err := tm.accountFacade.GetAccountSummary(ctx, userID)
	if err != nil {
		return 0, err
	}
	return summary.TotalRemainingSeconds(), nil
}

func (tm *unifiedTimeManager) refreshRemainingTime(ctx context.Context, userID int) (int, error) {
	balance, err := tm.getUserRemainingTime(ctx, userID)
	if err != nil {
		return 0, err
	}

	if balance >= 0 {
		tm.setUserCache(userID, balance)
	}

	return balance, nil
}

func (tm *unifiedTimeManager) refreshRemainingTimeLocked(ctx context.Context, session *SessionInfo) (int, error) {
	balance, err := tm.getUserRemainingTime(ctx, session.UserID)
	if err != nil {
		return 0, err
	}

	if balance >= 0 {
		session.RemainingTime = balance
		session.LastRemainingTimeUpdate = time.Now()
	}

	return balance, nil
}

// updateUserTimeBalance 调用 AccountStateFacade 扣费并维护缓存
func (tm *unifiedTimeManager) updateUserTimeBalance(ctx context.Context, req dto.AccountConsumeRequest) error {
	if req.UserID <= 0 {
		return fmt.Errorf("invalid user id")
	}
	if req.Seconds <= 0 {
		return fmt.Errorf("seconds must be positive")
	}

	result, err := tm.accountFacade.Consume(ctx, req)
	if err != nil {
		return err
	}

	tm.clearUserCache(req.UserID)
	if result != nil && result.Summary != nil {
		tm.setUserCache(req.UserID, result.Summary.TotalRemainingSeconds())
	}

	tm.logger.Debug("扣费成功",
		zap.Int("user_id", req.UserID),
		zap.Int("seconds", req.Seconds),
		zap.String("source", req.Source))

	return nil
}

// GetRemainingTime 获取用户剩余时间
func (tm *unifiedTimeManager) GetRemainingTime(ctx context.Context, userID int) (int, error) {
	return tm.getUserRemainingTimeCached(ctx, userID)
}

// GetRemainingTimeDirectQuery 直接查询用户剩余时间（不使用缓存）
func (tm *unifiedTimeManager) GetRemainingTimeDirectQuery(ctx context.Context, userID int) (int, error) {
	balance, err := tm.getUserRemainingTime(ctx, userID)
	if err != nil {
		return 0, err
	}

	// 更新缓存以保持一致性
	if balance >= 0 {
		tm.setUserCache(userID, balance)
	}

	return balance, nil
}

// GetSessionInfo 获取会话信息（用于恢复会话）
// P1 修复: 使用带超时的 context 代替 context.Background()，防止无限阻塞
func (tm *unifiedTimeManager) GetSessionInfo(userID int) (*SessionInfo, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := tm.sessionCache.Get(ctx, userID)
	if err != nil {
		return nil, false
	}

	// 创建副本以避免并发修改
	return session.Clone(), true
}

// setUserCache 设置用户余额缓存
// P1 修复: 使用带超时的 context 代替 context.Background()
func (tm *unifiedTimeManager) setUserCache(userID int, balance int) {
	if balance < 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 通过sessionCache更新剩余时间
	_ = tm.sessionCache.UpdateRemainingTime(ctx, userID, balance)
}

// clearUserCache 清除用户余额缓存
// P1 修复: 使用带超时的 context 代替 context.Background()
func (tm *unifiedTimeManager) clearUserCache(userID int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 通过sessionCache清除剩余时间缓存
	_ = tm.sessionCache.UpdateRemainingTime(ctx, userID, 0)
	tm.logger.Debug("用户余额缓存已清除",
		zap.Int("user_id", userID))
}

// getUserRemainingTimeCached 获取用户剩余时间（带分层缓存TTL）
func (tm *unifiedTimeManager) getUserRemainingTimeCached(ctx context.Context, userID int) (int, error) {
	// 尝试从sessionCache获取缓存的剩余时间
	if session, err := tm.sessionCache.Get(ctx, userID); err == nil {
		if session.RemainingTime >= 0 && !session.LastRemainingTimeUpdate.IsZero() {
			// 检查缓存是否过期（分层TTL策略）
			now := time.Now()
			cacheAge := now.Sub(session.LastRemainingTimeUpdate)

			// 根据余额选择TTL策略
			var cacheTTL time.Duration
			if session.RemainingTime <= tm.config.LowBalanceThresholdSeconds {
				cacheTTL = tm.config.LowBalanceCacheTTL // 低余额：20秒TTL
			} else {
				cacheTTL = tm.config.RemainingTimeCacheTTL // 正常余额：60秒TTL
			}

			// 缓存未过期，使用缓存数据
			if cacheAge <= cacheTTL {
				tm.statsMu.Lock()
				tm.stats["cache_hits"]++
				tm.statsMu.Unlock()
				return session.RemainingTime, nil
			}
		}
	}

	// 缓存未命中或过期，查询数据库
	tm.statsMu.Lock()
	tm.stats["database_queries"]++
	tm.statsMu.Unlock()

	balance, err := tm.getUserRemainingTime(ctx, userID)
	if err != nil {
		return 0, err
	}

	// 更新缓存
	tm.setUserCache(userID, balance)
	return balance, nil
}

// addMeetingStat 添加会议统计信息
func addMeetingStat(stats map[string]interface{}, key string, value int) {
	if stats == nil {
		return
	}

	value64 := int64(value)

	if existing, ok := stats[key]; ok {
		switch v := existing.(type) {
		case int64:
			stats[key] = v + value64
		case int:
			stats[key] = int64(v) + value64
		case float64:
			stats[key] = int64(v) + value64
		default:
			stats[key] = value64
		}
		return
	}

	stats[key] = value64
}

// processSingleSessionBilling 处理单个会话的扣费
func (tm *unifiedTimeManager) processSingleSessionBilling(ctx context.Context, session *SessionInfo) {
	now := time.Now()
	elapsedSeconds := int64(now.Sub(session.LastBillingTime).Seconds())

	if elapsedSeconds <= 0 {
		return
	}

	multiplier := session.getBillingMultiplier()
	if multiplier <= 0 {
		multiplier = 1
	}

	if session.MeetingID <= 0 {
		// 没有meeting_id时，只累积时长，跳过扣费和入库
		// 这里只累积基础会议秒数，等 meetingID 就绪后再统一落账，避免后续再次按倍率放大。
		tm.mu.Lock()
		session.ConsumedSeconds += elapsedSeconds
		session.LastBillingTime = now
		session.LastUpdate = now
		tm.mu.Unlock()

		tm.logger.Debug("缺少会议ID，只累积时长",
			zap.Int("user_id", session.UserID),
			zap.Int64("elapsed_seconds", elapsedSeconds),
			zap.Int64("consumed_seconds", session.ConsumedSeconds))
		return
	}

	// 计算总扣费时间：当前时间 + 之前累积的时间
	totalSeconds := elapsedSeconds
	if session.ConsumedSeconds > 0 {
		totalSeconds += session.ConsumedSeconds
		tm.logger.Debug("合并累积时长",
			zap.Int("user_id", session.UserID),
			zap.Int64("current_seconds", elapsedSeconds),
			zap.Int64("consumed_seconds", session.ConsumedSeconds),
			zap.Int64("total_seconds", totalSeconds))
	}

	req, transcriptionSeconds, translationSeconds := buildUsageRequest(session.UserID, session.MeetingID, totalSeconds, multiplier, balanceSourceRealtime)

	tm.logger.Debug("扣费请求详情",
		zap.Int("user_id", session.UserID),
		zap.Int64("base_seconds", totalSeconds),
		zap.Int("multiplier", multiplier),
		zap.Int("transcription_seconds", transcriptionSeconds),
		zap.Int("translation_seconds", translationSeconds),
		zap.Int("total_request_seconds", req.Seconds))

	if err := tm.updateUserTimeBalance(ctx, req); err != nil {
		tm.logger.Error("实时扣费失败，进行实时余额确认",
			zap.Int("user_id", session.UserID),
			zap.Int64("base_seconds", elapsedSeconds),
			zap.Int64("total_seconds", totalSeconds),
			zap.Error(err))

		// 实时查询用户余额进行最后确认
		realTimeBalance, queryErr := tm.getUserRemainingTimeDirectThrottled(ctx, session.UserID)
		if queryErr != nil {
			tm.logger.Error("计费失败后实时查询余额也失败，强制停止",
				zap.Error(queryErr),
				zap.Int("user_id", session.UserID))
		} else {
			tm.logger.Info("计费失败实时余额查询结果",
				zap.Int("user_id", session.UserID),
				zap.Int("cached_balance", session.RemainingTime),
				zap.Int("real_time_balance", realTimeBalance),
				zap.Error(err))

			// 如果用户仍有余额，可能是临时计费系统问题，允许继续
			if realTimeBalance > 60 { // 余额超过1分钟，允许继续
				tm.logger.Warn("计费失败但用户仍有余额，可能是临时系统问题，允许继续",
					zap.Int("user_id", session.UserID),
					zap.Int("real_time_balance", realTimeBalance),
					zap.Error(err))

				// 更新缓存
				tm.setUserCache(session.UserID, realTimeBalance)
				session.RemainingTime = realTimeBalance
				session.LastRemainingTimeUpdate = time.Now()

				// 记录计费失败但继续的情况，用于后续分析
				tm.logger.Warn("计费系统异常，已记录但允许用户继续使用",
					zap.Int("user_id", session.UserID),
					zap.String("billing_error", err.Error()))
				return
			}
		}

		// 确认需要停止（余额不足或查询失败）
		tm.logger.Error("确认计费失败且余额不足，强制停止",
			zap.Int("user_id", session.UserID),
			zap.Error(err))
		tm.forceStopTranscription(ctx, session.UserID, string(CleanupReasonBillingFailed))
		return
	}

	tm.mu.Lock()
	session.PersistedBaseSeconds += elapsedSeconds
	session.PersistedSeconds += int64(transcriptionSeconds + translationSeconds)
	session.ConsumedSeconds = 0 // 清零累积时长
	session.LastBillingTime = now
	session.BillingCycleCount++
	tm.mu.Unlock()

	tm.logger.Debug("实时扣费成功",
		zap.Int("user_id", session.UserID),
		zap.Int64("elapsed_seconds", elapsedSeconds),
		zap.Int("transcription_seconds", transcriptionSeconds),
		zap.Int("translation_seconds", translationSeconds),
		zap.Int64("persisted_total", session.PersistedSeconds),
		zap.Int64("persisted_base", session.PersistedBaseSeconds))
}

// processRealtimeBilling 处理实时扣费
func (tm *unifiedTimeManager) processRealtimeBilling(ctx context.Context) {
	// 使用复合操作获取需要计费的会话（一次锁操作）
	sessionsToProcess, err := tm.sessionCache.GetActiveSessionsForBilling(ctx)
	if err != nil {
		tm.logger.Error("获取计费会话失败", zap.Error(err))
		return
	}

	if len(sessionsToProcess) == 0 {
		return
	}

	tm.logger.Debug("开始实时扣费",
		zap.Int("session_count", len(sessionsToProcess)))
	for _, session := range sessionsToProcess {
		tm.processSingleSessionBilling(ctx, session)
	}
}

// checkAllActiveSessions 检查所有活跃会话
func (tm *unifiedTimeManager) checkAllActiveSessions(ctx context.Context) {
	// 使用复合操作获取需要检查的会话（一次锁操作）
	sessionsToCheck, err := tm.sessionCache.GetActiveSessionsForCheck(ctx)
	if err != nil {
		tm.logger.Error("获取检查会话失败", zap.Error(err))
		return
	}

	for _, session := range sessionsToCheck {
		// 更新余额
		remainingTime, err := tm.getUserRemainingTimeCached(ctx, session.UserID)
		if err != nil {
			tm.logger.Error("获取余额失败",
				zap.Int("user_id", session.UserID),
				zap.Error(err))
			continue
		}

		// 使用无锁操作更新剩余时间（非关键信息）
		_ = tm.sessionCache.UpdateRemainingTime(ctx, session.UserID, remainingTime)

		// 直接更新非关键信息，无需加锁
		session.LastRemainingTimeUpdate = time.Now()

		// 检查警告和强制停止
		tm.checkWarningsAndForceStop(ctx, session)
	}
}

// checkWarningsAndForceStop 检查警告和强制停止
func (tm *unifiedTimeManager) checkWarningsAndForceStop(ctx context.Context, session *SessionInfo) {
	remainingTime := session.RemainingTime

	// 余额耗尽，实时查询确认后强制停止
	if remainingTime <= 0 {
		tm.logger.Warn("检测到余额耗尽，进行实时查询确认",
			zap.Int("user_id", session.UserID),
			zap.Int("cached_remaining_time", remainingTime))

		// 实时查询数据库进行最后确认
		realTimeBalance, err := tm.getUserRemainingTimeDirectThrottled(ctx, session.UserID)
		if err != nil {
			tm.logger.Error("实时查询余额失败，基于缓存数据强制停止",
				zap.Error(err),
				zap.Int("user_id", session.UserID))
		} else {
			tm.logger.Info("实时余额查询结果",
				zap.Int("user_id", session.UserID),
				zap.Int("cached_balance", remainingTime),
				zap.Int("real_time_balance", realTimeBalance))

			// 如果实时查询显示还有余额，更新缓存并继续
			if realTimeBalance > 0 {
				tm.logger.Info("实时查询显示仍有余额，更新缓存并继续转录",
					zap.Int("user_id", session.UserID),
					zap.Int("real_time_balance", realTimeBalance))

				// 更新缓存
				tm.setUserCache(session.UserID, realTimeBalance)
				session.RemainingTime = realTimeBalance
				session.LastRemainingTimeUpdate = time.Now()

				// 重新检查警告（基于实时余额）
				tm.checkWarningsAndForceStop(ctx, session)
				return
			}
		}

		// 确认余额耗尽，强制停止
		tm.logger.Warn("确认余额耗尽，强制停止转录",
			zap.Int("user_id", session.UserID))
		tm.forceStopTranscription(ctx, session.UserID, string(CleanupReasonTimeExhausted))
		return
	}

	// 5分钟关键警告 (5分钟到5分钟30秒内提示)
	criticalThreshold := tm.config.WarningThresholds["critical"]
	criticalWindow := tm.config.WarningWindows["critical"]
	if remainingTime <= criticalThreshold && remainingTime > (criticalThreshold-criticalWindow) {
		if !session.WarningsSent["critical"] {
			tm.sendWarning(session.UserID, "critical", remainingTime)
			tm.mu.Lock()
			session.WarningsSent["critical"] = true
			tm.mu.Unlock()
		}
		return
	}

	// 10分钟警告 (10分钟到10分钟30秒内提示)
	warningThreshold := tm.config.WarningThresholds["warning"]
	warningWindow := tm.config.WarningWindows["warning"]
	if remainingTime <= warningThreshold && remainingTime > (warningThreshold-warningWindow) {
		if !session.WarningsSent["warning"] {
			tm.sendWarning(session.UserID, "warning", remainingTime)
			tm.mu.Lock()
			session.WarningsSent["warning"] = true
			tm.mu.Unlock()
		}
		return
	}

	// 1小时信息警告 (1小时到1小时30秒内提示)
	infoThreshold := tm.config.WarningThresholds["info"]
	infoWindow := tm.config.WarningWindows["info"]
	if remainingTime <= infoThreshold && remainingTime > (infoThreshold-infoWindow) {
		if !session.WarningsSent["info"] {
			tm.sendWarning(session.UserID, "info", remainingTime)
			tm.mu.Lock()
			session.WarningsSent["info"] = true
			tm.mu.Unlock()
		}
	}

	// 会话时长检查已移除，现在只依赖余额检查
	return
}

// sendWarning 发送警告消息
func (tm *unifiedTimeManager) sendWarning(userID int, warningType string, remainingSeconds int) {
	tm.logger.Warn("发送时间警告",
		zap.Int("user_id", userID),
		zap.String("warning_type", warningType),
		zap.Int("remaining_seconds", remainingSeconds))

	// 通过回调发送警告消息
	if tm.warningCallback != nil {
		planID := ""
		if warningType == "warning" {
			summary, err := tm.accountFacade.GetAccountSummary(context.Background(), userID)
			if err == nil && summary != nil {
				planID = resolvePlanIDFromRole(summary.Role)
			}
		}
		tm.warningCallback(userID, warningType, remainingSeconds, planID)
	}
}

// forceStopTranscription 强制停止转录
func (tm *unifiedTimeManager) forceStopTranscription(ctx context.Context, userID int, reason string) {
	tm.logger.Warn("强制停止转录",
		zap.Int("user_id", userID),
		zap.String("reason", reason))

	// 通过回调强制停止
	if tm.forceStopCallback != nil {
		tm.forceStopCallback(userID, reason)
	}

	// 停止转录
	_, err := tm.StopTranscription(ctx, userID, nil, nil)
	if err != nil {
		tm.logger.Error("强制停止转录失败",
			zap.Int("user_id", userID),
			zap.Error(err))
	}
}

// silentCleanupSession 静默清理会话（不触发回调，不发送消息）
func (tm *unifiedTimeManager) silentCleanupSession(ctx context.Context, userID int, reason string) {
	session, err := tm.sessionCache.Get(ctx, userID)
	if err != nil || session == nil {
		// 会话不存在，认为已经清理
		tm.logger.Debug("静默清理跳过：会话不存在",
			zap.Int("user_id", userID),
			zap.String("reason", reason))
		return
	}

	tm.logger.Info("静默清理会话",
		zap.Int("user_id", userID),
		zap.String("reason", reason))

	// 尝试开始结算，防止与实时扣费竞争
	if !session.TryStartSettlement() {
		tm.logger.Debug("会话已在结算中，跳过静默清理",
			zap.Int("user_id", userID))
		return
	}

	// 确保在函数结束时释放结算锁
	defer session.FinishSettlement()

	// 简单地标记会话为停止状态
	session.IsTranscribing = false
	session.IsPaused = false
	session.Status = tm.config.StatusCompleted
	session.LastUpdate = time.Now()

	// 从sessionCache删除会话
	_ = tm.sessionCache.Delete(ctx, userID)

	tm.logger.Debug("静默清理会话完成",
		zap.Int("user_id", userID))
}

// SetWarningCallback 设置警告回调
func (tm *unifiedTimeManager) SetWarningCallback(callback func(userID int, warningType string, remainingSeconds int, planID string)) {
	tm.warningCallback = callback
}

func (tm *unifiedTimeManager) SetForceStopCallback(callback func(userID int, reason string)) {
	tm.forceStopCallback = callback
}

// GetStats 获取统计信息（用于监控）
func (tm *unifiedTimeManager) GetStats() map[string]int64 {
	tm.statsMu.Lock()
	defer tm.statsMu.Unlock()

	stats := make(map[string]int64)
	for k, v := range tm.stats {
		stats[k] = v
	}

	// 活跃会话数由sessionCache管理，这里设为0
	stats["active_sessions"] = 0

	return stats
}

// StartAdvancedServices 启动高级服务
func (tm *unifiedTimeManager) StartAdvancedServices(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.servicesStarted {
		return nil
	}

	// 初始化高级服务
	tm.healthService = NewHealthCheckService(tm, tm.logger)
	tm.batchProcessor = NewBatchProcessor(tm.logger)
	tm.errorLogger = NewErrorLogger(tm.logger)

	// 启动健康检查服务
	if err := tm.healthService.Start(ctx, nil); err != nil {
		tm.logger.Error("启动健康检查服务失败", zap.Error(err))
		return err
	}

	// 启动批量处理器
	go tm.batchProcessor.StartAutoFlush(ctx)

	// 启动错误日志记录器
	go tm.errorLogger.StartAutoFlush(ctx)

	tm.servicesStarted = true

	tm.logger.Info("高级服务已启动",
		zap.Bool("health_service", tm.healthService.IsRunning()))

	return nil
}

// StopAdvancedServices 停止高级服务
func (tm *unifiedTimeManager) StopAdvancedServices() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if !tm.servicesStarted {
		return
	}

	// 停止健康检查服务
	if tm.healthService != nil {
		tm.healthService.Stop()
	}

	// 批量处理器和错误日志记录器会在上下文取消时自动停止

	tm.servicesStarted = false

	tm.logger.Info("高级服务已停止")
}

// GetHealthStats 获取健康统计
func (tm *unifiedTimeManager) GetHealthStats() *HealthStats {
	if tm.healthService == nil {
		return nil
	}
	return tm.healthService.GetHealthStats()
}

// GetBatchProcessorStats 获取批量处理器统计
func (tm *unifiedTimeManager) GetBatchProcessorStats() map[string]interface{} {
	if tm.batchProcessor == nil {
		return nil
	}
	return tm.batchProcessor.GetStats()
}

// GetErrorLoggerStats 获取错误日志记录器统计
func (tm *unifiedTimeManager) GetErrorLoggerStats() map[string]interface{} {
	if tm.errorLogger == nil {
		return nil
	}
	return tm.errorLogger.GetStats()
}

// LogError 记录错误（便捷方法）
func (tm *unifiedTimeManager) LogError(level string, message string, err error, userID *int, sessionID *string, context map[string]interface{}) {
	if tm.errorLogger != nil {
		tm.errorLogger.LogError(level, message, err, userID, sessionID, context)
	}
}

// checkSessionDurationLimit 已移除，现在只依赖余额检查

// GetSessionStats 获取会话统计信息
func (tm *unifiedTimeManager) GetSessionStats(ctx context.Context, userID int) (map[string]interface{}, error) {
	session, err := tm.sessionCache.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("session not found for user %d", userID)
	}

	stats := map[string]interface{}{
		"user_id":                  userID,
		"session_uuid":             session.SessionUUID,
		"meeting_id":               session.MeetingID,
		"is_transcribing":          session.IsTranscribing,
		"is_paused":                session.IsPaused,
		"is_billing_active":        session.SafeGetBillingState(),
		"current_session_time":     session.CurrentSessionTime,
		"paused_session_time":      session.PausedSessionTime,
		"total_duration":           session.TotalDuration,
		"persisted_seconds":        session.PersistedSeconds,
		"remaining_time":           session.RemainingTime,
		"billing_multiplier":       session.BillingMultiplier,
		"transcription_start_time": session.TranscriptionStartTime.Unix(),
		"last_billing_time":        session.LastBillingTime.Unix(),
		"last_update":              session.LastUpdate.Unix(),
	}

	return stats, nil
}

// UpdateSessionMeetingID 更新会话的会议ID
// P1 修复: 使用带超时的 context 代替 context.Background()
func (tm *unifiedTimeManager) UpdateSessionMeetingID(userID int, meetingID int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 使用复合操作更新（一次锁操作）
	if err := tm.sessionCache.UpdateSessionWithMeetingID(ctx, userID, meetingID); err != nil {
		return fmt.Errorf("failed to update meeting ID in cache: %w", err)
	}

	tm.logger.Info("更新会话的会议ID",
		zap.Int("user_id", userID),
		zap.Int("meeting_id", meetingID))

	return nil
}

// CreateInitialUsageLedger 创建初始的 UsageLedger 记录
func (tm *unifiedTimeManager) CreateInitialUsageLedger(ctx context.Context, userID int, meetingID int, metadata map[string]interface{}) error {
	tm.logger.Info("创建初始 UsageLedger 记录",
		zap.Int("user_id", userID),
		zap.Int("meeting_id", meetingID))

	req := dto.AccountConsumeRequest{
		UserID:               userID,
		Seconds:              0,
		Source:               "initial_meeting",
		BusinessID:           meetingID,
		TranscriptionSeconds: 0,
		TranslationSeconds:   0,
	}

	if _, err := tm.accountFacade.Consume(ctx, req); err != nil {
		tm.logger.Error("创建初始 UsageLedger 记录失败",
			zap.Error(err),
			zap.Int("user_id", userID),
			zap.Int("meeting_id", meetingID))
		return fmt.Errorf("创建初始 UsageLedger 记录失败: %w", err)
	}

	tm.logger.Info("初始 UsageLedger 记录创建成功",
		zap.Int("user_id", userID),
		zap.Int("meeting_id", meetingID))

	return nil
}

// CreateInitialUsageLedgerWithTime 创建初始的 UsageLedger 记录，使用指定的累积时间
func (tm *unifiedTimeManager) CreateInitialUsageLedgerWithTime(ctx context.Context, userID int, meetingID int, accumulatedSeconds int) error {
	tm.logger.Info("创建初始 UsageLedger 记录（使用累积时间）",
		zap.Int("user_id", userID),
		zap.Int("meeting_id", meetingID),
		zap.Int("accumulated_seconds", accumulatedSeconds))

	session, exists := tm.GetSessionInfo(userID)
	if !exists {
		return fmt.Errorf("session not found for user %d", userID)
	}

	multiplier := session.getBillingMultiplier()
	if multiplier <= 0 {
		multiplier = 1
	}

	baseSeconds := int64(accumulatedSeconds)
	if baseSeconds < 0 {
		baseSeconds = 0
	}

	req, transcriptionSeconds, translationSeconds := buildUsageRequest(
		userID,
		meetingID,
		baseSeconds,
		multiplier,
		"initial_meeting_accumulated",
	)

	if _, err := tm.accountFacade.Consume(ctx, req); err != nil {
		tm.logger.Error("创建初始 UsageLedger 记录失败（使用累积时间）",
			zap.Error(err),
			zap.Int("user_id", userID),
			zap.Int("meeting_id", meetingID),
			zap.Int("accumulated_seconds", accumulatedSeconds),
			zap.Int("transcription_seconds", transcriptionSeconds),
			zap.Int("translation_seconds", translationSeconds))
		return fmt.Errorf("创建初始 UsageLedger 记录失败: %w", err)
	}

	tm.logger.Info("初始 UsageLedger 记录创建成功（使用累积时间）",
		zap.Int("user_id", userID),
		zap.Int("meeting_id", meetingID),
		zap.Int("accumulated_seconds", accumulatedSeconds),
		zap.Int("transcription_seconds", transcriptionSeconds),
		zap.Int("translation_seconds", translationSeconds))
	return nil
}

func (tm *unifiedTimeManager) CreateAndMarkInitialUsageLedger(ctx context.Context, userID int, meetingID int) error {
	session, err := tm.sessionCache.Get(ctx, userID)
	if err != nil {
		return fmt.Errorf("session not found for user %d", userID)
	}

	_, _, accumulatedBaseSeconds := session.CalculateFinalTimes()
	if accumulatedBaseSeconds < 0 {
		accumulatedBaseSeconds = 0
	}

	if accumulatedBaseSeconds == 0 {
		session.LastBillingTime = time.Now()
		session.LastUpdate = session.LastBillingTime
		return nil
	}

	multiplier := session.getBillingMultiplier()
	if multiplier <= 0 {
		multiplier = 1
	}

	req, transcriptionSeconds, translationSeconds := buildUsageRequest(
		userID,
		meetingID,
		accumulatedBaseSeconds,
		multiplier,
		"initial_meeting_accumulated",
	)

	if _, err := tm.accountFacade.Consume(ctx, req); err != nil {
		tm.logger.Error("创建初始 UsageLedger 记录失败（同步并更新会话状态）",
			zap.Error(err),
			zap.Int("user_id", userID),
			zap.Int("meeting_id", meetingID),
			zap.Int64("accumulated_base_seconds", accumulatedBaseSeconds),
			zap.Int("transcription_seconds", transcriptionSeconds),
			zap.Int("translation_seconds", translationSeconds))
		return fmt.Errorf("创建初始 UsageLedger 记录失败: %w", err)
	}

	now := time.Now()
	tm.mu.Lock()
	session.PersistedBaseSeconds += accumulatedBaseSeconds
	session.PersistedSeconds += int64(transcriptionSeconds + translationSeconds)
	session.ConsumedSeconds = 0
	session.LastBillingTime = now
	session.LastUpdate = now
	tm.mu.Unlock()

	tm.logger.Info("初始 UsageLedger 记录创建成功并更新会话落账状态",
		zap.Int("user_id", userID),
		zap.Int("meeting_id", meetingID),
		zap.Int64("accumulated_base_seconds", accumulatedBaseSeconds),
		zap.Int("transcription_seconds", transcriptionSeconds),
		zap.Int("translation_seconds", translationSeconds),
		zap.Int64("persisted_base_seconds", session.PersistedBaseSeconds),
		zap.Int64("persisted_seconds", session.PersistedSeconds))
	return nil
}

// ...
