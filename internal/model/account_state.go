package model

// AccountRole 定义账本角色类型，保持与业务角色一致。
type AccountRole string

const (
	AccountRoleFree         AccountRole = "FREE"          // 免费用户
	AccountRolePayg         AccountRole = "PAYG"          // 按需计费用户
	AccountRoleSpecialOffer AccountRole = "SPECIAL_OFFER" // Special Offer 用户
	AccountRolePremium      AccountRole = "PREMIUM"       // 订阅用户
	AccountRolePro          AccountRole = "PRO"           // 专业版用户
	AccountRoleProMini      AccountRole = "PRO_MINI"      // 专业版迷你用户
)

const (
	HourPackDurationSeconds     = 18000   // 5 小时
	SpecialOfferDurationSeconds = 36000   // 10 小时
	HourPackValidityDays        = 30      // 小时包有效期
	PremiumCycleDurationSeconds = 72000   // Premium 单周期秒数 (20 小时)
	PremiumCycleDays            = 30      // Premium 单周期天数
	ProAnnualDurationSeconds    = 2160000 // Pro 年度总秒数 (600 小时)
	ProValidityDays             = 365     // Pro 年度有效期
)

// UserAccountState 表示用户在新账本体系中的整体余额快照。
type UserAccountState struct {
	UserID int `db:"user_id" json:"userId"`

	Role        AccountRole `db:"role" json:"role"`
	HasEverPaid bool        `db:"has_ever_paid" json:"hasEverPaid"`

	FreeCycleStart *int64 `db:"free_cycle_start" json:"freeCycleStart,omitempty"`
	FreeCycleEnd   *int64 `db:"free_cycle_end" json:"freeCycleEnd,omitempty"`
	FreeTotal      int    `db:"free_total_seconds" json:"freeTotalSeconds"`
	FreeUsed       int    `db:"free_used_seconds" json:"freeUsedSeconds"`

	PaygTotal int `db:"payg_total_seconds" json:"paygTotalSeconds"`
	PaygUsed  int `db:"payg_used_seconds" json:"paygUsedSeconds"`

	SpecialOfferTotal int `db:"special_offer_total_seconds" json:"specialOfferTotalSeconds"`
	SpecialOfferUsed  int `db:"special_offer_used_seconds" json:"specialOfferUsedSeconds"`

	PremiumCycleIndex int    `db:"premium_cycle_index" json:"premiumCycleIndex"`
	PremiumCycleStart *int64 `db:"premium_cycle_start" json:"premiumCycleStart,omitempty"`
	PremiumCycleEnd   *int64 `db:"premium_cycle_end" json:"premiumCycleEnd,omitempty"`
	PremiumTotal      int    `db:"premium_total_seconds" json:"premiumTotalSeconds"`
	PremiumUsed       int    `db:"premium_used_seconds" json:"premiumUsedSeconds"`
	PremiumBacklog    int    `db:"premium_backlog_seconds" json:"premiumBacklogSeconds"`

	ProExpireAt *int64 `db:"pro_expire_at" json:"proExpireAt,omitempty"`
	ProTotal    int    `db:"pro_total_seconds" json:"proTotalSeconds"`
	ProUsed     int    `db:"pro_used_seconds" json:"proUsedSeconds"`

	ProMiniTotal int `db:"pro_mini_total_seconds" json:"proMiniTotalSeconds"`
	ProMiniUsed  int `db:"pro_mini_used_seconds" json:"proMiniUsedSeconds"`

	SummaryRemaining int `db:"summary_balance" json:"summaryRemaining"`

	UpdatedAt int64 `db:"updated_at" json:"updatedAt"`
}

// UsageEvent 记录实际使用（扣费）的原子事件，用于审计与回滚。
type UsageEvent struct {
	ID        int64  `db:"id" json:"id"`
	UserID    int    `db:"user_id" json:"userId"`
	Source    string `db:"source" json:"source"`
	Seconds   int    `db:"seconds" json:"seconds"`
	ContextID string `db:"context_id" json:"contextId"`
	CreatedAt int64  `db:"created_at" json:"createdAt"`
}
