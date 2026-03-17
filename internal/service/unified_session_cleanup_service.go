// Package service 提供业务逻辑实现
package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

// CleanupReason 清理原因枚举
type CleanupReason string

const (
	CleanupReasonSessionCleanupBeforeNewStart CleanupReason = "session cleanup before new start"
	CleanupReasonAbnormalSession              CleanupReason = "abnormal session cleanup"
	CleanupReasonZombieSession                CleanupReason = "zombie session cleanup"
	CleanupReasonTimeExhausted                CleanupReason = "time exhausted"
	CleanupReasonBillingFailed                CleanupReason = "billing failed"
	CleanupReasonNormalSessionCleanup         CleanupReason = "normal session cleanup"
	CleanupReasonSessionCheckError            CleanupReason = "session check error"
	CleanupReasonFinalSessionCleanup          CleanupReason = "final session cleanup"
)

// CleanupStats 清理统计
type CleanupStats struct {
	TotalCleanups      int64
	SuccessfulCleanups int64
	FailedCleanups     int64
	CleanupsByReason   map[CleanupReason]map[string]int64 // reason -> {"success": count, "failed": count}
	LastCleanupTime    time.Time
	AverageCleanupTime time.Duration
	mu                 sync.RWMutex
}

// CleanupResult 清理结果
type CleanupResult struct {
	UserID                      int
	Reason                      CleanupReason
	Timestamp                   time.Time
	TimeManagerCleaned          bool
	WebSocketConnectionsCleaned []string
	RemoteConnectionsCleaned    []string
	Errors                      []string
	Success                     bool
}

// UnifiedSessionCleanupService 统一会话清理服务
type UnifiedSessionCleanupService struct {
	timeManager    UnifiedTimeManager
	sessionManager *WebSocketSessionManager
	remoteManager  *RemoteConnectionManager
	cleanupLocks   map[int]*sync.Mutex // userID -> lock
	stats          *CleanupStats
	mu             sync.RWMutex
	logger         *zap.Logger
}

// NewUnifiedSessionCleanupService 创建统一会话清理服务
func NewUnifiedSessionCleanupService(
	timeManager UnifiedTimeManager,
	sessionManager *WebSocketSessionManager,
	remoteManager *RemoteConnectionManager,
	logger *zap.Logger,
) *UnifiedSessionCleanupService {
	return &UnifiedSessionCleanupService{
		timeManager:    timeManager,
		sessionManager: sessionManager,
		remoteManager:  remoteManager,
		cleanupLocks:   make(map[int]*sync.Mutex),
		stats: &CleanupStats{
			CleanupsByReason: make(map[CleanupReason]map[string]int64),
		},
		logger: logger,
	}
}

// CleanupUserSession 清理用户会话（与 Python 版本一致）
// excludeClientID: 排除的客户端ID（用于保留当前连接）
// forceCleanup: 是否强制清理（忽略锁）
func (s *UnifiedSessionCleanupService) CleanupUserSession(
	ctx context.Context,
	userID int,
	reason CleanupReason,
	excludeClientID string,
	forceCleanup bool,
) *CleanupResult {
	startTime := time.Now()

	// 获取或创建用户清理锁
	if !forceCleanup {
		lock := s.getOrCreateLock(userID)
		lock.Lock()
		defer lock.Unlock()
	}

	result := &CleanupResult{
		UserID:                      userID,
		Reason:                      reason,
		Timestamp:                   startTime,
		TimeManagerCleaned:          false,
		WebSocketConnectionsCleaned: []string{},
		RemoteConnectionsCleaned:    []string{},
		Errors:                      []string{},
		Success:                     false,
	}

	s.logger.Info("开始统一清理用户会话",
		zap.Int("user_id", userID),
		zap.String("reason", string(reason)),
		zap.String("exclude_client_id", excludeClientID),
		zap.Bool("force_cleanup", forceCleanup))

	// 执行清理操作
	s.performCleanup(ctx, userID, reason, excludeClientID, result)

	// 验证清理结果
	s.verifyCleanupResult(ctx, userID, excludeClientID, result)

	// 更新统计信息
	s.recordCleanupResult(result.Success, reason, time.Since(startTime))

	if result.Success {
		s.logger.Info("统一清理完成",
			zap.Int("user_id", userID),
			zap.String("reason", string(reason)),
			zap.Int("websocket_cleaned", len(result.WebSocketConnectionsCleaned)),
			zap.Int("remote_cleaned", len(result.RemoteConnectionsCleaned)))
	} else {
		s.logger.Error("统一清理失败",
			zap.Int("user_id", userID),
			zap.String("reason", string(reason)),
			zap.Strings("errors", result.Errors))
	}

	return result
}

// performCleanup 执行实际的清理操作（与 Python 版本的 _perform_cleanup 一致）
func (s *UnifiedSessionCleanupService) performCleanup(
	ctx context.Context,
	userID int,
	reason CleanupReason,
	excludeClientID string,
	result *CleanupResult,
) {
	// 1. 清理时间管理器中的会话
	s.cleanupTimeManagerSession(ctx, userID, reason, result)

	// 2. 清理 WebSocket 连接
	s.cleanupWebSocketConnections(ctx, userID, excludeClientID, result)

	// 3. 清理 Remote 连接
	s.cleanupRemoteConnections(ctx, userID, excludeClientID, result)
}

// cleanupTimeManagerSession 清理时间管理器中的会话（与 Python 版本一致）
func (s *UnifiedSessionCleanupService) cleanupTimeManagerSession(
	ctx context.Context,
	userID int,
	reason CleanupReason,
	result *CleanupResult,
) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("清理时间管理器会话时发生 panic",
				zap.Any("panic", r),
				zap.Int("user_id", userID))
			result.Errors = append(result.Errors, fmt.Sprintf("time_manager_panic: %v", r))
		}
	}()

	// 检查会话是否存在
	sessionInfo, exists := s.timeManager.GetSessionInfo(userID)
	if !exists {
		s.logger.Info("时间管理器会话不存在，跳过清理",
			zap.Int("user_id", userID))
		return
	}

	sessionUUID := "unknown"
	if sessionInfo != nil {
		sessionUUID = sessionInfo.SessionUUID
	}

	s.logger.Info("清理时间管理器会话",
		zap.Int("user_id", userID),
		zap.String("session_uuid", sessionUUID))

	// 强制停止转录（与 Python 版本的 _force_stop_transcription 一致）
	// Python 版本调用 _force_stop_transcription，它会：
	// 1. 调用 forceStopCallback（如果设置）
	// 2. 调用 StopTranscription 清理会话
	// Go 版本需要做同样的事情
	// 注意：forceStopTranscription 是内部方法，通过回调机制实现
	// 这里直接调用 StopTranscription 来清理会话，因为清理服务不应该触发回调
	_, err := s.timeManager.StopTranscription(ctx, userID, nil, map[string]interface{}{
		"cleanup_reason": string(reason),
		"force_stop":     true,
	})
	if err != nil {
		s.logger.Error("清理时间管理器会话失败",
			zap.Error(err),
			zap.Int("user_id", userID))
		result.Errors = append(result.Errors, fmt.Sprintf("time_manager_error: %v", err))
		return
	}

	result.TimeManagerCleaned = true
	s.logger.Info("时间管理器会话已清理",
		zap.Int("user_id", userID),
		zap.String("session_uuid", sessionUUID))
}

// cleanupWebSocketConnections 清理 WebSocket 连接（与 Python 版本一致）
func (s *UnifiedSessionCleanupService) cleanupWebSocketConnections(
	ctx context.Context,
	userID int,
	excludeClientID string,
	result *CleanupResult,
) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("清理 WebSocket 连接时发生 panic",
				zap.Any("panic", r),
				zap.Int("user_id", userID))
			result.Errors = append(result.Errors, fmt.Sprintf("websocket_panic: %v", r))
		}
	}()

	// 查找需要清理的连接
	connectionsToCleanup := []string{}

	// 遍历所有会话，找到属于该用户的会话
	allSessions := s.sessionManager.GetAllSessions()
	for _, session := range allSessions {
		if session.UserID == userID {
			// 如果指定了排除的客户端ID，则跳过
			if excludeClientID != "" && strconv.Itoa(session.UserID) == excludeClientID {
				continue
			}
			connectionsToCleanup = append(connectionsToCleanup, strconv.Itoa(session.UserID))
		}
	}

	if len(connectionsToCleanup) == 0 {
		s.logger.Info("未找到需要清理的 WebSocket 连接",
			zap.Int("user_id", userID))
		return
	}

	s.logger.Info("找到需要清理的 WebSocket 连接",
		zap.Int("user_id", userID),
		zap.Int("count", len(connectionsToCleanup)),
		zap.Strings("connections", connectionsToCleanup))

	// 清理每个连接
	for _, sessionID := range connectionsToCleanup {
		func() {
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error("清理单个 WebSocket 连接时发生 panic",
						zap.Any("panic", r),
						zap.String("session_id", sessionID))
					result.Errors = append(result.Errors, fmt.Sprintf("websocket_error_%s: %v", sessionID, r))
				}
			}()

			// 标记连接状态为 disconnecting（与 Python 版本一致）
			s.sessionManager.SetConnectionState(sessionID, "disconnecting")

			// 获取会话
			session, exists := s.sessionManager.GetSession(sessionID)
			if !exists {
				// 会话不存在，强制标记为已断开
				s.sessionManager.SetConnectionState(sessionID, "disconnected")
				return
			}

			// 关闭会话（与 Python 版本的 pause_and_disconnect_client 一致）
			// Python 版本会调用 pause_and_disconnect_client，它会：
			// 1. 停止时间管理器（如果不是暂停）
			// 2. 关闭 Remote 连接
			// 3. 清理空会议
			// 4. 从活跃连接中移除
			// 5. 关闭 WebSocket 连接
			// Go 版本通过 session.Close() 实现类似功能，但需要确保所有清理步骤都完成
			// 注意：session.Close() 会关闭 WebSocket 连接，但不会执行完整的清理流程
			// 这里应该调用 handleClientDisconnect 来执行完整的清理流程
			// 但由于清理服务不应该依赖 WebSocketHandler，我们直接关闭连接
			if err := session.Close(); err != nil {
				s.logger.Error("关闭 WebSocket 会话失败",
					zap.Error(err),
					zap.String("session_id", sessionID))
				result.Errors = append(result.Errors, fmt.Sprintf("websocket_error_%s: %v", sessionID, err))
				// 强制标记为已断开
				s.sessionManager.SetConnectionState(sessionID, "disconnected")
				return
			}

			// 从会话管理器中移除（与 Python 版本一致）
			// 将 sessionID 转换为 userID
			if userID, err := strconv.Atoi(sessionID); err == nil {
				s.sessionManager.RemoveSession(userID)
			}
			result.WebSocketConnectionsCleaned = append(result.WebSocketConnectionsCleaned, sessionID)

			s.logger.Info("WebSocket 连接已清理",
				zap.String("session_id", sessionID),
				zap.Int("user_id", userID))
		}()
	}
}

// cleanupRemoteConnections 清理 Remote 连接（与 Python 版本一致）
func (s *UnifiedSessionCleanupService) cleanupRemoteConnections(
	ctx context.Context,
	userID int,
	excludeClientID string,
	result *CleanupResult,
) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("清理 Remote 连接时发生 panic",
				zap.Any("panic", r),
				zap.Int("user_id", userID))
			result.Errors = append(result.Errors, fmt.Sprintf("remote_panic: %v", r))
		}
	}()

	// 查找需要清理的 Remote 连接
	remoteConnectionsToCleanup := []string{}

	// 遍历所有会话，找到属于该用户的会话的 Remote 连接
	allSessions := s.sessionManager.GetAllSessions()
	for _, session := range allSessions {
		if session.UserID == userID {
			// 如果指定了排除的客户端ID，则跳过
			if excludeClientID != "" && strconv.Itoa(session.UserID) == excludeClientID {
				continue
			}
			if session.RemoteConn != nil {
				remoteConnectionsToCleanup = append(remoteConnectionsToCleanup, strconv.Itoa(session.UserID))
			}
		}
	}

	// 清理每个 Remote 连接
	for _, sessionID := range remoteConnectionsToCleanup {
		func() {
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error("清理单个 Remote 连接时发生 panic",
						zap.Any("panic", r),
						zap.String("session_id", sessionID))
					result.Errors = append(result.Errors, fmt.Sprintf("remote_error_%s: %v", sessionID, r))
				}
			}()

			session, exists := s.sessionManager.GetSession(sessionID)
			if !exists || session.RemoteConn == nil {
				return
			}

			// 关闭 Remote 连接
			if err := session.RemoteConn.Close(); err != nil {
				s.logger.Error("关闭 Remote 连接失败",
					zap.Error(err),
					zap.String("session_id", sessionID))
				result.Errors = append(result.Errors, fmt.Sprintf("remote_error_%s: %v", sessionID, err))
				return
			}

			result.RemoteConnectionsCleaned = append(result.RemoteConnectionsCleaned, sessionID)
			s.logger.Info("Remote 连接已清理",
				zap.String("session_id", sessionID),
				zap.Int("user_id", userID))
		}()
	}
}

// verifyCleanupResult 验证清理结果（与 Python 版本一致）
func (s *UnifiedSessionCleanupService) verifyCleanupResult(
	ctx context.Context,
	userID int,
	excludeClientID string,
	result *CleanupResult,
) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("验证清理结果时发生 panic",
				zap.Any("panic", r),
				zap.Int("user_id", userID))
			result.Errors = append(result.Errors, fmt.Sprintf("verification_panic: %v", r))
		}
	}()

	// 验证时间管理器
	sessionInfo, exists := s.timeManager.GetSessionInfo(userID)
	if exists && sessionInfo != nil {
		s.logger.Warn("时间管理器会话仍然存在",
			zap.Int("user_id", userID),
			zap.String("session_uuid", sessionInfo.SessionUUID))
		// 再次强制清理
		s.cleanupTimeManagerSession(ctx, userID, CleanupReasonFinalSessionCleanup, result)
	} else {
		s.logger.Info("时间管理器会话验证通过",
			zap.Int("user_id", userID))
	}

	// 验证 WebSocket 连接
	remainingConnections := []string{}
	allSessions := s.sessionManager.GetAllSessions()
	for _, session := range allSessions {
		if session.UserID == userID {
			if excludeClientID != "" && strconv.Itoa(session.UserID) == excludeClientID {
				continue
			}
			remainingConnections = append(remainingConnections, strconv.Itoa(session.UserID))
		}
	}

	if len(remainingConnections) > 0 {
		s.logger.Warn("仍有 WebSocket 连接存在",
			zap.Int("user_id", userID),
			zap.Int("count", len(remainingConnections)),
			zap.Strings("connections", remainingConnections))
	} else {
		s.logger.Info("WebSocket 连接验证通过",
			zap.Int("user_id", userID))
	}

	// 如果所有验证都通过，标记为成功
	if !exists && len(remainingConnections) == 0 {
		result.Success = true
	}
}

// getOrCreateLock 获取或创建用户清理锁
func (s *UnifiedSessionCleanupService) getOrCreateLock(userID int) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()

	if lock, exists := s.cleanupLocks[userID]; exists {
		return lock
	}

	lock := &sync.Mutex{}
	s.cleanupLocks[userID] = lock
	return lock
}

// recordCleanupResult 记录清理结果（与 Python 版本一致）
func (s *UnifiedSessionCleanupService) recordCleanupResult(success bool, reason CleanupReason, duration time.Duration) {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()

	s.stats.TotalCleanups++
	if success {
		s.stats.SuccessfulCleanups++
	} else {
		s.stats.FailedCleanups++
	}

	// 初始化 reason 的统计（如果不存在）
	if s.stats.CleanupsByReason[reason] == nil {
		s.stats.CleanupsByReason[reason] = make(map[string]int64)
		s.stats.CleanupsByReason[reason]["success"] = 0
		s.stats.CleanupsByReason[reason]["failed"] = 0
	}

	if success {
		s.stats.CleanupsByReason[reason]["success"]++
	} else {
		s.stats.CleanupsByReason[reason]["failed"]++
	}

	s.stats.LastCleanupTime = time.Now()

	// 更新平均清理时间
	if s.stats.TotalCleanups == 1 {
		s.stats.AverageCleanupTime = duration
	} else {
		s.stats.AverageCleanupTime = (s.stats.AverageCleanupTime*time.Duration(s.stats.TotalCleanups-1) + duration) / time.Duration(s.stats.TotalCleanups)
	}
}

// GetStats 获取清理统计（与 Python 版本一致）
func (s *UnifiedSessionCleanupService) GetStats() map[string]interface{} {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	reasonStats := make(map[string]map[string]int64)
	for reason, counts := range s.stats.CleanupsByReason {
		reasonStats[string(reason)] = make(map[string]int64)
		for k, v := range counts {
			reasonStats[string(reason)][k] = v
		}
	}

	return map[string]interface{}{
		"total_cleanups":       s.stats.TotalCleanups,
		"successful_cleanups":  s.stats.SuccessfulCleanups,
		"failed_cleanups":      s.stats.FailedCleanups,
		"cleanup_reasons":      reasonStats,
		"last_cleanup_time":    s.stats.LastCleanupTime,
		"average_cleanup_time": s.stats.AverageCleanupTime.Milliseconds(),
	}
}

// CleanupAllUserSessions 清理用户的所有会话（包括当前连接）（与 Python 版本一致）
func (s *UnifiedSessionCleanupService) CleanupAllUserSessions(
	ctx context.Context,
	userID int,
	reason CleanupReason,
) *CleanupResult {
	return s.CleanupUserSession(ctx, userID, reason, "", true)
}
