// Package service 提供业务逻辑实现
package service

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// BatchProcessor 批量处理器
type BatchProcessor struct {
	logger     *zap.Logger
	mu         sync.Mutex
	batchQueue []*BillingItem
	processing bool
	config     *BatchProcessorConfig
}

// BillingItem 扣费项目
type BillingItem struct {
	UserID    int
	Seconds   int
	Timestamp time.Time
	SessionID string
}

// BatchProcessorConfig 批量处理器配置
type BatchProcessorConfig struct {
	BatchSize     int           // 批量大小
	FlushInterval time.Duration // 刷新间隔
	MaxWaitTime   time.Duration // 最大等待时间
}

// DefaultBatchProcessorConfig 默认批量处理器配置
func DefaultBatchProcessorConfig() *BatchProcessorConfig {
	return &BatchProcessorConfig{
		BatchSize:     50,               // 50个项目一批
		FlushInterval: 5 * time.Second,  // 每5秒刷新一次
		MaxWaitTime:   30 * time.Second, // 最多等待30秒
	}
}

// NewBatchProcessor 创建批量处理器
func NewBatchProcessor(logger *zap.Logger) *BatchProcessor {
	return &BatchProcessor{
		logger:     logger,
		batchQueue: make([]*BillingItem, 0),
		config:     DefaultBatchProcessorConfig(),
	}
}

// SetConfig 设置配置
func (bp *BatchProcessor) SetConfig(config *BatchProcessorConfig) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.config = config
}

// AddBillingItem 添加扣费项目
func (bp *BatchProcessor) AddBillingItem(userID int, seconds int, sessionID string) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	item := &BillingItem{
		UserID:    userID,
		Seconds:   seconds,
		Timestamp: time.Now(),
		SessionID: sessionID,
	}

	bp.batchQueue = append(bp.batchQueue, item)

	// 检查是否需要立即处理
	if len(bp.batchQueue) >= bp.config.BatchSize {
		go bp.processBatch()
	}
}

// StartAutoFlush 启动自动刷新
func (bp *BatchProcessor) StartAutoFlush(ctx context.Context) {
	ticker := time.NewTicker(bp.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// 处理剩余项目
			bp.Flush()
			return
		case <-ticker.C:
			bp.checkAndFlush()
		}
	}
}

// checkAndFlush 检查并刷新
func (bp *BatchProcessor) checkAndFlush() {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if len(bp.batchQueue) == 0 {
		return
	}

	// 检查最老的项目是否超过最大等待时间
	oldestItem := bp.batchQueue[0]
	if time.Since(oldestItem.Timestamp) >= bp.config.MaxWaitTime {
		go bp.processBatch()
	}
}

// Flush 强制刷新所有项目
func (bp *BatchProcessor) Flush() {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if len(bp.batchQueue) > 0 {
		go bp.processBatch()
	}
}

// processBatch 处理批量项目
func (bp *BatchProcessor) processBatch() {
	bp.mu.Lock()
	if bp.processing || len(bp.batchQueue) == 0 {
		bp.mu.Unlock()
		return
	}

	bp.processing = true
	items := make([]*BillingItem, len(bp.batchQueue))
	copy(items, bp.batchQueue)
	bp.batchQueue = bp.batchQueue[:0] // 清空队列
	bp.mu.Unlock()

	defer func() {
		bp.mu.Lock()
		bp.processing = false
		bp.mu.Unlock()
	}()

	start := time.Now()

	// 按用户分组
	userGroups := make(map[int][]*BillingItem)
	for _, item := range items {
		userGroups[item.UserID] = append(userGroups[item.UserID], item)
	}

	// 批量处理每个用户的扣费
	successCount := 0
	failCount := 0

	for userID, userItems := range userGroups {
		totalSeconds := 0
		for _, item := range userItems {
			totalSeconds += item.Seconds
		}

		// TODO: 实现批量扣费逻辑
		// 这里需要调用时间管理器的扣费方法

		bp.logger.Debug("批量扣费处理",
			zap.Int("user_id", userID),
			zap.Int("total_seconds", totalSeconds),
			zap.Int("item_count", len(userItems)))

		successCount += len(userItems)
	}

	duration := time.Since(start)

	bp.logger.Info("批量处理完成",
		zap.Int("total_items", len(items)),
		zap.Int("success_count", successCount),
		zap.Int("fail_count", failCount),
		zap.Int("user_groups", len(userGroups)),
		zap.Duration("duration", duration))
}

// GetStats 获取批量处理器统计
func (bp *BatchProcessor) GetStats() map[string]interface{} {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	return map[string]interface{}{
		"queue_size":     len(bp.batchQueue),
		"processing":     bp.processing,
		"batch_size":     bp.config.BatchSize,
		"flush_interval": bp.config.FlushInterval.String(),
		"max_wait_time":  bp.config.MaxWaitTime.String(),
	}
}
