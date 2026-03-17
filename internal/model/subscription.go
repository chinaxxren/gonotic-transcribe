package model

// SubscriptionStatus 反映订阅的当前生命周期。
type SubscriptionStatus string

const (
	SubscriptionStatusPending  SubscriptionStatus = "PENDING"  // 待处理
	SubscriptionStatusActive   SubscriptionStatus = "ACTIVE"   // 激活
	SubscriptionStatusExpired  SubscriptionStatus = "EXPIRED"  // 过期
	SubscriptionStatusCanceled SubscriptionStatus = "CANCELED" // 取消
	SubscriptionStatusRefunded SubscriptionStatus = "REFUNDED" // 退款
)

// SubscriptionRenewalState 跟踪 Apple 的自动续订偏好。
type SubscriptionRenewalState string

const (
	SubscriptionRenewalOn      SubscriptionRenewalState = "AUTO_RENEW_ON"  // 自动续订
	SubscriptionRenewalOff     SubscriptionRenewalState = "AUTO_RENEW_OFF" // 自动续订关闭
	SubscriptionRenewalUnknown SubscriptionRenewalState = "UNKNOWN"        // 未知
)

// SubscriptionProductType 映射到内部的重复产品定义。
type SubscriptionProductType string

const (
	SubscriptionProductYearSub     SubscriptionProductType = "YEAR_SUB"      // 年订阅
	SubscriptionProductYearPro     SubscriptionProductType = "YEAR_PRO"      // 年 Pro
	SubscriptionProductProMini SubscriptionProductType = "YEAR_PRO_MINI" // 年 Pro Mini
	SubscriptionProductHourPack    SubscriptionProductType = "HOUR_PACK"     // 5 小时包
	SubscriptionProductFree        SubscriptionProductType = "FREE"          // 免费
	SubscriptionProductUnknown     SubscriptionProductType = "UNKNOWN"       // 未知
)

// Subscription 存储规范化重复购买元数据。
type Subscription struct {
	ID                 int64                    `db:"id" json:"id"`                                   // ID
	UserID             int                      `db:"user_id" json:"userId"`                          // 用户 ID
	Provider           string                   `db:"provider" json:"provider"`                       // 提供商
	ProductType        SubscriptionProductType  `db:"product_type" json:"productType"`                // 产品类型
	OriginalTxnID      string                   `db:"original_txn_id" json:"originalTxnId"`           // 原始交易 ID
	LatestTxnID        *string                  `db:"latest_txn_id" json:"latestTxnId,omitempty"`     // 最新交易 ID
	Status             SubscriptionStatus       `db:"status" json:"status"`                           // 状态
	RenewalState       SubscriptionRenewalState `db:"renewal_state" json:"renewalState"`              // 自动续订状态
	CurrentPeriodStart int64                    `db:"current_period_start" json:"currentPeriodStart"` // 当前周期开始
	CurrentPeriodEnd   int64                    `db:"current_period_end" json:"currentPeriodEnd"`     // 当前周期结束
	NextGrantAt        *int64                   `db:"next_grant_at" json:"nextGrantAt,omitempty"`     // 下次授予时间
	ExpiresAt          *int64                   `db:"expires_at" json:"expiresAt,omitempty"`          // 订阅有效期截止
	PeriodsGranted     int                      `db:"periods_granted" json:"periodsGranted"`          // 授予周期
	PeriodsConsumed    int                      `db:"periods_consumed" json:"periodsConsumed"`        // 消耗周期
	CreatedAt          int64                    `db:"created_at" json:"createdAt"`                    // 创建时间
	UpdatedAt          int64                    `db:"updated_at" json:"updatedAt"`                    // 更新时间
}
