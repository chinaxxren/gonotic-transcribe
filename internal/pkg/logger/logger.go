// Package logger 提供结构化日志功能，仅输出关键信息，移除冗余堆栈和无用字段
package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger 包装 zap.Logger，仅保留核心功能
type Logger struct {
	*zap.Logger
}

// Config 日志配置（精简必要参数）
type Config struct {
	Level       string // 日志级别：debug/info/warn/error（生产建议 info+）
	Format      string // 输出格式：json/console（生产建议 json）
	File        string // 日志文件路径（空则输出到stdout）
	MaxSize     int    // 单文件最大大小（MB）
	MaxBackups  int    // 保留旧文件最大数量
	MaxAge      int    // 保留旧文件最大天数
	Compress    bool   // 是否压缩旧文件
	Development bool   // 开发模式（显示颜色、简化格式）
}

// New 创建精简版日志实例
func New(cfg Config) (*Logger, error) {
	// 解析日志级别（默认info，避免调试日志泛滥）
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		level = zapcore.InfoLevel // 非法级别默认info
	}

	// 核心字段配置（仅保留关键字段，统一字段名与原有格式一致）
	encoderConfig := zapcore.EncoderConfig{
		LevelKey:     "L",                        // 日志级别（对应原有L字段）
		TimeKey:      "T",                        // 时间（对应原有T字段）
		CallerKey:    "C",                        // 调用位置（文件:行号，对应原有C字段）
		MessageKey:   "M",                        // 日志消息（对应原有M字段）
		EncodeLevel:  levelEncoder,               // 级别格式化（INFO/WARN/ERROR，去除颜色）
		EncodeTime:   zapcore.ISO8601TimeEncoder, // 时间格式（与原有一致）
		EncodeCaller: zapcore.ShortCallerEncoder, // 调用位置简化（仅文件名:行号）
	}

	// 开发模式优化（可选颜色、更友好格式）
	if cfg.Development {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder // 开发模式显示颜色
		encoderConfig.EncodeTime = zapcore.RFC3339TimeEncoder        // 开发模式时间格式更易读
	}

	// 选择编码器（JSON/Console）
	var baseEncoder zapcore.Encoder
	if cfg.Format == "json" {
		baseEncoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		baseEncoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	// 包装 encoder 以处理 JSON 字段（不转义）
	encoder := &rawJSONEncoder{Encoder: baseEncoder}

	// 日志输出目标（文件轮转+可选stdout）
	// P2 修复: 使用 BufferedWriteSyncer 启用异步批量写入，减少 IO 阻塞
	var writeSyncer zapcore.WriteSyncer
	if cfg.File != "" {
		// 生成带日期的日志文件名
		logFile := generateLogFileName(cfg.File)

		fileWriter := &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    cfg.MaxSize,    // 单文件最大大小（MB）
			MaxBackups: cfg.MaxBackups, // 保留旧文件最大数量
			MaxAge:     cfg.MaxAge,     // 保留旧文件最大天数
			Compress:   cfg.Compress,   // 是否压缩旧文件
			LocalTime:  true,           // 使用本地时间命名备份文件
		}

		// P2 修复: 使用 BufferedWriteSyncer 包装文件写入器，启用异步批量写入
		// 缓冲区大小 256KB，每 100ms 或缓冲区满时刷新
		bufferedFileWriter := &zapcore.BufferedWriteSyncer{
			WS:            zapcore.AddSync(fileWriter),
			Size:          256 * 1024, // 256KB 缓冲区
			FlushInterval: 100 * time.Millisecond,
		}

		// 开发模式同时输出到stdout和文件，生产模式仅输出到文件
		if cfg.Development {
			writeSyncer = zapcore.NewMultiWriteSyncer(zapcore.AddSync(os.Stdout), bufferedFileWriter)
		} else {
			writeSyncer = bufferedFileWriter
		}
	} else {
		writeSyncer = zapcore.AddSync(os.Stdout)
	}

	// 核心配置（禁用所有冗余功能）
	core := zapcore.NewCore(encoder, writeSyncer, level)
	zapLogger := zap.New(
		core,
		zap.AddCaller(), // 仅保留文件:行号（必要调试信息）
	)

	return &Logger{Logger: zapLogger}, nil
}

// levelEncoder 自定义级别格式化（与原有日志格式一致：INFO/WARN/ERROR）
func levelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	switch level {
	case zapcore.DebugLevel:
		enc.AppendString("DEBUG")
	case zapcore.InfoLevel:
		enc.AppendString("INFO")
	case zapcore.WarnLevel:
		enc.AppendString("WARN")
	case zapcore.ErrorLevel:
		enc.AppendString("ERROR")
	default:
		enc.AppendString(level.String())
	}
}

// rawJSONEncoder 包装 encoder，对 "json" 字段输出原始 JSON（不转义）
type rawJSONEncoder struct {
	zapcore.Encoder
}

// AddByteString 实现 zapcore.Encoder 接口
// 对于 "json" 字段，将 JSON 字节解析为对象后添加（避免字符串转义）
func (e *rawJSONEncoder) AddByteString(key string, val []byte) {
	if key == "json" {
		// 对于 "json" 字段，将 JSON 字节解析为对象
		// 这样 JSON encoder 会将其作为对象输出，而不是字符串，从而避免转义
		var jsonObj interface{}
		if err := json.Unmarshal(val, &jsonObj); err == nil {
			// 如果解析成功，直接添加对象（作为 JSON 对象输出，不转义）
			_ = e.Encoder.AddReflected(key, jsonObj)
			return
		}
		// 如果解析失败，回退到普通字符串处理
		e.Encoder.AddString(key, string(val))
	} else {
		e.Encoder.AddByteString(key, val)
	}
}

// Clone 实现 zapcore.Encoder 接口
func (e *rawJSONEncoder) Clone() zapcore.Encoder {
	return &rawJSONEncoder{Encoder: e.Encoder.Clone()}
}

// ------------------------------ 必要的上下文字段方法 ------------------------------
// WithRequestID 添加请求ID（关键追踪字段）
func (l *Logger) WithRequestID(requestID string) *Logger {
	return &Logger{Logger: l.With(zap.String("request_id", requestID))}
}

// WithUserID 添加用户ID（核心业务字段）
func (l *Logger) WithUserID(userID int) *Logger {
	return &Logger{Logger: l.With(zap.Int("user_id", userID))}
}

// WithError 添加错误描述（仅保留错误信息，移除堆栈）
func (l *Logger) WithError(err error) *Logger {
	if err == nil {
		return l
	}
	return &Logger{Logger: l.With(zap.String("error", err.Error()))}
}

// WithFields 添加自定义业务字段（仅保留必要字段，避免冗余）
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	zapFields := make([]zap.Field, 0, len(fields))
	for k, v := range fields {
		// 过滤掉可能的冗余字段（防止手动添加堆栈）
		if k == "stacktrace" || k == "stack" || k == "S" {
			continue
		}
		zapFields = append(zapFields, zap.Any(k, v))
	}
	return &Logger{Logger: l.With(zapFields...)}
}

// ------------------------------ 全局日志实例（简化使用） ------------------------------
var globalLogger *Logger

// InitGlobal 初始化全局日志（程序启动时调用一次）
func InitGlobal(cfg Config) error {
	logger, err := New(cfg)
	if err != nil {
		return fmt.Errorf("init logger failed: %w", err)
	}
	globalLogger = logger
	return nil
}

// Get 获取全局日志实例
func Get() *Logger {
	if globalLogger == nil {
		// 兜底：未初始化时创建默认日志（避免panic）
		cfg := Config{Level: "info", Format: "console", Development: true}
		logger, _ := New(cfg)
		globalLogger = logger
		globalLogger.Warn("logger not initialized, using default config")
	}
	return globalLogger
}

// Sync 刷新日志缓存（程序退出时调用）
func Sync() error {
	if globalLogger != nil {
		return globalLogger.Logger.Sync()
	}
	return nil
}

// ------------------------------ 简化日志调用方法（可选） ------------------------------
// Debug 调试日志（开发环境用，生产环境可禁用）
func Debug(msg string, fields ...zap.Field) {
	Get().Debug(msg, fields...)
}

// Info 信息日志（核心业务流程）
func Info(msg string, fields ...zap.Field) {
	Get().Info(msg, fields...)
}

// Warn 警告日志（需要关注但不影响运行）
func Warn(msg string, fields ...zap.Field) {
	Get().Warn(msg, fields...)
}

// Error 错误日志（关键错误，需排查）
func Error(msg string, fields ...zap.Field) {
	Get().Error(msg, fields...)
}

// generateLogFileName 生成带日期的日志文件名
// 例如：logs/app.log -> logs/app-2025-12-07.log
func generateLogFileName(originalPath string) string {
	if originalPath == "" {
		return ""
	}

	// 获取文件目录、文件名和扩展名
	dir := filepath.Dir(originalPath)
	filename := filepath.Base(originalPath)
	ext := filepath.Ext(filename)
	nameWithoutExt := strings.TrimSuffix(filename, ext)

	// 生成当前日期字符串
	dateStr := time.Now().Format("2006-01-02")

	// 构造新的文件名：原名-日期.扩展名
	newFilename := fmt.Sprintf("%s-%s%s", nameWithoutExt, dateStr, ext)

	return filepath.Join(dir, newFilename)
}
