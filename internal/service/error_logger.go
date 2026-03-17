// Package service 提供业务逻辑实现
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ErrorLogger 错误日志记录器
type ErrorLogger struct {
	logger     *zap.Logger
	mu         sync.RWMutex
	errorQueue []*ErrorLogEntry
	config     *ErrorLoggerConfig
}

// ErrorLogEntry 错误日志条目
type ErrorLogEntry struct {
	ID         string                 `json:"id"`
	Timestamp  time.Time              `json:"timestamp"`
	Level      string                 `json:"level"`
	Message    string                 `json:"message"`
	Error      string                 `json:"error,omitempty"`
	UserID     *int                   `json:"user_id,omitempty"`
	SessionID  *string                `json:"session_id,omitempty"`
	Context    map[string]interface{} `json:"context,omitempty"`
	StackTrace string                 `json:"stack_trace,omitempty"`
}

// ErrorLoggerConfig 错误日志记录器配置
type ErrorLoggerConfig struct {
	MaxQueueSize   int           // 最大队列大小
	FlushInterval  time.Duration // 刷新间隔
	MaxBatchSize   int           // 最大批量大小
	RetryAttempts  int           // 重试次数
	RetryDelay     time.Duration // 重试延迟
	EnableConsole  bool          // 是否启用控制台输出
	EnableDatabase bool          // 是否启用数据库存储
}

// DefaultErrorLoggerConfig 默认错误日志记录器配置
func DefaultErrorLoggerConfig() *ErrorLoggerConfig {
	return &ErrorLoggerConfig{
		MaxQueueSize:   1000,             // 最多1000条日志
		FlushInterval:  10 * time.Second, // 每10秒刷新
		MaxBatchSize:   50,               // 每批最多50条
		RetryAttempts:  3,                // 重试3次
		RetryDelay:     5 * time.Second,  // 重试延迟5秒
		EnableConsole:  true,             // 启用控制台
		EnableDatabase: false,            // 暂时禁用数据库（需要实现存储接口）
	}
}

// NewErrorLogger 创建错误日志记录器
func NewErrorLogger(logger *zap.Logger) *ErrorLogger {
	return &ErrorLogger{
		logger:     logger,
		errorQueue: make([]*ErrorLogEntry, 0),
		config:     DefaultErrorLoggerConfig(),
	}
}

// SetConfig 设置配置
func (el *ErrorLogger) SetConfig(config *ErrorLoggerConfig) {
	el.mu.Lock()
	defer el.mu.Unlock()
	el.config = config
}

// LogError 记录错误
func (el *ErrorLogger) LogError(level string, message string, err error, userID *int, sessionID *string, context map[string]interface{}) {
	entry := &ErrorLogEntry{
		ID:        generateLogID(),
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		UserID:    userID,
		SessionID: sessionID,
		Context:   context,
	}

	if err != nil {
		entry.Error = err.Error()
	}

	// 控制台输出
	if el.config.EnableConsole {
		el.logToConsole(entry)
	}

	// 添加到队列
	if el.config.EnableDatabase {
		el.addToQueue(entry)
	}
}

// LogErrorWithStack 记录带堆栈的错误
func (el *ErrorLogger) LogErrorWithStack(level string, message string, err error, stackTrace string, userID *int, sessionID *string, context map[string]interface{}) {
	entry := &ErrorLogEntry{
		ID:         generateLogID(),
		Timestamp:  time.Now(),
		Level:      level,
		Message:    message,
		UserID:     userID,
		SessionID:  sessionID,
		Context:    context,
		StackTrace: stackTrace,
	}

	if err != nil {
		entry.Error = err.Error()
	}

	// 控制台输出
	if el.config.EnableConsole {
		el.logToConsole(entry)
	}

	// 添加到队列
	if el.config.EnableDatabase {
		el.addToQueue(entry)
	}
}

// logToConsole 输出到控制台
func (el *ErrorLogger) logToConsole(entry *ErrorLogEntry) {
	fields := []zap.Field{
		zap.String("log_id", entry.ID),
		zap.Time("timestamp", entry.Timestamp),
		zap.String("level", entry.Level),
	}

	if entry.UserID != nil {
		fields = append(fields, zap.Int("user_id", *entry.UserID))
	}

	if entry.SessionID != nil {
		fields = append(fields, zap.String("session_id", *entry.SessionID))
	}

	if entry.Error != "" {
		fields = append(fields, zap.String("error", entry.Error))
	}

	if entry.Context != nil {
		contextJSON, _ := json.Marshal(entry.Context)
		fields = append(fields, zap.String("context", string(contextJSON)))
	}

	if entry.StackTrace != "" {
		fields = append(fields, zap.String("stack_trace", entry.StackTrace))
	}

	switch entry.Level {
	case "error":
		el.logger.Error(entry.Message, fields...)
	case "warn":
		el.logger.Warn(entry.Message, fields...)
	case "info":
		el.logger.Info(entry.Message, fields...)
	default:
		el.logger.Debug(entry.Message, fields...)
	}
}

// addToQueue 添加到队列
func (el *ErrorLogger) addToQueue(entry *ErrorLogEntry) {
	el.mu.Lock()
	defer el.mu.Unlock()

	// 检查队列大小
	if len(el.errorQueue) >= el.config.MaxQueueSize {
		// 移除最老的条目
		el.errorQueue = el.errorQueue[1:]
	}

	el.errorQueue = append(el.errorQueue, entry)

	// 检查是否需要立即刷新
	if len(el.errorQueue) >= el.config.MaxBatchSize {
		go el.flushToDatabase()
	}
}

// StartAutoFlush 启动自动刷新
func (el *ErrorLogger) StartAutoFlush(ctx context.Context) {
	if !el.config.EnableDatabase {
		return
	}

	ticker := time.NewTicker(el.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// 刷新剩余日志
			el.flushToDatabase()
			return
		case <-ticker.C:
			el.flushToDatabase()
		}
	}
}

// flushToDatabase 刷新到数据库
func (el *ErrorLogger) flushToDatabase() {
	el.mu.Lock()
	if len(el.errorQueue) == 0 {
		el.mu.Unlock()
		return
	}

	// 复制队列并清空
	entries := make([]*ErrorLogEntry, len(el.errorQueue))
	copy(entries, el.errorQueue)
	el.errorQueue = el.errorQueue[:0]
	el.mu.Unlock()

	// TODO: 实现数据库存储
	// 这里需要实现具体的数据库存储逻辑
	// 可以使用 SQL 数据库或 NoSQL 数据库

	el.logger.Debug("错误日志批量处理",
		zap.Int("entry_count", len(entries)))

	// 模拟数据库存储
	for _, entry := range entries {
		_ = entry // 避免未使用变量警告
		// 这里应该插入到数据库
	}
}

// GetStats 获取统计信息
func (el *ErrorLogger) GetStats() map[string]interface{} {
	el.mu.RLock()
	defer el.mu.RUnlock()

	return map[string]interface{}{
		"queue_size":      len(el.errorQueue),
		"max_queue_size":  el.config.MaxQueueSize,
		"flush_interval":  el.config.FlushInterval.String(),
		"max_batch_size":  el.config.MaxBatchSize,
		"enable_console":  el.config.EnableConsole,
		"enable_database": el.config.EnableDatabase,
	}
}

// generateLogID 生成日志ID
func generateLogID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), generateRandomString(6))
}

// generateRandomString 生成随机字符串
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(result)
}
