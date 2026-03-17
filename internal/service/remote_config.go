// Package service 提供业务逻辑实现
package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

func normalizeAndDedupeLanguageHints(hints []string) []string {
	if len(hints) == 0 {
		return nil
	}

	result := make([]string, 0, len(hints))
	seen := make(map[string]struct{}, len(hints))
	for _, h := range hints {
		normalized := strings.TrimSpace(h)
		if normalized == "" {
			continue
		}
		key := strings.ToLower(normalized)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, normalized)
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// RemoteConfig Remote 启动配置（完整的 STT 配置）
type RemoteConfig struct {
	Type                              string                   `json:"type"`
	APIKey                            string                   `json:"api_key"`
	Model                             string                   `json:"model,omitempty"`
	AudioFormat                       string                   `json:"audio_format,omitempty"`
	SampleRate                        int                      `json:"sample_rate,omitempty"`
	NumChannels                       int                      `json:"num_channels,omitempty"`
	LanguageHints                     []string                 `json:"language_hints,omitempty"`
	EnableEndpointDetection           bool                     `json:"enable_endpoint_detection,omitempty"`
	EnableSpeakerDiarization          bool                     `json:"enable_speaker_diarization,omitempty"`
	EnableGlobalSpeakerIdentification bool                     `json:"enable_global_speaker_identification,omitempty"`
	EnableSpeakerIdentification       bool                     `json:"enable_speaker_identification,omitempty"`
	EnableSpeakerChangeDetection      bool                     `json:"enable_speaker_change_detection,omitempty"`
	EnableProfanityFilter             bool                     `json:"enable_profanity_filter,omitempty"`
	EnableNonFinalTokens              bool                     `json:"enable_non_final_tokens,omitempty"`
	EnableLanguageIdentification      bool                     `json:"enable_language_identification,omitempty"`
	IncludeNonfinal                   bool                     `json:"include_nonfinal,omitempty"`
	Translation                       *RemoteTranslationConfig `json:"translation,omitempty"`
}

// RemoteTranslationConfig Remote 翻译配置（发送给 Remote 服务的格式）
type RemoteTranslationConfig struct {
	Type           string `json:"type"`                      // "one_way" 或 "two_way"
	TargetLanguage string `json:"target_language,omitempty"` // one_way 使用
	LanguageA      string `json:"language_a,omitempty"`      // two_way 使用
	LanguageB      string `json:"language_b,omitempty"`      // two_way 使用
}

// BuildRemoteStartPayload 构建 Remote 启动配置（完整的 STT 配置）
// 参数：
//   - apiKey: Remote API 密钥
//   - clientAudioFormat: 客户端发送的音频格式（优先使用）
//   - clientLanguageHints: 客户端发送的语言提示（优先使用）
//   - sttConfig: 服务器 STT 配置（默认值）
//   - audioConfig: 服务器 Audio 配置（默认值）
//   - translationConfig: 服务器 Translation 配置（可选）
func BuildRemoteStartPayload(
	apiKey string,
	clientAudioFormat string,
	clientLanguageHints []string,
	sttConfig ServerSTTConfig,
	audioConfig ServerAudioConfig,
	translationConfig *ServerTranslationConfig,
) *RemoteConfig {
	// 使用客户端配置，如果没有则使用服务器默认配置
	// 注意：客户端配置优先，即使为空字符串也要检查
	audioFormat := clientAudioFormat
	if audioFormat == "" {
		audioFormat = sttConfig.AudioFormat
	}

	// 语言提示：客户端配置优先，保持原始顺序
	languageHints := clientLanguageHints
	if len(languageHints) == 0 {
		languageHints = sttConfig.LanguageHints
	}
	// Remote 要求 language hints 唯一，这里统一做规范化和去重
	languageHints = normalizeAndDedupeLanguageHints(languageHints)

	config := &RemoteConfig{
		Type:                              "start",
		APIKey:                            apiKey,
		Model:                             sttConfig.Model,
		AudioFormat:                       audioFormat,
		SampleRate:                        audioConfig.SampleRate,
		NumChannels:                       audioConfig.Channels,
		LanguageHints:                     languageHints,
		EnableEndpointDetection:           true,
		EnableSpeakerDiarization:          sttConfig.EnableSpeakerDiarization,
		EnableGlobalSpeakerIdentification: sttConfig.EnableGlobalSpeakerIdentification,
		EnableSpeakerIdentification:       true,
		EnableSpeakerChangeDetection:      sttConfig.EnableSpeakerChangeDetection,
		EnableProfanityFilter:             sttConfig.EnableProfanityFilter,
		EnableNonFinalTokens:              true,
		EnableLanguageIdentification:      true,
		IncludeNonfinal:                   true,
	}

	// 构建翻译配置 - 完全由客户端语言提示控制
	if translationConfig != nil {
		translation := buildTranslationConfig(languageHints, translationConfig)
		if translation != nil {
			config.Translation = translation
		}
	}

	return config
}

// ServerSTTConfig STT 配置（从 config.Config 传递）
type ServerSTTConfig struct {
	Model                             string
	AudioFormat                       string
	LanguageHints                     []string
	EnableProfanityFilter             bool
	EnableSpeakerDiarization          bool
	EnableGlobalSpeakerIdentification bool
	EnableSpeakerChangeDetection      bool
}

// ServerAudioConfig Audio 配置（从 config.Config 传递）
type ServerAudioConfig struct {
	SampleRate int
	Channels   int
}

// ServerTranslationConfig Translation 配置（从 config.Config 传递）
type ServerTranslationConfig struct {
	Enabled         bool
	Type            string
	TargetLanguages []string
}

// buildTranslationConfig 构建翻译配置
// 客户端完全控制翻译行为，环境变量只作为默认值
func buildTranslationConfig(languageHints []string, translationConfig *ServerTranslationConfig) *RemoteTranslationConfig {
	// 如果客户端没有提供语言提示，不启用翻译
	if len(languageHints) == 0 {
		return nil
	}

	if len(languageHints) >= 2 {
		// Two-way translation - 客户端提供了多个语言
		languageA := languageHints[0]
		languageB := languageHints[1]

		return &RemoteTranslationConfig{
			Type:      "two_way",
			LanguageA: languageA,
			LanguageB: languageB,
		}
	} else if len(languageHints) == 1 {
		// 客户端只提供了一个语言，不启用翻译
		// 翻译需要客户端明确提供多个语言才启用
		return nil
	}

	return nil
}

// SendConfig 发送配置到 Remote 服务
// P1 修复: 缩小锁粒度，避免在持锁时进行阻塞 I/O
// P2 修复: 添加写超时
func (rc *RemoteConnection) SendConfig(config *RemoteConfig) error {
	// 序列化在锁外进行
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	// 1. 获取锁只用于检查状态和获取连接指针
	rc.mu.Lock()
	if rc.closed || rc.conn == nil {
		rc.mu.Unlock()
		return fmt.Errorf("连接已关闭")
	}
	sessionID := rc.sessionID
	rc.mu.Unlock()

	// 直接打印 JSON（不进行修改优化，不使用转义）
	rc.logger.Info("发送给 Remote 的配置",
		zap.String("session_id", sessionID),
		zap.ByteString("json", configJSON))

	if err := rc.writeMessage(websocket.TextMessage, configJSON, true); err != nil {
		return fmt.Errorf("发送配置失败: %w", err)
	}

	rc.mu.Lock()
	rc.lastConfig = config
	rc.mu.Unlock()

	return nil
}

// GetLastConfig 获取上次发送的配置（从内存中读取）
func (rc *RemoteConnection) GetLastConfig() *RemoteConfig {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.lastConfig == nil {
		return nil
	}

	// 返回配置的深拷贝以避免并发修改
	configCopy := *rc.lastConfig
	return &configCopy
}

// ResendLastConfig 重新发送上次保存的配置
func (rc *RemoteConnection) ResendLastConfig() error {
	rc.mu.Lock()
	lastConfig := rc.lastConfig
	rc.mu.Unlock()

	if lastConfig == nil {
		return fmt.Errorf("没有保存的配置可以重新发送")
	}

	rc.logger.Info("重新发送上次保存的配置",
		zap.String("session_id", rc.sessionID),
		zap.String("config_type", lastConfig.Type))

	return rc.SendConfig(lastConfig)
}

// WaitForInitialResponse 等待 Remote 初始响应
func (rc *RemoteConnection) WaitForInitialResponse(timeout time.Duration) (map[string]interface{}, error) {
	rc.mu.Lock()
	if rc.closed {
		rc.mu.Unlock()
		return nil, fmt.Errorf("连接已关闭")
	}
	rc.mu.Unlock()

	// 设置读取超时
	deadline := time.Now().Add(timeout)
	if err := rc.conn.SetReadDeadline(deadline); err != nil {
		return nil, fmt.Errorf("设置读取超时失败: %w", err)
	}

	// 读取响应
	messageType, message, err := rc.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("读取初始响应失败: %w", err)
	}

	// 重置读取超时
	if err := rc.conn.SetReadDeadline(time.Time{}); err != nil {
		rc.logger.Error("重置读取超时失败", zap.Error(err))
	}

	// 检查消息类型
	if messageType != websocket.TextMessage {
		return nil, fmt.Errorf("期望文本消息，收到类型: %d", messageType)
	}

	// 直接打印 JSON（不进行修改优化，不使用转义）
	rc.logger.Info("收到 Remote 的响应",
		zap.String("session_id", rc.sessionID),
		zap.ByteString("json", message))

	// 解析响应
	var response map[string]interface{}
	if err := json.Unmarshal(message, &response); err != nil {
		return nil, fmt.Errorf("解析初始响应失败: %w", err)
	}

	// 检查错误
	if errorCode, ok := response["error_code"]; ok && errorCode != nil {
		// Remote 可能返回 error_message 或 error 字段；优先使用更常见的 error_message。
		errorMsg, _ := response["error_message"].(string)
		if errorMsg == "" {
			errorMsg, _ = response["error"].(string)
		}
		return response, fmt.Errorf("remote 返回错误: %v - %s", errorCode, errorMsg)
	}

	return response, nil
}

// SendConfigAndWaitResponse 发送配置并等待响应（便捷方法）
func (rc *RemoteConnection) SendConfigAndWaitResponse(config *RemoteConfig, timeout time.Duration) (map[string]interface{}, error) {
	// 发送配置
	if err := rc.SendConfig(config); err != nil {
		return nil, err
	}

	// 等待响应
	response, err := rc.WaitForInitialResponse(timeout)
	if err != nil {
		return nil, err
	}

	return response, nil
}
