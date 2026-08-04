package domain

import "time"

// TemplateStatus 模板审核状态
type TemplateStatus string

const (
	TemplateStatusDraft         TemplateStatus = "draft"          // 草稿
	TemplateStatusPendingReview TemplateStatus = "pending_review" // 待审核
	TemplateStatusApproved      TemplateStatus = "approved"       // 已通过，可被活动引用
	TemplateStatusRejected      TemplateStatus = "rejected"       // 已驳回
	TemplateStatusDisabled      TemplateStatus = "disabled"       // 已停用
)

func (s TemplateStatus) Valid() bool {
	switch s {
	case TemplateStatusDraft, TemplateStatusPendingReview, TemplateStatusApproved,
		TemplateStatusRejected, TemplateStatusDisabled:
		return true
	default:
		return false
	}
}

func (s TemplateStatus) Editable() bool {
	return s == TemplateStatusDraft || s == TemplateStatusRejected
}

func (s TemplateStatus) CanSubmit() bool {
	return s == TemplateStatusDraft || s == TemplateStatusRejected
}

func (s TemplateStatus) CanReview() bool {
	return s == TemplateStatusPendingReview
}

func (s TemplateStatus) Usable() bool {
	return s == TemplateStatusApproved
}

// Template 推送模板
type Template struct {
	ID           uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	Code         string         `gorm:"size:64;uniqueIndex;not null" json:"code"` // 业务侧 template_id
	Name         string         `gorm:"size:128;not null" json:"name"`
	Body         string         `gorm:"type:text;not null" json:"body"` // 支持 {{var}}
	BizScene     string         `gorm:"size:64;index" json:"biz_scene"`
	ChannelHint  ChannelType    `gorm:"size:32" json:"channel_hint,omitempty"` // 建议渠道，可选
	Status       TemplateStatus `gorm:"size:32;not null;index;default:draft" json:"status"`
	Version      int64          `gorm:"not null;default:0" json:"version"` // 乐观锁
	RejectReason string         `gorm:"size:512" json:"reject_reason,omitempty"`
	CreatedBy    string         `gorm:"size:64" json:"created_by,omitempty"`
	UpdatedBy    string         `gorm:"size:64" json:"updated_by,omitempty"`
	ReviewedBy   string         `gorm:"size:64" json:"reviewed_by,omitempty"`
	ReviewedAt   *time.Time     `json:"reviewed_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func (Template) TableName() string { return "push_templates" }

// CreateTemplateInput 创建模板
type CreateTemplateInput struct {
	Code        string      `json:"code"` // 可选，空则系统生成
	Name        string      `json:"name" binding:"required"`
	Body        string      `json:"body" binding:"required"`
	BizScene    string      `json:"biz_scene"`
	ChannelHint ChannelType `json:"channel_hint"`
	CreatedBy   string      `json:"created_by"`
}

// UpdateTemplateInput 更新模板（仅 draft/rejected）
type UpdateTemplateInput struct {
	Name        *string      `json:"name"`
	Body        *string      `json:"body"`
	BizScene    *string      `json:"biz_scene"`
	ChannelHint *ChannelType `json:"channel_hint"`
	UpdatedBy   string       `json:"updated_by"`
	Version     *int64       `json:"version,omitempty"` // 可选：乐观锁期望版本
}

// ReviewTemplateInput 审核
type ReviewTemplateInput struct {
	ReviewedBy   string `json:"reviewed_by"`
	RejectReason string `json:"reject_reason"` // 驳回时必填
	Version      *int64 `json:"version,omitempty"`
}

// ListTemplateQuery 列表查询
type ListTemplateQuery struct {
	BizScene string         `form:"biz_scene"`
	Status   TemplateStatus `form:"status"`
	Keyword  string         `form:"keyword"`
	Page     int            `form:"page"`
	PageSize int            `form:"page_size"`
}
