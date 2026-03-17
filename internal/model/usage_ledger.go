package model

// UsageLedger 记录每次扣费或调整明细，便于对账与审计。
type UsageLedger struct {
	ID                int64   `db:"id" json:"id"`
	UserID            int     `db:"user_id" json:"userId"`             // 用户 ID
	BusinessID        int     `db:"business_id" json:"businessId"`     // 业务实体ID（会议ID、任务ID等）
	CycleID           *int64  `db:"cycle_id" json:"cycleId,omitempty"` // 关联的周期 ID
	OriginProductType *string `db:"origin_product_type" json:"originProductType,omitempty"`
	Seconds           int     `db:"seconds_consumed" json:"secondsConsumed"` // 扣费秒数
	BalanceBefore     int     `db:"balance_before" json:"balanceBefore"`     // 扣前余额
	BalanceAfter      int     `db:"balance_after" json:"balanceAfter"`       // 扣后余额
	Source            string  `db:"source" json:"source"`                    // 来源（usage_report/manual_adjust 等）

	// 结构化存储本次扣费的拆分（新增字段）
	TranscriptionSeconds int `db:"transcription_seconds" json:"transcriptionSeconds"` // 本次扣费中的转录秒数
	TranslationSeconds   int `db:"translation_seconds" json:"translationSeconds"`     // 本次扣费中的翻译秒数

	CreatedAt int64 `db:"created_at" json:"createdAt"` // 创建时间
}

// UsageStats 聚合统计数据
type UsageStats struct {
	TotalSecondsConsumed int64 `json:"totalSecondsConsumed"`
	TranscriptionSeconds int64 `json:"transcriptionSeconds"`
	TranslationSeconds   int64 `json:"translationSeconds"`
}

type UsageOriginProductStats struct {
	HourPackSeconds     int64 `json:"hourPackSeconds"`
	SpecialOfferSeconds int64 `json:"specialOfferSeconds"`
}

// BusinessID 特殊值定义
const (
	// BusinessIDManualAdjustment 手动调整余额
	BusinessIDManualAdjustment = -1

	// BusinessIDSystemAdjustment 系统调整
	BusinessIDSystemAdjustment = -2

	// BusinessIDRefund 退款调整
	BusinessIDRefund = -3

	// BusinessIDPromotion 促销赠送
	BusinessIDPromotion = -4

	// BusinessIDSummary 摘要次数扣减审计
	BusinessIDSummary = -5
)
