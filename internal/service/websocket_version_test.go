package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestUpdateSessionClientVersionForStart_UpdatesOnStart(t *testing.T) {
	session := &WebSocketSession{UserID: 1}
	msg := WebSocketMessage{Type: MessageTypeStart, Version: "1.2.3"}

	updateSessionClientVersionForStart(zap.NewNop(), session, msg)
	require.Equal(t, "1.2.3", session.ClientVersion)
}

func TestUpdateSessionClientVersionForStart_IgnoresNonStart(t *testing.T) {
	session := &WebSocketSession{UserID: 1, ClientVersion: "old"}
	msg := WebSocketMessage{Type: MessageTypePause, Version: "2.0.0"}

	updateSessionClientVersionForStart(zap.NewNop(), session, msg)
	require.Equal(t, "old", session.ClientVersion)
}

func TestUpdateSessionClientVersionForStart_IgnoresEmptyVersion(t *testing.T) {
	session := &WebSocketSession{UserID: 1, ClientVersion: "old"}
	msg := WebSocketMessage{Type: MessageTypeStart, Version: ""}

	updateSessionClientVersionForStart(zap.NewNop(), session, msg)
	require.Equal(t, "old", session.ClientVersion)
}
