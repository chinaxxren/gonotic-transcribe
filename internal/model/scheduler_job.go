package model

// SchedulerJobStatus 表示调度任务总体执行状态。
type SchedulerJobStatus string

const (
	SchedulerJobStatusPending SchedulerJobStatus = "PENDING" // 待执行
	SchedulerJobStatusRunning SchedulerJobStatus = "RUNNING" // 执行中
	SchedulerJobStatusSuccess SchedulerJobStatus = "SUCCESS" // 成功
	SchedulerJobStatusFailed  SchedulerJobStatus = "FAILED"  // 失败
)

// SchedulerJob 记录一次批量任务的执行元数据。
type SchedulerJob struct {
	ID         int64              `db:"id" json:"id"`                                // ID
	JobType    string             `db:"job_type" json:"jobType"`                     // 任务类型（如 grant_subscription_monthly）
	Status     SchedulerJobStatus `db:"status" json:"status"`                        // 状态
	StartedAt  *int64             `db:"started_at" json:"startedAt,omitempty"`       // 开始时间
	FinishedAt *int64             `db:"finished_at" json:"finishedAt,omitempty"`     // 结束时间
	Attempts   int                `db:"attempts" json:"attempts"`                    // 尝试次数
	ErrorMsg   *string            `db:"error_message" json:"errorMessage,omitempty"` // 错误信息
	CreatedAt  int64              `db:"created_at" json:"createdAt"`                 // 创建时间
	UpdatedAt  int64              `db:"updated_at" json:"updatedAt"`                 // 更新时间
}

// SchedulerJobItemStatus 表示单条任务明细的状态。
type SchedulerJobItemStatus string

const (
	SchedulerJobItemPending    SchedulerJobItemStatus = "PENDING"    // 待处理
	SchedulerJobItemProcessing SchedulerJobItemStatus = "PROCESSING" // 执行中
	SchedulerJobItemSuccess    SchedulerJobItemStatus = "SUCCESS"    // 完成
	SchedulerJobItemFailed     SchedulerJobItemStatus = "FAILED"     // 失败
	SchedulerJobItemSkipped    SchedulerJobItemStatus = "SKIPPED"    // 跳过
)

// SchedulerJobItem 记录具体用户或订阅的发放任务。
type SchedulerJobItem struct {
	ID         int64                  `db:"id" json:"id"`                                // ID
	JobID      int64                  `db:"job_id" json:"jobId"`                         // 批量任务 ID
	ItemKey    string                 `db:"item_key" json:"itemKey"`                     // 如 user:123 / sub:456
	Status     SchedulerJobItemStatus `db:"status" json:"status"`                        // 状态
	RetryCount int                    `db:"retry_count" json:"retryCount"`               // 尝试次数
	StartedAt  *int64                 `db:"started_at" json:"startedAt,omitempty"`       // 开始时间
	FinishedAt *int64                 `db:"finished_at" json:"finishedAt,omitempty"`     // 结束时间
	ErrorMsg   *string                `db:"error_message" json:"errorMessage,omitempty"` // 错误信息
	CreatedAt  int64                  `db:"created_at" json:"createdAt"`                 // 创建时间
	UpdatedAt  int64                  `db:"updated_at" json:"updatedAt"`                 // 更新时间
}
