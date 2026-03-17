// Package config provides configuration management for the NoticAI backend.
// It loads configuration from environment variables using viper and validates
// all required settings at startup.
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for the application.
// It is loaded from environment variables at startup and validated.
type Config struct {
	Server      ServerConfig
	Database    DatabaseConfig
	OSS         OSSConfig
	Summary     SummaryConfig
	Auth        AuthConfig
	Payment     PaymentConfig
	Billing     BillingConfig
	JWT         JWTConfig
	Security    SecurityConfig
	Remote      RemoteConfig
	Audio       AudioConfig
	STT         STTConfig
	Translation TranslationConfig
	Logging     LoggingConfig
	Email       EmailConfig
	Scheduler   SchedulerConfig
	MeetingSync MeetingSyncConfig
	Anthropic   AnthropicConfig
	Storage     StorageConfig
	Cache       CacheConfig
	Redis       RedisConfig
	Monitoring  MonitoringConfig
	Enterprise  EnterpriseConfig
	Frontend    FrontendConfig
}

// ServerConfig contains HTTP server configuration.
type ServerConfig struct {
	Host         string        // Server host address
	Port         int           // Server port number
	Environment  string        // Environment (development, staging, production)
	Debug        bool          // Enable debug mode
	Reload       bool          // Enable hot reload
	AccessLog    bool          // Enable access logging
	ReadTimeout  time.Duration // HTTP read timeout
	WriteTimeout time.Duration // HTTP write timeout
	IdleTimeout  time.Duration // HTTP idle timeout
}

// DatabaseConfig contains database configuration.
type DatabaseConfig struct {
	URL          string        // Database connection URL
	MaxConns     int           // Maximum number of connections
	MaxIdle      int           // Maximum number of idle connections
	ConnTimeout  time.Duration // Connection timeout
	ConnLifetime time.Duration // Connection lifetime
}

// OSSConfig contains Aliyun OSS configuration for transcript storage.
type OSSConfig struct {
	Endpoint        string        // OSS endpoint
	AccessKeyID     string        // OSS access key ID
	AccessKeySecret string        // OSS access key secret
	Bucket          string        // OSS bucket name
	KeyPrefix       string        // Object key prefix
	PresignTTL      time.Duration // Presigned URL TTL
}

// SummaryConfig contains configuration for summary quotas and template storage.
type SummaryConfig struct {
	FreeQuota         int
	PaygQuota         int
	SpecialOfferQuota int
	PremiumQuota      int
	ProQuota          int
	ProMiniQuota      int
	TemplateBasePath  string
	TemplateOSSBucket string
}

// AuthConfig contains authentication related toggles.
type AuthConfig struct {
	ForceDefaultVerificationCode bool   // Force using default verification code (development helper)
	DefaultVerificationCode      string // Default verification code value
}

// PaymentConfig contains Apple IAP related configuration.
type PaymentConfig struct {
	AppleSharedSecret         string // Shared secret for App Store server notifications
	AppleBundleID             string // Expected bundle identifier for Apple receipts
	SkipReceiptValidation     bool   // Allow skipping real receipt validation (development/testing)
	SkipNotificationSignature bool   // Allow skipping Apple Server Notification JWS signature verification
	AppleProductHourPack      string // Product ID for hour pack (pay-as-you-go)
	AppleProductSpecialOffer  string // Product ID for one-time special offer (PAYG-like)
	AppleProductYearSub       string // Product ID for yearly subscription
	AppleProductYearPro       string // Product ID for yearly pro subscription
	AppleProductProMini       string // Product ID for yearly pro mini subscription
	AppleServerIssuerID       string // App Store Server API issuer ID
	AppleServerKeyID          string // App Store Server API key ID
	AppleServerPrivateKeyB64  string // App Store Server API private key (.p8) in base64-encoded PEM
	AppleServerPrivateKeyPath string // App Store Server API private key (.p8) filesystem path
	AppleConsumptionConsented bool   // Whether user consent has been obtained for sending consumption info
}

// BillingConfig contains configurable durations and quotas for billing plans.
// All units are expressed in seconds to avoid ambiguity.
type BillingConfig struct {
	FreeCycleSeconds            int64
	FreeAllowanceSeconds        int
	PaygAllowanceSeconds        int
	SpecialOfferSeconds         int
	SpecialOfferAmountCents     int
	SpecialOfferValiditySeconds int64
	PaygValiditySeconds         int64
	PremiumCycleSeconds         int64
	PremiumCycleGrantSeconds    int
	ProCycleSeconds             int64
	ProTotalSeconds             int
	ProMiniCycleSeconds         int64
	ProMiniTotalSeconds         int
}

// JWTConfig contains JWT token configuration.
type JWTConfig struct {
	Secret     string        // JWT signing secret
	Algorithm  string        // JWT signing algorithm (HS256)
	Expiration time.Duration // Token expiration duration
}

// SecurityConfig contains security-related configuration.
type SecurityConfig struct {
	SecretKey            string        // Application secret key
	CORSOrigins          []string      // Allowed CORS origins
	CORSAllowCredentials bool          // Allow credentials in CORS
	RateLimitEnabled     bool          // Enable rate limiting
	RateLimitRequests    int           // Max requests per window
	RateLimitWindow      time.Duration // Rate limit time window
	RateLimitUseRedis    bool          // Use Redis for distributed rate limiting
	RateLimitRedisPrefix string        // Redis key prefix for rate limiting
}

// RemoteConfig contains remote STT service configuration.
type RemoteConfig struct {
	WebSocketURL string        // Remote WebSocket URL
	RestURL      string        // Remote REST API URL
	APIKey       string        // Remote API key
	Timeout      time.Duration // Request timeout
	MaxRetries   int           // Maximum retry attempts
}

// AudioConfig contains audio processing configuration.
type AudioConfig struct {
	SampleRate int // Audio sample rate (Hz)
	Channels   int // Number of audio channels
	BufferSize int // Audio buffer size (bytes)
}

// STTConfig contains speech-to-text configuration.
type STTConfig struct {
	Model                             string   // STT model name
	AudioFormat                       string   // Audio format (auto, wav, mp3, etc.)
	LanguageHints                     []string // Language hints for STT
	EnableProfanityFilter             bool     // Enable profanity filtering
	EnableSpeakerDiarization          bool     // Enable speaker diarization
	EnableGlobalSpeakerIdentification bool     // Enable speaker identification
	EnableSpeakerChangeDetection      bool     // Enable speaker change detection
}

// TranslationConfig contains translation service configuration.
type TranslationConfig struct {
	Enabled         bool     // Enable translation
	Type            string   // Translation type
	TargetLanguages []string // Target languages for translation
}

// LoggingConfig contains logging configuration.
type LoggingConfig struct {
	Level       string // Log level (debug, info, warn, error)
	Format      string // Log format (json, console)
	File        string // Log file path
	MaxSize     int    // Max log file size (MB)
	BackupCount int    // Number of backup log files
	MaxAge      int    // Max age of log files (days)
	Compress    bool   // Compress old log files
}

// EmailConfig contains SMTP email configuration.
type EmailConfig struct {
	SMTPHost       string // SMTP server host
	SMTPPort       int    // SMTP server port
	SMTPUseTLS     bool   // Use TLS for SMTP
	SMTPUsername   string // SMTP username
	SMTPPassword   string // SMTP password
	FromEmail      string // From email address
	FromName       string // From name
	SystemEmail    string // System email address
	SystemPassword string // System email password
}

// SchedulerConfig contains background scheduler settings.
type SchedulerConfig struct {
	Enabled                      bool
	Interval                     time.Duration
	SubscriptionBatchSize        int
	SubscriptionLookaheadSeconds int64
	FreeGrantBatchSize           int
}

type MeetingSyncConfig struct {
	Enabled  bool
	Interval time.Duration
	Limit    int
}

// AnthropicConfig contains Anthropic API configuration.
type AnthropicConfig struct {
	APIKey  string        // Anthropic API key
	Model   string        // Model name (claude-3-opus, etc.)
	Timeout time.Duration // API call timeout
}

// StorageConfig contains file storage configuration.
type StorageConfig struct {
	TranscriptionDir          string   // Transcription storage directory
	TempAudioDir              string   // Temporary audio directory
	LogDir                    string   // Log storage directory
	DataDir                   string   // Data storage directory
	MaxTranscriptionFileSize  int64    // Max transcription file size (bytes)
	MaxTempAudioFileSize      int64    // Max temp audio file size (bytes)
	AllowedTranscriptionTypes []string // Allowed transcription MIME types
	AllowedAudioTypes         []string // Allowed audio MIME types
}

// CacheConfig contains caching configuration.
type CacheConfig struct {
	Enabled bool          // Enable caching
	TTL     time.Duration // Cache TTL
	MaxSize int           // Maximum cache size
}

// RedisConfig contains redis connection details and feature toggles.
type RedisConfig struct {
	Host                string        // Redis host
	Port                int           // Redis port
	Password            string        // Redis password
	DB                  int           // Redis logical DB index
	TLSEnabled          bool          // Enable TLS
	BalanceCachePrefix  string        // Key prefix for balance cache entries
	DialTimeout         time.Duration // Optional dial timeout
	SessionStoreEnabled bool          // Enable Redis-backed WebSocket session store
	SessionStorePrefix  string        // Key prefix for session snapshots
	SessionStoreTTL     time.Duration // TTL for session snapshots
}

// MonitoringConfig contains monitoring configuration.
type MonitoringConfig struct {
	Enabled             bool          // Enable monitoring
	MetricsEnabled      bool          // Enable metrics collection
	HealthCheckInterval time.Duration // Health check interval
}

// EnterpriseConfig contains enterprise mode configuration.
type EnterpriseConfig struct {
	Enabled              bool   // Enable enterprise mode
	APIKeysJSON          string // Enterprise API keys (JSON format)
	LoadBalancerStrategy string // Load balancer strategy
	MaxQueueSize         int    // Maximum queue size
	QueueTimeout         int    // Queue timeout (seconds)
	HealthCheckInterval  int    // Health check interval (seconds)
	AutoScalingEnabled   bool   // Enable auto scaling
}

// FrontendConfig contains frontend-related configuration.
type FrontendConfig struct {
	Port    int    // Frontend port
	BaseURL string // Frontend base URL
	WSURL   string // Frontend WebSocket URL
	APIURL  string // Frontend API URL
}

// Load loads configuration from environment variables.
// It returns an error if required configuration is missing or invalid.
//
// Returns:
//   - *Config: Loaded configuration
//   - error: Error if configuration loading fails
func Load() (*Config, error) {
	v := viper.New()

	// Set config file paths
	v.SetConfigName(".env")
	v.SetConfigType("env")
	v.AddConfigPath(".")
	v.AddConfigPath("..")
	v.AddConfigPath("./backend")

	// Enable environment variable reading
	v.AutomaticEnv()

	// Try to read config file (optional)
	_ = v.ReadInConfig()

	cfg := &Config{}

	// Load server configuration
	port := v.GetInt("PORT")
	if port == 0 {
		port = 8090 // Default port for transcription service
	}
	cfg.Server = ServerConfig{
		Host:         v.GetString("HOST"),
		Port:         port,
		Environment:  v.GetString("ENVIRONMENT"),
		Debug:        v.GetBool("DEBUG"),
		Reload:       v.GetBool("RELOAD"),
		AccessLog:    v.GetBool("ACCESS_LOG"),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Load database configuration
	cfg.Database = DatabaseConfig{
		URL:          v.GetString("DATABASE_URL"),
		MaxConns:     v.GetInt("DB_POOL_SIZE"),
		MaxIdle:      v.GetInt("DB_MAX_OVERFLOW"),
		ConnTimeout:  time.Duration(v.GetInt("DB_POOL_TIMEOUT")) * time.Second,
		ConnLifetime: time.Duration(v.GetInt("DB_POOL_RECYCLE")) * time.Second,
	}

	// Load OSS configuration
	cfg.OSS = OSSConfig{
		Endpoint:        v.GetString("OSS_ENDPOINT"),
		AccessKeyID:     v.GetString("OSS_ACCESS_KEY_ID"),
		AccessKeySecret: v.GetString("OSS_ACCESS_KEY_SECRET"),
		Bucket:          v.GetString("OSS_BUCKET"),
		KeyPrefix:       v.GetString("OSS_KEY_PREFIX"),
		PresignTTL:      time.Duration(v.GetInt("OSS_PRESIGN_TTL_SECONDS")) * time.Second,
	}
	if cfg.OSS.KeyPrefix == "" {
		cfg.OSS.KeyPrefix = "meeting_records"
	}
	if cfg.OSS.PresignTTL == 0 {
		cfg.OSS.PresignTTL = 7 * 24 * time.Hour
	}

	// Load summary configuration
	cfg.Summary = SummaryConfig{
		FreeQuota:         getIntWithDefault(v, "SUMMARY_FREE_QUOTA", 1),
		PaygQuota:         getIntWithDefault(v, "SUMMARY_PAYG_QUOTA", 5),
		SpecialOfferQuota: getIntWithDefault(v, "SUMMARY_SPECIAL_OFFER_QUOTA", 10),
		PremiumQuota:      getIntWithDefault(v, "SUMMARY_PREMIUM_QUOTA", 20),
		ProQuota:          getIntWithDefault(v, "SUMMARY_PRO_QUOTA", 600),
		ProMiniQuota:      getIntWithDefault(v, "SUMMARY_PRO_MINI_QUOTA", 360),
		TemplateBasePath:  v.GetString("SUMMARY_TEMPLATE_BASE_PATH"),
		TemplateOSSBucket: v.GetString("SUMMARY_OSS_BUCKET"),
	}
	if cfg.Summary.TemplateOSSBucket == "" {
		cfg.Summary.TemplateOSSBucket = v.GetString("SUMMARY_S3_BUCKET")
	}
	if cfg.Summary.TemplateBasePath == "" {
		cfg.Summary.TemplateBasePath = "summary"
	}
	if cfg.Summary.TemplateOSSBucket == "" {
		cfg.Summary.TemplateOSSBucket = cfg.OSS.Bucket
	}

	// Load JWT configuration
	cfg.JWT = JWTConfig{
		Secret:     v.GetString("JWT_SECRET"),
		Algorithm:  v.GetString("JWT_ALGORITHM"),
		Expiration: time.Duration(v.GetInt("JWT_EXPIRATION_HOURS")) * time.Hour,
	}

	// Load billing configuration with defaults identical to legacy constants
	cfg.Billing = BillingConfig{
		FreeCycleSeconds:            getInt64WithDefault(v, "BILLING_FREE_CYCLE_SECONDS", 30*24*3600),
		FreeAllowanceSeconds:        getIntWithDefault(v, "BILLING_FREE_ALLOWANCE_SECONDS", 40*60),
		PaygAllowanceSeconds:        getIntWithDefault(v, "BILLING_PAYG_ALLOWANCE_SECONDS", 18000),
		SpecialOfferSeconds:         getIntWithDefault(v, "BILLING_SPECIAL_OFFER_SECONDS", 36000),
		SpecialOfferAmountCents:     getIntWithDefault(v, "BILLING_SPECIAL_OFFER_AMOUNT_CENTS", 990),
		SpecialOfferValiditySeconds: getInt64WithDefault(v, "BILLING_SPECIAL_OFFER_VALIDITY_SECONDS", 30*24*3600),
		PaygValiditySeconds:         getInt64WithDefault(v, "BILLING_PAYG_VALIDITY_SECONDS", 30*24*3600),
		PremiumCycleSeconds:         getInt64WithDefault(v, "BILLING_PREMIUM_CYCLE_SECONDS", 30*24*3600),
		PremiumCycleGrantSeconds:    getIntWithDefault(v, "BILLING_PREMIUM_CYCLE_GRANT_SECONDS", 1200*60),
		ProCycleSeconds:             getInt64WithDefault(v, "BILLING_PRO_CYCLE_SECONDS", 360*24*3600),
		ProTotalSeconds:             getIntWithDefault(v, "BILLING_PRO_TOTAL_SECONDS", 1200*60*60),
		ProMiniCycleSeconds:         getInt64WithDefault(v, "BILLING_PRO_MINI_CYCLE_SECONDS", 360*24*3600),
		ProMiniTotalSeconds:         getIntWithDefault(v, "BILLING_PRO_MINI_TOTAL_SECONDS", 360*60*60),
	}

	// Load security configuration
	corsOrigins := v.GetString("CORS_ORIGINS")
	cfg.Security = SecurityConfig{
		SecretKey:            v.GetString("SECRET_KEY"),
		CORSOrigins:          strings.Split(corsOrigins, ","),
		CORSAllowCredentials: v.GetBool("CORS_ALLOW_CREDENTIALS"),
		RateLimitEnabled:     v.GetBool("RATE_LIMIT_ENABLED"),
		RateLimitRequests:    v.GetInt("RATE_LIMIT_REQUESTS"),
		RateLimitWindow:      time.Duration(v.GetInt("RATE_LIMIT_WINDOW")) * time.Second,
		RateLimitUseRedis:    v.GetBool("RATE_LIMIT_USE_REDIS"),
		RateLimitRedisPrefix: v.GetString("RATE_LIMIT_REDIS_PREFIX"),
	}
	if cfg.Security.RateLimitRedisPrefix == "" {
		cfg.Security.RateLimitRedisPrefix = "ratelimit"
	}

	// Load remote service configuration
	// 从企业级密钥列表中获取第一个密钥
	apiKey := ""
	enterpriseKeysJSON := v.GetString("ENTERPRISE_API_KEYS")
	if enterpriseKeysJSON != "" {
		// 简单解析：提取第一个密钥
		// 格式: [{"key":"xxx",...}]
		if strings.Contains(enterpriseKeysJSON, `"key"`) {
			start := strings.Index(enterpriseKeysJSON, `"key":"`) + 7
			if start > 6 {
				end := strings.Index(enterpriseKeysJSON[start:], `"`)
				if end > 0 {
					apiKey = enterpriseKeysJSON[start : start+end]
				}
			}
		}
	}

	cfg.Remote = RemoteConfig{
		WebSocketURL: v.GetString("REMOTE_WEBSOCKET_URL"),
		RestURL:      v.GetString("REMOTE_REST_URL"),
		APIKey:       apiKey,
		Timeout:      time.Duration(v.GetInt("REMOTE_TIMEOUT")) * time.Second,
		MaxRetries:   v.GetInt("REMOTE_MAX_RETRIES"),
	}

	// Load audio configuration
	cfg.Audio = AudioConfig{
		SampleRate: v.GetInt("AUDIO_SAMPLE_RATE"),
		Channels:   v.GetInt("AUDIO_CHANNELS"),
		BufferSize: v.GetInt("AUDIO_BUFFER_SIZE"),
	}

	// Load STT configuration
	languageHints := v.GetString("STT_LANGUAGE_HINTS")
	cfg.STT = STTConfig{
		Model:                             v.GetString("STT_MODEL"),
		AudioFormat:                       v.GetString("STT_AUDIO_FORMAT"),
		LanguageHints:                     strings.Split(languageHints, ","),
		EnableProfanityFilter:             v.GetBool("STT_ENABLE_PROFANITY_FILTER"),
		EnableSpeakerDiarization:          v.GetBool("STT_ENABLE_SPEAKER_DIARIZATION"),
		EnableGlobalSpeakerIdentification: v.GetBool("STT_ENABLE_GLOBAL_SPEAKER_IDENTIFICATION"),
		EnableSpeakerChangeDetection:      v.GetBool("STT_ENABLE_SPEAKER_CHANGE_DETECTION"),
	}

	// Load translation configuration
	targetLangs := v.GetString("TRANSLATION_TARGET_LANGUAGES")
	cfg.Translation = TranslationConfig{
		Type:            v.GetString("TRANSLATION_TYPE"),
		TargetLanguages: strings.Split(targetLangs, ","),
	}

	// Load logging configuration with defaults
	logMaxSize := v.GetInt("LOG_MAX_SIZE")
	if logMaxSize <= 0 {
		logMaxSize = 5 // Default 5MB
	}
	logBackupCount := v.GetInt("LOG_BACKUP_COUNT")
	if logBackupCount <= 0 {
		logBackupCount = 10 // Keep 10 backup files
	}
	logMaxAge := v.GetInt("LOG_MAX_AGE_DAYS")
	if logMaxAge <= 0 {
		logMaxAge = 30 // Keep logs for 30 days
	}

	cfg.Logging = LoggingConfig{
		Level:       v.GetString("LOG_LEVEL"),
		Format:      v.GetString("LOG_FORMAT"),
		File:        v.GetString("LOG_FILE"),
		MaxSize:     logMaxSize,
		BackupCount: logBackupCount,
		MaxAge:      logMaxAge,
		Compress:    v.GetBool("LOG_COMPRESS"),
	}

	// Load email configuration
	cfg.Email = EmailConfig{
		SMTPHost:       v.GetString("SMTP_HOST"),
		SMTPPort:       v.GetInt("SMTP_PORT"),
		SMTPUseTLS:     v.GetBool("SMTP_USE_TLS"),
		SMTPUsername:   v.GetString("SMTP_USERNAME"),
		SMTPPassword:   v.GetString("SMTP_PASSWORD"),
		FromEmail:      v.GetString("EMAIL_FROM"),
		FromName:       v.GetString("EMAIL_FROM_NAME"),
		SystemEmail:    v.GetString("EMAIL_SYSTEM"),
		SystemPassword: v.GetString("EMAIL_PASSWORD"),
	}

	// Load scheduler configuration
	schedulerInterval := time.Duration(v.GetInt("SCHEDULER_INTERVAL_SECONDS")) * time.Second
	if schedulerInterval <= 0 {
		schedulerInterval = time.Hour
	}

	subscriptionBatch := v.GetInt("SCHEDULER_SUBSCRIPTION_BATCH")
	if subscriptionBatch <= 0 {
		subscriptionBatch = 100
	}

	freeBatch := v.GetInt("SCHEDULER_FREE_BATCH")
	if freeBatch <= 0 {
		freeBatch = 100
	}

	lookaheadSeconds := getInt64WithDefault(v, "SCHEDULER_SUBSCRIPTION_LOOKAHEAD_SECONDS", 24*3600)
	if lookaheadSeconds < 0 {
		lookaheadSeconds = 0
	}

	cfg.Scheduler = SchedulerConfig{
		Enabled:                      v.GetBool("SCHEDULER_ENABLED"),
		Interval:                     schedulerInterval,
		SubscriptionBatchSize:        subscriptionBatch,
		SubscriptionLookaheadSeconds: lookaheadSeconds,
		FreeGrantBatchSize:           freeBatch,
	}

	// Load meeting sync worker configuration
	meetingSyncInterval := time.Duration(v.GetInt("MEETING_SYNC_INTERVAL_SECONDS")) * time.Second
	if meetingSyncInterval <= 0 {
		meetingSyncInterval = time.Minute
	}
	meetingSyncLimit := v.GetInt("MEETING_SYNC_LIMIT")
	if meetingSyncLimit <= 0 {
		meetingSyncLimit = 20
	}
	cfg.MeetingSync = MeetingSyncConfig{
		Enabled:  v.GetBool("MEETING_SYNC_ENABLED"),
		Interval: meetingSyncInterval,
		Limit:    meetingSyncLimit,
	}

	// Load auth configuration
	cfg.Auth = AuthConfig{
		ForceDefaultVerificationCode: v.GetBool("AUTH_FORCE_DEFAULT_CODE"),
		DefaultVerificationCode:      v.GetString("AUTH_DEFAULT_CODE"),
	}
	if cfg.Auth.DefaultVerificationCode == "" {
		cfg.Auth.DefaultVerificationCode = "123456"
	}

	// Load payment configuration
	appleBundleID := v.GetString("APPLE_BUNDLE_ID")
	if appleBundleID == "" {
		appleBundleID = "com.getnotic.iosapp"
	}
	appleServerIssuerID := v.GetString("APPLE_SERVER_API_ISSUER_ID")
	if appleServerIssuerID == "" {
		appleServerIssuerID = "d2e4a9d3-95c7-413e-8475-4c7bb9e4301a"
	}
	appleServerKeyID := v.GetString("APPLE_SERVER_API_KEY_ID")
	if appleServerKeyID == "" {
		appleServerKeyID = "PNAYG4SZ5S"
	}
	appleServerPrivateKeyPath := v.GetString("APPLE_SERVER_API_PRIVATE_KEY_PATH")
	if appleServerPrivateKeyPath == "" {
		appleServerPrivateKeyPath = "AuthKey_PNAYG4SZ5S.p8"
	}
	appleProductHourPack := v.GetString("APPLE_PRODUCT_HOUR_PACK")
	if appleProductHourPack == "" {
		appleProductHourPack = "com.getnotic.iosapp.payg"
	}
	appleProductSpecialOffer := v.GetString("APPLE_PRODUCT_SPECIAL_OFFER")
	if appleProductSpecialOffer == "" {
		appleProductSpecialOffer = "com.getnotic.iosapp.hd"
	}
	appleProductYearSub := v.GetString("APPLE_PRODUCT_YEAR_SUB")
	if appleProductYearSub == "" {
		appleProductYearSub = "com.getnotic.iosapp.premium"
	}
	appleProductYearPro := v.GetString("APPLE_PRODUCT_YEAR_PRO")
	if appleProductYearPro == "" {
		appleProductYearPro = "com.getnotic.iosapp.pro"
	}
	appleProductProMini := v.GetString("APPLE_PRODUCT_YEAR_PRO_MINI")
	if appleProductProMini == "" {
		appleProductProMini = "com.getnotic.iosapp.prominis"
	}

	cfg.Payment = PaymentConfig{
		AppleSharedSecret:         v.GetString("APPLE_SHARED_SECRET"),
		AppleBundleID:             appleBundleID,
		SkipReceiptValidation:     v.GetBool("APPLE_SKIP_RECEIPT_VALIDATION"),
		SkipNotificationSignature: v.GetBool("APPLE_SKIP_NOTIFICATION_SIGNATURE"),
		AppleProductHourPack:      appleProductHourPack,
		AppleProductSpecialOffer:  appleProductSpecialOffer,
		AppleProductYearSub:       appleProductYearSub,
		AppleProductYearPro:       appleProductYearPro,
		AppleProductProMini:       appleProductProMini,
		AppleServerIssuerID:       appleServerIssuerID,
		AppleServerKeyID:          appleServerKeyID,
		AppleServerPrivateKeyB64:  v.GetString("APPLE_SERVER_API_PRIVATE_KEY_B64"),
		AppleServerPrivateKeyPath: appleServerPrivateKeyPath,
		AppleConsumptionConsented: v.GetBool("APPLE_CONSUMPTION_CONSENTED"),
	}

	// 生产环境安全检查
	if cfg.Server.Environment == "production" {
		if cfg.Payment.SkipReceiptValidation {
			return nil, fmt.Errorf("SECURITY: SkipReceiptValidation cannot be true in production environment")
		}
		if cfg.Payment.SkipNotificationSignature {
			return nil, fmt.Errorf("SECURITY: SkipNotificationSignature cannot be true in production environment")
		}
	}

	// Load Anthropic configuration
	cfg.Anthropic = AnthropicConfig{
		APIKey:  v.GetString("ANTHROPIC_API_KEY"),
		Model:   v.GetString("ANTHROPIC_MODEL"),
		Timeout: time.Duration(getIntWithDefault(v, "ANTHROPIC_TIMEOUT_SECONDS", 300)) * time.Second,
	}

	// Load storage configuration
	cfg.Storage = StorageConfig{
		TranscriptionDir:          v.GetString("TRANSCRIPTION_STORAGE_DIR"),
		TempAudioDir:              v.GetString("TEMP_AUDIO_DIR"),
		LogDir:                    v.GetString("LOG_STORAGE_DIR"),
		DataDir:                   v.GetString("DATA_STORAGE_DIR"),
		MaxTranscriptionFileSize:  v.GetInt64("MAX_TRANSCRIPTION_FILE_SIZE"),
		MaxTempAudioFileSize:      v.GetInt64("MAX_TEMP_AUDIO_FILE_SIZE"),
		AllowedTranscriptionTypes: parseJSONArray(v.GetString("ALLOWED_TRANSCRIPTION_TYPES")),
		AllowedAudioTypes:         parseJSONArray(v.GetString("ALLOWED_AUDIO_TYPES")),
	}

	// Load cache configuration
	cfg.Cache = CacheConfig{
		Enabled: v.GetBool("CACHE_ENABLED"),
		TTL:     time.Duration(v.GetInt("CACHE_TTL")) * time.Second,
		MaxSize: v.GetInt("CACHE_MAX_SIZE"),
	}
	if cfg.Cache.TTL <= 0 {
		cfg.Cache.TTL = 30 * time.Second
	}

	// Load Redis configuration
	redisPort := v.GetInt("REDIS_PORT")
	if redisPort == 0 {
		redisPort = 6379
	}
	redisDB := v.GetInt("REDIS_DB")
	cfg.Redis = RedisConfig{
		Host:                v.GetString("REDIS_HOST"),
		Port:                redisPort,
		Password:            v.GetString("REDIS_PASSWORD"),
		DB:                  redisDB,
		TLSEnabled:          v.GetBool("REDIS_TLS_ENABLED"),
		BalanceCachePrefix:  v.GetString("REDIS_BALANCE_CACHE_PREFIX"),
		DialTimeout:         time.Duration(v.GetInt("REDIS_DIAL_TIMEOUT_SECONDS")) * time.Second,
		SessionStoreEnabled: v.GetBool("REDIS_SESSION_STORE_ENABLED"),
		SessionStorePrefix:  v.GetString("REDIS_SESSION_STORE_PREFIX"),
		SessionStoreTTL:     time.Duration(v.GetInt("REDIS_SESSION_STORE_TTL_SECONDS")) * time.Second,
	}
	if cfg.Redis.BalanceCachePrefix == "" {
		cfg.Redis.BalanceCachePrefix = "balance_cache"
	}
	if cfg.Redis.SessionStorePrefix == "" {
		cfg.Redis.SessionStorePrefix = "ws_session"
	}
	if cfg.Redis.SessionStoreTTL == 0 {
		cfg.Redis.SessionStoreTTL = 24 * time.Hour
	}
	if cfg.Redis.DialTimeout <= 0 {
		cfg.Redis.DialTimeout = 5 * time.Second
	}

	// Load monitoring configuration
	cfg.Monitoring = MonitoringConfig{
		Enabled:             v.GetBool("MONITORING_ENABLED"),
		MetricsEnabled:      v.GetBool("METRICS_ENABLED"),
		HealthCheckInterval: time.Duration(v.GetInt("HEALTH_CHECK_INTERVAL")) * time.Second,
	}

	// Load enterprise configuration
	cfg.Enterprise = EnterpriseConfig{
		Enabled:              v.GetBool("ENTERPRISE_MODE"),
		APIKeysJSON:          v.GetString("ENTERPRISE_API_KEYS"),
		LoadBalancerStrategy: v.GetString("LOAD_BALANCER_STRATEGY"),
		MaxQueueSize:         v.GetInt("LOAD_BALANCER_MAX_QUEUE_SIZE"),
		QueueTimeout:         v.GetInt("LOAD_BALANCER_QUEUE_TIMEOUT"),
		HealthCheckInterval:  v.GetInt("LOAD_BALANCER_HEALTH_CHECK_INTERVAL"),
		AutoScalingEnabled:   v.GetBool("LOAD_BALANCER_AUTO_SCALING_ENABLED"),
	}

	// Load frontend configuration
	cfg.Frontend = FrontendConfig{
		Port:    v.GetInt("FRONTEND_PORT"),
		BaseURL: v.GetString("FRONTEND_BASE_URL"),
		WSURL:   v.GetString("FRONTEND_WS_URL"),
		APIURL:  v.GetString("FRONTEND_API_URL"),
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}

func getIntWithDefault(v *viper.Viper, key string, defaultValue int) int {
	if !v.IsSet(key) {
		return defaultValue
	}
	raw := strings.TrimSpace(v.GetString(key))
	if raw == "" {
		return defaultValue
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return defaultValue
	}
	first := strings.SplitN(fields[0], "#", 2)[0]
	value, err := strconv.Atoi(first)
	if err != nil {
		return defaultValue
	}
	if value <= 0 {
		return defaultValue
	}
	return value
}

func getInt64WithDefault(v *viper.Viper, key string, defaultValue int64) int64 {
	if !v.IsSet(key) {
		return defaultValue
	}
	raw := strings.TrimSpace(v.GetString(key))
	if raw == "" {
		return defaultValue
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return defaultValue
	}
	first := strings.SplitN(fields[0], "#", 2)[0]
	value, err := strconv.ParseInt(first, 10, 64)
	if err != nil {
		return defaultValue
	}
	if value <= 0 {
		return defaultValue
	}
	return value
}

// Validate validates the configuration and returns an error if any required
// fields are missing or invalid.
//
// Returns:
//   - error: Validation error if configuration is invalid
func (c *Config) Validate() error {
	// Validate server configuration
	if c.Server.Host == "" {
		return fmt.Errorf("HOST is required")
	}
	if c.Server.Port == 0 {
		return fmt.Errorf("PORT is required")
	}
	if c.Server.Environment == "" {
		return fmt.Errorf("ENVIRONMENT is required")
	}

	// Validate database configuration
	if c.Database.URL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	// Validate JWT configuration
	if c.JWT.Secret == "" {
		return fmt.Errorf("JWT_SECRET is required")
	}
	if c.JWT.Algorithm == "" {
		return fmt.Errorf("JWT_ALGORITHM is required")
	}

	// Validate security configuration
	if c.Security.SecretKey == "" {
		return fmt.Errorf("SECRET_KEY is required")
	}

	return nil
}

// parseJSONArray parses a JSON array string into a slice of strings.
// Returns an empty slice if parsing fails.
func parseJSONArray(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return []string{}
	}

	// Remove brackets and quotes
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, ",")

	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "\"")
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}
