package logger

import (
	"strings"

	"go.uber.org/zap"
)

func DebugNoise(l *zap.Logger, environment string, msg string, fields ...zap.Field) {
	if l == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(environment), "production") {
		return
	}
	l.Debug(msg, fields...)
}
