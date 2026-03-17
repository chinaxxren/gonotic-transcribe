// Package service 提供业务逻辑实现
package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"go.uber.org/zap"
)

// APIKeyConfig API 密钥配置
type APIKeyConfig struct {
	Key            string `json:"key"`             // API 密钥
	Name           string `json:"name"`            // 密钥名称
	MaxConnections int    `json:"max_connections"` // 最大连接数
	Weight         int    `json:"weight"`          // 权重
}

// APIKeyStatus API 密钥状态
type APIKeyStatus struct {
	Config             *APIKeyConfig // 配置
	CurrentConnections int           // 当前连接数
	TotalRequests      int64         // 总请求数
	FailedRequests     int64         // 失败请求数
	LastUsed           time.Time     // 最后使用时间
	IsHealthy          bool          // 是否健康
	LastHealthCheck    time.Time     // 最后健康检查时间
}

// EnterpriseManager 企业级管理器
type EnterpriseManager struct {
	apiKeys       []*APIKeyStatus // API 密钥列表
	mu            sync.RWMutex    // 读写锁
	strategy      string          // 负载均衡策略
	rrCursor      int             // round-robin 游标
	healthChecker *time.Ticker    // 健康检查定时器
	stopChan      chan struct{}   // 停止信号
	logger        *zap.Logger     // 日志记录器
}

// NewEnterpriseManager 创建企业级管理器
//
// 参数:
//   - configJSON: JSON 格式的配置字符串
//   - strategy: 负载均衡策略（least_connections, round_robin, weighted）
//   - logger: 日志记录器
//
// 返回:
//   - *EnterpriseManager: 企业级管理器实例
//   - error: 如果配置解析失败返回错误
func NewEnterpriseManager(configJSON string, strategy string, logger *zap.Logger) (*EnterpriseManager, error) {
	// 解析配置
	var configs []APIKeyConfig
	if err := json.Unmarshal([]byte(configJSON), &configs); err != nil {
		return nil, err
	}

	// 创建密钥状态列表
	apiKeys := make([]*APIKeyStatus, 0, len(configs))
	for i := range configs {
		apiKeys = append(apiKeys, &APIKeyStatus{
			Config:             &configs[i],
			CurrentConnections: 0,
			TotalRequests:      0,
			FailedRequests:     0,
			LastUsed:           time.Now(),
			IsHealthy:          true,
			LastHealthCheck:    time.Now(),
		})
	}

	em := &EnterpriseManager{
		apiKeys:  apiKeys,
		strategy: strategy,
		stopChan: make(chan struct{}),
		logger:   logger,
	}

	logger.Info("企业级管理器初始化成功",
		zap.Int("api_key_count", len(apiKeys)),
		zap.String("strategy", strategy))

	return em, nil
}

// GetAPIKey 获取可用的 API 密钥
//
// 根据负载均衡策略选择最优的 API 密钥
//
// 返回:
//   - string: API 密钥
//   - error: 如果没有可用密钥返回错误
func (em *EnterpriseManager) GetAPIKey() (string, error) {
	em.mu.Lock()
	defer em.mu.Unlock()

	// 根据策略选择密钥
	var selected *APIKeyStatus
	switch em.strategy {
	case "least_connections":
		selected = em.selectLeastConnections()
	case "weighted":
		selected = em.selectWeighted()
	case "round_robin":
		selected = em.selectRoundRobin()
	default:
		selected = em.selectLeastConnections()
	}

	if selected == nil {
		return "", ErrNoAvailableAPIKey
	}

	// 增加连接数
	selected.CurrentConnections++
	selected.TotalRequests++
	selected.LastUsed = time.Now()

	em.logger.Debug("选择 API 密钥",
		zap.String("name", selected.Config.Name),
		zap.Int("current_connections", selected.CurrentConnections),
		zap.String("strategy", em.strategy))

	return selected.Config.Key, nil
}

// ReleaseAPIKey 释放 API 密钥连接
//
// 参数:
//   - key: API 密钥
func (em *EnterpriseManager) ReleaseAPIKey(key string) {
	em.mu.Lock()
	defer em.mu.Unlock()

	for _, status := range em.apiKeys {
		if status.Config.Key == key && status.CurrentConnections > 0 {
			status.CurrentConnections--
			em.logger.Debug("释放 API 密钥",
				zap.String("name", status.Config.Name),
				zap.Int("current_connections", status.CurrentConnections))
			return
		}
	}
}

// RecordFailure 记录失败请求
//
// 参数:
//   - key: API 密钥
func (em *EnterpriseManager) RecordFailure(key string) {
	em.mu.Lock()
	defer em.mu.Unlock()

	for _, status := range em.apiKeys {
		if status.Config.Key == key {
			status.FailedRequests++

			// 如果失败率过高，标记为不健康
			if status.TotalRequests > 10 {
				failureRate := float64(status.FailedRequests) / float64(status.TotalRequests)
				if failureRate > 0.5 {
					status.IsHealthy = false
					em.logger.Warn("API 密钥标记为不健康",
						zap.String("name", status.Config.Name),
						zap.Float64("failure_rate", failureRate))
				}
			}
			return
		}
	}
}

// HasAPIKey checks if the provided key exists in the manager configuration.
//
// Returns:
//   - bool: True if the key is managed by the enterprise manager
func (em *EnterpriseManager) HasAPIKey(key string) bool {
	em.mu.RLock()
	defer em.mu.RUnlock()

	for _, status := range em.apiKeys {
		if status.Config.Key == key {
			return true
		}
	}

	return false
}

// selectLeastConnections 选择连接数最少的密钥
func (em *EnterpriseManager) selectLeastConnections() *APIKeyStatus {
	var selected *APIKeyStatus
	minConnections := int(^uint(0) >> 1) // Max int
	var oldest time.Time

	for _, status := range em.apiKeys {
		if !status.IsHealthy {
			continue
		}

		if status.CurrentConnections >= status.Config.MaxConnections {
			continue
		}

		// 优先选择连接数最少的；若连接数相同，则选择最久未使用的，避免一直落到第一个 key。
		if selected == nil || status.CurrentConnections < minConnections {
			minConnections = status.CurrentConnections
			oldest = status.LastUsed
			selected = status
			continue
		}
		if status.CurrentConnections == minConnections {
			if status.LastUsed.Before(oldest) {
				oldest = status.LastUsed
				selected = status
			}
		}
	}

	return selected
}

// selectWeighted 根据权重选择密钥
func (em *EnterpriseManager) selectWeighted() *APIKeyStatus {
	var selected *APIKeyStatus
	bestScore := -1.0
	var oldest time.Time

	for _, status := range em.apiKeys {
		if !status.IsHealthy {
			continue
		}

		if status.CurrentConnections >= status.Config.MaxConnections {
			continue
		}

		// 计算得分：权重 / (当前连接数 + 1)
		score := float64(status.Config.Weight) / float64(status.CurrentConnections+1)
		if selected == nil || score > bestScore {
			bestScore = score
			oldest = status.LastUsed
			selected = status
			continue
		}
		// 得分相同则选择最久未使用的，避免偏向第一个 key。
		if score == bestScore {
			if status.LastUsed.Before(oldest) {
				oldest = status.LastUsed
				selected = status
			}
		}
	}

	return selected
}

// selectRoundRobin 轮询选择密钥
func (em *EnterpriseManager) selectRoundRobin() *APIKeyStatus {
	// 真正的 round-robin：从 rrCursor 开始循环寻找下一个可用 key。
	if len(em.apiKeys) == 0 {
		return nil
	}
	start := em.rrCursor % len(em.apiKeys)
	for i := 0; i < len(em.apiKeys); i++ {
		idx := (start + i) % len(em.apiKeys)
		status := em.apiKeys[idx]
		if !status.IsHealthy {
			continue
		}
		if status.CurrentConnections >= status.Config.MaxConnections {
			continue
		}
		em.rrCursor = idx + 1
		return status
	}
	return nil
}

// GetStats 获取统计信息
//
// 返回:
//   - map[string]interface{}: 统计信息
func (em *EnterpriseManager) GetStats() map[string]interface{} {
	em.mu.RLock()
	defer em.mu.RUnlock()

	keys := make([]map[string]interface{}, 0, len(em.apiKeys))
	totalConnections := 0
	totalRequests := int64(0)
	totalFailures := int64(0)
	healthyCount := 0

	for _, status := range em.apiKeys {
		totalConnections += status.CurrentConnections
		totalRequests += status.TotalRequests
		totalFailures += status.FailedRequests
		if status.IsHealthy {
			healthyCount++
		}

		failureRate := 0.0
		if status.TotalRequests > 0 {
			failureRate = float64(status.FailedRequests) / float64(status.TotalRequests)
		}

		keys = append(keys, map[string]interface{}{
			"name":                status.Config.Name,
			"current_connections": status.CurrentConnections,
			"max_connections":     status.Config.MaxConnections,
			"weight":              status.Config.Weight,
			"total_requests":      status.TotalRequests,
			"failed_requests":     status.FailedRequests,
			"failure_rate":        failureRate,
			"is_healthy":          status.IsHealthy,
			"last_used":           status.LastUsed.Unix(),
			"last_health_check":   status.LastHealthCheck.Unix(),
		})
	}

	return map[string]interface{}{
		"strategy":          em.strategy,
		"total_keys":        len(em.apiKeys),
		"healthy_keys":      healthyCount,
		"total_connections": totalConnections,
		"total_requests":    totalRequests,
		"total_failures":    totalFailures,
		"keys":              keys,
	}
}

// StartHealthCheck 启动健康检查
//
// 参数:
//   - ctx: 上下文
//   - interval: 检查间隔
func (em *EnterpriseManager) StartHealthCheck(ctx context.Context, interval time.Duration) {
	em.healthChecker = time.NewTicker(interval)

	go func() {
		em.logger.Info("健康检查已启动",
			zap.Duration("interval", interval))

		for {
			select {
			case <-em.healthChecker.C:
				em.performHealthCheck()
			case <-em.stopChan:
				em.logger.Info("健康检查已停止")
				return
			case <-ctx.Done():
				em.logger.Info("健康检查因上下文取消而停止")
				return
			}
		}
	}()
}

// StopHealthCheck 停止健康检查
func (em *EnterpriseManager) StopHealthCheck() {
	if em.healthChecker != nil {
		em.healthChecker.Stop()
	}
	close(em.stopChan)
}

// performHealthCheck 执行健康检查
func (em *EnterpriseManager) performHealthCheck() {
	em.mu.Lock()
	defer em.mu.Unlock()

	now := time.Now()
	for _, status := range em.apiKeys {
		status.LastHealthCheck = now

		// 如果失败率低于阈值，恢复健康状态
		if !status.IsHealthy && status.TotalRequests > 10 {
			failureRate := float64(status.FailedRequests) / float64(status.TotalRequests)
			if failureRate < 0.3 {
				status.IsHealthy = true
				em.logger.Info("API 密钥恢复健康",
					zap.String("name", status.Config.Name),
					zap.Float64("failure_rate", failureRate))
			}
		}

		// 重置统计（每小时）
		if status.TotalRequests > 1000 {
			status.TotalRequests = status.TotalRequests / 2
			status.FailedRequests = status.FailedRequests / 2
		}
	}
}

// ErrNoAvailableAPIKey 没有可用的 API 密钥错误
var ErrNoAvailableAPIKey = &EnterpriseError{
	Code:    "NO_AVAILABLE_API_KEY",
	Message: "没有可用的 API 密钥",
}

// EnterpriseError 企业级错误
type EnterpriseError struct {
	Code    string
	Message string
}

// Error 实现 error 接口
func (e *EnterpriseError) Error() string {
	return e.Message
}
