package domain

import (
	"fmt"
	"strings"
	"time"
)

// TargetUser 推送目标用户（应用层通用结构，业务适配器填充）
type TargetUser struct {
	UserID   string            `json:"user_id"`
	Channels []ChannelType     `json:"channels,omitempty"` // 用户可达渠道，空则用任务默认渠道
	Vars     map[string]string `json:"vars,omitempty"`     // 个性化变量
	Extra    map[string]any    `json:"extra,omitempty"`
	Locale   string            `json:"locale,omitempty"`   // 如 zh-CN；空则用模板 default_locale
	Timezone string            `json:"timezone,omitempty"` // IANA，如 Asia/Shanghai；空则服务器本地时区
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
	MsgID            string                    `json:"msg_id"`
	MainTaskID       uint64                    `json:"main_task_id"`
	SubTaskID        uint64                    `json:"sub_task_id"`
	UserID           string                    `json:"user_id"`
	Channel          ChannelType               `json:"channel"`                // 主渠道（兼容；通常为 channels[0]）
	Channels         []ChannelType             `json:"channels,omitempty"`     // 渠道链（降级顺序或并行列表）
	ChannelMode      ChannelMode               `json:"channel_mode,omitempty"` // single | fallback | parallel | all_success
	TemplateID       string                    `json:"template_id"`
	Title            string                    `json:"title,omitempty"`
	Body             string                    `json:"body"`
	Contents         map[string]ChannelContent `json:"contents,omitempty"` // 分渠道内容；缺省回退 Title/Body
	Vars             map[string]string         `json:"vars,omitempty"`
	Extra            map[string]any            `json:"extra,omitempty"` // 活动 payload 透传
	BizScene         string                    `json:"biz_scene"`
	Topic            string                    `json:"topic,omitempty"`    // 订阅主题；空则回退 biz_scene
	Priority         Priority                  `json:"priority,omitempty"` // high | normal，决定投递 Stream
	Locale           string                    `json:"locale,omitempty"`
	Timezone         string                    `json:"timezone,omitempty"`
	MissingVarPolicy MissingVarPolicy          `json:"missing_var_policy,omitempty"`
	ExpireAt         *time.Time                `json:"expire_at,omitempty"`
	MaxFallback      int                       `json:"max_fallback,omitempty"`
	ChannelRoutes    []ChannelRouteRule        `json:"channel_routes,omitempty"`
	ChannelCosts     map[ChannelType]int       `json:"channel_costs,omitempty"`
	CreatedAt        time.Time                 `json:"created_at"`
}

// EffectiveTopic 订阅主题；空则回退 biz_scene
func (m PushMessage) EffectiveTopic() string { return ResolveTopic(m.Topic, m.BizScene) }

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
	if mode == ChannelModeConditional || mode == ChannelModeCostPriority {
		return mode
	}
	chs := m.EffectiveChannels()
	if len(chs) <= 1 {
		return ChannelModeSingle
	}
	if mode == ChannelModeSingle {
		return ChannelModeFallback // 多渠道未显式指定时默认降级
	}
	return mode
}

// ResolveSendChannels 按策略解析最终发送渠道链
func (m PushMessage) ResolveSendChannels() []ChannelType {
	base := m.EffectiveChannels()
	switch m.ChannelMode.Normalize() {
	case ChannelModeConditional:
		return MatchRouteRules(m.ChannelRoutes, m.Vars, m.Extra, base)
	case ChannelModeCostPriority:
		return SortChannelsByCost(base, m.ChannelCosts)
	default:
		return base
	}
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
	// ProviderTemplateID 厂商侧模板/签名 ID。短信、微信等强监管渠道只接受
	// 报备过的模板号 + 变量，正文由厂商拼装，因此必须与 Content 一起下发。
	ProviderTemplateID string `json:"provider_template_id,omitempty"`
	// ProviderSignName 厂商签名（短信）
	ProviderSignName string `json:"provider_sign_name,omitempty"`
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
	BizID string `json:"biz_id" binding:"required"`
	// BizScene / AudienceRef 在使用 segment_code 时由服务端从人群段回填，
	// 故只在未引用人群段时必填；服务端仍会在回填后做一次非空校验。
	BizScene      string         `json:"biz_scene" binding:"required_without=SegmentCode"`
	Priority      Priority       `json:"priority,omitempty"` // high=事务通知 normal=营销促销；空则按 biz_scene 映射
	Title         string         `json:"title" binding:"required"`
	Channel       ChannelType    `json:"channel"`                        // 主渠道；与 channels 二选一或同时传（channels 优先）
	Channels      []ChannelType  `json:"channels,omitempty"`             // 有序渠道链：fallback 按序降级，parallel 并行
	ChannelMode   ChannelMode    `json:"channel_mode,omitempty"`         // single|fallback|parallel|all_success|conditional|cost_priority
	TemplateID    string         `json:"template_id" binding:"required"` // 模板中心 code，须为已审核通过
	TemplateBody  string         `json:"template_body,omitempty"`        // 已废弃：由模板中心快照填充，传入将被忽略
	AudienceRef   string         `json:"audience_ref" binding:"required_without=SegmentCode"`
	AudienceExtra map[string]any `json:"audience_extra,omitempty"`
	// SegmentCode 引用已沉淀的人群段；填写后由服务端回填 audience_ref / audience_extra
	SegmentCode string `json:"segment_code,omitempty"`
	// ExcludeSegmentCode 排除名单人群段，拆分时从目标人群中剔除
	ExcludeSegmentCode string `json:"exclude_segment_code,omitempty"`
	// Topic 订阅主题/品类；空则回退 biz_scene
	Topic       string         `json:"topic,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
	WebhookURL  string         `json:"webhook_url,omitempty"`
	ScheduledAt *time.Time     `json:"scheduled_at,omitempty"`
	ExpireAt    *time.Time     `json:"expire_at,omitempty"`
	// ExperimentID / Salt / ControlPercent：实验抽样；对照组不发送
	ExperimentID             string `json:"experiment_id,omitempty"`
	ExperimentSalt           string `json:"experiment_salt,omitempty"`
	ExperimentControlPercent int    `json:"experiment_control_percent,omitempty"`
	// MaxFallback fallback 最大降级次数（不含首渠）；0=不限制
	MaxFallback int `json:"max_fallback,omitempty"`
	// ChannelRoutes 条件路由（channel_mode=conditional）
	ChannelRoutes []ChannelRouteRule `json:"channel_routes,omitempty"`
	// ChannelCosts 渠道成本（channel_mode=cost_priority）
	ChannelCosts map[ChannelType]int `json:"channel_costs,omitempty"`
	// SendWindows 分时投放窗，如 [{"start":"09:00","end":"21:00"}]；空表示全天可发
	SendWindows []SendWindow `json:"send_windows,omitempty"`
	// PaceQPS 本活动投放速率上限（Worker 入队节流）；0 表示不限制
	PaceQPS int `json:"pace_qps,omitempty"`
	// RampUp 渐进放量阶梯；配置后按活动运行时长逐级提升入队速率
	RampUp []RampUpStage `json:"ramp_up,omitempty"`
	// QuotaPolicy 渠道硬配额超额策略：queue=仍创建并排队；reject=拒绝创建（仅 admission=enforce 渠道生效）
	QuotaPolicy string `json:"quota_policy,omitempty"`
	// ExpectedFinishMinutes 期望完成时长（分钟），用于创建准入估算；0 则用渠道配置默认
	ExpectedFinishMinutes int `json:"expected_finish_minutes,omitempty"`
	// CreatedBy 业务负责人
	CreatedBy string `json:"created_by,omitempty"`
	// AsDraft=true 时创建为 draft，不会被 Scheduler 拆分；需 Publish
	AsDraft bool `json:"as_draft,omitempty"`
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
	// conditional / cost_priority 不因渠道数量被纠偏为 single/fallback
	if mode == ChannelModeConditional || mode == ChannelModeCostPriority {
		if err := in.validateStrategyFields(mode, chs); err != nil {
			return "", nil, "", err
		}
		return chs[0], chs, mode, nil
	}
	if len(chs) == 1 {
		mode = ChannelModeSingle
	} else if mode == ChannelModeSingle {
		mode = ChannelModeFallback
	}
	return chs[0], chs, mode, nil
}

// validateStrategyFields 校验条件路由 / 成本优先入参
func (in *CreateCampaignInput) validateStrategyFields(mode ChannelMode, base []ChannelType) error {
	switch mode {
	case ChannelModeConditional:
		if len(in.ChannelRoutes) == 0 {
			return fmt.Errorf("channel_routes required when channel_mode=conditional")
		}
		for i, r := range in.ChannelRoutes {
			if len(r.Channels) == 0 {
				return fmt.Errorf("channel_routes[%d].channels required", i)
			}
			for _, c := range r.Channels {
				if !c.Valid() {
					return fmt.Errorf("channel_routes[%d]: invalid channel %s", i, c)
				}
			}
			if r.When != nil && strings.TrimSpace(r.When.Var) == "" {
				return fmt.Errorf("channel_routes[%d].when.var required", i)
			}
		}
	case ChannelModeCostPriority:
		for ch, cost := range in.ChannelCosts {
			if !ch.Valid() {
				return fmt.Errorf("channel_costs: invalid channel %s", ch)
			}
			if cost < 0 {
				return fmt.Errorf("channel_costs[%s]: cost must be >= 0", ch)
			}
		}
		_ = base // 成本表可选，缺省用 DefaultChannelCosts
	}
	return nil
}

// SubTaskStatusSummary 子任务按状态汇总
type SubTaskStatusSummary struct {
	Status      TaskStatus `json:"status"`
	SubCount    int        `json:"sub_count"`
	UserTotal   int64      `json:"user_total"`
	UserSuccess int64      `json:"user_success"`
	UserFail    int64      `json:"user_fail"`
}

// ListCampaignQuery 活动列表筛选
type ListCampaignQuery struct {
	BizScene      string      `form:"biz_scene"`
	Status        TaskStatus  `form:"status"`
	Channel       ChannelType `form:"channel"`
	Priority      Priority    `form:"priority"`
	CreatedBy     string      `form:"created_by"`
	Keyword       string      `form:"keyword"` // 匹配 biz_id / title
	CreatedFrom   *time.Time  `form:"created_from" time_format:"2006-01-02T15:04:05Z07:00"`
	CreatedTo     *time.Time  `form:"created_to" time_format:"2006-01-02T15:04:05Z07:00"`
	ScheduledFrom *time.Time  `form:"scheduled_from" time_format:"2006-01-02T15:04:05Z07:00"`
	ScheduledTo   *time.Time  `form:"scheduled_to" time_format:"2006-01-02T15:04:05Z07:00"`
	Page          int         `form:"page"`
	PageSize      int         `form:"page_size"`
}

// ListPushRecordQuery 用户级流水查询
type ListPushRecordQuery struct {
	UserID   string      `form:"user_id"`
	Channel  ChannelType `form:"channel"`
	Status   PushStatus  `form:"status"`
	Keyword  string      `form:"keyword"` // error_msg / provider_id
	Page     int         `form:"page"`
	PageSize int         `form:"page_size"`
}

// AudienceEstimateInput 人群试算 / 预检共用入参
type AudienceEstimateInput struct {
	BizScene      string         `json:"biz_scene" binding:"required"`
	AudienceRef   string         `json:"audience_ref" binding:"required"`
	AudienceExtra map[string]any `json:"audience_extra,omitempty"`
	Channels      []ChannelType  `json:"channels,omitempty"`
	Channel       ChannelType    `json:"channel,omitempty"`
	MaxPages      int            `json:"max_pages,omitempty"`    // 试算最多翻页，默认 5
	SampleLimit   int            `json:"sample_limit,omitempty"` // 返回样本数，默认 20，最大 100
}

// DryRunInput 测试渲染 / dry-run
type DryRunInput struct {
	TemplateID       string            `json:"template_id" binding:"required"`
	Title            string            `json:"title,omitempty"`
	Vars             map[string]string `json:"vars,omitempty"`
	Channel          ChannelType       `json:"channel,omitempty"`
	Channels         []ChannelType     `json:"channels,omitempty"`
	ChannelMode      ChannelMode       `json:"channel_mode,omitempty"`
	UserID           string            `json:"user_id,omitempty"`
	Locale           string            `json:"locale,omitempty"`
	MissingVarPolicy MissingVarPolicy  `json:"missing_var_policy,omitempty"` // 覆盖模板策略；空则用模板
	// Send=true 时真实调用渠道（写入 is_test 流水，不计入活动统计）；默认仅渲染校验
	Send bool `json:"send,omitempty"`
}

// BatchActionInput 批量操作
type BatchActionInput struct {
	IDs []uint64 `json:"ids" binding:"required"`
}

// CopyCampaignInput 复制活动
type CopyCampaignInput struct {
	BizID     string `json:"biz_id" binding:"required"`
	Title     string `json:"title,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	AsDraft   *bool  `json:"as_draft,omitempty"` // 默认 true
}

// ListSubTaskQuery 某主任务下的子任务列表
type ListSubTaskQuery struct {
	Status   TaskStatus `form:"status"`
	Page     int        `form:"page"`
	PageSize int        `form:"page_size"`
}

// SubTaskView 子任务列表视图（不返回完整 user_ids JSON，避免大包）
type SubTaskView struct {
	ID           uint64     `json:"id"`
	MainTaskID   uint64     `json:"main_task_id"`
	ShardIndex   int        `json:"shard_index"`
	TotalCount   int        `json:"total_count"`
	SuccessCount int        `json:"success_count"`
	FailCount    int        `json:"fail_count"`
	Status       TaskStatus `json:"status"`
	RetryCount   int        `json:"retry_count"`
	WorkerID     string     `json:"worker_id,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	ClaimedAt    *time.Time `json:"claimed_at,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// ToView 转为不含人群 payload 的视图
func (s SubTask) ToView() SubTaskView {
	return SubTaskView{
		ID:           s.ID,
		MainTaskID:   s.MainTaskID,
		ShardIndex:   s.ShardIndex,
		TotalCount:   s.TotalCount,
		SuccessCount: s.SuccessCount,
		FailCount:    s.FailCount,
		Status:       s.Status,
		RetryCount:   s.RetryCount,
		WorkerID:     s.WorkerID,
		LastError:    s.LastError,
		ClaimedAt:    s.ClaimedAt,
		StartedAt:    s.StartedAt,
		FinishedAt:   s.FinishedAt,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
	}
}
