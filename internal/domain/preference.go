package domain

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// UserPreference 用户偏好中心：渠道退订、主题订阅、用户级免打扰与期望送达时段。
// 与 SuppressionEntry 的分工：抑制名单是运营/合规单方面加黑，偏好是用户自己的选择。
type UserPreference struct {
	ID     uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID string `gorm:"size:64;uniqueIndex;not null" json:"user_id"`
	// Timezone IANA 时区；空则回退活动/服务器时区
	Timezone string `gorm:"size:64" json:"timezone,omitempty"`
	// QuietStart / QuietEnd 用户级免打扰窗（HH:MM，本地时区）；两者都非空才生效
	QuietStart string `gorm:"size:8" json:"quiet_start,omitempty"`
	QuietEnd   string `gorm:"size:8" json:"quiet_end,omitempty"`
	// PreferredHour 期望送达小时 0-23（智能发送时间）；nil 表示不限
	PreferredHour *int `json:"preferred_hour,omitempty"`
	// OptOutChannelsJSON 已退订渠道 []ChannelType
	OptOutChannelsJSON *string `gorm:"type:json;column:opt_out_channels" json:"-"`
	// OptOutTopicsJSON 已退订主题/品类 []string
	OptOutTopicsJSON *string `gorm:"type:json;column:opt_out_topics" json:"-"`
	// MarketingOptOut 全局营销退订：拒收一切 normal 优先级消息，事务通知不受影响
	MarketingOptOut bool      `gorm:"not null;default:false;index" json:"marketing_opt_out"`
	UpdatedBy       string    `gorm:"size:64" json:"updated_by,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (UserPreference) TableName() string { return "user_preferences" }

func parseJSONStringList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "[]" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	return list
}

// OptOutChannels 已退订渠道
func (p *UserPreference) OptOutChannels() []ChannelType {
	if p == nil {
		return nil
	}
	raw := parseJSONStringList(JSONColumnValue(p.OptOutChannelsJSON, ""))
	out := make([]ChannelType, 0, len(raw))
	for _, s := range raw {
		if c := ChannelType(strings.TrimSpace(s)); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// OptOutTopics 已退订主题
func (p *UserPreference) OptOutTopics() []string {
	if p == nil {
		return nil
	}
	return parseJSONStringList(JSONColumnValue(p.OptOutTopicsJSON, ""))
}

// IsChannelOptedOut 是否已退订该渠道
func (p *UserPreference) IsChannelOptedOut(ch ChannelType) bool {
	if p == nil || ch == "" {
		return false
	}
	for _, c := range p.OptOutChannels() {
		if c == ch {
			return true
		}
	}
	return false
}

// IsTopicOptedOut 是否已退订该主题（大小写不敏感）
func (p *UserPreference) IsTopicOptedOut(topic string) bool {
	if p == nil {
		return false
	}
	topic = strings.ToLower(strings.TrimSpace(topic))
	if topic == "" {
		return false
	}
	for _, t := range p.OptOutTopics() {
		if strings.ToLower(strings.TrimSpace(t)) == topic {
			return true
		}
	}
	return false
}

// QuietWindow 用户级免打扰窗；ok=false 表示未配置
func (p *UserPreference) QuietWindow() (SendWindow, bool) {
	if p == nil {
		return SendWindow{}, false
	}
	start := strings.TrimSpace(p.QuietStart)
	end := strings.TrimSpace(p.QuietEnd)
	if start == "" || end == "" || start == end {
		return SendWindow{}, false
	}
	return SendWindow{Start: start, End: end}, true
}

// Blocks 判断该用户偏好是否拦截本次投递。
// reason 返回可读原因，供流水 error_msg 与运营排查使用。
func (p *UserPreference) Blocks(ch ChannelType, topic string, priority Priority) (bool, string) {
	if p == nil {
		return false, ""
	}
	// 事务/高优消息（验证码、支付通知）不受营销偏好约束
	if priority == PriorityHigh {
		return false, ""
	}
	if p.MarketingOptOut {
		return true, "user opted out of marketing"
	}
	if p.IsChannelOptedOut(ch) {
		return true, "user opted out of channel " + string(ch)
	}
	if p.IsTopicOptedOut(topic) {
		return true, "user opted out of topic " + topic
	}
	return false, ""
}

// PreferenceInput 偏好更新入参；nil 字段表示不修改
type PreferenceInput struct {
	Timezone        *string   `json:"timezone,omitempty"`
	QuietStart      *string   `json:"quiet_start,omitempty"`
	QuietEnd        *string   `json:"quiet_end,omitempty"`
	PreferredHour   *int      `json:"preferred_hour,omitempty"`
	OptOutChannels  *[]string `json:"opt_out_channels,omitempty"`
	OptOutTopics    *[]string `json:"opt_out_topics,omitempty"`
	MarketingOptOut *bool     `json:"marketing_opt_out,omitempty"`
	// Source 变更来源：console | api | user
	Source   string `json:"source,omitempty"`
	Operator string `json:"operator,omitempty"`
}

// ListPreferenceQuery 偏好列表筛选
type ListPreferenceQuery struct {
	UserID          string `form:"user_id"`
	Topic           string `form:"topic"`
	Channel         string `form:"channel"`
	MarketingOptOut *bool  `form:"-"`
	Page            int    `form:"page"`
	PageSize        int    `form:"page_size"`
}

// 同意变更动作
const (
	ConsentOptIn  = "opt_in"
	ConsentOptOut = "opt_out"
)

// ConsentLog 同意与偏好变更审计。合规要求能回答「这个用户什么时候、
// 通过什么渠道退订了什么」，仅靠偏好表的当前值答不了。
type ConsentLog struct {
	ID     uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID string `gorm:"size:64;index;not null" json:"user_id"`
	// Action opt_in | opt_out
	Action string `gorm:"size:16;not null;index" json:"action"`
	// Scope 变更对象：marketing | channel:sms | topic:promotion | quiet_hours | preferred_hour
	Scope string `gorm:"size:64;not null;index" json:"scope"`
	// Source console | api | user
	Source    string    `gorm:"size:32;index" json:"source,omitempty"`
	Operator  string    `gorm:"size:64" json:"operator,omitempty"`
	Detail    string    `gorm:"size:512" json:"detail,omitempty"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

func (ConsentLog) TableName() string { return "consent_logs" }

// ListConsentLogQuery 同意审计查询
type ListConsentLogQuery struct {
	UserID   string     `form:"user_id"`
	Action   string     `form:"action"`
	Scope    string     `form:"scope"`
	Since    *time.Time `form:"-"`
	Until    *time.Time `form:"-"`
	Page     int        `form:"page"`
	PageSize int        `form:"page_size"`
}

// NormalizeTopicList 去空去重并排序，作为 JSON 列写入前的归一化
func NormalizeTopicList(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// ResolveTopic 活动主题：显式 topic 优先，否则回退 biz_scene
func ResolveTopic(topic, bizScene string) string {
	if t := strings.TrimSpace(topic); t != "" {
		return t
	}
	return strings.TrimSpace(bizScene)
}
