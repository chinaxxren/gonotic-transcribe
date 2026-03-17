// Package repository provides data access layer implementations.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
	"github.com/chinaxxren/gonotic/internal/pkg/errors"
	"go.uber.org/zap"
)

// UserRepository定义了用户数据访问操作的接口。
// 所有方法都使用准备好的语句来防止 SQL 注入。
type UserRepository interface {
	// Create 在数据库中创建一个新用户
	Create(ctx context.Context, user *model.User) error

	// GetByID 根据 ID 获取用户
	GetByID(ctx context.Context, id int) (*model.User, error)

	// GetByEmail 根据电子邮件地址获取用户
	GetByEmail(ctx context.Context, email string) (*model.User, error)

	// GetByAppleSub 根据 Apple sub 获取用户
	GetByAppleSub(ctx context.Context, appleSub string) (*model.User, error)

	// Update 更新现有用户
	Update(ctx context.Context, user *model.User) error

	// BindAppleSub 绑定用户的 Apple sub
	BindAppleSub(ctx context.Context, userID int, appleSub string) error

	// GetByDeviceCode 根据设备码获取用户
	GetByDeviceCode(ctx context.Context, deviceCode string) (*model.User, error)

	// UpdateDeviceInfo 更新用户绑定的设备信息
	UpdateDeviceInfo(ctx context.Context, userID int, deviceCode, deviceType string) error

	// UpdateVerificationCode 更新用户的验证码
	UpdateVerificationCode(ctx context.Context, email, code string, expiry time.Time) error

	// UpdateLastActive 更新用户的最后活动时间戳
	UpdateLastActive(ctx context.Context, userID int) error

	// IncrementCodeAttempts 增加验证码尝试次数
	IncrementCodeAttempts(ctx context.Context, userID int) error

	// ClearVerificationCode 清除验证码并重置尝试次数
	ClearVerificationCode(ctx context.Context, userID int) error

	// UpdateRole 更新用户的订阅角色
	UpdateRole(ctx context.Context, userID int, role model.UserRole) error

	// UpdateActiveSession 更新用户的活动会话 ID
	UpdateActiveSession(ctx context.Context, userID int, sessionID string) error

	// HasPaidBefore 检查用户是否曾经付费
	HasPaidBefore(ctx context.Context, userID int) (bool, error)

	// UpgradeUserRole 升级用户角色（只允许向更高级别升级）
	UpgradeUserRole(ctx context.Context, userID int, targetRole model.UserRole) error
}

// UpdateDeviceInfo 更新用户绑定的设备信息。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - userID: User ID
//   - deviceCode: Device code to bind
//   - deviceType: Device type label
//
// Returns:
//   - error: Error if update fails
func (r *userRepository) UpdateDeviceInfo(ctx context.Context, userID int, deviceCode, deviceType string) error {
	query := `
		UPDATE users
		SET device_code = ?, device_type = ?, updated_at = ?
		WHERE id = ?
	`

	result, err := r.db.DB.ExecContext(ctx, r.db.Rebind(query), deviceCode, deviceType, time.Now().Unix(), userID)
	if err != nil {
		r.logger.Error("Failed to update device info",
			zap.Error(err),
			zap.Int("user_id", userID),
			zap.String("device_code", deviceCode))
		return errors.NewDatabaseError("Failed to update device info", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errors.NewDatabaseError("Failed to verify update", err)
	}

	if rowsAffected == 0 {
		return errors.New(errors.ErrUserNotFound, "User not found")
	}

	return nil
}

// userRepository implements UserRepository interface.
type userRepository struct {
	db     *database.DB
	logger *zap.Logger
}

func boolToSmallInt(v bool) int16 {
	if v {
		return 1
	}
	return 0
}

// NewUserRepository creates a new user repository instance.
//
// Parameters:
//   - db: Database connection
//   - logger: Logger instance
//
// Returns:
//   - UserRepository: User repository implementation
func NewUserRepository(db *database.DB, logger *zap.Logger) UserRepository {
	return &userRepository{
		db:     db,
		logger: logger,
	}
}

// GetByDeviceCode 根据设备码获取用户。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - deviceCode: Device unique code
//
// Returns:
//   - *model.User: User if found
//   - error: Error if query fails or user not found
func (r *userRepository) GetByDeviceCode(ctx context.Context, deviceCode string) (*model.User, error) {
	query := `
		SELECT id, email, role, last_active_at, active_session_id,
		       device_code, device_type, created_at, updated_at, is_active,
		       apple_sub,
		       verification_code, code_expiry, code_attempts
		FROM users
		WHERE device_code = ?
	`

	var user model.User
	err := r.db.GetContext(ctx, &user, r.db.Rebind(query), deviceCode)

	if err == sql.ErrNoRows {
		return nil, errors.New(errors.ErrUserNotFound, "User not found")
	}

	if err != nil {
		r.logger.Error("Failed to get user by device code",
			zap.Error(err),
			zap.String("device_code", deviceCode))
		return nil, errors.NewDatabaseError("Failed to get user", err)
	}

	return &user, nil
}

func (r *userRepository) GetByAppleSub(ctx context.Context, appleSub string) (*model.User, error) {
	query := `
		SELECT id, email, role, last_active_at, active_session_id,
		       device_code, device_type, created_at, updated_at, is_active,
		       apple_sub,
		       verification_code, code_expiry, code_attempts
		FROM users
		WHERE apple_sub = ?
	`

	var user model.User
	err := r.db.GetContext(ctx, &user, r.db.Rebind(query), appleSub)

	if err == sql.ErrNoRows {
		return nil, errors.New(errors.ErrUserNotFound, "User not found")
	}

	if err != nil {
		r.logger.Error("Failed to get user by apple sub",
			zap.Error(err),
			zap.String("apple_sub", appleSub))
		return nil, errors.NewDatabaseError("Failed to get user", err)
	}

	return &user, nil
}

// Create 在数据库中创建一个新用户。
// 用户 ID 自动生成并设置在用户对象上。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - user: User to create
//
// Returns:
//   - error: Error if creation fails
func (r *userRepository) Create(ctx context.Context, user *model.User) error {
	queryLegacy := `
		INSERT INTO users (
			email, role, created_at, updated_at, is_active, code_attempts, apple_sub
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	var id int64
	err := r.db.QueryRowContext(ctx, r.db.Rebind(queryLegacy),
		user.Email,
		user.Role,
		user.CreatedAt,
		user.UpdatedAt,
		boolToSmallInt(user.IsActive),
		0,
		user.AppleSub,
	).Scan(&id)

	if err != nil {
		r.logger.Error("Failed to create user",
			zap.Error(err),
			zap.String("email", user.Email))
		return errors.NewDatabaseError("Failed to create user", err)
	}

	user.ID = int(id)

	r.logger.Info("User created successfully",
		zap.Int("user_id", user.ID),
		zap.String("email", user.Email))

	return nil
}

// GetByID 根据 ID 获取用户。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - id: User ID
//
// Returns:
//   - *model.User: User if found
//   - error: Error if query fails or user not found
func (r *userRepository) GetByID(ctx context.Context, id int) (*model.User, error) {
	query := `
		SELECT id, email, role, last_active_at, active_session_id,
		       device_code, device_type, created_at, updated_at, is_active,
		       apple_sub,
		       verification_code, code_expiry, code_attempts
		FROM users
		WHERE id = ?
	`

	var user model.User
	err := r.db.GetContext(ctx, &user, r.db.Rebind(query), id)

	if err == sql.ErrNoRows {
		return nil, errors.New(errors.ErrUserNotFound, "User not found")
	}

	if err != nil {
		r.logger.Error("Failed to get user by ID",
			zap.Error(err),
			zap.Int("user_id", id))
		return nil, errors.NewDatabaseError("Failed to get user", err)
	}

	return &user, nil
}

// GetByEmail 根据电子邮件地址获取用户。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - email: User email address
//
// Returns:
//   - *model.User: User if found
//   - error: Error if query fails or user not found
func (r *userRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	query := `
		SELECT id, email, role, last_active_at, active_session_id,
		       device_code, device_type, created_at, updated_at, is_active,
		       apple_sub,
		       verification_code, code_expiry, code_attempts
		FROM users
		WHERE email = ?
	`

	var user model.User
	err := r.db.GetContext(ctx, &user, r.db.Rebind(query), email)

	if err == sql.ErrNoRows {
		return nil, errors.New(errors.ErrUserNotFound, "User not found")
	}

	if err != nil {
		r.logger.Error("Failed to get user by email",
			zap.Error(err),
			zap.String("email", email))
		return nil, errors.NewDatabaseError("Failed to get user", err)
	}

	return &user, nil
}

// Update 更新现有用户。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - user: User with updated fields
//
// Returns:
//   - error: Error if update fails
func (r *userRepository) Update(ctx context.Context, user *model.User) error {
	user.UpdatedAt = time.Now().Unix()

	query := `
		UPDATE users
		SET email = ?, role = ?, last_active_at = ?, active_session_id = ?,
		    device_code = ?, device_type = ?, updated_at = ?, is_active = ?,
		    verification_code = ?, code_expiry = ?, code_attempts = ?
		WHERE id = ?
	`

	result, err := r.db.DB.ExecContext(ctx, r.db.Rebind(query),
		user.Email,
		user.Role,
		user.LastActiveAt,
		user.ActiveSessionID,
		user.DeviceCode,
		user.DeviceType,
		user.UpdatedAt,
		boolToSmallInt(user.IsActive),
		user.VerificationCode,
		user.CodeExpiry,
		user.CodeAttempts,
		user.ID,
	)

	if err != nil {
		r.logger.Error("Failed to update user",
			zap.Error(err),
			zap.Int("user_id", user.ID))
		return errors.NewDatabaseError("Failed to update user", err)
	}

	// Check if user was found
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("Failed to get rows affected",
			zap.Error(err))
		return errors.NewDatabaseError("Failed to verify update", err)
	}

	if rowsAffected == 0 {
		return errors.New(errors.ErrUserNotFound, "User not found")
	}

	r.logger.Info("User updated successfully",
		zap.Int("user_id", user.ID))

	return nil
}

func (r *userRepository) BindAppleSub(ctx context.Context, userID int, appleSub string) error {
	query := `
		UPDATE users
		SET apple_sub = ?, updated_at = ?
		WHERE id = ?
	`

	result, err := r.db.DB.ExecContext(ctx, r.db.Rebind(query), appleSub, time.Now().Unix(), userID)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok {
			if string(pqErr.Code) == "23505" {
				return errors.New(errors.ErrResourceConflict, "Apple account already bound")
			}
		}
		r.logger.Error("Failed to bind apple sub",
			zap.Error(err),
			zap.Int("user_id", userID))
		return errors.NewDatabaseError("Failed to bind apple sub", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errors.NewDatabaseError("Failed to verify update", err)
	}
	if rowsAffected == 0 {
		return errors.New(errors.ErrUserNotFound, "User not found")
	}

	return nil
}

// UpdateVerificationCode 更新用户的验证码。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - email: User email address
//   - code: Verification code
//   - expiry: Code expiry time
//
// Returns:
//   - error: Error if update fails
func (r *userRepository) UpdateVerificationCode(ctx context.Context, email, code string, expiry time.Time) error {
	query := `
		UPDATE users
		SET verification_code = ?, code_expiry = ?, code_attempts = 0, updated_at = ?
		WHERE email = ?
	`

	expiryUnix := expiry.Unix()
	now := time.Now().Unix()

	result, err := r.db.ExecContext(ctx, r.db.Rebind(query), code, expiryUnix, now, email)

	if err != nil {
		r.logger.Error("Failed to update verification code",
			zap.Error(err),
			zap.String("email", email))
		return errors.NewDatabaseError("Failed to update verification code", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errors.NewDatabaseError("Failed to verify update", err)
	}

	if rowsAffected == 0 {
		return errors.New(errors.ErrUserNotFound, "User not found")
	}

	r.logger.Info("Verification code updated",
		zap.String("email", email))

	return nil
}

// UpdateLastActive 更新用户的最后活动时间戳。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - userID: User ID
//
// Returns:
//   - error: Error if update fails
func (r *userRepository) UpdateLastActive(ctx context.Context, userID int) error {
	query := `
		UPDATE users
		SET last_active_at = ?, updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()

	_, err := r.db.DB.ExecContext(ctx, r.db.Rebind(query), now, now, userID)

	if err != nil {
		r.logger.Error("Failed to update last active",
			zap.Error(err),
			zap.Int("user_id", userID))
		return errors.NewDatabaseError("Failed to update last active", err)
	}

	return nil
}

// IncrementCodeAttempts 增加验证码尝试次数。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - userID: User ID
//
// Returns:
//   - error: Error if update fails
func (r *userRepository) IncrementCodeAttempts(ctx context.Context, userID int) error {
	query := `
		UPDATE users
		SET code_attempts = code_attempts + 1, updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()

	_, err := r.db.DB.ExecContext(ctx, r.db.Rebind(query), now, userID)

	if err != nil {
		r.logger.Error("Failed to increment code attempts",
			zap.Error(err),
			zap.Int("user_id", userID))
		return errors.NewDatabaseError("Failed to increment code attempts", err)
	}

	return nil
}

// ClearVerificationCode 清除用户的验证码并重置尝试次数。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - userID: User ID
//
// Returns:
//   - error: Error if update fails
func (r *userRepository) ClearVerificationCode(ctx context.Context, userID int) error {
	query := `
		UPDATE users
		SET verification_code = NULL,
		    code_expiry = NULL,
		    code_attempts = 0,
		    updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()

	result, err := r.db.DB.ExecContext(ctx, r.db.Rebind(query), now, userID)
	if err != nil {
		r.logger.Error("Failed to clear verification code",
			zap.Error(err),
			zap.Int("user_id", userID))
		return errors.NewDatabaseError("Failed to clear verification code", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errors.NewDatabaseError("Failed to verify update", err)
	}

	if rowsAffected == 0 {
		return errors.New(errors.ErrUserNotFound, "User not found")
	}

	r.logger.Info("Verification code cleared",
		zap.Int("user_id", userID))

	return nil
}

// UpdateRole 更新用户的订阅角色。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - userID: User ID
//   - role: New user role
//
// Returns:
//   - error: Error if update fails
func (r *userRepository) UpdateRole(ctx context.Context, userID int, role model.UserRole) error {
	query := `
		UPDATE users
		SET role = ?, updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()
	result, err := r.db.DB.ExecContext(ctx, r.db.Rebind(query), role.String(), now, userID)

	if err != nil {
		r.logger.Error("Failed to update user role",
			zap.Error(err),
			zap.Int("user_id", userID),
			zap.String("role", role.String()))
		return errors.NewDatabaseError("Failed to update user role", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return errors.NewDatabaseError("Failed to verify update", err)
	}

	if rowsAffected == 0 {
		return errors.New(errors.ErrUserNotFound, "User not found")
	}

	r.logger.Info("User role updated",
		zap.Int("user_id", userID),
		zap.String("role", role.String()))

	return nil
}

// UpdateActiveSession 更新用户的活动会话 ID。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - userID: User ID
//   - sessionID: New session ID
//
// Returns:
//   - error: Error if update fails
func (r *userRepository) UpdateActiveSession(ctx context.Context, userID int, sessionID string) error {
	query := `
		UPDATE users
		SET active_session_id = ?, updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()

	_, err := r.db.DB.ExecContext(ctx, r.db.Rebind(query), sessionID, now, userID)

	if err != nil {
		r.logger.Error("Failed to update active session",
			zap.Error(err),
			zap.Int("user_id", userID))
		return errors.NewDatabaseError("Failed to update active session", err)
	}

	r.logger.Info("Active session updated",
		zap.Int("user_id", userID))

	return nil
}

// HasPaidBefore 检查用户是否曾经付费。
// 通过查询 transactions 表中是否有成功的付费记录来判断。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - userID: User ID
//
// Returns:
//   - bool: True if user has paid before
//   - error: Error if query fails
func (r *userRepository) HasPaidBefore(ctx context.Context, userID int) (bool, error) {
	query := `
		SELECT COUNT(*) > 0 as has_paid
		FROM transactions 
		WHERE user_id = ? 
		  AND status IN ('VALIDATED', 'FULFILLED')
		  AND product_type != 'FREE'
	`

	var hasPaid bool
	err := r.db.DB.QueryRowContext(ctx, r.db.Rebind(query), userID).Scan(&hasPaid)

	if err != nil {
		r.logger.Error("Failed to check user payment history",
			zap.Error(err),
			zap.Int("user_id", userID))
		return false, errors.NewDatabaseError("Failed to check payment history", err)
	}

	r.logger.Debug("Checked user payment history",
		zap.Int("user_id", userID),
		zap.Bool("has_paid", hasPaid))

	return hasPaid, nil
}

// UpgradeUserRole 升级用户角色（只允许向更高级别升级）。
// 该方法会检查目标角色是否比当前角色更高级，只有在升级的情况下才会更新。
//
// Parameters:
//   - ctx: Context for timeout and cancellation
//   - userID: User ID
//   - targetRole: Target role to upgrade to
//
// Returns:
//   - error: Error if upgrade fails or is not allowed
func (r *userRepository) UpgradeUserRole(ctx context.Context, userID int, targetRole model.UserRole) error {
	// 首先获取当前用户信息
	user, err := r.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user %d: %w", userID, err)
	}

	if user.IsVip() {
		return nil
	}

	currentRole := model.UserRole(user.Role)

	// 检查目标角色是否有效
	if !targetRole.IsValid() {
		return fmt.Errorf("invalid target role: %s", targetRole)
	}

	// 检查是否为升级（只允许升级，不允许降级）
	if !targetRole.IsHigherThan(currentRole) {
		r.logger.Warn("Role upgrade rejected: target role is not higher than current role",
			zap.Int("user_id", userID),
			zap.String("current_role", string(currentRole)),
			zap.String("target_role", string(targetRole)),
			zap.Int("current_level", currentRole.GetRoleLevel()),
			zap.Int("target_level", targetRole.GetRoleLevel()))

		return fmt.Errorf("cannot downgrade or maintain role from %s to %s", currentRole, targetRole)
	}

	// 执行角色升级
	query := `
		UPDATE users
		SET role = ?, updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()
	// P0 修复: 使用 database.ExecContext 以支持事务上下文，避免死锁
	result, err := database.ExecContext(ctx, r.db.DB.DB, query, targetRole.String(), now, userID)

	if err != nil {
		r.logger.Error("Failed to upgrade user role",
			zap.Error(err),
			zap.Int("user_id", userID),
			zap.String("current_role", string(currentRole)),
			zap.String("target_role", string(targetRole)))
		return errors.NewDatabaseError("Failed to upgrade user role", err)
	}

	// 检查是否更新成功
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("Failed to get rows affected for role upgrade",
			zap.Error(err))
		return errors.NewDatabaseError("Failed to verify role upgrade", err)
	}

	if rowsAffected == 0 {
		return errors.New(errors.ErrUserNotFound, "User not found for role upgrade")
	}

	r.logger.Info("User role upgraded successfully",
		zap.Int("user_id", userID),
		zap.String("from_role", string(currentRole)),
		zap.String("to_role", string(targetRole)),
		zap.Int("from_level", currentRole.GetRoleLevel()),
		zap.Int("to_level", targetRole.GetRoleLevel()))

	return nil
}
