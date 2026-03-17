package model

import "encoding/json"

// TransactionStatus 表示交易的生命周期状态。
type TransactionStatus string

const (
	TransactionStatusReceived  TransactionStatus = "RECEIVED"  // 收到
	TransactionStatusValidated TransactionStatus = "VALIDATED" // 验证
	TransactionStatusFulfilled TransactionStatus = "FULFILLED" // 满足
	TransactionStatusRevoked   TransactionStatus = "REVOKED"   // 撤销
	TransactionStatusFailed    TransactionStatus = "FAILED"    // 失败
)

// TransactionProductType 枚举支持的 Apple IAP 产品形状。
type TransactionProductType string

const (
	TransactionProductHourPack     TransactionProductType = "HOUR_PACK"     // 5 小时包
	TransactionProductSpecialOffer TransactionProductType = "SPECIAL_OFFER" // Special Offer (one-time PAYG-like)
	TransactionProductYearSub      TransactionProductType = "YEAR_SUB"      // 年订阅
	TransactionProductYearPro      TransactionProductType = "YEAR_PRO"      // 年 Pro
	TransactionProductProMini      TransactionProductType = "YEAR_PRO_MINI" // 年 Pro Mini
	TransactionProductFree         TransactionProductType = "FREE"          // 免费
	TransactionProductUnknown      TransactionProductType = "UNKNOWN"       // 未知
)

// Transaction 捕获跨提供商的购买或续订事件。
type Transaction struct {
	ID                   int64                  `db:"id" json:"id"`                               // ID
	UserID               int                    `db:"user_id" json:"userId"`                      // 用户 ID
	Provider             string                 `db:"provider" json:"provider"`                   // 提供商
	ProviderTransaction  string                 `db:"provider_txn_id" json:"providerTransaction"` // 提供商交易 ID
	ProductID            string                 `db:"product_id" json:"productId"`                // 产品 ID
	ProductType          TransactionProductType `db:"product_type" json:"productType"`            // 产品类型
	PromotionalOfferID   *string                `db:"promotional_offer_id" json:"promotionalOfferId,omitempty"`
	PromotionalOfferType *int                   `db:"promotional_offer_type" json:"promotionalOfferType,omitempty"`
	Status               TransactionStatus      `db:"status" json:"status"`                              // 状态
	AmountCents          int                    `db:"amount_cents" json:"amountCents"`                   // 金额（美分）
	Currency             string                 `db:"currency" json:"currency"`                          // 货币
	AccountSnapshot      json.RawMessage        `db:"account_snapshot" json:"accountSnapshot,omitempty"` // 账本快照
	PurchasedAt          int64                  `db:"purchased_at" json:"purchasedAt"`                   // 购买时间
	ExpiresAt            *int64                 `db:"expires_at" json:"expiresAt,omitempty"`             // 过期时间
	CreatedAt            int64                  `db:"created_at" json:"createdAt"`                       // 创建时间
	UpdatedAt            int64                  `db:"updated_at" json:"updatedAt"`                       // 更新时间
}
