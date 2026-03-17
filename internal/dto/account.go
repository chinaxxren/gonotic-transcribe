package dto

// QuotaBucket 描述一种额度的总量与已用秒数
// totalSeconds/usedSeconds 均以秒为单位
// start/end 字段用于周期性额度，比如免费额度和 Premium
// 对于 Payg/Pro 等非周期性额度，可以忽略时间窗口

// AccountSummary 聚合用户账本视图，供 API 返回使用。
type AccountSummary struct {
	UserID              int                `json:"userId"`
	Role                string             `json:"role"`
	HasEverPaid         bool               `json:"hasEverPaid"`
	UpdatedAt           int64              `json:"updatedAt"`
	SummaryRemaining    int                `json:"summaryRemaining"`
	SummaryTotal        int                `json:"summaryTotal"`
	SummaryUsed         int                `json:"summaryUsed"`
	Free                *QuotaBucket       `json:"free,omitempty"`
	Payg                *QuotaBucket       `json:"payg,omitempty"`
	PaygEntries         []PaygEntrySummary `json:"paygEntries,omitempty"`
	SpecialOffer        *QuotaBucket       `json:"specialOffer,omitempty"`
	SpecialOfferEntries []PaygEntrySummary `json:"specialOfferEntries,omitempty"`
	Premium             *PremiumBucket     `json:"premium,omitempty"`
	Pro                 *ProBucket         `json:"pro,omitempty"`
	ProMini             *ProMiniBucket     `json:"proMini,omitempty"`
}

// QuotaBucket 表示通用额度信息
// TotalSeconds: 总秒数，UsedSeconds: 已使用秒数
// Start/End: 可选周期窗口（Unix 时间戳，秒）
type QuotaBucket struct {
	TotalSeconds int    `json:"totalSeconds"`
	UsedSeconds  int    `json:"usedSeconds"`
	Start        *int64 `json:"start,omitempty"`
	End          *int64 `json:"end,omitempty"`
}

// PremiumBucket 扩展周期信息与 backlog
// CycleIndex: 第几期，BacklogSeconds: 待结转秒数
// Start/End 继承自 QuotaBucket

// ProBucket 包含年度订阅额度与到期时间

type PremiumBucket struct {
	QuotaBucket
	CycleIndex     int `json:"cycleIndex"`
	BacklogSeconds int `json:"backlogSeconds"`
}

type ProBucket struct {
	QuotaBucket
	ExpireAt *int64 `json:"expireAt,omitempty"`
}

type ProMiniBucket struct {
	QuotaBucket
	ExpireAt *int64 `json:"expireAt,omitempty"`
}

// PaygEntrySummary 描述单条 Payg 时长包

type PaygEntrySummary struct {
	EntryID   int64  `json:"entryId"`
	GrantedAt int64  `json:"grantedAt"`
	ExpiresAt int64  `json:"expiresAt"`
	OriginID  *int64 `json:"originTransactionId,omitempty"`
	Total     int    `json:"totalSeconds"`
	Used      int    `json:"usedSeconds"`
}

// AccountUsageStats 表示当前主计划用量统计

type UsageFeatureBreakdown struct {
	Transcription int `json:"transcription"`
	Translation   int `json:"translation"`
}

type AccountUsageStats struct {
	PlanID            string                `json:"planId"`
	Quota             int                   `json:"quotaSeconds"`
	Used              int                   `json:"usedSeconds"`
	Remaining         int                   `json:"remainingSeconds"`
	SummaryRemaining  int                   `json:"summaryRemaining"`
	SummaryTotal      int                   `json:"summaryTotal"`
	SummaryUsed       int                   `json:"summaryUsed"`
	ExpiresAt         *int64                `json:"expiresAt,omitempty"`
	Features          UsageFeatureBreakdown `json:"features"`
	OriginProductUsed map[string]int        `json:"originProductUsedSeconds,omitempty"`
}

// AccountConsumeRequest 描述一次扣费请求

type AccountConsumeRequest struct {
	UserID               int    `json:"userId"`
	Seconds              int    `json:"seconds"`
	Source               string `json:"source"`
	BusinessID           int    `json:"businessId"`
	TranscriptionSeconds int    `json:"transcriptionSeconds"`
	TranslationSeconds   int    `json:"translationSeconds"`
}

// ConsumptionBreakdown 表示扣费拆分来源

type ConsumptionBreakdown struct {
	FromFree         int `json:"fromFree"`
	FromPayg         int `json:"fromPayg"`
	FromSpecialOffer int `json:"fromSpecialOffer"`
	FromPremium      int `json:"fromPremium"`
	FromPro          int `json:"fromPro"`
	FromProMini      int `json:"fromProMini"`
}

// AccountConsumeResult 返回扣费详情与最新摘要

type AccountConsumeResult struct {
	Detail  ConsumptionBreakdown `json:"detail"`
	Summary *AccountSummary      `json:"summary"`
	Warning string               `json:"warning,omitempty"` // 用于标识账本扣费成功但UsageLedger写入失败等情况
}

// RemainingSeconds 返回剩余秒数

func (b *QuotaBucket) RemainingSeconds() int {
	if b == nil {
		return 0
	}
	remaining := b.TotalSeconds - b.UsedSeconds
	if remaining < 0 {
		return 0
	}
	return remaining
}

// TotalRemainingSeconds 统计所有额度剩余秒数合计

func (s *AccountSummary) TotalRemainingSeconds() int {
	if s == nil {
		return 0
	}
	total := 0
	if s.Free != nil {
		total += s.Free.RemainingSeconds()
	}
	if s.Payg != nil {
		total += s.Payg.RemainingSeconds()
	}
	if s.SpecialOffer != nil {
		total += s.SpecialOffer.RemainingSeconds()
	}
	if s.Premium != nil {
		total += s.Premium.RemainingSeconds()
	}
	if s.Pro != nil {
		total += s.Pro.RemainingSeconds()
	}
	if s.ProMini != nil {
		total += s.ProMini.RemainingSeconds()
	}
	return total
}

// RemainingSeconds 实现 PremiumBucket/ProBucket 与 QuotaBucket 一致的剩余额

func (p *PremiumBucket) RemainingSeconds() int {
	if p == nil {
		return 0
	}
	return p.QuotaBucket.RemainingSeconds()
}

func (p *ProBucket) RemainingSeconds() int {
	if p == nil {
		return 0
	}
	return p.QuotaBucket.RemainingSeconds()
}

func (p *ProMiniBucket) RemainingSeconds() int {
	if p == nil {
		return 0
	}
	return p.QuotaBucket.RemainingSeconds()
}
