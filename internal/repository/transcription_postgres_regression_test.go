package repository

import (
	"context"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
)

func TestBatchSaveTranscriptionEnqueuesClonedRecords(t *testing.T) {
	repo := &postgresTranscriptionRepository{
		logger:     zap.NewNop(),
		asyncQueue: make(chan []*TranscriptionRecord, 1),
	}

	record := &TranscriptionRecord{
		Text:      "first",
		MeetingID: 9,
	}

	if err := repo.BatchSaveTranscription(context.Background(), []*TranscriptionRecord{record}); err != nil {
		t.Fatalf("expected enqueue to succeed, got %v", err)
	}

	record.Text = "mutated"

	batch := <-repo.asyncQueue
	if len(batch) != 1 {
		t.Fatalf("expected one queued record, got %d", len(batch))
	}
	if batch[0].Text != "first" {
		t.Fatalf("expected queued record to be cloned, got %q", batch[0].Text)
	}
}

func TestBatchSaveTranscriptionReturnsQueueFull(t *testing.T) {
	repo := &postgresTranscriptionRepository{
		logger:     zap.NewNop(),
		asyncQueue: make(chan []*TranscriptionRecord, 1),
	}

	repo.asyncQueue <- []*TranscriptionRecord{{Text: "existing"}}

	err := repo.BatchSaveTranscription(context.Background(), []*TranscriptionRecord{{Text: "next"}})
	if err != ErrTranscriptionAsyncQueueFull {
		t.Fatalf("expected ErrTranscriptionAsyncQueueFull, got %v", err)
	}
}

func TestGetTranscriptionReturnsEmptyJSONArrayWhenNoRows(t *testing.T) {
	mdb, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer mdb.Close()

	repo := &postgresTranscriptionRepository{
		db:     mdb,
		logger: zap.NewNop(),
		recordPool: sync.Pool{
			New: func() interface{} {
				return &TranscriptionRecord{}
			},
		},
	}

	rows := sqlmock.NewRows([]string{"text", "speaker", "timestamp", "language", "translation_status", "uid", "meeting_id"})
	mock.ExpectQuery("SELECT text, speaker, timestamp, language, translation_status, uid, meeting_id").
		WithArgs(99).
		WillReturnRows(rows)

	data, err := repo.GetTranscription(context.Background(), 99)
	if err != nil {
		t.Fatalf("expected empty transcription query to succeed, got %v", err)
	}
	if string(data) != "[]" {
		t.Fatalf("expected empty json array, got %q", string(data))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
