package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

// TransactionRepository 定义交易表的读写接口，隔离上层服务与 SQL 细节。
type TransactionRepository interface {
	Create(ctx context.Context, tx *model.Transaction) error                                          // 新增交易
	GetByID(ctx context.Context, id int64) (*model.Transaction, error)                                // 通过 ID 查询
	GetByProviderTxn(ctx context.Context, provider, providerTxnID string) (*model.Transaction, error) // 通过第三方交易 ID 查询
	HasFulfilledProduct(ctx context.Context, userID int, productID string) (bool, error)              // 是否已成功购买过指定产品
	UpdateStatus(ctx context.Context, id int64, status model.TransactionStatus) error                 // 更新状态
	UpdateAccountSnapshot(ctx context.Context, id int64, snapshot []byte) error                       // 更新账本快照（JSON）
}

// transactionRepository is TransactionRepository's PostgreSQL implementation.
type transactionRepository struct {
	db *sql.DB
}

// NewTransactionRepository 创建交易仓储实例。
func NewTransactionRepository(db *database.DB) TransactionRepository {
	return &transactionRepository{db: db.DB.DB}
}

// Create 插入交易记录。
func (r *transactionRepository) Create(ctx context.Context, txModel *model.Transaction) error {
	query := `
		INSERT INTO transactions (
			user_id, provider, provider_txn_id, product_id, product_type,
			promotional_offer_id, promotional_offer_type,
			status, amount_cents, currency, account_snapshot, purchased_at, expires_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id
	`

	var promoID sql.NullString
	if txModel.PromotionalOfferID != nil {
		promoID.String = *txModel.PromotionalOfferID
		promoID.Valid = true
	}

	var promoType sql.NullInt64
	if txModel.PromotionalOfferType != nil {
		promoType.Int64 = int64(*txModel.PromotionalOfferType)
		promoType.Valid = true
	}

	var purchasedAt sql.NullInt64
	if txModel.PurchasedAt != 0 {
		purchasedAt.Int64 = txModel.PurchasedAt
		purchasedAt.Valid = true
	}

	var expiresAt sql.NullInt64
	if txModel.ExpiresAt != nil {
		expiresAt.Int64 = *txModel.ExpiresAt
		expiresAt.Valid = true
	}

	nowUnix := time.Now().Unix()
	var createdAt interface{} = nowUnix
	var updatedAt interface{} = nowUnix
	if txModel.CreatedAt != 0 {
		createdAt = txModel.CreatedAt
	}
	if txModel.UpdatedAt != 0 {
		updatedAt = txModel.UpdatedAt
	}

	var id int64
	row := database.QueryRowContext(ctx, r.db, query,
		txModel.UserID,
		txModel.Provider,
		txModel.ProviderTransaction,
		txModel.ProductID,
		txModel.ProductType,
		promoID,
		promoType,
		txModel.Status,
		txModel.AmountCents,
		txModel.Currency,
		txModel.AccountSnapshot,
		purchasedAt,
		expiresAt,
		createdAt,
		updatedAt,
	)
	if err := row.Scan(&id); err != nil {
		return fmt.Errorf("创建交易记录失败: %w", err)
	}
	txModel.ID = id
	txModel.CreatedAt = nowUnix
	txModel.UpdatedAt = nowUnix
	return nil
}

func (r *transactionRepository) GetByID(ctx context.Context, id int64) (*model.Transaction, error) {
	query := `
		SELECT id, user_id, provider, provider_txn_id, product_id, product_type,
			promotional_offer_id, promotional_offer_type,
			status, amount_cents, currency, account_snapshot, purchased_at, expires_at,
			created_at, updated_at
		FROM transactions
		WHERE id = ?
		LIMIT 1
	`

	var txModel model.Transaction
	var promoID sql.NullString
	var promoType sql.NullInt64
	var expiresAt sql.NullInt64
	var createdAt int64
	var updatedAt int64
	row := database.QueryRowContext(ctx, r.db, query, id)
	if err := row.Scan(
		&txModel.ID,
		&txModel.UserID,
		&txModel.Provider,
		&txModel.ProviderTransaction,
		&txModel.ProductID,
		&txModel.ProductType,
		&promoID,
		&promoType,
		&txModel.Status,
		&txModel.AmountCents,
		&txModel.Currency,
		&txModel.AccountSnapshot,
		&txModel.PurchasedAt,
		&expiresAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("查询交易失败: %w", err)
	}

	if expiresAt.Valid {
		value := expiresAt.Int64
		txModel.ExpiresAt = &value
	}
	if promoID.Valid {
		val := promoID.String
		txModel.PromotionalOfferID = &val
	}
	if promoType.Valid {
		val := int(promoType.Int64)
		txModel.PromotionalOfferType = &val
	}
	txModel.CreatedAt = createdAt
	txModel.UpdatedAt = updatedAt
	return &txModel, nil
}

// GetByProviderTxn 通过第三方交易 ID 获取交易。
func (r *transactionRepository) GetByProviderTxn(ctx context.Context, provider, providerTxnID string) (*model.Transaction, error) {
	query := `
		SELECT id, user_id, provider, provider_txn_id, product_id, product_type,
			promotional_offer_id, promotional_offer_type,
			status, amount_cents, currency, account_snapshot, purchased_at, expires_at,
			created_at, updated_at
		FROM transactions
		WHERE provider = ? AND provider_txn_id = ?
		LIMIT 1
	`

	var txModel model.Transaction
	var promoID sql.NullString
	var promoType sql.NullInt64
	var expiresAt sql.NullInt64
	var createdAt int64
	var updatedAt int64
	row := database.QueryRowContext(ctx, r.db, query, provider, providerTxnID)
	if err := row.Scan(
		&txModel.ID,
		&txModel.UserID,
		&txModel.Provider,
		&txModel.ProviderTransaction,
		&txModel.ProductID,
		&txModel.ProductType,
		&promoID,
		&promoType,
		&txModel.Status,
		&txModel.AmountCents,
		&txModel.Currency,
		&txModel.AccountSnapshot,
		&txModel.PurchasedAt,
		&expiresAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("查询交易失败: %w", err)
	}

	if expiresAt.Valid {
		value := expiresAt.Int64
		txModel.ExpiresAt = &value
	}
	if promoID.Valid {
		id := promoID.String
		txModel.PromotionalOfferID = &id
	}
	if promoType.Valid {
		val := int(promoType.Int64)
		txModel.PromotionalOfferType = &val
	}
	txModel.CreatedAt = createdAt
	txModel.UpdatedAt = updatedAt
	return &txModel, nil
}

func (r *transactionRepository) HasFulfilledProduct(ctx context.Context, userID int, productID string) (bool, error) {
	query := `
		SELECT 1
		FROM transactions
		WHERE user_id = ?
		  AND product_id = ?
		  AND status = ?
		LIMIT 1
	`
	var one int
	row := database.QueryRowContext(ctx, r.db, query, userID, productID, model.TransactionStatusFulfilled)
	if err := row.Scan(&one); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("查询用户购买记录失败: %w", err)
	}
	return true, nil
}

// UpdateStatus 更新交易状态。
func (r *transactionRepository) UpdateStatus(ctx context.Context, id int64, status model.TransactionStatus) error {
	query := `
		UPDATE transactions
		SET status = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query, status, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("更新交易状态失败: %w", err)
	}
	return nil
}

// UpdateAccountSnapshot 更新发货账本快照 JSON，便于下游审计。
func (r *transactionRepository) UpdateAccountSnapshot(ctx context.Context, id int64, snapshot []byte) error {
	query := `
		UPDATE transactions
		SET account_snapshot = ?, updated_at = ?
		WHERE id = ?
	`
	if _, err := database.ExecContext(ctx, r.db, query, snapshot, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("更新交易账本快照失败: %w", err)
	}
	return nil
}
