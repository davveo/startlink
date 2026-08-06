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
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	Code     string `gorm:"size:64;uniqueIndex;not null" json:"code"` // 业务侧 template_id
	Name     string `gorm:"size:128;not null" json:"name"`
	Body     string `gorm:"type:text;not null" json:"body"` // 支持 {{var}}；可与 contents 二选一
	BizScene string `gorm:"size:64;index" json:"biz_scene"`
	// ContentsJSON 分渠道内容 {"inbox":{"title":"...","body":"...","extra":{}}}
	ContentsJSON *string `gorm:"column:contents;type:json" json:"-"`
	// VarSchemaJSON 变量声明 [{name,type,required,default,example,sensitive}]
	VarSchemaJSON *string `gorm:"column:var_schema;type:json" json:"-"`
	// MissingVarPolicy error|keep|default|empty
	MissingVarPolicy MissingVarPolicy `gorm:"size:16;not null;default:empty" json:"missing_var_policy"`
	DefaultLocale    string           `gorm:"size:16" json:"default_locale,omitempty"`
	// LocalesJSON {"zh-CN":{"body":"...","contents":{...}}}
	LocalesJSON *string        `gorm:"column:locales;type:json" json:"-"`
	ChannelHint ChannelType    `gorm:"size:32" json:"channel_hint,omitempty"`
	Status      TemplateStatus `gorm:"size:32;not null;index;default:draft" json:"status"`
	Version     int64          `gorm:"not null;default:0" json:"version"` // 乐观锁（非内容版本历史）
	// Revision 内容版本号，每次落历史快照时 +1；与 Version 独立
	Revision     int64      `gorm:"not null;default:0" json:"revision"`
	RejectReason string     `gorm:"size:512" json:"reject_reason,omitempty"`
	CreatedBy    string     `gorm:"size:64" json:"created_by,omitempty"`
	UpdatedBy    string     `gorm:"size:64" json:"updated_by,omitempty"`
	ReviewedBy   string     `gorm:"size:64" json:"reviewed_by,omitempty"`
	ReviewedAt   *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	// 以下字段仅 API 序列化，不落库
	Contents  map[string]ChannelContent `gorm:"-" json:"contents,omitempty"`
	VarSchema []VarDef                  `gorm:"-" json:"var_schema,omitempty"`
	Locales   map[string]LocaleContent  `gorm:"-" json:"locales,omitempty"`
}

func (Template) TableName() string { return "push_templates" }

// HydrateJSON 从 JSON 列填充 API 字段
func (t *Template) HydrateJSON() {
	if t == nil {
		return
	}
	t.Contents = ParseContentsJSON(JSONColumnValue(t.ContentsJSON, ""))
	t.VarSchema = ParseVarSchemaJSON(JSONColumnValue(t.VarSchemaJSON, ""))
	t.Locales = ParseLocalesJSON(JSONColumnValue(t.LocalesJSON, ""))
}

// SyncJSONColumns 将 API 字段写回 JSON 列
func (t *Template) SyncJSONColumns() {
	if t == nil {
		return
	}
	t.ContentsJSON = MarshalJSONColumn(t.Contents, false)
	t.VarSchemaJSON = MarshalJSONColumn(t.VarSchema, true)
	t.LocalesJSON = MarshalJSONColumn(t.Locales, false)
}

// TemplateVersion 模板内容版本快照（与乐观锁 version 独立，用 revision）
type TemplateVersion struct {
	ID               uint64           `gorm:"primaryKey;autoIncrement" json:"id"`
	TemplateID       uint64           `gorm:"index:idx_tpl_rev,priority:1;not null" json:"template_id"`
	Revision         int64            `gorm:"index:idx_tpl_rev,priority:2;not null" json:"revision"`
	Code             string           `gorm:"size:64;not null" json:"code"`
	Name             string           `gorm:"size:128;not null" json:"name"`
	Body             string           `gorm:"type:text;not null" json:"body"`
	ContentsJSON     *string          `gorm:"column:contents;type:json" json:"-"`
	VarSchemaJSON    *string          `gorm:"column:var_schema;type:json" json:"-"`
	MissingVarPolicy MissingVarPolicy `gorm:"size:16" json:"missing_var_policy"`
	DefaultLocale    string           `gorm:"size:16" json:"default_locale,omitempty"`
	LocalesJSON      *string          `gorm:"column:locales;type:json" json:"-"`
	BizScene         string           `gorm:"size:64" json:"biz_scene"`
	ChannelHint      ChannelType      `gorm:"size:32" json:"channel_hint,omitempty"`
	Status           TemplateStatus   `gorm:"size:32" json:"status"`
	ChangeNote       string           `gorm:"size:64" json:"change_note,omitempty"` // update | approve | rollback
	CreatedBy        string           `gorm:"size:64" json:"created_by,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`

	Contents  map[string]ChannelContent `gorm:"-" json:"contents,omitempty"`
	VarSchema []VarDef                  `gorm:"-" json:"var_schema,omitempty"`
	Locales   map[string]LocaleContent  `gorm:"-" json:"locales,omitempty"`
}

func (TemplateVersion) TableName() string { return "template_versions" }

func (v *TemplateVersion) HydrateJSON() {
	if v == nil {
		return
	}
	v.Contents = ParseContentsJSON(JSONColumnValue(v.ContentsJSON, ""))
	v.VarSchema = ParseVarSchemaJSON(JSONColumnValue(v.VarSchemaJSON, ""))
	v.Locales = ParseLocalesJSON(JSONColumnValue(v.LocalesJSON, ""))
}

// SnapshotFromTemplate 从当前模板生成历史行（不含 ID / CreatedAt）
func SnapshotFromTemplate(tpl *Template, note, operator string) *TemplateVersion {
	tpl.SyncJSONColumns()
	return &TemplateVersion{
		TemplateID:       tpl.ID,
		Revision:         tpl.Revision,
		Code:             tpl.Code,
		Name:             tpl.Name,
		Body:             tpl.Body,
		ContentsJSON:     tpl.ContentsJSON,
		VarSchemaJSON:    tpl.VarSchemaJSON,
		MissingVarPolicy: tpl.MissingVarPolicy,
		DefaultLocale:    tpl.DefaultLocale,
		LocalesJSON:      tpl.LocalesJSON,
		BizScene:         tpl.BizScene,
		ChannelHint:      tpl.ChannelHint,
		Status:           tpl.Status,
		ChangeNote:       note,
		CreatedBy:        operator,
	}
}

// CreateTemplateInput 创建模板
type CreateTemplateInput struct {
	Code             string                    `json:"code"` // 可选，空则系统生成
	Name             string                    `json:"name" binding:"required"`
	Body             string                    `json:"body"` // 与 contents 至少其一非空
	Contents         map[string]ChannelContent `json:"contents,omitempty"`
	VarSchema        []VarDef                  `json:"var_schema,omitempty"`
	MissingVarPolicy MissingVarPolicy          `json:"missing_var_policy,omitempty"`
	DefaultLocale    string                    `json:"default_locale,omitempty"`
	Locales          map[string]LocaleContent  `json:"locales,omitempty"`
	BizScene         string                    `json:"biz_scene"`
	ChannelHint      ChannelType               `json:"channel_hint"`
	CreatedBy        string                    `json:"created_by"`
}

// UpdateTemplateInput 更新模板（仅 draft/rejected）
type UpdateTemplateInput struct {
	Name             *string                    `json:"name"`
	Body             *string                    `json:"body"`
	Contents         *map[string]ChannelContent `json:"contents"`
	VarSchema        *[]VarDef                  `json:"var_schema"`
	MissingVarPolicy *MissingVarPolicy          `json:"missing_var_policy"`
	DefaultLocale    *string                    `json:"default_locale"`
	Locales          *map[string]LocaleContent  `json:"locales"`
	BizScene         *string                    `json:"biz_scene"`
	ChannelHint      *ChannelType               `json:"channel_hint"`
	UpdatedBy        string                     `json:"updated_by"`
	Version          *int64                     `json:"version,omitempty"` // 可选：乐观锁期望版本
}

// ReviewTemplateInput 审核
type ReviewTemplateInput struct {
	ReviewedBy   string `json:"reviewed_by"`
	RejectReason string `json:"reject_reason"` // 驳回时必填
	Version      *int64 `json:"version,omitempty"`
}

// PreviewTemplateInput 模板预览渲染
type PreviewTemplateInput struct {
	TemplateID       string                    `json:"template_id"` // code；与 body 二选一
	Body             string                    `json:"body,omitempty"`
	Contents         map[string]ChannelContent `json:"contents,omitempty"`
	VarSchema        []VarDef                  `json:"var_schema,omitempty"`
	MissingVarPolicy MissingVarPolicy          `json:"missing_var_policy,omitempty"`
	DefaultLocale    string                    `json:"default_locale,omitempty"`
	Locales          map[string]LocaleContent  `json:"locales,omitempty"`
	Title            string                    `json:"title,omitempty"`
	Channel          ChannelType               `json:"channel,omitempty"`
	Locale           string                    `json:"locale,omitempty"`
	Vars             map[string]string         `json:"vars,omitempty"`
}

// RollbackTemplateInput 回滚到历史 revision（结果为 draft）
type RollbackTemplateInput struct {
	Revision  int64  `json:"revision" binding:"required"`
	UpdatedBy string `json:"updated_by"`
	Version   *int64 `json:"version,omitempty"`
}

// ListTemplateQuery 列表查询
type ListTemplateQuery struct {
	BizScene string         `form:"biz_scene"`
	Status   TemplateStatus `form:"status"`
	Keyword  string         `form:"keyword"`
	Page     int            `form:"page"`
	PageSize int            `form:"page_size"`
}
