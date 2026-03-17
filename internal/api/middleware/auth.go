// Package middleware 提供 HTTP 中间件实现
package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/pkg/errors"
	"github.com/chinaxxren/gonotic/internal/pkg/jwt"
)

// AuthMiddleware 认证中间件
type AuthMiddleware struct {
	jwtManager *jwt.Manager
	logger     *zap.Logger
}

// NewAuthMiddleware 创建新的认证中间件
//
// 参数:
//   - jwtManager: JWT 管理器
//   - logger: 日志记录器
//
// 返回:
//   - *AuthMiddleware: 认证中间件实例
func NewAuthMiddleware(jwtManager *jwt.Manager, logger *zap.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		jwtManager: jwtManager,
		logger:     logger,
	}
}

// RequireAuth 要求认证的中间件
// 验证 JWT 令牌并将用户信息存储到上下文
// 支持从 Authorization header 或查询参数 token 中获取令牌
//
// 返回:
//   - gin.HandlerFunc: Gin 中间件函数
func (m *AuthMiddleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		var token string

		// 优先从 Authorization 头获取令牌
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			// 检查 Bearer 前缀
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}

		// 如果 header 中没有，尝试从查询参数获取
		if token == "" {
			token = c.Query("token")
		}

		// 如果都没有，返回未授权错误
		if token == "" {
			m.respondWithError(c, errors.UnauthorizedErrorResponse())
			c.Abort()
			return
		}

		// 验证令牌
		claims, err := m.jwtManager.ValidateToken(token)
		if err != nil {
			m.logger.Warn("令牌验证失败",
				zap.Error(err),
				zap.String("ip", c.ClientIP()))

			appErr := errors.GetAppError(err)
			if appErr != nil {
				m.respondWithError(c, errors.ErrorResponse(appErr))
			} else {
				m.respondWithError(c, errors.UnauthorizedErrorResponse())
			}
			c.Abort()
			return
		}

		// 将用户信息存储到上下文
		c.Set("user_id", claims.UserID)
		c.Set("user_email", claims.Email)
		c.Set("user_role", claims.Role)
		c.Set("claims", claims)

		m.logger.Debug("用户认证成功",
			zap.Int("user_id", claims.UserID),
			zap.String("email", claims.Email))

		c.Next()
	}
}

// OptionalAuth 可选认证的中间件
// 如果提供了令牌则验证，否则继续处理
// 支持从 Authorization header 或查询参数 token 中获取令牌
//
// 返回:
//   - gin.HandlerFunc: Gin 中间件函数
func (m *AuthMiddleware) OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		var token string

		// 优先从 Authorization 头获取令牌
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			// 检查 Bearer 前缀
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}

		// 如果 header 中没有，尝试从查询参数获取
		if token == "" {
			token = c.Query("token")
		}

		// 如果没有令牌，继续处理
		if token == "" {
			c.Next()
			return
		}

		// 验证令牌
		claims, err := m.jwtManager.ValidateToken(token)
		if err != nil {
			// 令牌无效，但不阻止请求
			c.Next()
			return
		}

		// 将用户信息存储到上下文
		c.Set("user_id", claims.UserID)
		c.Set("user_email", claims.Email)
		c.Set("user_role", claims.Role)
		c.Set("claims", claims)

		c.Next()
	}
}

// respondWithError 返回错误响应
func (m *AuthMiddleware) respondWithError(c *gin.Context, response errors.APIResponse) {
	// 根据 success 字段判断状态码
	statusCode := 401
	if !response.Success {
		// 可以根据 code 字段进一步细化状态码
		statusCode = 401 // 认证错误统一返回 401
	}
	c.JSON(statusCode, response)
}

// GetUserID 从上下文获取用户 ID
//
// 参数:
//   - c: Gin 上下文
//
// 返回:
//   - int: 用户 ID
//   - bool: 是否存在
func GetUserID(c *gin.Context) (int, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	id, ok := userID.(int)
	return id, ok
}

// GetUserEmail 从上下文获取用户邮箱
//
// 参数:
//   - c: Gin 上下文
//
// 返回:
//   - string: 用户邮箱
//   - bool: 是否存在
func GetUserEmail(c *gin.Context) (string, bool) {
	email, exists := c.Get("user_email")
	if !exists {
		return "", false
	}
	e, ok := email.(string)
	return e, ok
}

// GetUserRole 从上下文获取用户角色
//
// 参数:
//   - c: Gin 上下文
//
// 返回:
//   - string: 用户角色
//   - bool: 是否存在
func GetUserRole(c *gin.Context) (string, bool) {
	role, exists := c.Get("user_role")
	if !exists {
		return "", false
	}
	r, ok := role.(string)
	return r, ok
}

// AdminMiddleware 管理员权限中间件
// 要求用户必须是管理员角色
//
// 返回:
//   - gin.HandlerFunc: Gin 中间件函数
func AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从上下文获取用户角色
		role, exists := c.Get("user_role")
		if !exists {
			c.JSON(403, errors.ForbiddenErrorResponse("需要管理员权限"))
			c.Abort()
			return
		}

		// 检查是否是管理员
		if role != "admin" {
			c.JSON(403, errors.ForbiddenErrorResponse("需要管理员权限"))
			c.Abort()
			return
		}

		c.Next()
	}
}
