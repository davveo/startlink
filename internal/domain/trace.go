package domain

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// 全链路埋点：活动/子任务级节点全量写；用户级只记失败/抑制/限流等异常。
// 成功发送靠 push_records 下钻，避免事件表被正常流量淹没。

const (
	TraceStageCampaign   = "campaign"
	TraceStageSplit      = "split"
	TraceStageWorker     = "worker"
	TraceStagePusher     = "pusher"
	TraceStageCallback   = "callback"
	TraceStageAggregator = "aggregator"
	TraceStageDryRun     = "dryrun"
)

const (
	TraceLevelInfo  = "info"
	TraceLevelWarn  = "warn"
	TraceLevelError = "error"
)

// 事件名约定：stage.action
const (
	TraceEventCampaignCreated   = "campaign.created"
	TraceEventCampaignPublished = "campaign.published"
	TraceEventCampaignCancelled = "campaign.cancelled"
	TraceEventCampaignPaused    = "campaign.paused"
	TraceEventCampaignResumed   = "campaign.resumed"

	TraceEventSplitStarted = "split.started"
	TraceEventSplitShard   = "split.shard_created"
	TraceEventSplitDone    = "split.done"
	TraceEventSplitFailed  = "split.failed"

	TraceEventSubClaimed   = "subtask.claimed"
	TraceEventSubEnqueued  = "subtask.enqueued"
	TraceEventSubFailed    = "subtask.failed"
	TraceEventSubCancelled = "subtask.cancelled"

	TraceEventPushReceived   = "push.received"
	TraceEventPushSuppressed = "push.suppressed"
	TraceEventPushThrottled  = "push.throttled"
	TraceEventPushDeferred   = "push.deferred"
	TraceEventPushFailed     = "push.failed"
	TraceEventPushExpired    = "push.expired"

	TraceEventReceiptApplied = "receipt.applied"
	TraceEventReceiptIgnored = "receipt.ignored"

	TraceEventSubAggregated    = "subtask.aggregated"
	TraceEventCampaignFinalized = "campaign.finalized"
)

// TraceEvent 全链路事件。TraceID 与活动一一对应，贯穿 api→scheduler→pusher→callback。
type TraceEvent struct {
	ID         uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TraceID    string     `gorm:"size:64;not null;index:idx_trace_time,priority:1;index" json:"trace_id"`
	BizID      string     `gorm:"size:64;index" json:"biz_id,omitempty"`
	MainTaskID uint64     `gorm:"index:idx_trace_main_time,priority:1;index" json:"main_task_id"`
	SubTaskID  uint64     `gorm:"index" json:"sub_task_id"`
	MsgID      string     `gorm:"size:128;index" json:"msg_id,omitempty"`
	RecordID   uint64     `gorm:"index" json:"record_id"`
	UserID     string     `gorm:"size:128;index" json:"user_id,omitempty"`
	Channel    string     `gorm:"size:32" json:"channel,omitempty"`
	Stage      string     `gorm:"size:32;not null;index" json:"stage"`
	Event      string     `gorm:"size:64;not null;index" json:"event"`
	Level      string     `gorm:"size:16;not null;default:info;index" json:"level"`
	Service    string     `gorm:"size:32" json:"service,omitempty"`
	Message    string     `gorm:"size:512" json:"message,omitempty"`
	DetailJSON *string    `gorm:"type:json;column:detail" json:"-"`
	CreatedAt  time.Time  `gorm:"index:idx_trace_time,priority:2;index:idx_trace_main_time,priority:2;index" json:"created_at"`
}

func (TraceEvent) TableName() string { return "trace_events" }

// DetailMap 解析 JSON 详情；失败返回空 map
func (e *TraceEvent) DetailMap() map[string]any {
	if e == nil {
		return nil
	}
	raw := strings.TrimSpace(JSONColumnValue(e.DetailJSON, ""))
	if raw == "" || raw == "null" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}

// TraceEventView 对外响应（展开 detail）
type TraceEventView struct {
	TraceEvent
	Detail map[string]any `json:"detail,omitempty"`
}

func (e TraceEvent) View() TraceEventView {
	return TraceEventView{TraceEvent: e, Detail: e.DetailMap()}
}

// ListTraceQuery 列表筛选
type ListTraceQuery struct {
	TraceID    string `form:"trace_id"`
	BizID      string `form:"biz_id"`
	MainTaskID uint64 `form:"main_task_id"`
	SubTaskID  uint64 `form:"sub_task_id"`
	UserID     string `form:"user_id"`
	Stage      string `form:"stage"`
	Event      string `form:"event"`
	Level      string `form:"level"`
	// AnomalyOnly 仅 error/warn（时间线「仅异常」快捷筛选）
	AnomalyOnly bool   `form:"anomaly_only"`
	Order       string `form:"order"` // asc|desc；默认 desc，时间线用 asc
	Page        int    `form:"page"`
	PageSize    int    `form:"page_size"`
}

// TraceStageStat 单阶段事件计数（时间线顶部概览）
type TraceStageStat struct {
	Stage string `json:"stage"`
	Count int64  `json:"count"`
	Error int64  `json:"error"`
	Warn  int64  `json:"warn"`
}

// TraceStats 一条链路的聚合统计（不拉全量事件）
type TraceStats struct {
	EventCount int64            `json:"event_count"`
	ErrorCount int64            `json:"error_count"`
	WarnCount  int64            `json:"warn_count"`
	Stages     []TraceStageStat `json:"stages,omitempty"`
}

// TraceSummary 一次活动的链路摘要（列表行）
type TraceSummary struct {
	TraceID      string     `json:"trace_id"`
	BizID        string     `json:"biz_id,omitempty"`
	MainTaskID   uint64     `json:"main_task_id,omitempty"`
	Title        string     `json:"title,omitempty"`
	Status       TaskStatus `json:"status,omitempty"`
	EventCount   int64      `json:"event_count"`
	ErrorCount   int64      `json:"error_count"`
	WarnCount    int64      `json:"warn_count"`
	FirstAt      *time.Time `json:"first_at,omitempty"`
	LastAt       *time.Time `json:"last_at,omitempty"`
	LastEvent    string     `json:"last_event,omitempty"`
	LastMessage  string     `json:"last_message,omitempty"`
}

// NewTraceID 生成活动级 trace_id
func NewTraceID() string {
	return "tr_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

// NewRequestID 生成请求级 request_id（HTTP 中间件用，不等于活动 trace）
func NewRequestID() string {
	return "req_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}
