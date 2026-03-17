package model

// NotificationState 表示通知任务的处理状态。
type NotificationState string

const (
	NotificationStatePending    NotificationState = "PENDING"       // 待处理
	NotificationStateProcessing NotificationState = "PROCESSING"    // 处理中
	NotificationStateDone       NotificationState = "DONE"          // 已完成
	NotificationStateRetry      NotificationState = "RETRY_PENDING" // 等待重试
	NotificationStateDead       NotificationState = "DEAD"          // 死信
)

// NotificationInbox 对应 notification_inbox 表，记录 Apple/其他 Provider 的回调事件。
type NotificationInbox struct {
	ID                    int64             `db:"id" json:"id"`                                         // ID
	Provider              string            `db:"provider" json:"provider"`                             // 提供商
	OriginalTransactionID string            `db:"original_transaction_id" json:"originalTransactionId"` // 原始交易 ID
	EventType             string            `db:"event_type" json:"eventType"`                          // 事件类型
	Sequence              int64             `db:"sequence" json:"sequence"`                             // 同一 original_txn_id 内的序号
	Payload               []byte            `db:"payload" json:"payload"`                               // 事件内容
	State                 NotificationState `db:"state" json:"state"`                                   // 状态
	RetryCount            int               `db:"retry_count" json:"retryCount"`                        // 重试次数
	AvailableAt           int64             `db:"available_at" json:"availableAt"`                      // 可用时间
	ErrorMessage          *string           `db:"error_message" json:"errorMessage,omitempty"`          // 错误信息
	CreatedAt             int64             `db:"created_at" json:"createdAt"`                          // 创建时间
	UpdatedAt             int64             `db:"updated_at" json:"updatedAt"`                          // 更新时间
}
