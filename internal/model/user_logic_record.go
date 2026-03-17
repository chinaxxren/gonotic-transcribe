package model

// UserLogicRecord 存储辅助的用户业务状态，如免费配额计划。
type UserLogicRecord struct {
	UserID          int    `db:"user_id" json:"userId"`                               // 用户 ID
	NextFreeGrantAt *int64 `db:"next_free_grant_at" json:"nextFreeGrantAt,omitempty"` // 下次免费授予时间
	CreatedAt       int64  `db:"created_at" json:"createdAt"`                         // 创建时间
	UpdatedAt       int64  `db:"updated_at" json:"updatedAt"`                         // 更新时间
}
