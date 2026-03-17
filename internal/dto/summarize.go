// Package dto 定义数据传输对象
package dto

import (
	"encoding/json"
	"strings"
)

// GenerateSummaryRequest 生成摘要请求
type GenerateSummaryRequest struct {
	MeetingID          int    `json:"meeting_id" binding:"required"`
	SummaryType        string `json:"summary_type" binding:"omitempty,oneof=meeting class auto"`
	TemplateID         int64  `json:"template_id" binding:"required"`
	Language           string `json:"language" binding:"required"`
	Regenerate         bool   `json:"regenerate"`
	IncludeKeyPoints   *bool  `json:"include_key_points,omitempty"`
	IncludeActionItems *bool  `json:"include_action_items,omitempty"`
	IncludeKeyConcepts *bool  `json:"include_key_concepts,omitempty"`
	IncludeHomework    *bool  `json:"include_homework,omitempty"`
	MaxLength          *int   `json:"max_length,omitempty"`
	Subject            string `json:"subject,omitempty"`
}

// UnmarshalJSON 支持 snake_case 与 camelCase，兼容客户端字段
func (r *GenerateSummaryRequest) UnmarshalJSON(data []byte) error {
	type Alias GenerateSummaryRequest
	aux := &struct {
		MeetingIDAlt          *int   `json:"meetingId"`
		SummaryTypeAlt        string `json:"summaryType"`
		TemplateIDAlt         *int64 `json:"templateId"`
		RegenerateAlt         *bool  `json:"regenerate"`
		IncludeKeyPointsAlt   *bool  `json:"includeKeyPoints"`
		IncludeActionItemsAlt *bool  `json:"includeActionItems"`
		IncludeKeyConceptsAlt *bool  `json:"includeKeyConcepts"`
		IncludeHomeworkAlt    *bool  `json:"includeHomework"`
		MaxLengthAlt          *int   `json:"maxLength"`
		SubjectAlt            string `json:"subject"`
		*Alias
	}{
		Alias: (*Alias)(r),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.MeetingIDAlt != nil {
		r.MeetingID = *aux.MeetingIDAlt
	}
	if aux.TemplateIDAlt != nil {
		r.TemplateID = *aux.TemplateIDAlt
	}
	if aux.SummaryTypeAlt != "" && strings.TrimSpace(r.SummaryType) == "" {
		r.SummaryType = aux.SummaryTypeAlt
	}
	if aux.RegenerateAlt != nil {
		r.Regenerate = *aux.RegenerateAlt
	}
	if aux.IncludeKeyPointsAlt != nil {
		if r.IncludeKeyPoints == nil {
			r.IncludeKeyPoints = new(bool)
		}
		*r.IncludeKeyPoints = *aux.IncludeKeyPointsAlt
	}
	if aux.IncludeActionItemsAlt != nil {
		if r.IncludeActionItems == nil {
			r.IncludeActionItems = new(bool)
		}
		*r.IncludeActionItems = *aux.IncludeActionItemsAlt
	}
	if aux.IncludeKeyConceptsAlt != nil {
		if r.IncludeKeyConcepts == nil {
			r.IncludeKeyConcepts = new(bool)
		}
		*r.IncludeKeyConcepts = *aux.IncludeKeyConceptsAlt
	}
	if aux.IncludeHomeworkAlt != nil {
		if r.IncludeHomework == nil {
			r.IncludeHomework = new(bool)
		}
		*r.IncludeHomework = *aux.IncludeHomeworkAlt
	}
	if aux.MaxLengthAlt != nil {
		if r.MaxLength == nil {
			r.MaxLength = new(int)
		}
		*r.MaxLength = *aux.MaxLengthAlt
	}
	if aux.SubjectAlt != "" && strings.TrimSpace(r.Subject) == "" {
		r.Subject = aux.SubjectAlt
	}
	return nil
}

// SummaryResponse 摘要响应
type SummaryResponse struct {
	Summary          string `json:"summary"`
	SummaryType      string `json:"summary_type"`
	MeetingID        int    `json:"meeting_id"`
	Language         string `json:"language"`
	TranscriptLength int    `json:"transcript_length"`
	Title            string `json:"title,omitempty"`
}

// SummaryType 摘要类型
type SummaryType struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// SummaryTypesResponse 摘要类型列表响应
type SummaryTypesResponse struct {
	Types []SummaryType `json:"types"`
}
