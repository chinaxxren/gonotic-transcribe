package dto

// PaymentVerifyRequest 表示 /payments/verify 请求体。
type PaymentVerifyRequest struct {
	Provider         string `json:"provider" binding:"required"`         // 支付提供者
	ProductID        string `json:"productId" binding:"required"`        // 产品ID
	TransactionToken string `json:"transactionToken" binding:"required"` // 交易令牌
	DeviceID         string `json:"deviceId"`                            // 设备ID
}

// PaymentVerifyResponse 返回发货后的交易与最新账本快照。
type PaymentVerifyResponse struct {
	Transaction  PaymentTransactionSummary  `json:"transaction"`  // 交易摘要
	AccountState PaymentAccountStateSummary `json:"accountState"` // 最新账本快照
}

// PaymentTransactionSummary 提供基础交易信息。
type PaymentTransactionSummary struct {
	ID            int64  `json:"id"`                    // 交易ID
	ProductID     string `json:"productId"`             // 产品ID
	ProductType   string `json:"productType"`           // 产品类型
	Status        string `json:"status"`                // 交易状态
	ProviderTxnID string `json:"providerTransactionId"` // 提供者交易ID
}

// PaymentAccountStateSummary 将 AccountStateService 返回的账本快照简化给前端使用。
type PaymentAccountStateSummary struct {
	Role              string `json:"role"`                     // 当前角色
	HasEverPaid       bool   `json:"hasEverPaid"`              // 是否曾经付费
	FreeCycleStart    *int64 `json:"freeCycleStart,omitempty"` // 免费周期开始
	FreeCycleEnd      *int64 `json:"freeCycleEnd,omitempty"`   // 免费周期结束
	FreeTotal         int    `json:"freeTotalSeconds"`         // 免费额度总秒数
	FreeUsed          int    `json:"freeUsedSeconds"`          // 免费额度已用秒数
	PaygTotal         int    `json:"paygTotalSeconds"`         // Payg 总秒数
	PaygUsed          int    `json:"paygUsedSeconds"`          // Payg 已用秒数
	SpecialOfferTotal int    `json:"specialOfferTotalSeconds"` // Special Offer 总秒数
	SpecialOfferUsed  int    `json:"specialOfferUsedSeconds"`  // Special Offer 已用秒数
	PremiumIndex      int    `json:"premiumCycleIndex"`        // Premium 当前周期编号
	PremiumStart      *int64 `json:"premiumCycleStart,omitempty"`
	PremiumEnd        *int64 `json:"premiumCycleEnd,omitempty"`
	PremiumTotal      int    `json:"premiumTotalSeconds"`
	PremiumUsed       int    `json:"premiumUsedSeconds"`
	PremiumBacklog    int    `json:"premiumBacklogSeconds"`
	ProExpireAt       *int64 `json:"proExpireAt,omitempty"`
	ProTotal          int    `json:"proTotalSeconds"`
	ProUsed           int    `json:"proUsedSeconds"`
	UpdatedAt         int64  `json:"updatedAt"` // 快照生成时间
}

// SubscriptionPlan 定义前端展示的订阅/套餐配置。
type SubscriptionPlan struct {
	PlanID        string  `json:"planId"`
	ProductID     string  `json:"productId"`
	Price         string  `json:"price"`
	PriceUnit     string  `json:"priceUnit"`
	Value         string  `json:"value,omitempty"`
	ValueUnit     string  `json:"valueUnit,omitempty"`
	PriceDetail   string  `json:"priceDetail"`
	Badge         *string `json:"badge,omitempty"`
	SummaryQuota  int     `json:"summaryQuota,omitempty"`
	SummaryDetail string  `json:"summaryDetail,omitempty"`
}

// SubscriptionPlansResponse 套餐配置响应结构。
type SubscriptionPlansResponse struct {
	Plans []SubscriptionPlan `json:"plans"`
}
