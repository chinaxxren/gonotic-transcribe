// Package service 提供业务逻辑实现
package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TranscriptionBatcher P1 优化: 批量写入转录记录，支持高并发（1000+）
// 使用缓冲队列收集转录记录，定时或达到阈值后批量写入数据库
type TranscriptionBatcher struct {
	storage     TranscriptionStorage
	logger      *zap.Logger
	config      TranscriptionBatcherConfig

	mu          sync.Mutex
	buffer      []*TranscriptionRecord
	flushTimer  *time.Timer
	closed      bool
	closeCh     chan struct{}
	flushSem    chan struct{}
	wg          sync.WaitGroup
}

var ErrTranscriptionBatcherFull = fmt.Errorf("transcription batcher buffer is full")

// TranscriptionBatcherConfig 批量写入配置
type TranscriptionBatcherConfig struct {
	// BatchSize 达到此数量后立即写入
	BatchSize int
	// FlushInterval 最大等待时间，超过后强制写入
	FlushInterval time.Duration
	// MaxBufferSize 缓冲区最大容量，超过后拒绝新记录（防止 OOM）
	MaxBufferSize int
	// Workers 并发写入的 worker 数量
	Workers int
}

// DefaultTranscriptionBatcherConfig 返回支持 1000 并发的默认配置
func DefaultTranscriptionBatcherConfig() TranscriptionBatcherConfig {
	return TranscriptionBatcherConfig{
		BatchSize:     100,                   // 100 条一批
		FlushInterval: 500 * time.Millisecond, // 最多等待 500ms
		MaxBufferSize: 10000,                 // 最大缓冲 10000 条（防止 OOM）
		Workers:       10,                    // 10 个并发写入 worker
	}
}

// NewTranscriptionBatcher 创建批量写入器
func NewTranscriptionBatcher(storage TranscriptionStorage, logger *zap.Logger, config *TranscriptionBatcherConfig) *TranscriptionBatcher {
	cfg := DefaultTranscriptionBatcherConfig()
	if config != nil {
		if config.BatchSize > 0 {
			cfg.BatchSize = config.BatchSize
		}
		if config.FlushInterval > 0 {
			cfg.FlushInterval = config.FlushInterval
		}
		if config.MaxBufferSize > 0 {
			cfg.MaxBufferSize = config.MaxBufferSize
		}
		if config.Workers > 0 {
			cfg.Workers = config.Workers
		}
	}

	b := &TranscriptionBatcher{
		storage: storage,
		logger:  logger,
		config:  cfg,
		buffer:  make([]*TranscriptionRecord, 0, cfg.BatchSize),
		closeCh: make(chan struct{}),
		flushSem: make(chan struct{}, cfg.Workers),
	}

	// 启动定时刷新
	b.startFlushLoop()

	return b
}

// Add 添加转录记录到缓冲区（非阻塞）
func (b *TranscriptionBatcher) Add(record *TranscriptionRecord) error {
	if record == nil {
		return nil
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return fmt.Errorf("transcription batcher is closed")
	}

	// 检查缓冲区是否已满
	if len(b.buffer) >= b.config.MaxBufferSize {
		b.mu.Unlock()
		return ErrTranscriptionBatcherFull
	}

	b.buffer = append(b.buffer, record)
	shouldFlush := len(b.buffer) >= b.config.BatchSize
	b.mu.Unlock()

	// 达到批量大小，立即刷新
	if shouldFlush {
		b.flush()
	}

	return nil
}

// startFlushLoop 启动定时刷新循环
func (b *TranscriptionBatcher) startFlushLoop() {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ticker := time.NewTicker(b.config.FlushInterval)
		defer ticker.Stop()

		for {
			select {
			case <-b.closeCh:
				// 关闭前刷新剩余数据
				b.flush()
				return
			case <-ticker.C:
				b.flush()
			}
		}
	}()
}

// flush 刷新缓冲区到数据库
func (b *TranscriptionBatcher) flush() {
	select {
	case b.flushSem <- struct{}{}:
	default:
		return
	}

	b.mu.Lock()
	if len(b.buffer) == 0 {
		b.mu.Unlock()
		<-b.flushSem
		return
	}

	// 取出所有待写入的记录
	records := b.buffer
	b.buffer = make([]*TranscriptionRecord, 0, b.config.BatchSize)
	b.mu.Unlock()

	b.wg.Add(1)
	go func() {
		defer func() {
			<-b.flushSem
			b.wg.Done()
		}()
		b.batchWrite(records)
	}()
}

// batchWrite 批量写入数据库
func (b *TranscriptionBatcher) batchWrite(records []*TranscriptionRecord) {
	if len(records) == 0 || b.storage == nil {
		return
	}

	start := time.Now()

	// P1 优化: 使用真正的批量插入接口，减少数据库往返
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := b.storage.BatchSaveTranscription(ctx, records); err != nil {
		b.logger.Error("批量写入转录记录失败",
			zap.Error(err),
			zap.Int("count", len(records)))
		// 批量失败时，所有记录都视为失败
		// 如果需要更细粒度的重试，可以在这里拆分重试或丢弃
	}

	elapsed := time.Since(start)
	if b.logger != nil {
		b.logger.Info("转录批量写入完成",
			zap.Int("total", len(records)),
			zap.Duration("elapsed", elapsed),
			zap.Float64("records_per_sec", float64(len(records))/elapsed.Seconds()))
	}
}

// Close 关闭批量写入器，等待所有数据写入完成
func (b *TranscriptionBatcher) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.mu.Unlock()

	close(b.closeCh)
	b.wg.Wait()
}

// Stats 返回当前统计信息
func (b *TranscriptionBatcher) Stats() map[string]interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return map[string]interface{}{
		"buffer_size":     len(b.buffer),
		"max_buffer_size": b.config.MaxBufferSize,
		"batch_size":      b.config.BatchSize,
		"flush_interval":  b.config.FlushInterval.String(),
	}
}

// 全局转录批量写入器
var (
	globalTranscriptionBatcher *TranscriptionBatcher
	transcriptionBatcherOnce   sync.Once
)

// InitTranscriptionBatcher 初始化全局转录批量写入器
func InitTranscriptionBatcher(storage TranscriptionStorage, logger *zap.Logger) {
	transcriptionBatcherOnce.Do(func() {
		globalTranscriptionBatcher = NewTranscriptionBatcher(storage, logger, nil)
		logger.Info("转录批量写入器已初始化",
			zap.Int("batch_size", globalTranscriptionBatcher.config.BatchSize),
			zap.Duration("flush_interval", globalTranscriptionBatcher.config.FlushInterval),
			zap.Int("max_buffer_size", globalTranscriptionBatcher.config.MaxBufferSize))
	})
}

// GetTranscriptionBatcher 获取全局转录批量写入器
func GetTranscriptionBatcher() *TranscriptionBatcher {
	return globalTranscriptionBatcher
}

// AddTranscription 便捷方法：添加转录记录到全局批量写入器
func AddTranscription(record *TranscriptionRecord) error {
	if globalTranscriptionBatcher != nil {
		return globalTranscriptionBatcher.Add(record)
	}
	return nil
}
