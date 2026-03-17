package model

// SummaryLedger records each summary quota consumption event.
type SummaryLedger struct {
	ID           int64  `db:"id" json:"id"`
	UserID       int    `db:"user_id" json:"userId"`
	BusinessID   int    `db:"business_id" json:"businessId"`
	CycleID      int64  `db:"cycle_id" json:"cycleId"`
	TemplateID   *int64 `db:"template_id" json:"templateId,omitempty"`
	SummaryDelta int    `db:"summary_delta" json:"summaryDelta"`
	SummaryKey   string `db:"summary_key" json:"summaryKey"`
	Source       string `db:"source" json:"source"`
	CreatedAt    int64  `db:"created_at" json:"createdAt"`
}
