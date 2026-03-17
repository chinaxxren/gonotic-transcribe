package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/api/handlers"
	"github.com/chinaxxren/gonotic/internal/api/middleware"
	"github.com/chinaxxren/gonotic/internal/config"
	"github.com/chinaxxren/gonotic/internal/pkg/jwt"
	"github.com/chinaxxren/gonotic/internal/pkg/logger"
	"github.com/chinaxxren/gonotic/internal/service"
)

func main() {
	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志
	loggerCfg := logger.Config{
		Level:       cfg.Logging.Level,
		Format:      cfg.Logging.Format,
		File:        cfg.Logging.File,
		MaxSize:     cfg.Logging.MaxSize,
		MaxBackups:  cfg.Logging.BackupCount,
		MaxAge:      cfg.Logging.MaxAge,
		Compress:    cfg.Logging.Compress,
		Development: cfg.Server.Environment == "development",
	}

	if err := logger.InitGlobal(loggerCfg); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	log := logger.Get()

	log.Info("Starting gonotic-transcribe service",
		zap.String("version", "1.0.0"),
		zap.String("environment", cfg.Server.Environment),
		zap.Int("port", cfg.Server.Port))

	// 设置 Gin 模式
	if cfg.Server.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	// 创建 JWT 管理器
	jwtManager := jwt.NewManager(cfg.JWT.Secret, cfg.JWT.Expiration)

	// 创建认证中间件
	authMiddleware := middleware.NewAuthMiddleware(jwtManager, log.Logger)

	// TODO: 初始化其他依赖（数据库、存储等）
	// 这里需要根据实际需求初始化相应的依赖

	// 创建 WebSocket 处理器配置
	wsConfig := &handlers.WebSocketConfig{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		AllowedOrigins:  []string{}, // 空表示允许所有（开发模式）
	}

	// TODO: 创建实际的 WebSocket 处理器
	// 需要传入必要的依赖项
	// websocketHandler := service.NewWebSocketHandler(...)

	// 创建临时的 WebSocket 处理器（用于测试）
	websocketHandler := &service.WebSocketHandler{}

	// 创建 WebSocket HTTP 处理器
	wsHTTPHandler := handlers.NewWebSocketHandler(websocketHandler, wsConfig, log.Logger)

	// 创建 Gin 引擎
	r := gin.New()

	// 添加中间件
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// 注册 WebSocket 路由
	ws := r.Group("/ws")
	ws.Use(authMiddleware.RequireAuth())
	{
		ws.GET("/transcription", wsHTTPHandler.HandleTranscription)
	}

	// 创建 HTTP 服务器
	server := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// 启动服务器
	go func() {
		log.Info("Starting HTTP server",
			zap.String("addr", server.Addr))

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("Failed to start server", zap.Error(err))
			os.Exit(1)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error("Server forced to shutdown", zap.Error(err))
	}

	log.Info("Server exited")
}
