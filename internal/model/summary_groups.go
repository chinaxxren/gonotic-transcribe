package model

// SummaryGroup 表示摘要模板的分组节点（仅系统分组）。
type SummaryGroup struct {
	ID           int64  `db:"id" json:"id"`
	Name         string `db:"name" json:"name"`
	ImageURL     string `db:"image_url" json:"imageUrl"`
	GroupType    string `db:"group_type" json:"groupType"` // system
	Visible      bool   `db:"is_visible" json:"visible"`
	DisplayOrder int    `db:"display_order" json:"displayOrder"`
	CreatedAt    int64  `db:"created_at" json:"createdAt"`
	UpdatedAt    int64  `db:"updated_at" json:"updatedAt"`
}

// SummaryTemplate defines a reusable template for summaries.
type SummaryTemplate struct {
	ID           int64  `db:"id" json:"id"`
	GroupID      int64  `db:"group_id" json:"groupId"`
	Name         string `db:"name" json:"name"`
	Intro        string `db:"intro" json:"intro"`
	Storage      string `db:"storage" json:"storage"`   // local / oss
	Location     string `db:"location" json:"location"` // local path or OSS key
	Description  string `db:"description" json:"description"`
	TemplateType string `db:"template_type" json:"templateType"`
	OwnerID      int64  `db:"owner_id" json:"ownerId"`
	Visible      bool   `db:"is_visible" json:"visible"`
	DisplayOrder int    `db:"display_order" json:"displayOrder"`
	CreatedAt    int64  `db:"created_at" json:"createdAt"`
	UpdatedAt    int64  `db:"updated_at" json:"updatedAt"`
}

// SummaryTemplateUsage tracks per-user template usage to power "recent" suggestions.
type SummaryTemplateUsage struct {
	ID         int64 `db:"id" json:"id"`
	UserID     int   `db:"user_id" json:"userId"`
	TemplateID int64 `db:"template_id" json:"templateId"`
	UsedTimes  int   `db:"used_times" json:"usedTimes"`
	UpdatedAt  int64 `db:"updated_at" json:"updatedAt"`
	CreatedAt  int64 `db:"created_at" json:"createdAt"`
}
