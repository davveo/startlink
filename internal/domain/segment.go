package domain

import (
	"encoding/json"
	"strings"
	"time"
)

// SegmentKind 人群段用途：include 作为投放目标，exclude 作为排除名单。
// 两者共用同一套 Provider 解析链路，只是在活动上挂载的位置不同。
type SegmentKind string

const (
	SegmentKindInclude SegmentKind = "include"
	SegmentKindExclude SegmentKind = "exclude"
)

func (k SegmentKind) Normalize() SegmentKind {
	switch k {
	case SegmentKindInclude, SegmentKindExclude:
		return k
	default:
		return SegmentKindInclude
	}
}

func (k SegmentKind) Valid() bool {
	switch k {
	case "", SegmentKindInclude, SegmentKindExclude:
		return true
	default:
		return false
	}
}

const (
	SegmentStatusActive   = "active"
	SegmentStatusDisabled = "disabled"
)

// SegmentSource 人群段成员来源。
type SegmentSource string

const (
	// SegmentSourceProvider 走 AudienceProvider（Demo / HTTP）动态圈人
	SegmentSourceProvider SegmentSource = "provider"
	// SegmentSourceStatic 成员落库（CSV / JSON 导入），由 StaticProvider 分页读出
	SegmentSourceStatic SegmentSource = "static"
)

// BizSceneStatic 静态人群专用业务场景；StaticProvider 仅响应此 scene。
const BizSceneStatic = "static"

func (s SegmentSource) Normalize() SegmentSource {
	switch s {
	case SegmentSourceStatic:
		return SegmentSourceStatic
	default:
		return SegmentSourceProvider
	}
}

func (s SegmentSource) Valid() bool {
	switch s {
	case "", SegmentSourceProvider, SegmentSourceStatic:
		return true
	default:
		return false
	}
}

// AudienceSegment 可复用人群段：把一次性的 audience_ref 沉淀为平台资产，
// 供多个活动引用，并缓存成员数避免每次预检都重新圈人。
type AudienceSegment struct {
	ID          uint64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Code        string      `gorm:"size:64;uniqueIndex;not null" json:"code"`
	Name        string      `gorm:"size:128;not null" json:"name"`
	Kind        SegmentKind `gorm:"size:16;not null;index;default:include" json:"kind"`
	// Source provider=动态圈人；static=CSV/JSON 导入的静态名单
	Source      SegmentSource `gorm:"size:16;not null;index;default:provider" json:"source"`
	BizScene    string        `gorm:"size:64;index;not null" json:"biz_scene"`
	AudienceRef string        `gorm:"size:128;not null" json:"audience_ref"`
	// AudienceExtraJSON 透传给 Provider 的圈人参数
	AudienceExtraJSON *string `gorm:"type:json;column:audience_extra" json:"-"`
	Description       string  `gorm:"size:512" json:"description,omitempty"`
	Status            string  `gorm:"size:16;not null;index;default:active" json:"status"`
	// MemberCount / CountedAt 最近一次刷新的成员数快照；RefreshError 记录刷新失败原因
	MemberCount  int64      `gorm:"not null;default:0" json:"member_count"`
	CountedAt    *time.Time `json:"counted_at,omitempty"`
	RefreshError string     `gorm:"size:512" json:"refresh_error,omitempty"`
	CreatedBy    string     `gorm:"size:64" json:"created_by,omitempty"`
	UpdatedBy    string     `gorm:"size:64" json:"updated_by,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// IsStatic 是否为静态导入人群
func (s *AudienceSegment) IsStatic() bool {
	return s != nil && s.Source.Normalize() == SegmentSourceStatic
}

func (AudienceSegment) TableName() string { return "audience_segments" }

// ExtraMap 解析圈人参数
func (s *AudienceSegment) ExtraMap() map[string]any {
	if s == nil {
		return nil
	}
	raw := JSONColumnValue(s.AudienceExtraJSON, "")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}

// Active 是否可被活动引用
func (s *AudienceSegment) Active() bool {
	return s != nil && s.Status != SegmentStatusDisabled
}

// ListSegmentQuery 人群段列表筛选
type ListSegmentQuery struct {
	Kind     SegmentKind   `form:"kind"`
	Source   SegmentSource `form:"source"`
	BizScene string        `form:"biz_scene"`
	Status   string        `form:"status"`
	Keyword  string        `form:"keyword"` // 匹配 code / name
	Page     int           `form:"page"`
	PageSize int           `form:"page_size"`
}

// SegmentInput 创建/更新人群段入参
type SegmentInput struct {
	Code   string         `json:"code,omitempty"`
	Name   string         `json:"name" binding:"required"`
	Kind   SegmentKind    `json:"kind,omitempty"`
	Source SegmentSource  `json:"source,omitempty"` // provider | static；默认 provider
	// BizScene / AudienceRef：provider 必填；static 可省略（服务端填 static / code）
	BizScene      string         `json:"biz_scene,omitempty"`
	AudienceRef   string         `json:"audience_ref,omitempty"`
	AudienceExtra map[string]any `json:"audience_extra,omitempty"`
	Description   string         `json:"description,omitempty"`
	Status        string         `json:"status,omitempty"`
	Operator      string         `json:"operator,omitempty"`
}

// AudienceSegmentMember 静态人群段成员（CSV/JSON 导入）。
// phone / email 写入 Extra 后经拆分透传到渠道 SendRequest，供短信/邮件 Sender 使用。
type AudienceSegmentMember struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	SegmentCode string    `gorm:"size:64;not null;uniqueIndex:uk_seg_member;index" json:"segment_code"`
	UserID      string    `gorm:"size:64;not null;uniqueIndex:uk_seg_member" json:"user_id"`
	Phone       string    `gorm:"size:32;index" json:"phone,omitempty"`
	Email       string    `gorm:"size:128;index" json:"email,omitempty"`
	// VarsJSON 个性化变量（可选）
	VarsJSON  *string   `gorm:"type:json;column:vars" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (AudienceSegmentMember) TableName() string { return "audience_segment_members" }

// VarsMap 解析个性化变量
func (m *AudienceSegmentMember) VarsMap() map[string]string {
	if m == nil {
		return nil
	}
	raw := JSONColumnValue(m.VarsJSON, "")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// ToTargetUser 转为圈人结果；phone/email 放入 Extra
func (m *AudienceSegmentMember) ToTargetUser() TargetUser {
	extra := map[string]any{}
	if p := strings.TrimSpace(m.Phone); p != "" {
		extra["phone"] = p
	}
	if e := strings.TrimSpace(m.Email); e != "" {
		extra["email"] = e
	}
	var extraOut map[string]any
	if len(extra) > 0 {
		extraOut = extra
	}
	return TargetUser{
		UserID: m.UserID,
		Vars:   m.VarsMap(),
		Extra:  extraOut,
	}
}

// ListSegmentMemberQuery 静态成员列表
type ListSegmentMemberQuery struct {
	Keyword  string `form:"keyword"` // 匹配 user_id / phone / email
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// SegmentMemberInput 单条导入成员
type SegmentMemberInput struct {
	UserID string            `json:"user_id,omitempty"`
	Phone  string            `json:"phone,omitempty"`
	Email  string            `json:"email,omitempty"`
	Vars   map[string]string `json:"vars,omitempty"`
}

// ImportSegmentMembersInput JSON 批量导入
type ImportSegmentMembersInput struct {
	// Mode append（默认）| replace（先清空再写入）
	Mode     string               `json:"mode,omitempty"`
	Members  []SegmentMemberInput `json:"members,omitempty"`
	Operator string               `json:"operator,omitempty"`
}

// ImportSegmentMembersResult 导入结果
type ImportSegmentMembersResult struct {
	Submitted    int   `json:"submitted"`
	Accepted     int   `json:"accepted"`
	Skipped      int   `json:"skipped"`
	MemberCount  int64 `json:"member_count"`
	Replaced     bool  `json:"replaced"`
	InvalidRows  int   `json:"invalid_rows,omitempty"`
	SampleErrors []string `json:"sample_errors,omitempty"`
}

// SuppressionKind 抑制名单类型
type SuppressionKind string

const (
	// SuppressionBlacklist 全渠道拉黑，channel 固定为 SuppressionAllChannels
	SuppressionBlacklist SuppressionKind = "blacklist"
	// SuppressionUnsubscribe 按渠道退订
	SuppressionUnsubscribe SuppressionKind = "unsubscribe"
)

// SuppressionAllChannels 黑名单占位渠道值；参与唯一键，避免空串语义含糊
const SuppressionAllChannels = "*"

func (k SuppressionKind) Valid() bool {
	switch k {
	case SuppressionBlacklist, SuppressionUnsubscribe:
		return true
	default:
		return false
	}
}

// SuppressionEntry 黑名单 / 退订的可管理副本。
// 发送链路仍走 Redis SET 快路径，本表负责列表查询、批量导入导出与操作留痕——
// 否则名单只能靠运维手工 redis-cli 维护，既查不了也审计不了。
type SuppressionEntry struct {
	ID      uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	Kind    SuppressionKind `gorm:"size:16;not null;index;uniqueIndex:uk_suppress_target" json:"kind"`
	UserID  string          `gorm:"size:64;not null;index;uniqueIndex:uk_suppress_target" json:"user_id"`
	Channel string          `gorm:"size:32;not null;uniqueIndex:uk_suppress_target" json:"channel"`
	Reason  string          `gorm:"size:256" json:"reason,omitempty"`
	// Source console | api | import
	Source    string    `gorm:"size:32;index" json:"source,omitempty"`
	Operator  string    `gorm:"size:64;index" json:"operator,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (SuppressionEntry) TableName() string { return "suppression_entries" }

// ListSuppressionQuery 抑制名单查询
type ListSuppressionQuery struct {
	Kind     SuppressionKind `form:"kind"`
	UserID   string          `form:"user_id"`
	Channel  string          `form:"channel"`
	Keyword  string          `form:"keyword"`
	Page     int             `form:"page"`
	PageSize int             `form:"page_size"`
}

// SuppressionInput 单条/批量加入名单
type SuppressionInput struct {
	Kind SuppressionKind `json:"kind" binding:"required"`
	// UserIDs 支持批量导入
	UserIDs  []string `json:"user_ids" binding:"required"`
	Channel  string   `json:"channel,omitempty"` // unsubscribe 必填；blacklist 忽略
	Reason   string   `json:"reason,omitempty"`
	Source   string   `json:"source,omitempty"`
	Operator string   `json:"operator,omitempty"`
}

// NormalizeChannel blacklist 固定 "*"；unsubscribe 保留原渠道
func (in *SuppressionInput) NormalizeChannel() string {
	if in.Kind == SuppressionBlacklist {
		return SuppressionAllChannels
	}
	return strings.TrimSpace(in.Channel)
}
