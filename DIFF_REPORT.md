# Diff Report: gonotic vs gonotic-transcribe

## Scope

- `internal/**.go`
- `cmd/transcribe_ws/**.go`

## Assumptions (Allowed Differences)

- Default port difference (`3000` vs `8090`) is allowed and required by `gonotic-transcribe`.
- Module path/import path difference (`github.com/noticai/gonotic` vs `github.com/chinaxxren/gonotic`) is allowed.
- Import grouping / blank lines / alignment whitespace differences are treated as formatting-only.

## Summary

- **Only in `gonotic`**: 70 files (expected: non-WS REST surface / other domains)
- **Only in `gonotic-transcribe`**: 1 files (expected: WS-only service entrypoints)
- **Common files**: 95
- **Common files with unexpected diffs (after filtering allowed diffs & ignoring whitespace)**: 4

## Files only in `gonotic` (first 60)

- `internal/api/handlers/apple_admin_handler.go`
- `internal/api/handlers/auth.go`
- `internal/api/handlers/enterprise.go`
- `internal/api/handlers/meeting.go`
- `internal/api/handlers/payment_handler.go`
- `internal/api/handlers/recording.go`
- `internal/api/handlers/summarize.go`
- `internal/api/handlers/user.go`
- `internal/api/handlers/user_settings_test.go`
- `internal/api/handlers/webhook_handler.go`
- `internal/api/middleware/client_version.go`
- `internal/api/middleware/client_version_test.go`
- `internal/api/middleware/cors.go`
- `internal/api/middleware/logger.go`
- `internal/api/middleware/metrics.go`
- `internal/api/middleware/ratelimit.go`
- `internal/api/middleware/ratelimit_redis.go`
- `internal/api/middleware/recovery.go`
- `internal/api/middleware/response_logger.go`
- `internal/api/router.go`
- `internal/pkg/circuitbreaker/breaker.go`
- `internal/pkg/errors/logging.go`
- `internal/pkg/redisutil/redis.go`
- `internal/service/account_state_facade_test.go`
- `internal/service/account_state_integration_test.go`
- `internal/service/account_state_service_test.go`
- `internal/service/apple_backfill_service.go`
- `internal/service/apple_expires.go`
- `internal/service/apple_identity_verifier.go`
- `internal/service/apple_notification_service.go`
- `internal/service/apple_notification_service_test.go`
- `internal/service/apple_server_api_client.go`
- `internal/service/async_worker.go`
- `internal/service/auth_service.go`
- `internal/service/auth_service_test.go`
- `internal/service/cycle_expiration_service_test.go`
- `internal/service/cycle_state_service_fake_test.go`
- `internal/service/cycle_state_service_test.go`
- `internal/service/email.go`
- `internal/service/email_noop_test.go`
- `internal/service/enterprise_test.go`
- `internal/service/lifecycle_worker.go`
- `internal/service/lifecycle_worker_test.go`
- `internal/service/login_prompt.go`
- `internal/service/meeting.go`
- `internal/service/meeting_sync.go`
- `internal/service/meeting_sync_worker.go`
- `internal/service/offer_constants.go`
- `internal/service/payment_service.go`
- `internal/service/payment_service_test.go`
- `internal/service/purchase_service.go`
- `internal/service/purchase_service_test.go`
- `internal/service/remote_connection_regression_test.go`
- `internal/service/scheduler_service.go`
- `internal/service/scheduler_service_test.go`
- `internal/service/session_manager_test.go`
- `internal/service/subscription_helpers.go`
- `internal/service/subscription_test_helpers.go`
- `internal/service/subscription_test_helpers_test.go`
- `internal/service/summarizer.go`
- ...(and 10 more)

## Files only in `gonotic-transcribe`

- `cmd/transcribe_ws/main.go`

## Unexpected diffs in common files

### `internal/api/handlers/websocket.go`

```diff
-// @Router /api/v1/recording/transcribe [get]
+// @Router /ws/transcription [get]
-}
-
-// GetConnectionStats 获取连接统计
-//
-// @Summary 获取 WebSocket 连接统计
-// @Description 获取当前活跃的 WebSocket 连接数和详细统计信息
-// @Tags WebSocket
-// @Accept json
-// @Produce json
-// @Success 200 {object} map[string]interface{}
-// @Router /api/v1/recording/stats [get]
-func (h *WebSocketHandler) GetConnectionStats(c *gin.Context) {
-	stats := h.websocketHandler.GetStats()
-	c.JSON(http.StatusOK, errors.SuccessResponse(stats))
-}
-
-// GetSessionStatus 获取会话状态
-//
-// @Summary 获取转录会话状态
-// @Description 获取指定会话的转录状态信息
-// @Tags WebSocket
-// @Accept json
-// @Produce json
-// @Security BearerAuth
-// @Param session_id path string true "会话 ID"
-// @Success 200 {object} map[string]interface{}
-// @Failure 401 {object} errors.APIError "未授权"
-// @Failure 404 {object} errors.APIError "会话不存在"
-// @Router /api/v1/recording/session/{session_id} [get]
-func (h *WebSocketHandler) GetSessionStatus(c *gin.Context) {
-	// 从上下文获取用户 ID
-	userID, exists := middleware.GetUserID(c)
-	if !exists {
-		c.JSON(http.StatusUnauthorized, errors.UnauthorizedErrorResponse())
-		return
-	}
-
-	// 获取会话 ID
-	sessionID := c.Param("session_id")
-	if sessionID == "" {
-		c.JSON(http.StatusBadRequest, errors.ValidationErrorResponse("缺少 session_id 参数"))
-		return
-	}
-
-	// 获取会话信息（简化版：直接使用userID）
-	sessionInfo, err := h.websocketHandler.GetSessionInfo(userID)
-	if err != nil {
-		h.logger.Error("获取会话状态失败",
-			zap.Error(err),
-			zap.String("session_id", sessionID),
-			zap.Int("user_id", userID))
-
-		c.JSON(http.StatusNotFound, errors.NotFoundErrorResponse("转录会话"))
-		return
-	}
-
-	c.JSON(http.StatusOK, errors.SuccessResponse(sessionInfo))
```

### `internal/pkg/logger/logger.go`

```diff
-	// P2 修复: 使用 BufferedWriteSyncer 启用异步批量写入，减少 IO 阻塞
-		// P2 修复: 使用 BufferedWriteSyncer 包装文件写入器，启用异步批量写入
-		// 缓冲区大小 256KB，每 100ms 或缓冲区满时刷新
+		// 使用 BufferedWriteSyncer 包装文件写入器，启用异步批量写入
```

### `internal/repository/transcription_helpers.go`

```diff
-	json "github.com/bytedance/sonic"
+
+	json "github.com/bytedance/sonic"
```

### `internal/service/remote_connection.go`

```diff
-	json "github.com/bytedance/sonic"
+
+	json "github.com/bytedance/sonic"
```
