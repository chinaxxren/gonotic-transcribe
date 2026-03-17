// Package service 提供业务逻辑实现
package service

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"go.uber.org/zap"
)

// CountWordsUniversal 通用的单词统计方法，支持多种语言（与 Python 版本的 _count_words_universal 一致）
// 支持中文、日文、韩文、阿拉伯文、俄文等多种语言
func CountWordsUniversal(text string, logger *zap.Logger) int {
	if text == "" || strings.TrimSpace(text) == "" {
		return 0
	}

	// 移除多余的空白字符
	text = strings.TrimSpace(text)

	// 方法1: 按空白字符分割（适用于大多数语言）
	wordsBySpace := len(strings.Fields(text))

	// 方法2: 使用正则表达式匹配单词边界（适用于英文等）
	wordRegex := regexp.MustCompile(`\b\w+\b`)
	wordsByRegex := len(wordRegex.FindAllString(text, -1))

	// 方法3: 中文字符统计（每个中文字符算一个词）
	// 使用 Unicode 范围：\u4e00-\u9fff 是中文汉字范围
	chineseRegex := regexp.MustCompile("[\u4e00-\u9fff]")
	chineseChars := len(chineseRegex.FindAllString(text, -1))

	// 方法4: 其他CJK字符（日文、韩文等）
	// 包含中文、日文、韩文等CJK字符
	cjkRegex := regexp.MustCompile("[\u4e00-\u9fff\u3400-\u4dbf\u3300-\u33ff\u2e80-\u2eff\u2f00-\u2fdf\u31c0-\u31ef\u3200-\u32ff\uf900-\ufaff]")
	cjkChars := len(cjkRegex.FindAllString(text, -1))

	// 方法5: 阿拉伯文、俄文等使用字母的字符
	// 包含拉丁字母、西里尔字母、阿拉伯字母等
	letterRegex := regexp.MustCompile("[a-zA-Z\u0400-\u04ff\u0500-\u052f\u0600-\u06ff\u0750-\u077f\u08a0-\u08ff\ufb50-\ufdff\ufe70-\ufeff]")
	letterChars := len(letterRegex.FindAllString(text, -1))

	// 选择最合适的统计方法（与 Python 版本一致）
	// 如果包含中文字符，优先使用中文字符数
	if chineseChars > 0 {
		return chineseChars
	}
	// 如果包含其他CJK字符，使用CJK字符数
	if cjkChars > 0 {
		return cjkChars
	}
	// 如果包含字母字符，使用字母字符数
	if letterChars > 0 {
		return letterChars
	}
	// 否则使用空白分割的单词数
	if wordsBySpace > wordsByRegex {
		return wordsBySpace
	}
	return wordsByRegex
}

// ExtractSpeakerInfo 提取说话人信息（返回英文格式字符串）
// 与 Python 版本的 _extract_speaker_info 一致
func ExtractSpeakerInfo(tokens []map[string]interface{}) string {
	for _, token := range tokens {
		if speaker, ok := token["speaker"]; ok && speaker != nil {
			switch v := speaker.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					return v
				}
			case int:
				return fmt.Sprintf("%d", v)
			case float64:
				if v > 0 {
					return fmt.Sprintf("%.0f", v)
				}
			}
		}
	}
	return "1"
}

// truncateString 截断字符串到指定长度（用于日志记录）
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// LogIncoming 记录接收消息（与 Python 版本的 _log_incoming 一致）
func LogIncoming(logger *zap.Logger, clientID string, messageType string, messageLength int, preview string) {
	logger.Info("收到客户端消息",
		zap.String("client_id", clientID),
		zap.String("type", messageType),
		zap.Int("length", messageLength))
	// if preview != "" {
	// 	logger.Debug("收到客户端消息预览",
	// 		zap.String("client_id", clientID),
	// 		zap.String("preview", preview))
	// }
}

// IsCJKCharacter 检查字符是否为CJK字符
func IsCJKCharacter(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
