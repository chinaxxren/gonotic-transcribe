package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
)

func newUsageLedgerTestDB(t *testing.T) (*database.DB, sqlmock.Sqlmock, func()) {
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

func TestUsageLedgerRepository_Create(t *testing.T) {
	appDB, mock, cleanup := newUsageLedgerTestDB(t)
	defer cleanup()

	repo := NewUsageLedgerRepository(appDB)
	cycleID := int64(3)

	entry := &model.UsageLedger{
		UserID:               1,
		BusinessID:           2,
		CycleID:              &cycleID,
		Seconds:              -300,
		BalanceBefore:        1000,
		BalanceAfter:         700,
		Source:               "test",
		TranscriptionSeconds: 240,
		TranslationSeconds:   60,
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
        INSERT INTO usage_ledger (
			user_id, business_id, cycle_id, origin_product_type, seconds_consumed,
			balance_before, balance_after, source,
			transcription_seconds, translation_seconds,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
        RETURNING id
    `)).
		WithArgs(entry.UserID, entry.BusinessID, entry.CycleID, nil, entry.Seconds, entry.BalanceBefore, entry.BalanceAfter, entry.Source, entry.TranscriptionSeconds, entry.TranslationSeconds, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(101)))

	err := repo.Create(context.Background(), entry)
	require.NoError(t, err)
	require.Equal(t, int64(101), entry.ID)
	require.NotZero(t, entry.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLedgerRepository_ListByUser(t *testing.T) {
	appDB, mock, cleanup := newUsageLedgerTestDB(t)
	defer cleanup()

	repo := NewUsageLedgerRepository(appDB)
	userID := 1
	limit := 5

	rows := sqlmock.NewRows([]string{
		"id", "user_id", "business_id", "seconds_consumed", "balance_before", "balance_after",
		"source", "cycle_id", "origin_product_type",
		"transcription_seconds", "translation_seconds", "created_at",
	}).AddRow(
		int64(1), userID, int64(10), -300, 1000, 700, "meeting", int64(100), "HOUR_PACK", 240, 60, 1234567890,
	).AddRow(
		int64(2), userID, int64(11), -120, 700, 580, "summary", int64(101), nil, 120, 0, 1234567950,
	)

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT id, user_id, business_id, seconds_consumed,
            balance_before, balance_after, source, cycle_id,
			origin_product_type,
			transcription_seconds, translation_seconds, created_at
        FROM usage_ledger
        WHERE user_id = $1
        ORDER BY created_at DESC
        LIMIT 5
    `)).WithArgs(userID).WillReturnRows(rows)

	result, err := repo.ListByUser(context.Background(), userID, limit)
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Equal(t, userID, result[0].UserID)
	require.Equal(t, -300, result[0].Seconds)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLedgerRepository_UpdateConsumption(t *testing.T) {
	appDB, mock, cleanup := newUsageLedgerTestDB(t)
	defer cleanup()

	repo := NewUsageLedgerRepository(appDB)
	id := int64(9)
	additionalSeconds := -180
	additionalTranscription := 120
	additionalTranslation := 60
	newBalanceAfter := 520

	mock.ExpectExec(regexp.QuoteMeta(`
        UPDATE usage_ledger
        SET seconds_consumed = seconds_consumed + $1,
            transcription_seconds = transcription_seconds + $2,
            translation_seconds = translation_seconds + $3,
            balance_after = $4,
            updated_at = $5
        WHERE id = $6
    `)).
		WithArgs(additionalSeconds, additionalTranscription, additionalTranslation, newBalanceAfter, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateConsumption(context.Background(), id, additionalSeconds, additionalTranscription, additionalTranslation, newBalanceAfter)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLedgerRepository_UpdateConsumptionWithMeta(t *testing.T) {
	appDB, mock, cleanup := newUsageLedgerTestDB(t)
	defer cleanup()

	repo := NewUsageLedgerRepository(appDB)
	id := int64(9)
	additionalSeconds := -180
	additionalTranscription := 120
	additionalTranslation := 60
	newBalanceAfter := 520
	originProductType := "SPECIAL_OFFER"

	mock.ExpectExec(regexp.QuoteMeta(`
        UPDATE usage_ledger
        SET seconds_consumed = seconds_consumed + $1,
            transcription_seconds = transcription_seconds + $2,
            translation_seconds = translation_seconds + $3,
            balance_after = $4,
			origin_product_type = $5,
			updated_at = $6
		WHERE id = $7
    `)).
		WithArgs(additionalSeconds, additionalTranscription, additionalTranslation, newBalanceAfter, originProductType, sqlmock.AnyArg(), id).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.UpdateConsumptionWithMeta(context.Background(), id, additionalSeconds, additionalTranscription, additionalTranslation, newBalanceAfter, &originProductType)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLedgerRepository_GetAggregatedStats(t *testing.T) {
	appDB, mock, cleanup := newUsageLedgerTestDB(t)
	defer cleanup()

	repo := NewUsageLedgerRepository(appDB)
	userID := 1
	startTime := int64(1234560000)
	endTime := int64(1234569999)

	rows := sqlmock.NewRows([]string{
		"total_seconds", "transcription_seconds", "translation_seconds",
	}).AddRow(int64(-7200), float64(-6000), float64(-1200))

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT 
            SUM(seconds_consumed) as total_seconds,
            SUM(transcription_seconds) as transcription_seconds,
            SUM(translation_seconds) as translation_seconds
        FROM usage_ledger
        WHERE user_id = $1 AND created_at >= $2 AND created_at <= $3
    `)).WithArgs(userID, startTime, endTime).WillReturnRows(rows)

	result, err := repo.GetAggregatedStats(context.Background(), userID, startTime, endTime)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, int64(-7200), result.TotalSecondsConsumed)
	require.Equal(t, int64(-6000), result.TranscriptionSeconds)
	require.Equal(t, int64(-1200), result.TranslationSeconds)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLedgerRepository_GetOriginProductStats(t *testing.T) {
	appDB, mock, cleanup := newUsageLedgerTestDB(t)
	defer cleanup()

	repo := NewUsageLedgerRepository(appDB)
	userID := 1
	startTime := int64(1234560000)
	endTime := int64(1234569999)

	rows := sqlmock.NewRows([]string{"hour_pack_seconds", "special_offer_seconds"}).AddRow(int64(-180), int64(-120))

	mock.ExpectQuery(regexp.QuoteMeta(`
        SELECT
            COALESCE(SUM(CASE WHEN origin_product_type = 'HOUR_PACK' THEN seconds_consumed ELSE 0 END), 0) AS hour_pack_seconds,
            COALESCE(SUM(CASE WHEN origin_product_type = 'SPECIAL_OFFER' THEN seconds_consumed ELSE 0 END), 0) AS special_offer_seconds
        FROM usage_ledger
        WHERE user_id = $1 AND created_at >= $2 AND created_at <= $3
    `)).WithArgs(userID, startTime, endTime).WillReturnRows(rows)

	result, err := repo.GetOriginProductStats(context.Background(), userID, startTime, endTime)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, int64(-180), result.HourPackSeconds)
	require.Equal(t, int64(-120), result.SpecialOfferSeconds)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLedgerRepository_BatchCreate(t *testing.T) {
	appDB, mock, cleanup := newUsageLedgerTestDB(t)
	defer cleanup()

	repo := NewUsageLedgerRepository(appDB)
	cycleID1 := int64(100)
	cycleID2 := int64(101)
	entries := []*model.UsageLedger{
		{UserID: 1, BusinessID: 10, CycleID: &cycleID1, Seconds: -60, BalanceBefore: 500, BalanceAfter: 440, Source: "a", TranscriptionSeconds: 60, TranslationSeconds: 0},
		{UserID: 1, BusinessID: 11, CycleID: &cycleID2, Seconds: -30, BalanceBefore: 440, BalanceAfter: 410, Source: "b", TranscriptionSeconds: 0, TranslationSeconds: 30},
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`
        INSERT INTO usage_ledger (
			user_id, business_id, cycle_id, origin_product_type, seconds_consumed,
			balance_before, balance_after, source,
			transcription_seconds, translation_seconds,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
        RETURNING id
    `)).
		WithArgs(entries[0].UserID, entries[0].BusinessID, entries[0].CycleID, nil, entries[0].Seconds, entries[0].BalanceBefore, entries[0].BalanceAfter, entries[0].Source, entries[0].TranscriptionSeconds, entries[0].TranslationSeconds, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectQuery(regexp.QuoteMeta(`
        INSERT INTO usage_ledger (
			user_id, business_id, cycle_id, origin_product_type, seconds_consumed,
			balance_before, balance_after, source,
			transcription_seconds, translation_seconds,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
        RETURNING id
    `)).
		WithArgs(entries[1].UserID, entries[1].BusinessID, entries[1].CycleID, nil, entries[1].Seconds, entries[1].BalanceBefore, entries[1].BalanceAfter, entries[1].Source, entries[1].TranscriptionSeconds, entries[1].TranslationSeconds, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(2)))
	mock.ExpectCommit()

	err := repo.BatchCreate(context.Background(), entries)
	require.NoError(t, err)
	require.Equal(t, int64(1), entries[0].ID)
	require.Equal(t, int64(2), entries[1].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLedgerRepository_BatchCreate_Empty(t *testing.T) {
	appDB, _, cleanup := newUsageLedgerTestDB(t)
	defer cleanup()

	repo := NewUsageLedgerRepository(appDB)
	err := repo.BatchCreate(context.Background(), []*model.UsageLedger{})
	require.NoError(t, err)
}

func TestUsageLedgerRepository_FindByBusinessAndCycle(t *testing.T) {
	appDB, mock, cleanup := newUsageLedgerTestDB(t)
	defer cleanup()

	repo := NewUsageLedgerRepository(appDB)
	businessID := 10
	cycleID := int64(100)
	userID := 1

	entrySeconds := -60
	balanceBefore := 500
	balanceAfter := 440
	source := "a"
	transcriptionSeconds := 60
	translationSeconds := 0

	rows := sqlmock.NewRows([]string{"id", "user_id", "business_id", "cycle_id", "origin_product_type", "seconds_consumed",
		"balance_before", "balance_after", "source",
		"transcription_seconds", "translation_seconds", "created_at"}).
		AddRow(int64(1), userID, businessID, cycleID, nil, entrySeconds,
			balanceBefore, balanceAfter, source,
			transcriptionSeconds, translationSeconds, 1234567890)

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, business_id, cycle_id, origin_product_type, seconds_consumed,
			balance_before, balance_after, source,
			transcription_seconds, translation_seconds, created_at
		FROM usage_ledger
		WHERE business_id = $1 AND cycle_id = $2
		LIMIT 1
	`)).WithArgs(businessID, cycleID).WillReturnRows(rows)

	result, err := repo.FindByBusinessAndCycle(context.Background(), businessID, &cycleID)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, businessID, result.BusinessID)
	require.Equal(t, cycleID, *result.CycleID)
	require.NoError(t, mock.ExpectationsWereMet())
}
