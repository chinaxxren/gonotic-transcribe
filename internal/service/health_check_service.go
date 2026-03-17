// Package service 提供业务逻辑实现
package service

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// HealthCheckService 健康检查服务
type HealthCheckService struct {
	timeManager UnifiedTimeManager
	logger      *zap.Logger
	mu          sync.RWMutex
	running     bool
	cancel      context.CancelFunc
	lastCheck   time.Time
	healthStats *HealthStats
}

// HealthStats 健康统计
type HealthStats struct {
	LastCheckTime     time.Time `json:"last_check_time"`
	TotalChecks       int64     `json:"total_checks"`
	HealthyChecks     int64     `json:"healthy_checks"`
	UnhealthyChecks   int64     `json:"unhealthy_checks"`
	TimeManagerHealth bool      `json:"time_manager_health"`
	OverallHealth     bool      `json:"overall_health"`
}

// TimeManagerHealthCheckConfig 时间管理器健康检查配置
type TimeManagerHealthCheckConfig struct {
	CheckInterval      time.Duration // 检查间隔
	TimeoutDuration    time.Duration // 检查超时时间
	UnhealthyThreshold int           // 不健康阈值
}

// DefaultTimeManagerHealthCheckConfig 默认时间管理器健康检查配置
func DefaultTimeManagerHealthCheckConfig() *TimeManagerHealthCheckConfig {
	return &TimeManagerHealthCheckConfig{
		CheckInterval:      30 * time.Second, // 每30秒检查一次
		TimeoutDuration:    5 * time.Second,  // 5秒超时
		UnhealthyThreshold: 3,                // 连续3次失败认为不健康
	}
}

// NewHealthCheckService 创建健康检查服务
func NewHealthCheckService(timeManager UnifiedTimeManager, logger *zap.Logger) *HealthCheckService {
	return &HealthCheckService{
		timeManager: timeManager,
		logger:      logger,
		healthStats: &HealthStats{},
	}
}

// Start 启动健康检查服务
func (h *HealthCheckService) Start(ctx context.Context, config *TimeManagerHealthCheckConfig) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.running {
		return nil // 已经在运行
	}

	if config == nil {
		config = DefaultTimeManagerHealthCheckConfig()
	}

	ctx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	h.running = true

	go h.healthCheckLoop(ctx, config)

	h.logger.Info("健康检查服务已启动",
		zap.Duration("check_interval", config.CheckInterval),
		zap.Duration("timeout", config.TimeoutDuration))

	return nil
}

// Stop 停止健康检查服务
func (h *HealthCheckService) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return
	}

	if h.cancel != nil {
		h.cancel()
	}
	h.running = false

	h.logger.Info("健康检查服务已停止")
}

// IsRunning 检查服务是否运行中
func (h *HealthCheckService) IsRunning() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.running
}

// GetHealthStats 获取健康统计
func (h *HealthCheckService) GetHealthStats() *HealthStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 返回副本
	stats := *h.healthStats
	return &stats
}

// healthCheckLoop 健康检查循环
func (h *HealthCheckService) healthCheckLoop(ctx context.Context, config *TimeManagerHealthCheckConfig) {
	ticker := time.NewTicker(config.CheckInterval)
	defer ticker.Stop()

	// 立即执行一次检查
	h.performHealthCheck(ctx, config)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.performHealthCheck(ctx, config)
		}
	}
}

// performHealthCheck 执行健康检查
func (h *HealthCheckService) performHealthCheck(ctx context.Context, config *TimeManagerHealthCheckConfig) {
	start := time.Now()

	// 创建超时上下文
	checkCtx, cancel := context.WithTimeout(ctx, config.TimeoutDuration)
	defer cancel()

	// 检查时间管理器健康状态
	timeManagerHealthy := h.checkTimeManagerHealth(checkCtx)

	// 计算总体健康状态
	overallHealthy := timeManagerHealthy

	// 更新统计
	h.mu.Lock()
	h.healthStats.LastCheckTime = start
	h.healthStats.TotalChecks++
	h.healthStats.TimeManagerHealth = timeManagerHealthy
	h.healthStats.OverallHealth = overallHealthy

	if overallHealthy {
		h.healthStats.HealthyChecks++
	} else {
		h.healthStats.UnhealthyChecks++
	}
	h.lastCheck = start
	h.mu.Unlock()

	duration := time.Since(start)

	if overallHealthy {
		h.logger.Debug("健康检查完成",
			zap.Duration("duration", duration),
			zap.Bool("time_manager", timeManagerHealthy))
	} else {
		h.logger.Warn("健康检查发现问题",
			zap.Duration("duration", duration),
			zap.Bool("time_manager", timeManagerHealthy))
	}
}

// checkTimeManagerHealth 检查时间管理器健康状态
func (h *HealthCheckService) checkTimeManagerHealth(ctx context.Context) bool {
	// 检查时间管理器是否响应
	stats := h.timeManager.GetStats()
	if stats == nil {
		h.logger.Error("时间管理器统计信息为空")
		return false
	}

	// 检查活跃会话数是否合理
	activeSessions, ok := stats["active_sessions"]
	if !ok {
		h.logger.Error("无法获取活跃会话数")
		return false
	}

	// 检查是否有异常多的会话（可能表示内存泄漏）
	if activeSessions > 10000 {
		h.logger.Warn("活跃会话数异常高",
			zap.Int64("active_sessions", activeSessions))
		return false
	}

	return true
}

// IsHealthy 检查系统是否健康
func (h *HealthCheckService) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 如果最近没有检查，认为不健康
	if time.Since(h.lastCheck) > 2*time.Minute {
		return false
	}

	return h.healthStats.OverallHealth
}
