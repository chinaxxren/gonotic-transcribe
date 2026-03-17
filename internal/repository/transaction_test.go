package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

func newTransactionTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
	t.Helper()
	mdb, mock, err := sqlmock.New()
	require.NoError(t, err)

	sqlxDB := sqlx.NewDb(mdb, "mysql")
	appDB := database.NewFromSQLX(sqlxDB, zap.NewNop())
	cleanup := func() {
		sqlxDB.Close()
		mdb.Close()
	}
	return appDB, mock, cleanup
}

func TestTransactionRepository_CreateAndGet(t *testing.T) {
	appDB, mock, cleanup := newTransactionTestDB(t)
	defer cleanup()

	repo := NewTransactionRepository(appDB)
	now := time.Now().Unix()
	purchased := now - 3600
	expires := now + 7200
	promoID := "promo-123"
	promoTypeVal := 1
	promoType := &promoTypeVal

	tx := &model.Transaction{
		UserID:               1,
		Provider:             "apple",
		ProviderTransaction:  "txn-1",
		ProductID:            "prod-1",
		ProductType:          model.TransactionProductYearSub,
		PromotionalOfferID:   &promoID,
		PromotionalOfferType: promoType,
		Status:               model.TransactionStatusFulfilled,
		AmountCents:          299,
		Currency:             "USD",
		AccountSnapshot:      []byte(`{"balance":100}`),
		PurchasedAt:          purchased,
		ExpiresAt:            &expires,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
        INSERT INTO transactions (
            user_id, provider, provider_txn_id, product_id, product_type,
            promotional_offer_id, promotional_offer_type,
            status, amount_cents, currency, account_snapshot, purchased_at, expires_at,
            created_at, updated_at
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
        RETURNING id
    `)).
		WithArgs(
			tx.UserID,
			tx.Provider,
			tx.ProviderTransaction,
			tx.ProductID,
			tx.ProductType,
			*tx.PromotionalOfferID,
			*tx.PromotionalOfferType,
			tx.Status,
			tx.AmountCents,
			tx.Currency,
			tx.AccountSnapshot,
			tx.PurchasedAt,
			*tx.ExpiresAt,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(101)))

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT id, user_id, provider, provider_txn_id, product_id, product_type,
            promotional_offer_id, promotional_offer_type,
            status, amount_cents, currency, account_snapshot, purchased_at, expires_at,
            created_at, updated_at
        FROM transactions
        WHERE provider = $1 AND provider_txn_id = $2
        LIMIT 1
    `)).
		WithArgs(tx.Provider, tx.ProviderTransaction).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "provider", "provider_txn_id", "product_id", "product_type",
			"promotional_offer_id", "promotional_offer_type",
			"status", "amount_cents", "currency", "account_snapshot", "purchased_at", "expires_at",
			"created_at", "updated_at",
		}).AddRow(
			int64(101), tx.UserID, tx.Provider, tx.ProviderTransaction, tx.ProductID, tx.ProductType,
			*tx.PromotionalOfferID, *tx.PromotionalOfferType,
			tx.Status, tx.AmountCents, tx.Currency, tx.AccountSnapshot, tx.PurchasedAt, *tx.ExpiresAt,
			now, now,
		))

	err := repo.Create(context.Background(), tx)
	require.NoError(t, err)
	require.Equal(t, int64(101), tx.ID)

	fetched, err := repo.GetByProviderTxn(context.Background(), tx.Provider, tx.ProviderTransaction)
	require.NoError(t, err)
	require.Equal(t, tx.ID, fetched.ID)
	require.Equal(t, *tx.PromotionalOfferID, *fetched.PromotionalOfferID)
	require.Equal(t, tx.AccountSnapshot, fetched.AccountSnapshot)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_UpdateMutations(t *testing.T) {
	appDB, mock, cleanup := newTransactionTestDB(t)
	defer cleanup()

	repo := NewTransactionRepository(appDB)
	id := int64(9)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE transactions
        SET status = $1, updated_at = $2
        WHERE id = $3`)).
		WithArgs(model.TransactionStatusRevoked, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE transactions
        SET account_snapshot = $1, updated_at = $2
        WHERE id = $3`)).
		WithArgs([]byte(`{"balance":0}`), sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.UpdateStatus(context.Background(), id, model.TransactionStatusRevoked))
	require.NoError(t, repo.UpdateAccountSnapshot(context.Background(), id, []byte(`{"balance":0}`)))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_GetByProviderTxn_NoRows(t *testing.T) {
	appDB, mock, cleanup := newTransactionTestDB(t)
	defer cleanup()

	repo := NewTransactionRepository(appDB)
	provider := "apple"
	txn := "missing"

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT id, user_id, provider, provider_txn_id, product_id, product_type,
            promotional_offer_id, promotional_offer_type,
            status, amount_cents, currency, account_snapshot, purchased_at, expires_at,
            created_at, updated_at
        FROM transactions
        WHERE provider = $1 AND provider_txn_id = $2
        LIMIT 1
    `)).WithArgs(provider, txn).WillReturnError(sql.ErrNoRows)

	got, err := repo.GetByProviderTxn(context.Background(), provider, txn)
	require.NoError(t, err)
	require.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_HasFulfilledProduct_FalseWhenNoRows(t *testing.T) {
	appDB, mock, cleanup := newTransactionTestDB(t)
	defer cleanup()

	repo := NewTransactionRepository(appDB)
	userID := 9
	productID := "com.app.hd"

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT 1
		FROM transactions
		WHERE user_id = $1
		  AND product_id = $2
		  AND status = $3
		LIMIT 1
	`)).
		WithArgs(userID, productID, model.TransactionStatusFulfilled).
		WillReturnError(sql.ErrNoRows)

	ok, err := repo.HasFulfilledProduct(context.Background(), userID, productID)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTransactionRepository_HasFulfilledProduct_TrueWhenExists(t *testing.T) {
	appDB, mock, cleanup := newTransactionTestDB(t)
	defer cleanup()

	repo := NewTransactionRepository(appDB)
	userID := 9
	productID := "com.app.hd"

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT 1
		FROM transactions
		WHERE user_id = $1
		  AND product_id = $2
		  AND status = $3
		LIMIT 1
	`)).
		WithArgs(userID, productID, model.TransactionStatusFulfilled).
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))

	ok, err := repo.HasFulfilledProduct(context.Background(), userID, productID)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, mock.ExpectationsWereMet())
}
