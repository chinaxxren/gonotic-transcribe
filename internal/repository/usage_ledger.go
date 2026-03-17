package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
	"go.uber.org/zap"
)

// UsageLedgerRepository 定义 usage_ledger 相关的数据库操作。
type UsageLedgerRepository interface {
	Create(ctx context.Context, entry *model.UsageLedger) error                                                                                                // 创建记录
	ListByUser(ctx context.Context, userID int, limit int) ([]*model.UsageLedger, error)                                                                       // 查询最近流水
	GetAggregatedStats(ctx context.Context, userID int, startTime int64, endTime int64) (*model.UsageStats, error)                                             // 聚合统计
	GetOriginProductStats(ctx context.Context, userID int, startTime int64, endTime int64) (*model.UsageOriginProductStats, error)                             // 按 origin_product_type 聚合
	UpdateConsumption(ctx context.Context, id int64, additionalSeconds int, additionalTranscription int, additionalTranslation int, newBalanceAfter int) error // 更新消费记录
	UpdateConsumptionWithMeta(ctx context.Context, id int64, additionalSeconds int, additionalTranscription int, additionalTranslation int, newBalanceAfter int, originProductType *string) error
	BatchCreate(ctx context.Context, entries []*model.UsageLedger) error                                    // 批量创建
	FindByBusinessAndCycle(ctx context.Context, businessID int, cycleID *int64) (*model.UsageLedger, error) // 按业务ID和周期ID查找记录
}

type usageLedgerRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

func nullableStringPtr(ptr *string) interface{} {
	if ptr == nil {
		return nil
	}
	return *ptr
}

// NewUsageLedgerRepository 创建仓储。
func NewUsageLedgerRepository(db *database.DB) UsageLedgerRepository {
	return &usageLedgerRepository{
		db:     db.DB.DB,
		logger: zap.NewNop(), // 默认使用无操作logger
	}
}

// NewUsageLedgerRepositoryWithLogger 创建带logger的仓储。
func NewUsageLedgerRepositoryWithLogger(db *database.DB, logger *zap.Logger) UsageLedgerRepository {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &usageLedgerRepository{
		db:     db.DB.DB,
		logger: logger,
	}
}

// Create 写入一条 usage_ledger 记录。

func (r *usageLedgerRepository) Create(ctx context.Context, entry *model.UsageLedger) error {
	// BusinessID 校验和告警
	r.validateBusinessID(entry)

	query := `
		INSERT INTO usage_ledger (
			user_id, business_id, cycle_id, origin_product_type, seconds_consumed,
			balance_before, balance_after, source,
			transcription_seconds, translation_seconds,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	now := time.Now().Unix()

	var id int64
	err := database.QueryRowContext(ctx, r.db, query,
		entry.UserID,
		entry.BusinessID,
		nullableInt64Ptr(entry.CycleID),
		nullableStringPtr(entry.OriginProductType),
		entry.Seconds,
		entry.BalanceBefore,
		entry.BalanceAfter,
		entry.Source,
		entry.TranscriptionSeconds,
		entry.TranslationSeconds,
		now,
		now,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("创建 usage_ledger 失败: %w", err)
	}
	entry.ID = id
	entry.CreatedAt = now
	return nil
}

func (r *usageLedgerRepository) UpdateConsumptionWithMeta(ctx context.Context, id int64, additionalSeconds int, additionalTranscription int, additionalTranslation int, newBalanceAfter int, originProductType *string) error {
	query := `
		UPDATE usage_ledger 
		SET 
			seconds_consumed = seconds_consumed + ?,
			transcription_seconds = transcription_seconds + ?,
			translation_seconds = translation_seconds + ?,
			balance_after = ?,
			origin_product_type = ?,
			updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()

	result, err := database.ExecContext(ctx, r.db, query,
		additionalSeconds,
		additionalTranscription,
		additionalTranslation,
		newBalanceAfter,
		nullableStringPtr(originProductType),
		now,
		id,
	)
	if err != nil {
		return fmt.Errorf("更新 usage_ledger 失败: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("获取更新行数失败: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("没有找到 ID 为 %d 的记录", id)
	}

	return nil
}

// ListByUser 查询最近的 usage 记录（按时间倒序）。
func (r *usageLedgerRepository) ListByUser(ctx context.Context, userID int, limit int) ([]*model.UsageLedger, error) {
	if limit <= 0 {
		limit = 50
	}
	query := fmt.Sprintf(`
		SELECT id, user_id, business_id, seconds_consumed,
			balance_before, balance_after, source, cycle_id,
			origin_product_type,
			transcription_seconds, translation_seconds, created_at
		FROM usage_ledger
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT %d
	`, limit)

	rows, err := database.QueryContext(ctx, r.db, query, userID)
	if err != nil {
		return nil, fmt.Errorf("查询 usage_ledger 失败: %w", err)
	}
	defer rows.Close()

	var results []*model.UsageLedger
	for rows.Next() {
		var entry model.UsageLedger
		var cycleID sql.NullInt64
		var originProductType sql.NullString
		if err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.BusinessID,
			&entry.Seconds,
			&entry.BalanceBefore,
			&entry.BalanceAfter,
			&entry.Source,
			&cycleID,
			&originProductType,
			&entry.TranscriptionSeconds,
			&entry.TranslationSeconds,
			&entry.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("扫描 usage_ledger 失败: %w", err)
		}
		if cycleID.Valid {
			value := cycleID.Int64
			entry.CycleID = &value
		}
		if originProductType.Valid {
			value := originProductType.String
			entry.OriginProductType = &value
		}
		results = append(results, &entry)
	}

	return results, nil
}

// UpdateConsumption 更新消费记录的累计消费
func (r *usageLedgerRepository) UpdateConsumption(ctx context.Context, id int64, additionalSeconds int, additionalTranscription int, additionalTranslation int, newBalanceAfter int) error {
	query := `
		UPDATE usage_ledger 
		SET 
			seconds_consumed = seconds_consumed + ?,
			transcription_seconds = transcription_seconds + ?,
			translation_seconds = translation_seconds + ?,
			balance_after = ?,
			updated_at = ?
		WHERE id = ?
	`

	now := time.Now().Unix()

	result, err := database.ExecContext(ctx, r.db, query,
		additionalSeconds,
		additionalTranscription,
		additionalTranslation,
		newBalanceAfter,
		now,
		id,
	)
	if err != nil {
		return fmt.Errorf("更新 usage_ledger 失败: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("获取更新行数失败: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("没有找到 ID 为 %d 的记录", id)
	}

	return nil
}

// GetAggregatedStats 统计用户在指定时间范围内的用量。
func (r *usageLedgerRepository) GetAggregatedStats(ctx context.Context, userID int, startTime int64, endTime int64) (*model.UsageStats, error) {
	query := `
		SELECT 
			SUM(seconds_consumed) as total_seconds,
			SUM(transcription_seconds) as transcription_seconds,
			SUM(translation_seconds) as translation_seconds
		FROM usage_ledger
		WHERE user_id = ? AND created_at >= ? AND created_at <= ?
	`

	var totalSeconds sql.NullInt64
	var transcriptionSeconds sql.NullFloat64
	var translationSeconds sql.NullFloat64

	err := database.QueryRowContext(ctx, r.db, query, userID, startTime, endTime).Scan(
		&totalSeconds,
		&transcriptionSeconds,
		&translationSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("查询 usage stats 失败: %w", err)
	}

	stats := &model.UsageStats{
		TotalSecondsConsumed: totalSeconds.Int64,
		TranscriptionSeconds: int64(transcriptionSeconds.Float64),
		TranslationSeconds:   int64(translationSeconds.Float64),
	}

	return stats, nil
}

func (r *usageLedgerRepository) GetOriginProductStats(ctx context.Context, userID int, startTime int64, endTime int64) (*model.UsageOriginProductStats, error) {
	query := `
		SELECT
			COALESCE(SUM(CASE WHEN origin_product_type = 'HOUR_PACK' THEN seconds_consumed ELSE 0 END), 0) AS hour_pack_seconds,
			COALESCE(SUM(CASE WHEN origin_product_type = 'SPECIAL_OFFER' THEN seconds_consumed ELSE 0 END), 0) AS special_offer_seconds
		FROM usage_ledger
		WHERE user_id = ? AND created_at >= ? AND created_at <= ?
	`

	var hourPack sql.NullInt64
	var specialOffer sql.NullInt64
	if err := database.QueryRowContext(ctx, r.db, query, userID, startTime, endTime).Scan(&hourPack, &specialOffer); err != nil {
		return nil, fmt.Errorf("查询 origin product stats 失败: %w", err)
	}

	stats := &model.UsageOriginProductStats{
		HourPackSeconds:     hourPack.Int64,
		SpecialOfferSeconds: specialOffer.Int64,
	}
	return stats, nil
}

// BatchCreate 批量创建记录
// P1 修复: 使用事务包装批量插入，确保原子性
func (r *usageLedgerRepository) BatchCreate(ctx context.Context, entries []*model.UsageLedger) error {
	if len(entries) == 0 {
		return nil
	}

	// 检查是否已在事务中，直接使用现有事务
	if tx, ok := database.GetTx(ctx); ok {
		return r.batchCreateWithSqlxTx(ctx, tx, entries)
	}

	// 开启新事务
	sqlxDB := sqlx.NewDb(r.db, "postgres")
	tx, err := sqlxDB.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}

	txCtx := database.ContextWithTx(ctx, tx)
	if err := r.batchCreateWithSqlxTx(txCtx, tx, entries); err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败: %w", err)
	}
	return nil
}

// batchCreateWithSqlxTx 在 sqlx 事务中批量创建记录
func (r *usageLedgerRepository) batchCreateWithSqlxTx(ctx context.Context, tx *sqlx.Tx, entries []*model.UsageLedger) error {
	query := `
		INSERT INTO usage_ledger (
			user_id, business_id, cycle_id, origin_product_type, seconds_consumed,
			balance_before, balance_after, source,
			transcription_seconds, translation_seconds,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	query = sqlx.Rebind(sqlx.DOLLAR, query)

	now := time.Now().Unix()
	for _, entry := range entries {
		// BusinessID 校验和告警
		r.validateBusinessID(entry)

		var id int64
		err := tx.QueryRowContext(ctx, query,
			entry.UserID,
			entry.BusinessID,
			nullableInt64Ptr(entry.CycleID),
			nullableStringPtr(entry.OriginProductType),
			entry.Seconds,
			entry.BalanceBefore,
			entry.BalanceAfter,
			entry.Source,
			entry.TranscriptionSeconds,
			entry.TranslationSeconds,
			now,
			now,
		).Scan(&id)
		if err != nil {
			return fmt.Errorf("批量创建 usage_ledger 失败: %w", err)
		}
		entry.ID = id
		entry.CreatedAt = now
	}

	return nil
}

// validateBusinessID 校验BusinessID的合规性并记录告警
func (r *usageLedgerRepository) validateBusinessID(entry *model.UsageLedger) {
	// 检查BusinessID是否为空或异常值
	if entry.BusinessID == 0 {
		r.logger.Warn("UsageLedger BusinessID为空，可能影响会议维度统计",
			zap.Int("user_id", entry.UserID),
			zap.Int("business_id", entry.BusinessID),
			zap.String("source", entry.Source),
			zap.Int("seconds", entry.Seconds))
		return
	}

	// 检查BusinessID是否为负数（异常值）
	if entry.BusinessID < 0 {
		r.logger.Warn("UsageLedger BusinessID为负数，格式异常",
			zap.Int("user_id", entry.UserID),
			zap.Int("business_id", entry.BusinessID),
			zap.String("source", entry.Source),
			zap.Int("seconds", entry.Seconds))
		return
	}

	// 对于转录相关的Source，BusinessID应该是有效的meeting_id
	if entry.Source == "transcription" || entry.Source == "meeting" {
		if entry.BusinessID <= 0 {
			r.logger.Warn("转录相关扣费的BusinessID应为有效的meeting_id",
				zap.Int("user_id", entry.UserID),
				zap.Int("business_id", entry.BusinessID),
				zap.String("source", entry.Source),
				zap.Int("seconds", entry.Seconds))
		}
	}
}

// FindByBusinessAndCycle 按业务ID和周期ID查找现有的用量记录
func (r *usageLedgerRepository) FindByBusinessAndCycle(ctx context.Context, businessID int, cycleID *int64) (*model.UsageLedger, error) {
	// 对于转录扣费，cycleID 必须存在
	if cycleID == nil {
		return nil, fmt.Errorf("cycleID 不能为空")
	}

	query := `
		SELECT id, user_id, business_id, cycle_id, origin_product_type, seconds_consumed,
			balance_before, balance_after, source,
			transcription_seconds, translation_seconds, created_at
		FROM usage_ledger
		WHERE business_id = ? AND cycle_id = ?
		LIMIT 1
	`
	args := []interface{}{businessID, *cycleID}

	row := database.QueryRowContext(ctx, r.db, query, args...)

	var entry model.UsageLedger
	var dbCycleID sql.NullInt64
	var originProductType sql.NullString

	err := row.Scan(
		&entry.ID,
		&entry.UserID,
		&entry.BusinessID,
		&dbCycleID,
		&originProductType,
		&entry.Seconds,
		&entry.BalanceBefore,
		&entry.BalanceAfter,
		&entry.Source,
		&entry.TranscriptionSeconds,
		&entry.TranslationSeconds,
		&entry.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // 没找到记录，返回 nil 而不是错误
		}
		return nil, fmt.Errorf("查询 usage_ledger 失败: %w", err)
	}

	if dbCycleID.Valid {
		value := dbCycleID.Int64
		entry.CycleID = &value
	}
	if originProductType.Valid {
		value := originProductType.String
		entry.OriginProductType = &value
	}

	return &entry, nil
}
