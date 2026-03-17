package service

import "github.com/chinaxxren/gonotic/internal/model"

// BalanceConsumeDetail 描述一次扣减操作的来源构成。
type BalanceConsumeDetail struct {
	FromPro          int // 来自 Pro 的秒数
	FromProMini      int // 来自 Pro Mini 的秒数
	FromPremium      int // 来自 Premium 周期的秒数
	FromPayg         int // 来自 Payg 明细的秒数
	FromSpecialOffer int // 来自 Special Offer 的秒数
	FromFree         int // 来自 Free 周期的秒数
	Deductions       []CycleDeduction
}

// AccountCycleAggregate 聚合周期状态，用于内部统计。
type AccountCycleAggregate struct {
	UserID              int
	Role                model.AccountRole
	HasEverPaid         bool
	UpdatedAt           int64
	SummaryRemaining    int
	SummaryTotal        int
	SummaryUsed         int
	Free                *CycleQuota
	Payg                *CycleQuota
	SpecialOffer        *CycleQuota
	Premium             *PremiumCycleInfo
	Pro                 *ProCycleInfo
	ProMini             *ProMiniCycleInfo
	PaygEntries         []PaygCycleEntry
	SpecialOfferEntries []PaygCycleEntry
}

// AccountCycleSnapshot 汇总周期状态，用于 Facade 聚合及外部消费。
type AccountCycleSnapshot struct {
	UserID              int
	Role                model.AccountRole
	HasEverPaid         bool
	UpdatedAt           int64
	SummaryRemaining    int
	SummaryTotal        int
	SummaryUsed         int
	Free                *CycleQuota
	Payg                *CycleQuota
	SpecialOffer        *CycleQuota
	Premium             *PremiumCycleInfo
	Pro                 *ProCycleInfo
	ProMini             *ProMiniCycleInfo
	PaygEntries         []PaygCycleEntry
	SpecialOfferEntries []PaygCycleEntry
}

// cycleBuckets 将用户活跃周期按层级分组，便于聚合统计。
type cycleBuckets struct {
	Free         []*model.UserCycleState
	Payg         []*model.UserCycleState
	SpecialOffer []*model.UserCycleState
	Premium      []*model.UserCycleState
	Pro          []*model.UserCycleState
	ProMini      []*model.UserCycleState
}

// CycleQuota 描述单个周期或聚合周期的额度信息。
type CycleQuota struct {
	Total int
	Used  int
	Start *int64
	End   *int64
}

// PremiumCycleInfo 在额度信息基础上增加周期索引和 backlog。
type PremiumCycleInfo struct {
	Quota          CycleQuota
	CycleIndex     int
	BacklogSeconds int
}

// ProCycleInfo 则附带订阅的过期时间。
type ProCycleInfo struct {
	Quota    CycleQuota
	ExpireAt *int64
}

// ProMiniCycleInfo 则附带 Pro Mini 订阅的过期时间。
type ProMiniCycleInfo struct {
	Quota    CycleQuota
	ExpireAt *int64
}

// PaygCycleEntry 用于兼容旧的 Payg 明细展示。
type PaygCycleEntry struct {
	ID          int64
	GrantedAt   int64
	ExpiresAt   int64
	OriginTxnID *int64
	Total       int
	Used        int
}
