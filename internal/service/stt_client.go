// Package service 提供业务逻辑实现
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// STTClient STT 服务客户端
type STTClient struct {
	conn       *websocket.Conn
	sessionID  string
	config     *STTConfig
	audioChan  chan []byte
	resultChan chan *TranscriptionResult
	errorChan  chan error
	stopChan   chan struct{}
	logger     *zap.Logger
	mu         sync.RWMutex
	closed     bool
}

// STTClientConfig STT 客户端配置
type STTClientConfig struct {
	WebSocketURL string
	RestURL      string
	Timeout      time.Duration
	MaxRetries   int
}

// NewSTTClient 创建新的 STT 客户端
//
// 参数:
//   - sessionID: 会话 ID
//   - config: STT 配置
//   - clientConfig: 客户端配置
//   - logger: 日志记录器
//
// 返回:
//   - *STTClient: STT 客户端实例
func NewSTTClient(
	sessionID string,
	config *STTConfig,
	clientConfig *STTClientConfig,
	logger *zap.Logger,
) *STTClient {
	return &STTClient{
		sessionID:  sessionID,
		config:     config,
		audioChan:  make(chan []byte, 100),
		resultChan: make(chan *TranscriptionResult, 50),
		errorChan:  make(chan error, 10),
		stopChan:   make(chan struct{}),
		logger:     logger,
		closed:     false,
	}
}

// Connect 连接到远程 STT 服务
//
// 参数:
//   - ctx: 上下文
//   - url: WebSocket URL
//
// 返回:
//   - error: 如果连接失败返回错误
func (c *STTClient) Connect(ctx context.Context, url string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return fmt.Errorf("已经连接到 STT 服务")
	}

	// 建立 WebSocket 连接
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		c.logger.Error("连接 STT 服务失败",
			zap.Error(err),
			zap.String("url", url),
			zap.String("session_id", c.sessionID))
		return NewTranscriptionError(ErrCodeSTTConnection, "连接 STT 服务失败", err)
	}

	c.conn = conn
	c.logger.Info("已连接到 STT 服务",
		zap.String("url", url),
		zap.String("session_id", c.sessionID))

	// 发送初始化配置
	if err := c.sendConfig(); err != nil {
		conn.Close()
		c.conn = nil
		return err
	}

	// 启动接收 goroutine
	go c.receiveLoop()

	return nil
}

// sendConfig 发送配置到 STT 服务
func (c *STTClient) sendConfig() error {
	configMsg := map[string]interface{}{
		"type":   "config",
		"config": c.config,
	}

	if err := c.conn.WriteJSON(configMsg); err != nil {
		c.logger.Error("发送配置失败",
			zap.Error(err),
			zap.String("session_id", c.sessionID))
		return NewTranscriptionError(ErrCodeSTTConnection, "发送配置失败", err)
	}

	return nil
}

// SendAudio 发送音频数据到 STT 服务
//
// 参数:
//   - audioData: 音频数据
//
// 返回:
//   - error: 如果发送失败返回错误
func (c *STTClient) SendAudio(audioData []byte) error {
	c.mu.RLock()
	if c.closed || c.conn == nil {
		c.mu.RUnlock()
		return fmt.Errorf("STT 客户端已关闭")
	}
	c.mu.RUnlock()

	// 发送二进制音频数据
	if err := c.conn.WriteMessage(websocket.BinaryMessage, audioData); err != nil {
		c.logger.Error("发送音频数据失败",
			zap.Error(err),
			zap.String("session_id", c.sessionID),
			zap.Int("data_size", len(audioData)))
		return NewTranscriptionError(ErrCodeSTTConnection, "发送音频数据失败", err)
	}

	c.logger.Debug("已发送音频数据",
		zap.String("session_id", c.sessionID),
		zap.Int("data_size", len(audioData)))

	return nil
}

// receiveLoop 接收转录结果的循环
func (c *STTClient) receiveLoop() {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("STT 接收循环 panic",
				zap.Any("panic", r),
				zap.String("session_id", c.sessionID))
		}
	}()

	for {
		select {
		case <-c.stopChan:
			return
		default:
			messageType, data, err := c.conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					c.logger.Error("STT 连接异常关闭",
						zap.Error(err),
						zap.String("session_id", c.sessionID))
				}
				select {
				case c.errorChan <- err:
				default:
				}
				return
			}

			if messageType == websocket.TextMessage {
				// 解析 JSON 消息
				var result TranscriptionResult
				if err := json.Unmarshal(data, &result); err != nil {
					c.logger.Error("解析转录结果失败",
						zap.Error(err),
						zap.String("session_id", c.sessionID))
					continue
				}

				// 发送结果到通道
				select {
				case c.resultChan <- &result:
				case <-c.stopChan:
					return
				default:
					c.logger.Warn("结果通道已满，丢弃结果",
						zap.String("session_id", c.sessionID))
				}
			}
		}
	}
}

// GetResultChan 获取结果通道
func (c *STTClient) GetResultChan() <-chan *TranscriptionResult {
	return c.resultChan
}

// GetErrorChan 获取错误通道
func (c *STTClient) GetErrorChan() <-chan error {
	return c.errorChan
}

// Close 关闭 STT 客户端
func (c *STTClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true

	// 关闭停止通道
	close(c.stopChan)

	// 关闭 WebSocket 连接
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	c.logger.Info("STT 客户端已关闭",
		zap.String("session_id", c.sessionID))

	return nil
}

// Reconnect 重新连接到 STT 服务
//
// 参数:
//   - ctx: 上下文
//   - url: WebSocket URL
//   - maxRetries: 最大重试次数
//
// 返回:
//   - error: 如果重连失败返回错误
func (c *STTClient) Reconnect(ctx context.Context, url string, maxRetries int) error {
	c.logger.Info("开始重连 STT 服务",
		zap.String("session_id", c.sessionID),
		zap.Int("max_retries", maxRetries))

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		// 等待一段时间后重试
		if i > 0 {
			backoff := time.Duration(i) * time.Second
			c.logger.Info("等待后重试",
				zap.String("session_id", c.sessionID),
				zap.Int("retry", i+1),
				zap.Duration("backoff", backoff))
			time.Sleep(backoff)
		}

		// 尝试连接
		err := c.Connect(ctx, url)
		if err == nil {
			c.logger.Info("重连成功",
				zap.String("session_id", c.sessionID),
				zap.Int("retry", i+1))
			return nil
		}

		lastErr = err
		c.logger.Warn("重连失败",
			zap.String("session_id", c.sessionID),
			zap.Int("retry", i+1),
			zap.Error(err))
	}

	c.logger.Error("重连失败，已达最大重试次数",
		zap.String("session_id", c.sessionID),
		zap.Int("max_retries", maxRetries),
		zap.Error(lastErr))

	return NewTranscriptionError(ErrCodeSTTConnection, "重连 STT 服务失败", lastErr)
}

// IsConnected 检查是否已连接
func (c *STTClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil && !c.closed
}

// DecodeBase64Audio 解码 Base64 编码的音频数据
func DecodeBase64Audio(encodedData string) ([]byte, error) {
	audioData, err := base64.StdEncoding.DecodeString(encodedData)
	if err != nil {
		return nil, NewTranscriptionError(ErrCodeAudioFormat, "解码音频数据失败", err)
	}
	return audioData, nil
}
