package dto

// SummaryGroupNode 用于返回模板分组树。
type SummaryGroupNode struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	ImageURL      string `json:"imageUrl,omitempty"`
	GroupType     string `json:"groupType"`
	Visible       bool   `json:"visible"`
	DisplayOrder  int    `json:"displayOrder"`
	TemplateCount int    `json:"templateCount"`
}

// SummaryTemplateItem 用于返回模板列表。
type SummaryTemplateItem struct {
	ID           int64  `json:"id"`
	GroupID      int64  `json:"groupId"`
	Name         string `json:"name"`
	Intro        string `json:"intro,omitempty"`
	Visible      bool   `json:"visible"`
	DisplayOrder int    `json:"displayOrder"`
	TemplateType string `json:"templateType"`
	OwnerID      int64  `json:"ownerId"`
	Description  string `json:"description,omitempty"`
}
