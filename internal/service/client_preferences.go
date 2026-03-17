// Package service 提供业务逻辑实现
package service

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// ClientPreferences 客户端偏好设置
type ClientPreferences struct {
	AudioFormat   string   `json:"audio_format,omitempty"`
	LanguageHints []string `json:"language_hints,omitempty"`
}

// AllowedAudioFormats 允许的音频格式
var AllowedAudioFormats = map[string]bool{
	"aac":  true,
	"pcm":  true,
	"opus": true,
	"mp3":  true,
	"wav":  true,
}

// ParseClientPreferences 解析客户端偏好设置
func ParseClientPreferences(message map[string]interface{}, logger *zap.Logger) *ClientPreferences {
	prefs := &ClientPreferences{}

	// 解析音频格式
	if audioFormat, ok := message["audio_format"]; ok {
		if format, ok := audioFormat.(string); ok {
			normalized, err := ValidateAudioFormat(format)
			if err == nil {
				prefs.AudioFormat = normalized
				logger.Info("使用客户端音频格式",
					zap.String("audio_format", normalized))
			} else {
				logger.Warn("无效的音频格式，跳过设置",
					zap.String("provided", format),
					zap.Error(err))
				// 不设置音频格式，保持空值
			}
		}
	}
	// 如果没有提供音频格式，保持空值

	// 解析语言提示
	if languageHints, ok := message["language_hints"]; ok {
		hints := NormalizeLanguageHints(languageHints)
		if len(hints) > 0 {
			prefs.LanguageHints = hints
			logger.Info("使用客户端语言提示",
				zap.Strings("language_hints", hints))
		}
		// 如果语言提示无效，保持空值
	}
	// 如果没有提供语言提示，保持空值

	return prefs
}

// ValidateAudioFormat 验证音频格式
func ValidateAudioFormat(format string) (string, error) {
	// 规范化：去除空格，转小写
	normalized := strings.TrimSpace(strings.ToLower(format))

	if normalized == "" {
		return "", fmt.Errorf("音频格式不能为空")
	}

	// 检查是否在允许列表中
	if !AllowedAudioFormats[normalized] {
		return "", fmt.Errorf("不支持的音频格式: %s", format)
	}

	return normalized, nil
}

// NormalizeLanguageHints 规范化语言提示
func NormalizeLanguageHints(hints interface{}) []string {
	var result []string

	switch v := hints.(type) {
	case string:
		// 字符串格式：逗号分隔
		parts := strings.Split(v, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}

	case []interface{}:
		// 数组格式
		for _, item := range v {
			if str, ok := item.(string); ok {
				trimmed := strings.TrimSpace(str)
				if trimmed != "" {
					result = append(result, trimmed)
				}
			}
		}

	case []string:
		// 字符串数组格式
		for _, str := range v {
			trimmed := strings.TrimSpace(str)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
	}

	return result
}

// ApplyClientPreferences 应用客户端偏好设置到会话
func ApplyClientPreferences(session *WebSocketSession, prefs *ClientPreferences) {
	if session == nil || prefs == nil {
		return
	}

	copyPrefs := &ClientPreferences{
		AudioFormat:   prefs.AudioFormat,
		LanguageHints: append([]string(nil), prefs.LanguageHints...),
	}

	session.mu.Lock()
	prevAudio := session.AudioFormat
	prevHints := append([]string(nil), session.LanguageHints...)
	hadPrefs := session.clientPrefs != nil

	session.AudioFormat = copyPrefs.AudioFormat
	session.LanguageHints = append([]string(nil), copyPrefs.LanguageHints...)
	session.clientPrefs = copyPrefs

	sameAudio := prevAudio == copyPrefs.AudioFormat
	sameHints := len(prevHints) == len(copyPrefs.LanguageHints)
	if sameHints {
		for i := range prevHints {
			if prevHints[i] != copyPrefs.LanguageHints[i] {
				sameHints = false
				break
			}
		}
	}
	updated := !hadPrefs || !sameAudio || !sameHints
	session.mu.Unlock()

	if updated {
		session.notifyUpdated()
	}
}

func GetSessionClientPreferences(session *WebSocketSession) *ClientPreferences {
	if session == nil {
		return nil
	}

	return session.GetClientPreferences()
}
