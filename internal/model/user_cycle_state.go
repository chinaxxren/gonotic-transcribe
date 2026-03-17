package model

import "time"

// CycleState 表示权益周期的生命周期状态。
type CycleState string

const (
	CycleStatePending   CycleState = "PENDING"   // 待激活
	CycleStateActive    CycleState = "ACTIVE"    // 激活中
	CycleStatePaused    CycleState = "PAUSED"    // 暂停中
	CycleStateCompleted CycleState = "COMPLETED" // 已完成（自然耗尽）
	CycleStateCancelled CycleState = "CANCELLED" // 已取消
	CycleStateExpired   CycleState = "EXPIRED"   // 已过期
	CycleStateRefunded  CycleState = "REFUNDED"  // 已退款
)

// CycleOriginType 标记周期的来源类型。
type CycleOriginType string

const (
	CycleOriginSubscription CycleOriginType = "SUBSCRIPTION"
	CycleOriginTransaction  CycleOriginType = "TRANSACTION"
	CycleOriginSystem       CycleOriginType = "SYSTEM"
)

// UserCycleState 记录单个 Free/Premium/Pro/PAYG 周期。
type UserCycleState struct {
	ID         int64           `db:"id" json:"id"`
	UserID     int             `db:"user_id" json:"userId"`
	Tier       AccountRole     `db:"tier" json:"tier"`
	State      CycleState      `db:"state" json:"state"`
	OriginType CycleOriginType `db:"origin_type" json:"originType"`
	OriginID   *int64          `db:"origin_id" json:"originId,omitempty"`
	CycleNo    int             `db:"cycle_no" json:"cycleNo"`

	PeriodStart  *int64 `db:"period_start" json:"periodStart,omitempty"`
	PeriodEnd    *int64 `db:"period_end" json:"periodEnd,omitempty"`
	TotalSeconds int    `db:"total_seconds" json:"totalSeconds"`
	UsedSeconds  int    `db:"used_seconds" json:"usedSeconds"`
	SummaryTotal int    `db:"summary_total" json:"summaryTotal"`
	SummaryUsed  int    `db:"summary_used" json:"summaryUsed"`

	CreatedAt int64 `db:"created_at" json:"createdAt"`
	UpdatedAt int64 `db:"updated_at" json:"updatedAt"`
}

// NewUserCycleState helper to create user cycle state with timestamps.
func NewUserCycleState(userID int, tier AccountRole, originType CycleOriginType, originID *int64, cycleNo int, periodStart, periodEnd *int64, totalSeconds int) *UserCycleState {
	now := time.Now().Unix()
	return &UserCycleState{
		UserID:       userID,
		Tier:         tier,
		State:        CycleStatePending,
		OriginType:   originType,
		OriginID:     originID,
		CycleNo:      cycleNo,
		PeriodStart:  periodStart,
		PeriodEnd:    periodEnd,
		TotalSeconds: totalSeconds,
		UsedSeconds:  0,
		SummaryTotal: 0,
		SummaryUsed:  0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}
