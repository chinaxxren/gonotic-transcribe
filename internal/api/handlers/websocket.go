// Package handlers 提供 HTTP 请求处理器
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/api/middleware"
	"github.com/chinaxxren/gonotic/internal/pkg/errors"
	"github.com/chinaxxren/gonotic/internal/service"
)

// WebSocketHandler 处理 WebSocket 相关的 HTTP 请求
type WebSocketHandler struct {
	websocketHandler *service.WebSocketHandler // 使用正确的实现
	upgrader         websocket.Upgrader
	allowedOrigins   map[string]bool
	logger           *zap.Logger
}

// WebSocketConfig WebSocket 处理器配置
type WebSocketConfig struct {
	ReadBufferSize  int
	WriteBufferSize int
	AllowedOrigins  []string // 允许的源列表，空表示允许所有
}

// NewWebSocketHandler 创建新的 WebSocket HTTP 处理器实例
//
// 参数:
//   - websocketHandler: WebSocket 处理器（service 层实现）
//   - config: WebSocket 配置
//   - logger: 日志记录器
//
// 返回:
//   - *WebSocketHandler: WebSocket HTTP 处理器实例
func NewWebSocketHandler(
	websocketHandler *service.WebSocketHandler,
	config *WebSocketConfig,
	logger *zap.Logger,
) *WebSocketHandler {
	// 构建允许的源映射
	allowedOrigins := make(map[string]bool)
	for _, origin := range config.AllowedOrigins {
		allowedOrigins[origin] = true
	}

	handler := &WebSocketHandler{
		websocketHandler: websocketHandler,
		allowedOrigins:   allowedOrigins,
		logger:           logger,
	}

	// 配置 upgrader
	handler.upgrader = websocket.Upgrader{
		ReadBufferSize:  config.ReadBufferSize,
		WriteBufferSize: config.WriteBufferSize,
		CheckOrigin:     handler.checkOrigin,
	}

	return handler
}

// checkOrigin 检查请求源是否允许
func (h *WebSocketHandler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")

	// 如果没有配置允许的源，允许所有（开发环境）
	if len(h.allowedOrigins) == 0 {
		h.logger.Debug("允许所有源（开发模式）",
			zap.String("origin", origin))
		return true
	}

	// 检查源是否在白名单中
	if h.allowedOrigins[origin] {
		return true
	}

	h.logger.Warn("拒绝不允许的源",
		zap.String("origin", origin))
	return false
}

// HandleTranscription 处理转录 WebSocket 连接
//
// @Summary WebSocket 实时转录
// @Description 建立 WebSocket 连接进行实时语音转录
// @Tags WebSocket
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 101 {string} string "Switching Protocols"
// @Failure 401 {object} errors.APIError "未授权"
// @Failure 400 {object} errors.APIError "WebSocket 升级失败"
// @Router /ws/transcription [get]
func (h *WebSocketHandler) HandleTranscription(c *gin.Context) {
	// 从上下文获取用户 ID
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, errors.UnauthorizedErrorResponse())
		return
	}

	// 升级到 WebSocket
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("WebSocket 升级失败",
			zap.Error(err),
			zap.Int("user_id", userID))
		return
	}

	// 处理连接（使用正确的实现）
	if err := h.websocketHandler.HandleConnection(c.Request.Context(), conn, userID); err != nil {
		h.logger.Error("WebSocket 连接处理失败",
			zap.Error(err),
			zap.Int("user_id", userID))
	}
}
