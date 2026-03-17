// Package jwt 提供 JWT 令牌生成和验证功能
package jwt

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/chinaxxren/gonotic/internal/pkg/errors"
)

// Claims JWT 声明结构
// 包含用户信息和标准 JWT 声明
type Claims struct {
	UserID int    `json:"user_id"` // 用户 ID
	Email  string `json:"email"`   // 用户邮箱
	Role   string `json:"role"`    // 用户角色
	jwt.RegisteredClaims
}

// Manager JWT 管理器
// 负责令牌的生成、验证和刷新
type Manager struct {
	secret     []byte        // JWT 签名密钥
	expiration time.Duration // 令牌过期时间
}

// NewManager 创建新的 JWT 管理器
//
// 参数:
//   - secret: JWT 签名密钥
//   - expiration: 令牌过期时间
//
// 返回:
//   - *Manager: JWT 管理器实例
func NewManager(secret string, expiration time.Duration) *Manager {
	return &Manager{
		secret:     []byte(secret),
		expiration: expiration,
	}
}

// GenerateToken 生成 JWT 令牌
// 令牌包含用户 ID、邮箱和角色信息
//
// 参数:
//   - userID: 用户 ID
//   - email: 用户邮箱
//   - role: 用户角色
//
// 返回:
//   - string: JWT 令牌字符串
//   - error: 如果生成失败返回错误
//
// 示例:
//
//	token, err := manager.GenerateToken(123, "user@example.com", "pro")
func (m *Manager) GenerateToken(userID int, email, role string) (string, error) {
	return m.GenerateTokenWithTTL(userID, email, role, m.expiration)
}

// GenerateTokenWithTTL 生成指定有效期的 JWT 令牌
func (m *Manager) GenerateTokenWithTTL(userID int, email, role string, ttl time.Duration) (string, error) {
	now := time.Now()
	expiresAt := now.Add(ttl)

	// 创建声明
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	// 创建令牌
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 签名令牌
	tokenString, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("签名令牌失败: %w", err)
	}

	return tokenString, nil
}

// ValidateToken 验证 JWT 令牌
// 检查令牌签名和过期时间
//
// 参数:
//   - tokenString: JWT 令牌字符串
//
// 返回:
//   - *Claims: 如果令牌有效返回声明
//   - error: 如果令牌无效返回错误
//
// 可能的错误:
//   - ErrTokenInvalid: 令牌格式无效或签名错误
//   - ErrTokenExpired: 令牌已过期
func (m *Manager) ValidateToken(tokenString string) (*Claims, error) {
	// 解析令牌
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// 验证签名算法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("意外的签名方法: %v", token.Header["alg"])
		}
		return m.secret, nil
	})

	if err != nil {
		// 检查是否是过期错误
		if jwt.ErrTokenExpired.Error() == err.Error() {
			return nil, errors.New(errors.ErrTokenExpired, "令牌已过期")
		}
		return nil, errors.Wrap(errors.ErrTokenInvalid, "令牌无效", err)
	}

	// 提取声明
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New(errors.ErrTokenInvalid, "令牌声明无效")
	}

	return claims, nil
}

// RefreshToken 刷新 JWT 令牌
// 从旧令牌中提取信息并生成新令牌
//
// 参数:
//   - tokenString: 旧的 JWT 令牌字符串
//
// 返回:
//   - string: 新的 JWT 令牌字符串
//   - error: 如果刷新失败返回错误
//
// 注意: 即使旧令牌已过期，只要在合理时间内（如 7 天内），仍可刷新
func (m *Manager) RefreshToken(tokenString string) (string, error) {
	// 解析令牌（不验证过期时间）
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("意外的签名方法: %v", token.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithoutClaimsValidation())

	if err != nil {
		return "", errors.Wrap(errors.ErrTokenInvalid, "解析令牌失败", err)
	}

	// 提取声明
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return "", errors.New(errors.ErrTokenInvalid, "令牌声明无效")
	}

	// 检查令牌是否过期太久（超过 7 天不允许刷新）
	if time.Since(claims.ExpiresAt.Time) > 7*24*time.Hour {
		return "", errors.New(errors.ErrTokenExpired, "令牌过期时间过长，无法刷新")
	}

	// 生成新令牌
	return m.GenerateToken(claims.UserID, claims.Email, claims.Role)
}

// ExtractClaims 从令牌中提取声明（不验证）
// 用于需要读取令牌信息但不需要验证的场景
//
// 参数:
//   - tokenString: JWT 令牌字符串
//
// 返回:
//   - *Claims: 令牌声明
//   - error: 如果提取失败返回错误
func (m *Manager) ExtractClaims(tokenString string) (*Claims, error) {
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, &Claims{})
	if err != nil {
		return nil, errors.Wrap(errors.ErrTokenInvalid, "解析令牌失败", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, errors.New(errors.ErrTokenInvalid, "令牌声明无效")
	}

	return claims, nil
}

// GetExpiration 获取令牌过期时间
//
// 返回:
//   - time.Duration: 令牌过期时间
func (m *Manager) GetExpiration() time.Duration {
	return m.expiration
}

// IsTokenExpired 检查令牌是否已过期
//
// 参数:
//   - tokenString: JWT 令牌字符串
//
// 返回:
//   - bool: 如果令牌已过期返回 true
//   - error: 如果解析失败返回错误
func (m *Manager) IsTokenExpired(tokenString string) (bool, error) {
	claims, err := m.ExtractClaims(tokenString)
	if err != nil {
		return false, err
	}

	return time.Now().After(claims.ExpiresAt.Time), nil
}

// GetRemainingTime 获取令牌剩余有效时间
//
// 参数:
//   - tokenString: JWT 令牌字符串
//
// 返回:
//   - time.Duration: 剩余有效时间（如果已过期返回负值）
//   - error: 如果解析失败返回错误
func (m *Manager) GetRemainingTime(tokenString string) (time.Duration, error) {
	claims, err := m.ExtractClaims(tokenString)
	if err != nil {
		return 0, err
	}

	return time.Until(claims.ExpiresAt.Time), nil
}
