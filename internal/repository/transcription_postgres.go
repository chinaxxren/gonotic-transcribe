package repository

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	json "github.com/bytedance/sonic"
	_ "github.com/lib/pq"
	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/pkg/errors"
	pkgerrors "github.com/chinaxxren/gonotic/internal/pkg/errors"
)

var ErrTranscriptionAsyncQueueFull = stderrors.New("transcription async queue full")

// truncateString 截断字符串到指定长度
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// TranscriptionRecord 转录记录结构
type TranscriptionRecord struct {
	Text              string `json:"text"`                         // 转录文本
	Speaker           string `json:"speaker,omitempty"`            // 说话人标识
	Timestamp         int64  `json:"timestamp"`                    // Unix 时间戳
	Language          string `json:"language,omitempty"`           // 语言
	TranslationStatus string `json:"translation_status,omitempty"` // 翻译状态
	UID               int    `json:"uid"`                          // 用户 ID
	MeetingID         int    `json:"meeting_id"`                   // 会议 ID
}

func (r *TranscriptionRecord) reset() {
	r.Text = ""
	r.Speaker = ""
	r.Timestamp = 0
	r.Language = ""
	r.TranslationStatus = ""
	r.UID = 0
	r.MeetingID = 0
}

// TranscriptionRepository 定义转录内容持久化接口
type TranscriptionRepository interface {
	PutTranscription(ctx context.Context, meetingID int, content []byte) error
	BatchSaveTranscription(ctx context.Context, records []*TranscriptionRecord) error
	GetTranscription(ctx context.Context, meetingID int) ([]byte, error)
	CountTranscription(ctx context.Context, meetingID int) (int, error)
	StreamTranscriptionJSONL(ctx context.Context, meetingID int, w io.Writer) error
	DeleteTranscription(ctx context.Context, meetingID int) error
	HealthCheck(ctx context.Context) error
}

// PostgresTranscriptionConfig PostgreSQL 连接配置
type PostgresTranscriptionConfig struct {
	DSN          string
	MaxOpenConns int           // 最大打开连接数（默认 100）
	MaxIdleConns int           // 最大空闲连接数（默认 50）
	ConnLifetime time.Duration // 连接最大生命周期（默认 30 分钟）
}

type postgresTranscriptionRepository struct {
	db     *sql.DB
	logger *zap.Logger

	recordPool sync.Pool

	asyncQueue chan []*TranscriptionRecord
	flushEvery time.Duration
	batchSize  int
	workers    int
}

// NewPostgresTranscriptionRepository 创建 PostgreSQL 版本的转录仓储
func NewPostgresTranscriptionRepository(cfg PostgresTranscriptionConfig, logger *zap.Logger) (TranscriptionRepository, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("transcription postgres dsn is required")
	}

	db, err := sql.Open("postgres", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("connect postgres failed: %w", err)
	}

	// P1 优化: 配置连接池，支持 1000+ 并发
	maxOpenConns := cfg.MaxOpenConns
	if maxOpenConns <= 0 {
		maxOpenConns = 100 // 默认 100 连接
	}
	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = 50 // 默认 50 空闲连接
	}
	connLifetime := cfg.ConnLifetime
	if connLifetime <= 0 {
		connLifetime = 30 * time.Minute // 默认 30 分钟
	}

	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connLifetime)
	db.SetConnMaxIdleTime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres failed: %w", err)
	}

	logger.Info("✅ PostgreSQL transcription repository initialized",
		zap.Int("max_open_conns", maxOpenConns),
		zap.Int("max_idle_conns", maxIdleConns),
		zap.Duration("conn_lifetime", connLifetime))

	repo := &postgresTranscriptionRepository{
		db:     db,
		logger: logger,
		recordPool: sync.Pool{
			New: func() interface{} {
				return &TranscriptionRecord{}
			},
		},
		asyncQueue: make(chan []*TranscriptionRecord, 2048),
		flushEvery: 200 * time.Millisecond,
		batchSize:  512,
		workers:    2,
	}

	repo.startAsyncWorkers()

	return repo, nil
}

func (r *postgresTranscriptionRepository) startAsyncWorkers() {
	for i := 0; i < r.workers; i++ {
		go r.runAsyncWorker()
	}
}

func (r *postgresTranscriptionRepository) runAsyncWorker() {
	ticker := time.NewTicker(r.flushEvery)
	defer ticker.Stop()

	batch := make([]*TranscriptionRecord, 0, r.batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := r.batchSaveTranscriptionDirect(context.Background(), batch); err != nil {
			r.logger.Error("[TRANSCRIPTION_INSERT] 异步批量插入失败",
				zap.Error(err),
				zap.Int("record_count", len(batch)))
		}
		batch = batch[:0]
	}

	for {
		select {
		case records, ok := <-r.asyncQueue:
			if !ok {
				flush()
				return
			}
			if len(records) == 0 {
				continue
			}
			if cap(batch)-len(batch) < len(records) {
				flush()
			}
			batch = append(batch, records...)
			if len(batch) >= r.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (r *postgresTranscriptionRepository) BatchSaveTranscription(ctx context.Context, records []*TranscriptionRecord) error {
	return r.enqueueRecords(records)
}

func cloneTranscriptionRecords(records []*TranscriptionRecord) []*TranscriptionRecord {
	if len(records) == 0 {
		return nil
	}

	cloned := make([]*TranscriptionRecord, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		recordCopy := *record
		cloned = append(cloned, &recordCopy)
	}
	return cloned
}

func (r *postgresTranscriptionRepository) enqueueRecords(records []*TranscriptionRecord) error {
	records = cloneTranscriptionRecords(records)
	if len(records) == 0 {
		return nil
	}

	select {
	case r.asyncQueue <- records:
		return nil
	default:
		r.logger.Warn("[TRANSCRIPTION_INSERT] 异步队列已满，拒绝本次转录写入",
			zap.Int("record_count", len(records)),
			zap.Int("queue_capacity", cap(r.asyncQueue)))
		return ErrTranscriptionAsyncQueueFull
	}
}

func (r *postgresTranscriptionRepository) batchSaveTranscriptionDirect(ctx context.Context, records []*TranscriptionRecord) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx failed: %w", err)
	}
	defer tx.Rollback()

	// P3 优化: 使用单条 SQL 批量插入，减少 RTT
	now := time.Now().Unix()
	var queryBuilder strings.Builder
	queryBuilder.WriteString("INSERT INTO meeting_record (meeting_id, timestamp, text, speaker, language, translation_status, uid, created_at, updated_at) VALUES ")

	vals := make([]interface{}, 0, len(records)*9)
	for i, record := range records {
		if i > 0 {
			queryBuilder.WriteString(",")
		}
		// 计算参数占位符索引
		n := i * 9
		queryBuilder.WriteString("($")
		queryBuilder.WriteString(strconv.Itoa(n + 1))
		queryBuilder.WriteString(", $")
		queryBuilder.WriteString(strconv.Itoa(n + 2))
		queryBuilder.WriteString(", $")
		queryBuilder.WriteString(strconv.Itoa(n + 3))
		queryBuilder.WriteString(", $")
		queryBuilder.WriteString(strconv.Itoa(n + 4))
		queryBuilder.WriteString(", $")
		queryBuilder.WriteString(strconv.Itoa(n + 5))
		queryBuilder.WriteString(", $")
		queryBuilder.WriteString(strconv.Itoa(n + 6))
		queryBuilder.WriteString(", $")
		queryBuilder.WriteString(strconv.Itoa(n + 7))
		queryBuilder.WriteString(", $")
		queryBuilder.WriteString(strconv.Itoa(n + 8))
		queryBuilder.WriteString(", $")
		queryBuilder.WriteString(strconv.Itoa(n + 9))
		queryBuilder.WriteString(")")

		vals = append(vals,
			record.MeetingID,
			record.Timestamp,
			record.Text,
			record.Speaker,
			record.Language,
			record.TranslationStatus,
			record.UID,
			now,
			now,
		)
	}

	if _, err := tx.ExecContext(ctx, queryBuilder.String(), vals...); err != nil {
		return pkgerrors.Wrap(pkgerrors.ErrStorageError, "保存转录内容失败", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx failed: %w", err)
	}
	return nil
}

func (r *postgresTranscriptionRepository) PutTranscription(ctx context.Context, meetingID int, content []byte) error {
	records, err := parseTranscriptionPayload(content, meetingID)
	if err != nil {
		r.logger.Error("解析转录内容失败",
			zap.Error(err),
			zap.Int("meeting_id", meetingID),
			zap.Int("payload_size", len(content)))
		return pkgerrors.Wrap(pkgerrors.ErrStorageError, "解析转录内容失败", err)
	}

	if len(records) == 0 {
		r.logger.Info("没有新的转录记录需要保存",
			zap.Int("meeting_id", meetingID))
		return nil
	}

	records = normalizeTranscriptionRecords(records, meetingID)
	return r.enqueueRecords(records)
}

func (r *postgresTranscriptionRepository) GetTranscription(ctx context.Context, meetingID int) ([]byte, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT text, speaker, timestamp, language, translation_status, uid, meeting_id
        FROM meeting_record
        WHERE meeting_id = $1
        ORDER BY id ASC`, meetingID)
	if err != nil {
		r.logger.Error("[TRANSCRIPTION_QUERY] 查询转录内容失败",
			zap.Error(err),
			zap.Int("meeting_id", meetingID))
		return nil, pkgerrors.Wrap(pkgerrors.ErrStorageError, "查询转录内容失败", err)
	}
	defer rows.Close()

	var buffer bytes.Buffer
	buffer.WriteByte('[')
	recordCount := 0
	for rows.Next() {
		rec := r.recordPool.Get().(*TranscriptionRecord)
		rec.reset()
		if err := rows.Scan(
			&rec.Text,
			&rec.Speaker,
			&rec.Timestamp,
			&rec.Language,
			&rec.TranslationStatus,
			&rec.UID,
			&rec.MeetingID,
		); err != nil {
			r.logger.Error("[TRANSCRIPTION_QUERY] 解析转录记录失败",
				zap.Error(err),
				zap.Int("meeting_id", meetingID))
			r.recordPool.Put(rec)
			return nil, pkgerrors.Wrap(pkgerrors.ErrStorageError, "解析转录记录失败", err)
		}

		line, err := json.Marshal(rec)
		if err != nil {
			r.recordPool.Put(rec)
			r.logger.Error("[TRANSCRIPTION_QUERY] 序列化转录记录失败",
				zap.Error(err),
				zap.Int("meeting_id", meetingID),
				zap.Int("record_count", recordCount))
			return nil, pkgerrors.Wrap(pkgerrors.ErrStorageError, "序列化转录记录失败", err)
		}

		if recordCount > 0 {
			buffer.WriteByte(',')
		}
		buffer.Write(line)
		recordCount++
		r.recordPool.Put(rec)
	}

	if err := rows.Err(); err != nil {
		r.logger.Error("[TRANSCRIPTION_QUERY] 遍历转录记录失败",
			zap.Error(err),
			zap.Int("meeting_id", meetingID))
		return nil, pkgerrors.Wrap(pkgerrors.ErrStorageError, "遍历转录记录失败", err)
	}

	if recordCount == 0 {
		return []byte("[]"), nil
	}
	buffer.WriteByte(']')
	data := buffer.Bytes()

	r.logger.Info("转录记录获取成功",
		zap.Int("meeting_id", meetingID),
		zap.Int("record_count", recordCount),
		zap.Int("json_size", len(data)))

	return data, nil
}

func (r *postgresTranscriptionRepository) CountTranscription(ctx context.Context, meetingID int) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM meeting_record WHERE meeting_id = $1`, meetingID).Scan(&count); err != nil {
		r.logger.Error("[TRANSCRIPTION_QUERY] 统计转录记录数失败",
			zap.Error(err),
			zap.Int("meeting_id", meetingID))
		return 0, pkgerrors.Wrap(pkgerrors.ErrStorageError, "统计转录记录数失败", err)
	}
	return count, nil
}

func (r *postgresTranscriptionRepository) StreamTranscriptionJSONL(ctx context.Context, meetingID int, w io.Writer) error {
	rows, err := r.db.QueryContext(ctx, `
        SELECT text, speaker, timestamp, language, translation_status, uid, meeting_id
        FROM meeting_record
        WHERE meeting_id = $1
        ORDER BY id ASC`, meetingID)
	if err != nil {
		r.logger.Error("[TRANSCRIPTION_STREAM] 查询转录内容失败",
			zap.Error(err),
			zap.Int("meeting_id", meetingID))
		return errors.Wrap(errors.ErrStorageError, "查询转录内容失败", err)
	}
	defer rows.Close()

	writer := bufio.NewWriter(w)
	defer writer.Flush()

	streamedCount := 0
	for rows.Next() {
		rec := r.recordPool.Get().(*TranscriptionRecord)
		rec.reset()
		if err := rows.Scan(
			&rec.Text,
			&rec.Speaker,
			&rec.Timestamp,
			&rec.Language,
			&rec.TranslationStatus,
			&rec.UID,
			&rec.MeetingID,
		); err != nil {
			r.logger.Error("[TRANSCRIPTION_STREAM] 解析转录记录失败",
				zap.Error(err),
				zap.Int("meeting_id", meetingID),
				zap.Int("streamed_count", streamedCount))
			r.recordPool.Put(rec)
			return errors.Wrap(errors.ErrStorageError, "解析转录记录失败", err)
		}

		line, err := json.Marshal(rec)
		if err != nil {
			r.logger.Error("[TRANSCRIPTION_STREAM] 序列化转录记录失败",
				zap.Error(err),
				zap.Int("meeting_id", meetingID),
				zap.Int("streamed_count", streamedCount),
				zap.Int64("timestamp", rec.Timestamp))
			r.recordPool.Put(rec)
			return pkgerrors.Wrap(pkgerrors.ErrStorageError, "序列化转录记录失败", err)
		}

		if _, err := writer.Write(line); err != nil {
			r.logger.Error("[TRANSCRIPTION_STREAM] 写入转录记录失败",
				zap.Error(err),
				zap.Int("meeting_id", meetingID),
				zap.Int("streamed_count", streamedCount),
				zap.Int64("timestamp", rec.Timestamp))
			r.recordPool.Put(rec)
			return err
		}
		if _, err := writer.Write([]byte("\n")); err != nil {
			r.logger.Error("[TRANSCRIPTION_STREAM] 写入换行符失败",
				zap.Error(err),
				zap.Int("meeting_id", meetingID),
				zap.Int("streamed_count", streamedCount))
			r.recordPool.Put(rec)
			return err
		}

		r.recordPool.Put(rec)
		streamedCount++
	}

	r.logger.Info("转录记录流式输出完成",
		zap.Int("meeting_id", meetingID),
		zap.Int("streamed_count", streamedCount))

	if err := rows.Err(); err != nil {
		r.logger.Error("[TRANSCRIPTION_STREAM] 流式读取过程中出现错误",
			zap.Error(err),
			zap.Int("meeting_id", meetingID),
			zap.Int("streamed_count", streamedCount))
		return err
	}
	return nil
}

func (r *postgresTranscriptionRepository) DeleteTranscription(ctx context.Context, meetingID int) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM meeting_record WHERE meeting_id = $1`, meetingID); err != nil {
		r.logger.Error("删除转录内容失败",
			zap.Error(err),
			zap.Int("meeting_id", meetingID))
		return pkgerrors.Wrap(pkgerrors.ErrStorageError, "删除转录内容失败", err)
	}

	r.logger.Info("转录内容删除成功",
		zap.Int("meeting_id", meetingID))
	return nil
}

func (r *postgresTranscriptionRepository) HealthCheck(ctx context.Context) error {
	if err := r.db.PingContext(ctx); err != nil {
		return pkgerrors.Wrap(pkgerrors.ErrExternalService, "PostgreSQL 不可用", err)
	}
	return nil
}
