package domain

import (
	"fmt"
	"time"
)

// TargetUser 推送目标用户（应用层通用结构，业务适配器填充）
type TargetUser struct {
	UserID   string            `json:"user_id"`
	Channels []ChannelType     `json:"channels,omitempty"` // 用户可达渠道，空则用任务默认渠道
	Vars     map[string]string `json:"vars,omitempty"`     // 个性化变量
	Extra    map[string]any    `json:"extra,omitempty"`
}

// AudienceQuery 人群圈选请求（业务快速对接的标准入参）
type AudienceQuery struct {
	AudienceRef string         `json:"audience_ref"`
	BizScene    string         `json:"biz_scene"`
	Extra       map[string]any `json:"extra,omitempty"`
	PageToken   string         `json:"page_token,omitempty"`
	PageSize    int            `json:"page_size"`
}

// AudiencePage 分页人群结果
type AudiencePage struct {
	Users         []TargetUser `json:"users"`
	NextPageToken string       `json:"next_page_token,omitempty"`
	TotalHint     int64        `json:"total_hint,omitempty"` // 可选总量提示
	HasMore       bool         `json:"has_more"`
}

// PushMessage MQ 中的单条推送消息
type PushMessage struct {
	MsgID       string            `json:"msg_id"`
	MainTaskID  uint64            `json:"main_task_id"`
	SubTaskID   uint64            `json:"sub_task_id"`
	UserID      string            `json:"user_id"`
	Channel     ChannelType       `json:"channel"`                // 主渠道（兼容；通常为 channels[0]）
	Channels    []ChannelType     `json:"channels,omitempty"`     // 渠道链（降级顺序或并行列表）
	ChannelMode ChannelMode       `json:"channel_mode,omitempty"` // single | fallback | parallel
	TemplateID  string            `json:"template_id"`
	Title       string            `json:"title,omitempty"`
	Body        string            `json:"body"`
	Vars        map[string]string `json:"vars,omitempty"`
	Extra       map[string]any    `json:"extra,omitempty"` // 活动 payload 透传
	BizScene    string            `json:"biz_scene"`
	Priority    Priority          `json:"priority,omitempty"` // high | normal，决定投递 Stream
	CreatedAt   time.Time         `json:"created_at"`
}

// EffectiveChannels 解析实际要走的渠道列表
func (m PushMessage) EffectiveChannels() []ChannelType {
	if len(m.Channels) > 0 {
		return m.Channels
	}
	if m.Channel != "" {
		return []ChannelType{m.Channel}
	}
	return nil
}

func (m PushMessage) EffectiveMode() ChannelMode {
	mode := m.ChannelMode.Normalize()
	chs := m.EffectiveChannels()
	if len(chs) <= 1 {
		return ChannelModeSingle
	}
	if mode == ChannelModeSingle {
		return ChannelModeFallback // 多渠道未显式指定时默认降级
	}
	return mode
}

// SendRequest 渠道发送请求
type SendRequest struct {
	MsgID   string            `json:"msg_id"`
	UserID  string            `json:"user_id"`
	Channel ChannelType       `json:"channel"`
	Title   string            `json:"title,omitempty"`
	Content string            `json:"content"`
	Vars    map[string]string `json:"vars,omitempty"`
	Extra   map[string]any    `json:"extra,omitempty"`
}

// SendResult 渠道发送结果
type SendResult struct {
	Success    bool   `json:"success"`
	Provider   string `json:"provider,omitempty"` // 供应商标识；空则回退为 channel 名
	ProviderID string `json:"provider_id,omitempty"`
	ErrorMsg   string `json:"error_msg,omitempty"`
	Retryable  bool   `json:"retryable"`
	// Throttled 厂商限流（如 HTTP 429）；Pusher 可据此收缩渠道有效 QPS
	Throttled bool `json:"throttled,omitempty"`
}

// MergeExtra 合并活动 Payload 与用户 Extra：用户字段覆盖同名活动字段。
func MergeExtra(campaign, user map[string]any) map[string]any {
	if len(campaign) == 0 && len(user) == 0 {
		return nil
	}
	out := make(map[string]any, len(campaign)+len(user))
	for k, v := range campaign {
		out[k] = v
	}
	for k, v := range user {
		out[k] = v
	}
	return out
}

// CreateCampaignInput 创建推送活动的通用入参（应用层 API）
type CreateCampaignInput struct {
	BizID         string         `json:"biz_id" binding:"required"`
	BizScene      string         `json:"biz_scene" binding:"required"`
	Priority      Priority       `json:"priority,omitempty"` // high=事务通知 normal=营销促销；空则按 biz_scene 映射
	Title         string         `json:"title" binding:"required"`
	Channel       ChannelType    `json:"channel"`                        // 主渠道；与 channels 二选一或同时传（channels 优先）
	Channels      []ChannelType  `json:"channels,omitempty"`             // 有序渠道链：fallback 按序降级，parallel 并行
	ChannelMode   ChannelMode    `json:"channel_mode,omitempty"`         // single | fallback | parallel
	TemplateID    string         `json:"template_id" binding:"required"` // 模板中心 code，须为已审核通过
	TemplateBody  string         `json:"template_body,omitempty"`        // 已废弃：由模板中心快照填充，传入将被忽略
	AudienceRef   string         `json:"audience_ref" binding:"required"`
	AudienceExtra map[string]any `json:"audience_extra,omitempty"`
	Payload       map[string]any `json:"payload,omitempty"`
	WebhookURL    string         `json:"webhook_url,omitempty"`
	ScheduledAt   *time.Time     `json:"scheduled_at,omitempty"`
	// SendWindows 分时投放窗，如 [{"start":"09:00","end":"21:00"}]；空表示全天可发
	SendWindows []SendWindow `json:"send_windows,omitempty"`
	// PaceQPS 本活动投放速率上限（Worker 入队节流）；0 表示不限制
	PaceQPS int `json:"pace_qps,omitempty"`
	// QuotaPolicy 渠道硬配额超额策略：queue=仍创建并排队；reject=拒绝创建（仅 admission=enforce 渠道生效）
	QuotaPolicy string `json:"quota_policy,omitempty"`
	// ExpectedFinishMinutes 期望完成时长（分钟），用于创建准入估算；0 则用渠道配置默认
	ExpectedFinishMinutes int `json:"expected_finish_minutes,omitempty"`
}

// SendWindow 日内投放时间窗（本地时区，HH:MM）
type SendWindow struct {
	Start string `json:"start"` // "09:00"
	End   string `json:"end"`   // "21:00"
}

// ApplyDefaultChannel 未指定渠道时回填默认渠道
func (in *CreateCampaignInput) ApplyDefaultChannel(defaultCh ChannelType) {
	if defaultCh == "" || !defaultCh.Valid() {
		return
	}
	if len(in.Channels) == 0 && in.Channel == "" {
		in.Channel = defaultCh
	}
}

// IntersectChannels 任务渠道链与用户可达渠道求交（保持任务链顺序）。
// userChs 空表示用户未声明可达渠道，沿用任务链。
func IntersectChannels(taskChs, userChs []ChannelType) []ChannelType {
	if len(taskChs) == 0 {
		return nil
	}
	if len(userChs) == 0 {
		out := make([]ChannelType, len(taskChs))
		copy(out, taskChs)
		return out
	}
	allow := make(map[ChannelType]struct{}, len(userChs))
	for _, c := range userChs {
		allow[c] = struct{}{}
	}
	out := make([]ChannelType, 0, len(taskChs))
	for _, c := range taskChs {
		if _, ok := allow[c]; ok {
			out = append(out, c)
		}
	}
	return out
}

// NormalizeChannels 归一化渠道配置，返回主渠道、渠道链、模式
func (in *CreateCampaignInput) NormalizeChannels() (ChannelType, []ChannelType, ChannelMode, error) {
	mode := in.ChannelMode.Normalize()
	chs := make([]ChannelType, 0, len(in.Channels)+1)
	seen := map[ChannelType]struct{}{}

	appendCh := func(c ChannelType) error {
		if !c.Valid() {
			return fmt.Errorf("invalid channel: %s", c)
		}
		if _, ok := seen[c]; ok {
			return nil
		}
		seen[c] = struct{}{}
		chs = append(chs, c)
		return nil
	}

	if len(in.Channels) > 0 {
		for _, c := range in.Channels {
			if err := appendCh(c); err != nil {
				return "", nil, "", err
			}
		}
	} else if in.Channel != "" {
		if err := appendCh(in.Channel); err != nil {
			return "", nil, "", err
		}
	} else {
		return "", nil, "", fmt.Errorf("channel or channels required")
	}

	if !mode.Valid() {
		return "", nil, "", fmt.Errorf("invalid channel_mode: %s", in.ChannelMode)
	}
	if len(chs) == 1 {
		mode = ChannelModeSingle
	} else if mode == ChannelModeSingle {
		mode = ChannelModeFallback
	}
	return chs[0], chs, mode, nil
}

// SubTaskStatusSummary 子任务按状态汇总
type SubTaskStatusSummary struct {
	Status      TaskStatus `json:"status"`
	SubCount    int        `json:"sub_count"`
	UserTotal   int64      `json:"user_total"`
	UserSuccess int64      `json:"user_success"`
	UserFail    int64      `json:"user_fail"`
}
