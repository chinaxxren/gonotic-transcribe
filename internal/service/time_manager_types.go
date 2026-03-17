package service

import (
	"sync"
	"time"

	"github.com/chinaxxren/gonotic/internal/dto"
)

// TimeManagerConfig 定义 UnifiedTimeManager 的配置。
type TimeManagerConfig struct {
	RemainingTimeCacheTTL      time.Duration     // 正常余额的刷新周期（例如 ≥5 分钟剩余时）
	LowBalanceCacheTTL         time.Duration     // 低余额时的紧凑刷新周期
	LowBalanceThresholdSeconds int               // 低余额阈值（秒），用于决定使用哪个 TTL
	BillingInterval            time.Duration     // 计费间隔
	MonitoringInterval         time.Duration     // 监控间隔
	WarningThresholds          map[string]int    // 警告阈值
	WarningWindows             map[string]int    // 警告窗口（秒）
	StatusTranscribing         string            // 转录状态
	StatusPaused               string            // 暂停状态
	StatusCompleted            string            // 完成状态
	Messages                   map[string]string // 消息
}

// DefaultTimeManagerConfig 返回默认配置值。
func DefaultTimeManagerConfig() *TimeManagerConfig {
	return &TimeManagerConfig{
		RemainingTimeCacheTTL:      60 * time.Second, // 高余额每分钟刷新一次即可
		LowBalanceCacheTTL:         20 * time.Second, // 低余额保持 20 秒刷新，确保实时性
		LowBalanceThresholdSeconds: 5 * 60,           // 低于 5 分钟视为低余额
		BillingInterval:            20 * time.Second, // 计费间隔
		MonitoringInterval:         10 * time.Second, // 监控间隔
		WarningThresholds: map[string]int{ // 警告阈值
			"critical": 300,  // 5分钟关键警告阈值
			"warning":  600,  // 10分钟警告阈值
			"info":     3600, // 1小时信息警告阈值
		},
		WarningWindows: map[string]int{ // 警告窗口（秒）
			"critical": 30, // 5分钟到4分钟40秒内提示
			"warning":  60, // 10分钟到9分钟40秒内提示
			"info":     60, // 1小时到59分钟40秒内提示
		},
		StatusTranscribing: "transcribing", // 转录状态
		StatusPaused:       "paused",       // 暂停状态
		StatusCompleted:    "completed",    // 完成状态
		Messages: map[string]string{ // 消息
			"session_not_found": "Session not found",          // 会话未找到
			"not_transcribing":  "Not currently transcribing", // 未转录
			"not_paused":        "Not paused",                 // 未暂停
			"recording_paused":  "Recording paused",           // 录音暂停
			"recording_resumed": "Recording resumed",          // 录音恢复
			"recording_stopped": "Recording stopped",          // 录音停止
			"insufficient_time": "Insufficient time balance",  // 余额不足
			"time_exhausted":    "Time exhausted",             // 余额耗尽
		},
	}
}

// SessionInfo 存储每个用户会话的运行时数据。
// 作为会话ID和会议ID的单一数据源，统一管理WebSocket会话状态和转录缓存
type SessionInfo struct {
	// 基础标识字段
	SessionID   string `json:"session_id"`   // WebSocket会话ID（从SessionSnapshot合并）
	UserID      int    `json:"user_id"`      // 用户ID
	SessionUUID string `json:"session_uuid"` // 会话UUID (单一数据源)
	MeetingID   int    `json:"meeting_id"`   // 会议ID，用于计费记录 (单一数据源)

	// 状态字段
	IsTranscribing bool   `json:"is_transcribing"` // 是否正在转录
	IsPaused       bool   `json:"is_paused"`       // 是否暂停
	Status         string `json:"status"`          // 状态

	TranscriptionStartTime time.Time // 转录开始时间
	CurrentSessionTime     int64     // 当前会话时间（秒）
	PausedSessionTime      int64     // 暂停会话时间（秒）
	TotalDuration          int64     // 总时长（秒）
	PauseCount             int       // 暂停次数

	RemainingTime        int   // 剩余时间
	ConsumedSeconds      int64 // 消耗秒数（已计费秒）
	PersistedSeconds     int64 // 持久化秒数（已落账总额）
	PersistedBaseSeconds int64 // 已落账的转录基准秒数

	LastBillingTime   time.Time // 最后计费时间
	BillingCycleCount int       // 计费周期数
	IsBillingActive   bool      // 计费是否激活

	LastUpdate              time.Time `json:"updated_at"`                 // 最后更新时间
	LastRemainingTimeUpdate time.Time `json:"last_remaining_time_update"` // 最后剩余时间更新时间
	LastActivityTime        time.Time `json:"last_activity_time"`         // 最后活动时间（从SessionSnapshot合并）
	CreatedAt               time.Time `json:"created_at"`                 // 创建时间（从SessionSnapshot合并）

	WarningsSent map[string]bool `json:"warnings_sent"` // 警告已发送

	// WebSocket会话相关字段（从SessionSnapshot合并）
	AudioFormat      string   `json:"audio_format"`      // 音频格式
	LanguageHints    []string `json:"language_hints"`    // 语言提示
	RemoteConnecting bool     `json:"remote_connecting"` // Remote连接中
	RemoteReady      bool     `json:"remote_ready"`      // Remote就绪
	ConnectionState  string   `json:"connection_state"`  // 连接状态

	// 保持向后兼容的字段
	AudioFormatOverride   string   `json:"audio_format_override"`   // 音频格式覆盖（保留）
	LanguageHintsOverride []string `json:"language_hints_override"` // 语言提示覆盖（保留）

	BillingMultiplier int // 计费倍数

	// 扣费竞争控制字段
	mu                sync.RWMutex // 会话级读写锁
	IsSettling        bool         // 是否正在结算中（防止实时扣费与停止结算竞争）
	SettlementStarted time.Time    // 结算开始时间
}

// NewSessionInfo 初始化 SessionInfo。
func NewSessionInfo(userID int, sessionUUID string, meetingID int) *SessionInfo {
	now := time.Now()
	return &SessionInfo{
		// 基础标识字段
		UserID:      userID,
		SessionUUID: sessionUUID,
		MeetingID:   meetingID,
		Status:      "created",

		// 时间字段
		TranscriptionStartTime:  now,
		LastBillingTime:         now,
		LastUpdate:              now,
		LastRemainingTimeUpdate: now,
		LastActivityTime:        now,
		CreatedAt:               now,

		// 初始化字段
		WarningsSent:      make(map[string]bool),
		BillingMultiplier: 1, // Default multiplier

		// WebSocket会话字段初始化
		AudioFormat:      "",
		LanguageHints:    []string{},
		RemoteConnecting: false,
		RemoteReady:      false,
		ConnectionState:  "created",
	}
}

// NewSessionInfoWithSessionID 创建带SessionID的SessionInfo（用于WebSocket会话）
func NewSessionInfoWithSessionID(sessionID string, userID int, sessionUUID string, meetingID int) *SessionInfo {
	session := NewSessionInfo(userID, sessionUUID, meetingID)
	session.SessionID = sessionID
	return session
}

func (s *SessionInfo) Clone() *SessionInfo {
	if s == nil {
		return nil
	}

	copySession := &SessionInfo{}
	copySession.SessionID = s.SessionID
	copySession.UserID = s.UserID
	copySession.SessionUUID = s.SessionUUID
	copySession.MeetingID = s.MeetingID

	copySession.IsTranscribing = s.IsTranscribing
	copySession.IsPaused = s.IsPaused
	copySession.Status = s.Status

	copySession.TranscriptionStartTime = s.TranscriptionStartTime
	copySession.CurrentSessionTime = s.CurrentSessionTime
	copySession.PausedSessionTime = s.PausedSessionTime
	copySession.TotalDuration = s.TotalDuration
	copySession.PauseCount = s.PauseCount

	copySession.RemainingTime = s.RemainingTime
	copySession.ConsumedSeconds = s.ConsumedSeconds
	copySession.PersistedSeconds = s.PersistedSeconds
	copySession.PersistedBaseSeconds = s.PersistedBaseSeconds

	copySession.LastBillingTime = s.LastBillingTime
	copySession.BillingCycleCount = s.BillingCycleCount
	copySession.IsBillingActive = s.IsBillingActive

	copySession.LastUpdate = s.LastUpdate
	copySession.LastRemainingTimeUpdate = s.LastRemainingTimeUpdate
	copySession.LastActivityTime = s.LastActivityTime
	copySession.CreatedAt = s.CreatedAt

	if s.WarningsSent != nil {
		copySession.WarningsSent = make(map[string]bool, len(s.WarningsSent))
		for k, v := range s.WarningsSent {
			copySession.WarningsSent[k] = v
		}
	}
	copySession.AudioFormat = s.AudioFormat
	if s.LanguageHints != nil {
		copySession.LanguageHints = append([]string(nil), s.LanguageHints...)
	}
	copySession.RemoteConnecting = s.RemoteConnecting
	copySession.RemoteReady = s.RemoteReady
	copySession.ConnectionState = s.ConnectionState

	copySession.AudioFormatOverride = s.AudioFormatOverride
	if s.LanguageHintsOverride != nil {
		copySession.LanguageHintsOverride = append([]string(nil), s.LanguageHintsOverride...)
	}

	copySession.BillingMultiplier = s.BillingMultiplier
	copySession.IsSettling = s.IsSettling
	copySession.SettlementStarted = s.SettlementStarted

	return copySession
}

// UpdateCurrentTime 根据当前时间刷新持续时间和消耗计数器。
func (s *SessionInfo) UpdateCurrentTime() {
	if s.IsTranscribing && !s.IsPaused {
		elapsed := int64(time.Since(s.TranscriptionStartTime) / time.Second)
		if elapsed < 0 {
			elapsed = 0
		}
		s.CurrentSessionTime = elapsed
		s.TotalDuration = s.PausedSessionTime + s.CurrentSessionTime

		elapsedSinceUpdate := int64(time.Since(s.LastUpdate) / time.Second)
		if elapsedSinceUpdate > 0 {
			s.ConsumedSeconds += elapsedSinceUpdate
		}
	}
	s.LastUpdate = time.Now()
}

// CalculateFinalTimes 在会话停止时完成持续时间统计。
func (s *SessionInfo) CalculateFinalTimes() (totalSessionTime int64, finalTotalDuration int64, finalConsumedSeconds int64) {
	if s.IsTranscribing && !s.IsPaused {
		currentSessionTime := int64(time.Since(s.TranscriptionStartTime) / time.Second)
		if currentSessionTime < 0 {
			currentSessionTime = 0
		}
		s.CurrentSessionTime = currentSessionTime
		s.TotalDuration = s.PausedSessionTime + currentSessionTime

		elapsedSinceUpdate := int64(time.Since(s.LastUpdate) / time.Second)
		if elapsedSinceUpdate > 0 {
			s.ConsumedSeconds += elapsedSinceUpdate
		}
	}

	totalSessionTime = s.TotalDuration
	finalTotalDuration = s.TotalDuration
	finalConsumedSeconds = s.ConsumedSeconds
	return
}

// ==========================================
// Translation Billing Helpers
// ==========================================

// getBillingMultiplier 返回会话的有效计费乘数。
func (s *SessionInfo) getBillingMultiplier() int {
	if s.BillingMultiplier <= 0 {
		return 1
	}
	return s.BillingMultiplier
}

// computeUsageSeconds 根据基础会议时长和倍率返回转录/翻译秒数。
func computeUsageSeconds(baseSeconds int64, multiplier int) (int, int) {
	if baseSeconds < 0 {
		baseSeconds = 0
	}

	transcription := int(baseSeconds)
	if multiplier <= 1 {
		return transcription, 0
	}

	translation := transcription * (multiplier - 1)
	return transcription, translation
}

// buildUsageRequest 构建 AccountStateFacade 消耗请求所需的秒数明细。
func buildUsageRequest(userID int, businessID int, baseSeconds int64, multiplier int, source string) (dto.AccountConsumeRequest, int, int) {
	transcriptionSeconds, translationSeconds := computeUsageSeconds(baseSeconds, multiplier)
	totalSeconds := transcriptionSeconds + translationSeconds
	return dto.AccountConsumeRequest{
		UserID:               userID,
		Seconds:              totalSeconds,
		Source:               source,
		BusinessID:           businessID,
		TranscriptionSeconds: transcriptionSeconds,
		TranslationSeconds:   translationSeconds,
	}, transcriptionSeconds, translationSeconds
}

// TryStartSettlement 尝试开始结算，如果已经在结算中则返回false
func (s *SessionInfo) TryStartSettlement() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsSettling {
		return false // 已经在结算中
	}

	s.IsSettling = true
	s.SettlementStarted = time.Now()
	s.IsBillingActive = false // 停止实时扣费
	return true
}

// FinishSettlement 完成结算，释放结算锁
func (s *SessionInfo) FinishSettlement() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.IsSettling = false
	s.SettlementStarted = time.Time{}
}

// IsInSettlement 检查是否正在结算中（线程安全）
func (s *SessionInfo) IsInSettlement() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.IsSettling
}

// SafeUpdateBillingState 线程安全地更新计费状态
func (s *SessionInfo) SafeUpdateBillingState(active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果正在结算中，不允许重新激活计费
	if s.IsSettling && active {
		return
	}

	s.IsBillingActive = active
}

// SafeGetBillingState 线程安全地获取计费状态
func (s *SessionInfo) SafeGetBillingState() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.IsBillingActive && !s.IsSettling
}
