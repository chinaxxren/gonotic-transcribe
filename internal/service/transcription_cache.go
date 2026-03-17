package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/chinaxxren/gonotic/internal/config"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 全局单例实例
var (
	globalTranscriptionCache TranscriptionCache
	cacheOnce                sync.Once
)

// ==========================================
// 转录缓存管理器
// ==========================================

// TranscriptionCache 转录缓存管理器接口
// 负责转录会话的存储、获取、更新、删除，支持内存和Redis存储
// 以UserID作为唯一标识，简化架构设计
type TranscriptionCache interface {
	// 核心操作（UserID作为唯一标识）
	Get(ctx context.Context, userID int) (*SessionInfo, error)
	Set(ctx context.Context, userID int, session *SessionInfo) error
	Delete(ctx context.Context, userID int) error

	// 更新操作
	UpdateMeetingID(ctx context.Context, userID int, meetingID int) error
	UpdateStatus(ctx context.Context, userID int, status string, isTranscribing, isPaused bool) error
	UpdatePreferences(ctx context.Context, userID int, audioFormat string, languageHints []string) error

	// 复合操作（减少多次锁操作）
	CreateOrUpdateSession(ctx context.Context, userID int, session *SessionInfo) error
	UpdateSessionWithMeetingID(ctx context.Context, userID int, meetingID int) error
	UpdateRemainingTime(ctx context.Context, userID int, remainingTime int) error

	// 批量查询操作
	List(ctx context.Context) ([]*SessionInfo, error)
	GetActiveSessionsForBilling(ctx context.Context) ([]*SessionInfo, error)
	GetActiveSessionsForCheck(ctx context.Context) ([]*SessionInfo, error)

	// 查询操作
	Exists(ctx context.Context, userID int) (bool, error)
	ListActive(ctx context.Context) ([]int, error)

	// 存储后端切换
	SwitchToMemory() error
	SwitchToRedis(addr, password string) error
	GetBackendType() string

	// 统计信息
	GetStats() CacheStats
}

// CacheStats 缓存统计信息
type CacheStats struct {
	BackendType     string           `json:"backend_type"`
	TotalSessions   int              `json:"total_sessions"`
	ActiveSessions  int              `json:"active_sessions"`
	OperationCounts map[string]int64 `json:"operation_counts"`
}

// ==========================================
// 内存存储实现（单例，集中锁管理）
// ==========================================

type memoryCache struct {
	data   map[int]*SessionInfo // 按用户ID索引（唯一）
	mu     sync.RWMutex         // 集中的锁管理
	statsMu sync.Mutex
	logger *zap.Logger
	stats  CacheStats
}

// NewMemoryTranscriptionCache 创建内存转录缓存
func NewMemoryTranscriptionCache(logger *zap.Logger) TranscriptionCache {
	return &memoryCache{
		data:   make(map[int]*SessionInfo),
		logger: logger,
		stats: CacheStats{
			BackendType:     "memory",
			OperationCounts: make(map[string]int64),
		},
	}
}

func (m *memoryCache) Get(ctx context.Context, userID int) (*SessionInfo, error) {
	m.mu.RLock()
	session, exists := m.data[userID]
	m.mu.RUnlock()

	// 统计更新（无需锁保护，原子操作）
	if !exists {
		m.incrementStat("get_misses")
		return nil, fmt.Errorf("session not found for user %d", userID)
	}

	m.incrementStat("gets")
	return session, nil
}

func (m *memoryCache) Set(ctx context.Context, userID int, session *SessionInfo) error {
	m.mu.Lock()
	m.data[userID] = session
	m.incrementStat("sets")
	m.mu.Unlock()

	// 日志记录在锁外进行
	m.logger.Debug("转录缓存已保存",
		zap.Int("user_id", userID),
		zap.String("session_uuid", session.SessionUUID),
		zap.String("backend", "memory"))

	return nil
}

func (m *memoryCache) Delete(ctx context.Context, userID int) error {
	m.mu.Lock()
	delete(m.data, userID)
	m.incrementStat("deletes")
	m.mu.Unlock()

	// 日志记录在锁外进行
	m.logger.Debug("转录缓存已删除",
		zap.Int("user_id", userID),
		zap.String("backend", "memory"))

	return nil
}

func (m *memoryCache) UpdateMeetingID(ctx context.Context, userID int, meetingID int) error {
	m.mu.Lock()
	session, exists := m.data[userID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("session not found for user %d", userID)
	}
	session.MeetingID = meetingID
	m.incrementStat("updates")
	m.mu.Unlock()
	return nil
}

func (m *memoryCache) UpdateStatus(ctx context.Context, userID int, status string, isTranscribing, isPaused bool) error {
	m.mu.Lock()
	session, exists := m.data[userID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("session not found for user %d", userID)
	}
	session.Status = status
	session.IsTranscribing = isTranscribing
	session.IsPaused = isPaused
	m.incrementStat("updates")
	m.mu.Unlock()
	return nil
}

func (m *memoryCache) UpdatePreferences(ctx context.Context, userID int, audioFormat string, languageHints []string) error {
	m.mu.Lock()
	session, exists := m.data[userID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("session not found for user %d", userID)
	}

	// 更新音频格式（同时更新主字段和覆盖字段）
	if audioFormat != "" {
		session.AudioFormat = audioFormat
		session.AudioFormatOverride = audioFormat
	}

	// 更新语言提示（同时更新主字段和覆盖字段）
	if len(languageHints) > 0 {
		session.LanguageHints = append([]string(nil), languageHints...)
		session.LanguageHintsOverride = append([]string(nil), languageHints...)
	}

	// 设置计费倍率：当存在多语言提示时启用翻译倍率
	if len(languageHints) >= 2 {
		session.BillingMultiplier = 2 // 翻译倍率
	} else {
		session.BillingMultiplier = 1 // 默认倍率
	}

	m.incrementStat("updates")
	m.mu.Unlock()
	return nil
}

func (m *memoryCache) Exists(ctx context.Context, userID int) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.data[userID]
	return exists, nil
}

func (m *memoryCache) ListActive(ctx context.Context) ([]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var activeUsers []int
	for userID, session := range m.data {
		if session.IsTranscribing || session.IsPaused {
			activeUsers = append(activeUsers, userID)
		}
	}

	return activeUsers, nil
}

func (m *memoryCache) SwitchToMemory() error {
	// 已经是内存存储
	return nil
}

func (m *memoryCache) SwitchToRedis(addr, password string) error {
	// TODO: 实现Redis切换
	m.logger.Warn("Redis后端切换未实现，保持内存存储")
	return fmt.Errorf("redis backend not implemented")
}

func (m *memoryCache) GetBackendType() string {
	return "memory"
}

// ==========================================
// 复合操作方法（减少锁操作）
// ==========================================

// CreateOrUpdateSession 创建或更新会话（一次锁操作完成）
func (m *memoryCache) CreateOrUpdateSession(ctx context.Context, userID int, session *SessionInfo) error {
	m.mu.Lock()
	m.data[userID] = session
	m.incrementStat("create_or_updates")
	m.mu.Unlock()

	m.logger.Debug("会话已创建或更新",
		zap.Int("user_id", userID),
		zap.String("session_uuid", session.SessionUUID),
		zap.String("status", session.Status))

	return nil
}

// UpdateSessionWithMeetingID 更新会话并设置MeetingID（一次锁操作）
func (m *memoryCache) UpdateSessionWithMeetingID(ctx context.Context, userID int, meetingID int) error {
	m.mu.Lock()
	session, exists := m.data[userID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("session not found for user %d", userID)
	}

	session.MeetingID = meetingID
	m.incrementStat("meeting_id_updates")
	m.mu.Unlock()

	return nil
}

// UpdateRemainingTime 更新剩余时间（无锁操作，只更新非关键信息）
func (m *memoryCache) UpdateRemainingTime(ctx context.Context, userID int, remainingTime int) error {
	// P3 修复: 使用写锁保护修改操作，避免数据竞争
	m.mu.Lock()
	session, exists := m.data[userID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("session not found for user %d", userID)
	}
	session.RemainingTime = remainingTime
	session.LastRemainingTimeUpdate = time.Now()
	m.mu.Unlock()
	return nil
}

// GetActiveSessionsForBilling 获取需要计费的活跃会话
func (m *memoryCache) GetActiveSessionsForBilling(ctx context.Context) ([]*SessionInfo, error) {
	m.mu.RLock()
	sessions := make([]*SessionInfo, 0)
	for _, session := range m.data {
		// 使用线程安全的方法检查计费状态
		if session.SafeGetBillingState() && session.IsTranscribing && !session.IsPaused {
			sessions = append(sessions, session)
		}
	}
	m.mu.RUnlock()

	return sessions, nil
}

// GetActiveSessionsForCheck 获取需要检查的活跃会话
func (m *memoryCache) GetActiveSessionsForCheck(ctx context.Context) ([]*SessionInfo, error) {
	m.mu.RLock()
	sessions := make([]*SessionInfo, 0)
	for _, session := range m.data {
		if session.IsTranscribing && !session.IsPaused {
			sessions = append(sessions, session)
		}
	}
	m.mu.RUnlock()

	return sessions, nil
}

func (m *memoryCache) GetStats() CacheStats {
	m.mu.RLock()
	activeCount := 0
	for _, session := range m.data {
		if session.IsTranscribing || session.IsPaused {
			activeCount++
		}
	}
	totalSessions := len(m.data)
	m.mu.RUnlock()

	m.statsMu.Lock()
	m.stats.TotalSessions = totalSessions
	m.stats.ActiveSessions = activeCount
	stats := m.stats
	m.statsMu.Unlock()

	return stats
}

func (m *memoryCache) incrementStat(operation string) {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()
	m.stats.OperationCounts[operation]++
}

// List 列出所有会话
func (m *memoryCache) List(ctx context.Context) ([]*SessionInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*SessionInfo, 0, len(m.data))
	for _, session := range m.data {
		sessions = append(sessions, session)
	}

	m.incrementStat("list")
	return sessions, nil
}

// ==========================================
// Redis存储实现
// ==========================================

type redisCache struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
	logger *zap.Logger
	stats  CacheStats
	mu     sync.RWMutex // 用于统计信息的锁
}

// NewRedisTranscriptionCache 创建Redis转录缓存
func NewRedisTranscriptionCache(cfg config.RedisConfig, logger *zap.Logger) (TranscriptionCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	prefix := cfg.SessionStorePrefix
	if prefix == "" {
		prefix = "transcription_cache"
	}

	ttl := cfg.SessionStoreTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	return &redisCache{
		client: client,
		prefix: prefix,
		ttl:    ttl,
		logger: logger,
		stats: CacheStats{
			BackendType:     "redis",
			OperationCounts: make(map[string]int64),
		},
	}, nil
}

func (r *redisCache) sessionKey(userID int) string {
	return fmt.Sprintf("%s:session:%d", r.prefix, userID)
}

func (r *redisCache) activeKey() string {
	return fmt.Sprintf("%s:active", r.prefix)
}

func (r *redisCache) Get(ctx context.Context, userID int) (*SessionInfo, error) {
	key := r.sessionKey(userID)
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			r.incrementStat("get_misses")
			return nil, fmt.Errorf("session not found for user %d", userID)
		}
		return nil, fmt.Errorf("redis get error: %w", err)
	}

	var session SessionInfo
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session: %w", err)
	}

	r.incrementStat("gets")
	return &session, nil
}

func (r *redisCache) Set(ctx context.Context, userID int, session *SessionInfo) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	key := r.sessionKey(userID)
	if err := r.client.Set(ctx, key, data, r.ttl).Err(); err != nil {
		return fmt.Errorf("redis set error: %w", err)
	}

	// 如果是活跃会话，添加到活跃集合
	if session.IsTranscribing || session.IsPaused {
		r.client.SAdd(ctx, r.activeKey(), userID)
		r.client.Expire(ctx, r.activeKey(), r.ttl)
	}

	r.incrementStat("sets")
	r.logger.Debug("转录缓存已保存到Redis",
		zap.Int("user_id", userID),
		zap.String("session_uuid", session.SessionUUID),
		zap.String("backend", "redis"))

	return nil
}

func (r *redisCache) Delete(ctx context.Context, userID int) error {
	key := r.sessionKey(userID)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("redis delete error: %w", err)
	}

	// 从活跃集合中移除
	r.client.SRem(ctx, r.activeKey(), userID)

	r.incrementStat("deletes")
	r.logger.Debug("转录缓存已从Redis删除",
		zap.Int("user_id", userID),
		zap.String("backend", "redis"))

	return nil
}

// Redis版本的其他方法实现...

func (r *redisCache) UpdateMeetingID(ctx context.Context, userID int, meetingID int) error {
	session, err := r.Get(ctx, userID)
	if err != nil {
		return err
	}
	session.MeetingID = meetingID
	return r.Set(ctx, userID, session)
}

func (r *redisCache) UpdateStatus(ctx context.Context, userID int, status string, isTranscribing, isPaused bool) error {
	session, err := r.Get(ctx, userID)
	if err != nil {
		return err
	}
	session.Status = status
	session.IsTranscribing = isTranscribing
	session.IsPaused = isPaused
	return r.Set(ctx, userID, session)
}

func (r *redisCache) UpdatePreferences(ctx context.Context, userID int, audioFormat string, languageHints []string) error {
	session, err := r.Get(ctx, userID)
	if err != nil {
		return err
	}

	// 更新音频格式（同时更新主字段和覆盖字段）
	if audioFormat != "" {
		session.AudioFormat = audioFormat
		session.AudioFormatOverride = audioFormat
	}

	// 更新语言提示（同时更新主字段和覆盖字段）
	if len(languageHints) > 0 {
		session.LanguageHints = append([]string(nil), languageHints...)
		session.LanguageHintsOverride = append([]string(nil), languageHints...)
	}
	if len(languageHints) >= 2 {
		session.BillingMultiplier = 2
	} else {
		session.BillingMultiplier = 1
	}

	return r.Set(ctx, userID, session)
}

func (r *redisCache) CreateOrUpdateSession(ctx context.Context, userID int, session *SessionInfo) error {
	return r.Set(ctx, userID, session)
}

func (r *redisCache) UpdateSessionWithMeetingID(ctx context.Context, userID int, meetingID int) error {
	return r.UpdateMeetingID(ctx, userID, meetingID)
}

func (r *redisCache) UpdateRemainingTime(ctx context.Context, userID int, remainingTime int) error {
	// 对于Redis，剩余时间也通过正常更新，因为需要持久化
	session, err := r.Get(ctx, userID)
	if err != nil {
		return err
	}
	session.RemainingTime = remainingTime
	return r.Set(ctx, userID, session)
}

func (r *redisCache) GetActiveSessionsForBilling(ctx context.Context) ([]*SessionInfo, error) {
	activeUsers, err := r.client.SMembers(ctx, r.activeKey()).Result()
	if err != nil {
		if err == redis.Nil {
			return []*SessionInfo{}, nil
		}
		return nil, err
	}

	sessions := make([]*SessionInfo, 0)
	for _, userIDStr := range activeUsers {
		var userID int
		if _, err := fmt.Sscanf(userIDStr, "%d", &userID); err != nil {
			continue
		}

		session, err := r.Get(ctx, userID)
		if err != nil {
			continue
		}

		if session.SafeGetBillingState() && session.IsTranscribing && !session.IsPaused {
			sessions = append(sessions, session)
		}
	}

	return sessions, nil
}

func (r *redisCache) GetActiveSessionsForCheck(ctx context.Context) ([]*SessionInfo, error) {
	activeUsers, err := r.client.SMembers(ctx, r.activeKey()).Result()
	if err != nil {
		if err == redis.Nil {
			return []*SessionInfo{}, nil
		}
		return nil, err
	}

	sessions := make([]*SessionInfo, 0)
	for _, userIDStr := range activeUsers {
		var userID int
		if _, err := fmt.Sscanf(userIDStr, "%d", &userID); err != nil {
			continue
		}

		session, err := r.Get(ctx, userID)
		if err != nil {
			continue
		}

		if session.IsTranscribing && !session.IsPaused {
			sessions = append(sessions, session)
		}
	}

	return sessions, nil
}

func (r *redisCache) Exists(ctx context.Context, userID int) (bool, error) {
	key := r.sessionKey(userID)
	exists, err := r.client.Exists(ctx, key).Result()
	return exists > 0, err
}

func (r *redisCache) ListActive(ctx context.Context) ([]int, error) {
	activeUsers, err := r.client.SMembers(ctx, r.activeKey()).Result()
	if err != nil {
		if err == redis.Nil {
			return []int{}, nil
		}
		return nil, err
	}

	userIDs := make([]int, 0, len(activeUsers))
	for _, userIDStr := range activeUsers {
		var userID int
		if _, err := fmt.Sscanf(userIDStr, "%d", &userID); err != nil {
			continue
		}
		userIDs = append(userIDs, userID)
	}

	return userIDs, nil
}

func (r *redisCache) SwitchToMemory() error {
	return fmt.Errorf("cannot switch from Redis to memory at runtime")
}

func (r *redisCache) SwitchToRedis(addr, password string) error {
	// 已经是Redis存储
	return nil
}

func (r *redisCache) GetBackendType() string {
	return "redis"
}

func (r *redisCache) GetStats() CacheStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 获取Redis中的会话数量
	ctx := context.Background()
	pattern := fmt.Sprintf("%s:session:*", r.prefix)
	keys, _ := r.client.Keys(ctx, pattern).Result()

	activeUsers, _ := r.client.SMembers(ctx, r.activeKey()).Result()

	r.stats.TotalSessions = len(keys)
	r.stats.ActiveSessions = len(activeUsers)

	return r.stats
}

func (r *redisCache) incrementStat(operation string) {
	r.mu.Lock()
	r.stats.OperationCounts[operation]++
	r.mu.Unlock()
}

// List 列出所有会话
func (r *redisCache) List(ctx context.Context) ([]*SessionInfo, error) {
	pattern := fmt.Sprintf("%s:session:*", r.prefix)
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	sessions := make([]*SessionInfo, 0, len(keys))
	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Bytes()
		if err != nil {
			continue // 跳过错误的键
		}

		var session SessionInfo
		if err := json.Unmarshal(data, &session); err != nil {
			continue // 跳过无法解析的数据
		}

		sessions = append(sessions, &session)
	}

	r.incrementStat("list")
	return sessions, nil
}

// ==========================================
// 单例管理
// ==========================================

// GetTranscriptionCache 获取全局单例转录缓存
func GetTranscriptionCache() TranscriptionCache {
	cacheOnce.Do(func() {
		// 使用默认logger创建单例
		logger := zap.NewNop() // 可以后续通过SetLogger更新
		globalTranscriptionCache = NewMemoryTranscriptionCache(logger)
	})
	return globalTranscriptionCache
}

// InitTranscriptionCache 初始化全局单例（可选，用于自定义配置）
func InitTranscriptionCache(backendType string, logger *zap.Logger) TranscriptionCache {
	cacheOnce.Do(func() {
		switch backendType {
		case "memory":
			globalTranscriptionCache = NewMemoryTranscriptionCache(logger)
		case "redis":
			// 使用默认Redis配置，实际使用中应该传入真实配置
			logger.Warn("使用默认Redis配置初始化，建议使用InitTranscriptionCacheWithConfig")
			globalTranscriptionCache = NewMemoryTranscriptionCache(logger)
		default:
			logger.Warn("未知后端类型，使用内存后端", zap.String("backend", backendType))
			globalTranscriptionCache = NewMemoryTranscriptionCache(logger)
		}
	})
	return globalTranscriptionCache
}

// InitTranscriptionCacheWithConfig 使用配置文件初始化全局单例
func InitTranscriptionCacheWithConfig(cfg config.RedisConfig, logger *zap.Logger) TranscriptionCache {
	cacheOnce.Do(func() {
		// 根据配置决定使用Redis还是内存
		if cfg.SessionStoreEnabled && cfg.Host != "" {
			// 尝试创建Redis缓存
			if redisCache, err := NewRedisTranscriptionCache(cfg, logger); err == nil {
				globalTranscriptionCache = redisCache
				logger.Info("转录缓存已初始化为Redis后端",
					zap.String("host", cfg.Host),
					zap.Int("port", cfg.Port),
					zap.String("prefix", cfg.SessionStorePrefix))
			} else {
				// Redis连接失败，降级到内存
				logger.Error("Redis连接失败，降级到内存后端", zap.Error(err))
				globalTranscriptionCache = NewMemoryTranscriptionCache(logger)
			}
		} else {
			// 配置为使用内存或Redis未配置
			globalTranscriptionCache = NewMemoryTranscriptionCache(logger)
			logger.Info("转录缓存已初始化为内存后端")
		}
	})
	return globalTranscriptionCache
}

// ==========================================
// 工厂函数（保持兼容性）
// ==========================================

// NewTranscriptionCache 创建转录缓存管理器（非单例版本，保持兼容性）
func NewTranscriptionCache(backendType string, logger *zap.Logger) TranscriptionCache {
	switch backendType {
	case "memory":
		return NewMemoryTranscriptionCache(logger)
	case "redis":
		// TODO: 实现Redis版本
		logger.Warn("Redis后端未实现，使用内存后端")
		return NewMemoryTranscriptionCache(logger)
	default:
		logger.Warn("未知后端类型，使用内存后端", zap.String("backend", backendType))
		return NewMemoryTranscriptionCache(logger)
	}
}
