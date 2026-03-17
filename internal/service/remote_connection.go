package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	json "github.com/bytedance/sonic"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var (
	// ErrRemoteConnectionClosed 表示底层连接已关闭
	ErrRemoteConnectionClosed = errors.New("remote connection closed")
	// ErrRemoteAudioQueueFull 表示异步音频发送队列已满
	ErrRemoteAudioQueueFull = errors.New("remote audio queue full")
)

// RemoteConnection Remote 服务连接
type RemoteConnection struct {
	conn              *websocket.Conn
	userID            int
	sessionID         string
	apiKey            string             // 实际使用的API Key
	enterpriseManager *EnterpriseManager // 企业级管理器引用（用于释放Key）
	mu                sync.Mutex
	writeMu           sync.Mutex
	closed            bool
	closeOnce         sync.Once
	logger            *zap.Logger

	// 配置信息（保存在内存中用于恢复）
	lastConfig *RemoteConfig

	// 统计信息
	stats RemoteStreamStats

	// P3 修复: Soniox keepalive 机制
	keepaliveStop   chan struct{} // 停止 keepalive goroutine 的信号
	lastSendTime    time.Time     // 最后发送数据的时间
	audioQueue      chan []byte
	audioSenderDone chan struct{}
}

// RemoteStreamStats Remote 流统计信息
type RemoteStreamStats struct {
	ChunkCount    int64     // 音频块数量
	ByteCount     int64     // 字节数量
	LastAudioTime time.Time // 最后音频时间
	StartTime     time.Time // 开始时间
}

// RemoteConnectionManager Remote 连接管理器
type RemoteConnectionManager struct {
	connections       map[string]*RemoteConnection // sessionID -> RemoteConnection
	mu                sync.RWMutex
	remoteURL         string
	enterpriseManager *EnterpriseManager // 企业级多API Key管理器
	retryConfig       *RetryConfig
	logger            *zap.Logger
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries int           // 最大重试次数
	RetryDelay time.Duration // 重试延迟
}

// DefaultRetryConfig 默认重试配置
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries: 2,               // 最多重试2次
		RetryDelay: 2 * time.Second, // 重试间隔2秒
	}
}

// NewRemoteConnectionManager 创建带企业级管理器的 Remote 连接管理器
func NewRemoteConnectionManager(remoteURL string, enterpriseManager *EnterpriseManager, logger *zap.Logger) *RemoteConnectionManager {
	return &RemoteConnectionManager{
		connections:       make(map[string]*RemoteConnection),
		remoteURL:         remoteURL,
		enterpriseManager: enterpriseManager,
		retryConfig:       DefaultRetryConfig(),
		logger:            logger,
	}
}

// SetRetryConfig 设置重试配置
func (m *RemoteConnectionManager) SetRetryConfig(config *RetryConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retryConfig = config
}

// Connect 连接到 Remote 服务（带重试）
func (m *RemoteConnectionManager) Connect(ctx context.Context, userID int, sessionID string) (*RemoteConnection, error) {
	m.mu.Lock()
	retryConfig := m.retryConfig
	m.mu.Unlock()

	// 使用重试机制连接
	return m.ConnectWithRetry(ctx, userID, sessionID, retryConfig)
}

// ConnectWithRetry 带重试机制连接到 Remote 服务
func (m *RemoteConnectionManager) ConnectWithRetry(ctx context.Context, userID int, sessionID string, config *RetryConfig) (*RemoteConnection, error) {
	if config == nil {
		config = DefaultRetryConfig()
	}

	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			m.logger.Info("重试连接 Remote 服务",
				zap.Int("attempt", attempt),
				zap.Int("max_retries", config.MaxRetries),
				zap.Int("user_id", userID),
				zap.String("session_id", sessionID))

			// 等待重试延迟
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(config.RetryDelay):
			}
		}

		// 尝试连接
		conn, err := m.attemptConnection(ctx, userID, sessionID)
		if err == nil {
			return conn, nil
		}

		lastErr = err
		m.logger.Warn("连接 Remote 服务失败",
			zap.Error(err),
			zap.Int("attempt", attempt+1),
			zap.Int("max_retries", config.MaxRetries+1))
	}

	// 所有重试都失败
	m.logger.Error("连接 Remote 服务失败，已达最大重试次数",
		zap.Error(lastErr),
		zap.Int("max_retries", config.MaxRetries),
		zap.Int("user_id", userID))

	return nil, fmt.Errorf("连接 Remote 服务失败（已重试 %d 次）: %w", config.MaxRetries, lastErr)
}

// attemptConnection 尝试连接到 Remote 服务
func (m *RemoteConnectionManager) attemptConnection(ctx context.Context, userID int, sessionID string) (*RemoteConnection, error) {
	// 只在访问 connections map 时持有 manager 锁，避免 Dial 阻塞导致全局串行化。
	m.mu.Lock()
	if existingConn, exists := m.connections[sessionID]; exists {
		if !existingConn.IsClosed() {
			m.mu.Unlock()
			return existingConn, nil
		}
		// 清理已关闭的连接
		delete(m.connections, sessionID)
	}
	m.mu.Unlock()

	// 创建新连接（不持有 manager 锁）
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	// 获取API Key（企业级管理器模式）
	apiKey, err := m.enterpriseManager.GetAPIKey()
	if err != nil {
		return nil, fmt.Errorf("获取API Key失败: %w", err)
	}

	// 添加认证头
	header := make(map[string][]string)
	if apiKey != "" {
		header["Authorization"] = []string{"Bearer " + apiKey}
	}

	conn, _, err := dialer.DialContext(ctx, m.remoteURL, header)
	if err != nil {
		// 连接失败时释放API Key并记录失败
		m.enterpriseManager.ReleaseAPIKey(apiKey)
		m.enterpriseManager.RecordFailure(apiKey)
		return nil, fmt.Errorf("连接失败: %w", err)
	}

	remote := &RemoteConnection{
		conn:              conn,
		userID:            userID,
		sessionID:         sessionID,
		apiKey:            apiKey,              // 保存实际使用的API Key
		enterpriseManager: m.enterpriseManager, // 保存企业级管理器引用
		closed:            false,
		logger:            m.logger,
		stats: RemoteStreamStats{
			StartTime: time.Now(),
		},
		// P3 修复: 初始化 Soniox keepalive 机制
		keepaliveStop:   make(chan struct{}),
		lastSendTime:    time.Now(),
		audioQueue:      make(chan []byte, remoteAudioQueueSize),
		audioSenderDone: make(chan struct{}),
	}

	conn.SetCloseHandler(func(code int, text string) error {
		remote.logger.Warn("Remote 连接被远端关闭",
			zap.Int("user_id", userID),
			zap.String("session_id", sessionID),
			zap.Int("code", code),
			zap.String("reason", text))
		return nil
	})

	// Dial 完成后再写入 map；若此时已有其他 goroutine 建立了可用连接，则放弃当前连接。
	m.mu.Lock()
	if existingConn, exists := m.connections[sessionID]; exists && !existingConn.IsClosed() {
		m.mu.Unlock()
		_ = remote.Close()
		return existingConn, nil
	}
	m.connections[sessionID] = remote
	m.mu.Unlock()

	// 启动异步发送与 keepalive goroutine
	go remote.audioSenderLoop()
	go remote.keepaliveLoop()

	m.logger.Info("Remote 连接已建立",
		zap.Int("user_id", userID),
		zap.String("session_id", sessionID))

	return remote, nil
}

// Close 关闭 Remote 连接
func (m *RemoteConnectionManager) Close(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, exists := m.connections[sessionID]
	if !exists {
		return nil
	}

	err := conn.Close()
	delete(m.connections, sessionID)

	return err
}

// GetConnection 获取 Remote 连接
func (m *RemoteConnectionManager) GetConnection(sessionID string) (*RemoteConnection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.connections[sessionID]
	return conn, exists
}

// CloseAll 关闭所有连接
func (m *RemoteConnectionManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for sessionID, conn := range m.connections {
		_ = conn.Close()
		delete(m.connections, sessionID)
	}
}

// remoteWriteTimeout 是 Remote 连接写操作的超时时间
const remoteWriteTimeout = 5 * time.Second
const remoteAudioQueueSize = 512
const remoteCloseDrainTimeout = remoteWriteTimeout + time.Second

func (rc *RemoteConnection) writeMessage(messageType int, payload []byte, trackSendTime bool) error {
	rc.mu.Lock()
	if rc.closed || rc.conn == nil {
		rc.mu.Unlock()
		return ErrRemoteConnectionClosed
	}
	conn := rc.conn
	apiKey := rc.apiKey
	em := rc.enterpriseManager
	rc.mu.Unlock()

	rc.writeMu.Lock()
	defer rc.writeMu.Unlock()

	if err := conn.SetWriteDeadline(time.Now().Add(remoteWriteTimeout)); err != nil {
		return fmt.Errorf("设置写超时失败: %w", err)
	}

	if err := conn.WriteMessage(messageType, payload); err != nil {
		if em != nil && apiKey != "" {
			em.RecordFailure(apiKey)
		}
		return err
	}

	if trackSendTime {
		rc.mu.Lock()
		rc.lastSendTime = time.Now()
		rc.mu.Unlock()
	}

	return nil
}

// SendAudio 发送音频数据到 Remote
// P1 修复: 改为异步入队，避免客户端读取循环被底层写阻塞
func (rc *RemoteConnection) SendAudio(audioData []byte) error {
	if len(audioData) == 0 {
		return nil
	}

	rc.mu.Lock()
	if rc.closed || rc.conn == nil {
		rc.mu.Unlock()
		return ErrRemoteConnectionClosed
	}
	stopCh := rc.keepaliveStop
	audioQueue := rc.audioQueue
	rc.mu.Unlock()

	packetCopy := append([]byte(nil), audioData...)
	select {
	case <-stopCh:
		return ErrRemoteConnectionClosed
	default:
	}

	select {
	case audioQueue <- packetCopy:
		return nil
	default:
		return ErrRemoteAudioQueueFull
	}
}

// GetStats 获取统计信息
func (rc *RemoteConnection) GetStats() RemoteStreamStats {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.stats
}

// SendCommand 发送命令到 Remote
// P1 修复: 缩小锁粒度，避免在持锁时进行阻塞 I/O
// P2 修复: 添加写超时
func (rc *RemoteConnection) SendCommand(command map[string]interface{}) error {
	// 序列化在锁外进行
	data, err := json.Marshal(command)
	if err != nil {
		return err
	}

	rc.mu.Lock()
	userID := rc.userID
	sessionID := rc.sessionID
	rc.mu.Unlock()

	if err := rc.writeMessage(websocket.TextMessage, data, true); err != nil {
		rc.logger.Error("发送命令失败",
			zap.Error(err),
			zap.Int("user_id", userID),
			zap.String("session_id", sessionID))
		return err
	}

	return nil
}

func (rc *RemoteConnection) initiateClose() {
	rc.closeOnce.Do(func() {
		rc.mu.Lock()
		if rc.closed {
			rc.mu.Unlock()
			return
		}
		stopCh := rc.keepaliveStop
		audioSenderDone := rc.audioSenderDone
		rc.mu.Unlock()

		if stopCh != nil {
			close(stopCh)
		}
		if audioSenderDone != nil {
			select {
			case <-audioSenderDone:
			case <-time.After(remoteCloseDrainTimeout):
				rc.logger.Warn("等待 Remote 音频发送队列排空超时，强制关闭连接",
					zap.Int("user_id", rc.userID),
					zap.String("session_id", rc.sessionID))
			}
		}

		rc.mu.Lock()
		rc.closed = true
		rc.mu.Unlock()
		rc.cleanupConnection()
	})
}

func (rc *RemoteConnection) cleanupConnection() {
	rc.mu.Lock()
	conn := rc.conn
	rc.conn = nil
	apiKey := rc.apiKey
	rc.apiKey = ""
	em := rc.enterpriseManager
	rc.mu.Unlock()

	if em != nil && apiKey != "" {
		em.ReleaseAPIKey(apiKey)
		rc.logger.Debug("已释放API Key",
			zap.Int("user_id", rc.userID),
			zap.String("session_id", rc.sessionID))
	}

	if conn != nil {
		rc.writeMu.Lock()
		err := conn.Close()
		rc.writeMu.Unlock()
		if err != nil {
			// 避免重复噪声，使用 Warn
			rc.logger.Warn("关闭 Remote 连接失败",
				zap.Error(err),
				zap.Int("user_id", rc.userID),
				zap.String("session_id", rc.sessionID))
		} else {
			rc.logger.Info("Remote 连接已关闭",
				zap.Int("user_id", rc.userID),
				zap.String("session_id", rc.sessionID))
		}
	}
}

func (rc *RemoteConnection) markAPIKeyFailure() {
	rc.mu.Lock()
	apiKey := rc.apiKey
	em := rc.enterpriseManager
	rc.mu.Unlock()

	if em != nil && apiKey != "" {
		em.RecordFailure(apiKey)
	}
}

// remoteReadTimeout 是 Remote 连接读操作的超时时间
// 设置为 60 秒，因为 ASR 服务在静默期间可能不发送数据
const remoteReadTimeout = 60 * time.Second

// ReadMessage 读取 Remote 消息
// P2 修复: 添加读超时，防止 Goroutine 永久阻塞（如半开连接）
func (rc *RemoteConnection) ReadMessage() (int, []byte, error) {
	rc.mu.Lock()
	if rc.closed || rc.conn == nil {
		rc.mu.Unlock()
		return 0, nil, ErrRemoteConnectionClosed
	}
	conn := rc.conn
	rc.mu.Unlock()

	// 设置读超时，防止半开连接导致 Goroutine 永久阻塞
	if err := conn.SetReadDeadline(time.Now().Add(remoteReadTimeout)); err != nil {
		return 0, nil, fmt.Errorf("设置读超时失败: %w", err)
	}

	return conn.ReadMessage()
}

// Close 关闭连接
func (rc *RemoteConnection) Close() error {
	rc.initiateClose()
	return nil
}

// IsClosed 检查连接是否已关闭
func (rc *RemoteConnection) IsClosed() bool {
	rc.mu.Lock()
	closed := rc.closed
	rc.mu.Unlock()
	return closed
}

// SendJSON 发送 JSON 消息到 Remote
// P1 修复: 缩小锁粒度，避免在持锁时进行阻塞 I/O
// P2 修复: 添加写超时
func (rc *RemoteConnection) SendJSON(data interface{}) error {
	// 序列化在锁外进行
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("JSON 序列化失败: %w", err)
	}

	rc.mu.Lock()
	if rc.closed || rc.conn == nil {
		rc.mu.Unlock()
		return fmt.Errorf("连接已关闭")
	}
	userID := rc.userID
	sessionID := rc.sessionID
	rc.mu.Unlock()

	if err := rc.writeMessage(websocket.TextMessage, jsonData, true); err != nil {
		rc.logger.Error("发送 JSON 消息失败",
			zap.Error(err),
			zap.Int("user_id", userID),
			zap.String("session_id", sessionID))
		return err
	}

	return nil
}

// Soniox keepalive 间隔：文档要求至少 20 秒，建议 5-10 秒
const sonioxKeepaliveInterval = 10 * time.Second

// keepaliveLoop P3 修复: 定期发送 Soniox keepalive 消息
// 根据 Soniox 文档，超过 20 秒无音频/keepalive 连接会被关闭
func (rc *RemoteConnection) keepaliveLoop() {
	ticker := time.NewTicker(sonioxKeepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rc.keepaliveStop:
			return
		case <-ticker.C:
			rc.mu.Lock()
			if rc.closed || rc.conn == nil {
				rc.mu.Unlock()
				return
			}
			// 检查上次发送时间，如果最近发送过音频则跳过 keepalive
			timeSinceLastSend := time.Since(rc.lastSendTime)
			rc.mu.Unlock()

			// 只有超过 8 秒没发送数据才需要发送 keepalive（留 2 秒余量）
			if timeSinceLastSend < 8*time.Second {
				continue
			}

			// 发送 Soniox keepalive 消息
			keepaliveMsg := map[string]interface{}{
				"type": "keepalive",
			}
			if err := rc.SendJSON(keepaliveMsg); err != nil {
				rc.logger.Debug("发送 Soniox keepalive 失败",
					zap.Error(err),
					zap.Int("user_id", rc.userID),
					zap.String("session_id", rc.sessionID))
				// keepalive 失败不主动关闭连接，让读超时来处理
				return
			}

			rc.logger.Debug("已发送 Soniox keepalive",
				zap.Int("user_id", rc.userID),
				zap.String("session_id", rc.sessionID))
		}
	}
}

func (rc *RemoteConnection) audioSenderLoop() {
	defer func() {
		if rc.audioSenderDone != nil {
			close(rc.audioSenderDone)
		}
	}()

	for {
		select {
		case <-rc.keepaliveStop:
			rc.drainQueuedAudio()
			return
		case audioData := <-rc.audioQueue:
			if len(audioData) == 0 {
				continue
			}

			if err := rc.writeMessage(websocket.BinaryMessage, audioData, true); err != nil {
				rc.logger.Warn("异步发送音频到 Remote 失败，关闭连接",
					zap.Error(err),
					zap.Int("user_id", rc.userID),
					zap.String("session_id", rc.sessionID))
				go rc.initiateClose()
				return
			}

			now := time.Now()
			rc.mu.Lock()
			rc.stats.ChunkCount++
			rc.stats.ByteCount += int64(len(audioData))
			rc.stats.LastAudioTime = now
			rc.lastSendTime = now
			rc.mu.Unlock()
		}
	}
}

func (rc *RemoteConnection) drainQueuedAudio() {
	for {
		select {
		case audioData := <-rc.audioQueue:
			if len(audioData) == 0 {
				continue
			}
			if err := rc.writeMessage(websocket.BinaryMessage, audioData, true); err != nil {
				rc.logger.Warn("关闭前发送尾部音频失败，停止排空队列",
					zap.Error(err),
					zap.Int("user_id", rc.userID),
					zap.String("session_id", rc.sessionID))
				return
			}

			now := time.Now()
			rc.mu.Lock()
			rc.stats.ChunkCount++
			rc.stats.ByteCount += int64(len(audioData))
			rc.stats.LastAudioTime = now
			rc.lastSendTime = now
			rc.mu.Unlock()
		default:
			return
		}
	}
}

// updateLastSendTime 更新最后发送时间
func (rc *RemoteConnection) updateLastSendTime() {
	rc.mu.Lock()
	rc.lastSendTime = time.Now()
	rc.mu.Unlock()
}
