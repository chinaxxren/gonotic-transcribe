// Package service 提供业务逻辑实现
package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// SessionHealthChecker 会话健康检查器
type SessionHealthChecker struct {
	sessionManager *WebSocketSessionManager
	timeManager    UnifiedTimeManager
	config         *HealthCheckConfig
	isRunning      bool
	stopChan       chan struct{}
	stats          *HealthCheckStats // 统计信息
	mu             sync.RWMutex
	logger         *zap.Logger
}

// HealthCheckStats 健康检查统计信息
type HealthCheckStats struct {
	TotalChecks          int64     // 总检查次数
	TotalTimeoutCleaned  int64     // 超时清理次数
	TotalZombieCleaned   int64     // 僵尸清理次数
	TotalDurationCleaned int64     // 时长超限清理次数
	LastCheckTime        time.Time // 最后检查时间
	mu                   sync.RWMutex
}

// HealthCheckConfig 健康检查配置
type HealthCheckConfig struct {
	CheckInterval        time.Duration // 检查间隔
	SessionTimeout       time.Duration // 会话超时时间
	MaxSessionDuration   time.Duration // 最大会话时长
	ZombieThreshold      time.Duration // 僵尸会话阈值（无活动时间）
	PausedSessionTimeout time.Duration // 暂停会话超时时间
	DurationWarningTime  time.Duration // 时长警告时间
}

// DefaultHealthCheckConfig 默认健康检查配置
func DefaultHealthCheckConfig() *HealthCheckConfig {
	return &HealthCheckConfig{
		CheckInterval:        1 * time.Minute,  // 每分钟检查一次
		SessionTimeout:       24 * time.Hour,   // 24小时超时
		MaxSessionDuration:   5 * time.Hour,    // 最大5小时
		ZombieThreshold:      30 * time.Minute, // 30分钟无活动（转录中状态）
		PausedSessionTimeout: 24 * time.Hour,   // 24小时（暂停状态）
		DurationWarningTime:  4*time.Hour + 30*time.Minute,
	}
}

// NewSessionHealthChecker 创建会话健康检查器
func NewSessionHealthChecker(
	sessionManager *WebSocketSessionManager,
	timeManager UnifiedTimeManager,
	config *HealthCheckConfig,
	logger *zap.Logger,
) *SessionHealthChecker {
	if config == nil {
		config = DefaultHealthCheckConfig()
	}

	return &SessionHealthChecker{
		sessionManager: sessionManager,
		timeManager:    timeManager,
		config:         config,
		stopChan:       make(chan struct{}),
		stats:          &HealthCheckStats{},
		logger:         logger,
	}
}

// Start 启动健康检查
func (h *SessionHealthChecker) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.isRunning {
		h.mu.Unlock()
		return fmt.Errorf("健康检查已在运行中")
	}
	h.isRunning = true
	h.mu.Unlock()

	h.logger.Info("启动会话健康检查",
		zap.Duration("check_interval", h.config.CheckInterval),
		zap.Duration("session_timeout", h.config.SessionTimeout),
		zap.Duration("zombie_threshold", h.config.ZombieThreshold))

	go h.healthCheckLoop(ctx)

	return nil
}

// Stop 停止健康检查
func (h *SessionHealthChecker) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.isRunning {
		return fmt.Errorf("健康检查未运行")
	}

	close(h.stopChan)
	h.isRunning = false

	h.logger.Info("会话健康检查已停止")

	return nil
}

// IsRunning 检查是否正在运行
func (h *SessionHealthChecker) IsRunning() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.isRunning
}

// healthCheckLoop 健康检查循环
func (h *SessionHealthChecker) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(h.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("健康检查循环退出（context 取消）")
			return
		case <-h.stopChan:
			h.logger.Info("健康检查循环退出（手动停止）")
			return
		case <-ticker.C:
			h.performHealthCheck(ctx)
		}
	}
}

// performHealthCheck 执行健康检查
func (h *SessionHealthChecker) performHealthCheck(ctx context.Context) {
	h.logger.Debug("执行会话健康检查")

	// 获取所有会话
	sessions := h.sessionManager.GetAllSessions()

	h.logger.Debug("检查会话健康",
		zap.Int("total_sessions", len(sessions)))

	now := time.Now()
	var (
		timeoutCount  int
		zombieCount   int
		durationCount int
	)

	for _, session := range sessions {
		// 1. 检查会话超时（24小时）
		sessionAge := now.Sub(session.StartTime)
		if sessionAge > h.config.SessionTimeout {
			h.logger.Warn("发现超时会话",
				zap.Int("user_id", session.UserID),
				zap.Duration("age", sessionAge))

			h.cleanupSession(ctx, session, "session_timeout")
			timeoutCount++
			continue
		}

		// 2. 检查僵尸会话（根据状态使用不同超时时间）
		session.mu.RLock()
		lastActivity := session.LastActivityTime
		isTranscribing := session.IsTranscribing()
		isPaused := session.IsPaused()
		session.mu.RUnlock()

		if !lastActivity.IsZero() {
			inactiveTime := now.Sub(lastActivity)

			// 根据会话状态选择不同的超时时间
			var threshold time.Duration
			var reasonSuffix string

			if isPaused {
				// 暂停状态：24小时超时
				threshold = h.config.PausedSessionTimeout
				reasonSuffix = "paused_session_timeout"
			} else {
				// 转录中或其他状态：30分钟超时
				threshold = h.config.ZombieThreshold
				reasonSuffix = "zombie_session"
			}

			if inactiveTime > threshold {
				h.logger.Warn("发现超时会话",
					zap.Int("user_id", session.UserID),
					zap.Duration("inactive_time", inactiveTime),
					zap.Duration("threshold", threshold),
					zap.Bool("is_paused", isPaused),
					zap.Bool("is_transcribing", isTranscribing))

				h.cleanupSession(ctx, session, reasonSuffix)
				zombieCount++
				continue
			}
		}

		// 3. 检查会话时长超限（5小时）
		if isTranscribing {
			totalDuration := now.Sub(session.StartTime)
			if totalDuration > h.config.MaxSessionDuration {
				h.logger.Warn("发现时长超限会话",
					zap.Int("user_id", session.UserID),
					zap.Duration("duration", totalDuration))

				// 发送时长超限消息
				_ = session.SendMessage(map[string]interface{}{
					"type":     "time_exhausted",
					"message":  "会话时长已达上限，即将自动停止",
					"duration": int(totalDuration.Seconds()),
				})

				h.cleanupSession(ctx, session, "duration_exceeded")
				durationCount++
				continue
			}

			// 4. 发送时长警告（4.5小时）
			if totalDuration > h.config.DurationWarningTime {
				remaining := h.config.MaxSessionDuration - totalDuration
				if remaining > 0 {
					_ = session.SendMessage(map[string]interface{}{
						"type":      "time_warning",
						"message":   "会话即将达到时长上限",
						"remaining": int(remaining.Seconds()),
					})
				}
			}
		}
	}

	// 更新统计信息
	h.stats.mu.Lock()
	h.stats.TotalChecks++
	h.stats.TotalTimeoutCleaned += int64(timeoutCount)
	h.stats.TotalZombieCleaned += int64(zombieCount)
	h.stats.TotalDurationCleaned += int64(durationCount)
	h.stats.LastCheckTime = now
	h.stats.mu.Unlock()

	if timeoutCount > 0 || zombieCount > 0 || durationCount > 0 {
		h.logger.Info("健康检查完成",
			zap.Int("total_sessions", len(sessions)),
			zap.Int("timeout_cleaned", timeoutCount),
			zap.Int("zombie_cleaned", zombieCount),
			zap.Int("duration_cleaned", durationCount))
	}
}

// cleanupSession 清理会话
func (h *SessionHealthChecker) cleanupSession(ctx context.Context, session *WebSocketSession, reason string) {
	h.logger.Info("清理会话",
		zap.Int("user_id", session.UserID),
		zap.String("reason", reason))

	// 1. 发送会话结束消息
	_ = session.SendMessage(map[string]interface{}{
		"type":    "session_end",
		"reason":  reason,
		"message": "会话已被系统清理",
	})

	// 2. 停止时间管理器
	if h.timeManager != nil {
		_, _ = h.timeManager.StopTranscription(
			ctx,
			session.UserID,
			nil,
			nil,
		)
	}

	// 3. 关闭 Remote 连接
	session.mu.Lock()
	if session.RemoteConn != nil {
		_ = session.RemoteConn.Close()
		session.RemoteConn = nil
	}
	session.mu.Unlock()
	session.SetTranscribing(false)
	session.SetPaused(false)

	// 4. 关闭 WebSocket 连接
	_ = session.Close()

	// 5. 从会话管理器中移除
	h.sessionManager.RemoveSession(session.UserID)

	h.logger.Info("会话已清理",
		zap.Int("user_id", session.UserID),
		zap.String("reason", reason))
}

// GetStats 获取健康检查统计信息
func (h *SessionHealthChecker) GetStats() map[string]interface{} {
	h.mu.RLock()
	isRunning := h.isRunning
	h.mu.RUnlock()

	h.stats.mu.RLock()
	defer h.stats.mu.RUnlock()

	return map[string]interface{}{
		"is_running":             isRunning,
		"check_interval":         h.config.CheckInterval.String(),
		"session_timeout":        h.config.SessionTimeout.String(),
		"max_session_duration":   h.config.MaxSessionDuration.String(),
		"zombie_threshold":       h.config.ZombieThreshold.String(),
		"duration_warning_time":  h.config.DurationWarningTime.String(),
		"total_checks":           h.stats.TotalChecks,
		"total_timeout_cleaned":  h.stats.TotalTimeoutCleaned,
		"total_zombie_cleaned":   h.stats.TotalZombieCleaned,
		"total_duration_cleaned": h.stats.TotalDurationCleaned,
		"last_check_time":        h.stats.LastCheckTime.Format(time.RFC3339),
	}
}

// GetHealthReport 获取详细的健康报告
func (h *SessionHealthChecker) GetHealthReport() map[string]interface{} {
	// 获取所有会话
	sessions := h.sessionManager.GetAllSessions()

	now := time.Now()
	var (
		activeSessions       int
		pausedSessions       int
		transcribingSessions int
		oldestSessionAge     time.Duration
		newestSessionAge     time.Duration
		avgSessionAge        time.Duration
		totalSessionAge      time.Duration
	)

	for i, session := range sessions {
		session.mu.RLock()
		isTranscribing := session.IsTranscribing()
		isPaused := session.IsPaused()
		startTime := session.StartTime
		session.mu.RUnlock()

		sessionAge := now.Sub(startTime)
		totalSessionAge += sessionAge

		if i == 0 || sessionAge > oldestSessionAge {
			oldestSessionAge = sessionAge
		}
		if i == 0 || sessionAge < newestSessionAge {
			newestSessionAge = sessionAge
		}

		if isTranscribing {
			transcribingSessions++
			activeSessions++
		} else if isPaused {
			pausedSessions++
		}
	}

	if len(sessions) > 0 {
		avgSessionAge = totalSessionAge / time.Duration(len(sessions))
	}

	h.stats.mu.RLock()
	stats := map[string]interface{}{
		"total_checks":           h.stats.TotalChecks,
		"total_timeout_cleaned":  h.stats.TotalTimeoutCleaned,
		"total_zombie_cleaned":   h.stats.TotalZombieCleaned,
		"total_duration_cleaned": h.stats.TotalDurationCleaned,
		"last_check_time":        h.stats.LastCheckTime.Format(time.RFC3339),
	}
	h.stats.mu.RUnlock()

	return map[string]interface{}{
		"timestamp":             now.Format(time.RFC3339),
		"total_sessions":        len(sessions),
		"active_sessions":       activeSessions,
		"paused_sessions":       pausedSessions,
		"transcribing_sessions": transcribingSessions,
		"oldest_session_age":    oldestSessionAge.String(),
		"newest_session_age":    newestSessionAge.String(),
		"average_session_age":   avgSessionAge.String(),
		"health_check_stats":    stats,
		"config": map[string]interface{}{
			"check_interval":        h.config.CheckInterval.String(),
			"session_timeout":       h.config.SessionTimeout.String(),
			"max_session_duration":  h.config.MaxSessionDuration.String(),
			"zombie_threshold":      h.config.ZombieThreshold.String(),
			"duration_warning_time": h.config.DurationWarningTime.String(),
		},
	}
}
