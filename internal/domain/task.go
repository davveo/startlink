package domain

import (
	"encoding/json"
	"time"
)

// MainTask 主任务：一次营销推送活动
type MainTask struct {
	ID           uint64      `gorm:"primaryKey;autoIncrement" json:"id"`
	BizID        string      `gorm:"size:64;uniqueIndex;not null" json:"biz_id"`
	BizScene     string      `gorm:"size:64;index;not null" json:"biz_scene"`
	Priority     Priority    `gorm:"size:16;not null;index;default:normal" json:"priority"` // high=事务通知 normal=营销
	Title        string      `gorm:"size:256;not null" json:"title"`
	Channel      ChannelType `gorm:"size:32;not null;index" json:"channel"`
	Channels     string      `gorm:"type:json" json:"channels,omitempty"`
	ChannelMode  ChannelMode `gorm:"size:16;not null;default:single" json:"channel_mode"`
	TemplateID   string      `gorm:"size:64" json:"template_id"`
	TemplateBody string      `gorm:"type:text" json:"template_body"`
	// TemplateContents 模板分渠道内容快照
	TemplateContents *string `gorm:"column:template_contents;type:json" json:"-"`
	// MissingVarPolicy 快照自模板
	MissingVarPolicy MissingVarPolicy `gorm:"size:16;not null;default:empty" json:"missing_var_policy"`
	DefaultLocale    string           `gorm:"size:16" json:"default_locale,omitempty"`
	TemplateLocales  *string          `gorm:"column:template_locales;type:json" json:"-"`
	AudienceRef      string           `gorm:"size:128;not null" json:"audience_ref"`
	AudienceExtra    string           `gorm:"type:json" json:"audience_extra,omitempty"`
	Payload          string           `gorm:"type:json" json:"payload,omitempty"`
	TotalCount       int64            `gorm:"not null;default:0" json:"total_count"`
	SuccessCount     int64            `gorm:"not null;default:0" json:"success_count"`
	FailCount        int64            `gorm:"not null;default:0" json:"fail_count"`
	SubTaskTotal     int              `gorm:"not null;default:0" json:"sub_task_total"`
	SubTaskDone      int              `gorm:"not null;default:0" json:"sub_task_done"`
	Status           TaskStatus       `gorm:"size:32;not null;index;default:pending" json:"status"`
	Version          int64            `gorm:"not null;default:0" json:"version"`
	WebhookURL       string           `gorm:"size:512" json:"webhook_url,omitempty"`
	// CreatedBy 业务负责人/创建人（非拆分租约）
	CreatedBy string `gorm:"size:64;index" json:"created_by,omitempty"`
	// CopiedFromID 复制来源主任务
	CopiedFromID *uint64 `gorm:"index" json:"copied_from_id,omitempty"`
	// AudienceRawCount / Filtered / Reachable：拆分或预检写入的漏斗计数（可选）
	AudienceRawCount       int64 `gorm:"not null;default:0" json:"audience_raw_count"`
	AudienceFilteredCount  int64 `gorm:"not null;default:0" json:"audience_filtered_count"`
	AudienceReachableCount int64 `gorm:"not null;default:0" json:"audience_reachable_count"`
	// SendWindowsJSON 分时投放窗 JSON，如 [{"start":"09:00","end":"21:00"}]
	SendWindowsJSON string `gorm:"type:json;column:send_windows" json:"send_windows,omitempty"`
	// PaceQPS 本活动入队速率上限；0 不限制
	PaceQPS int `gorm:"not null;default:0" json:"pace_qps,omitempty"`
	// ExpireAt 活动过期时间；超时消息标记 expired，不调渠道
	ExpireAt *time.Time `gorm:"index" json:"expire_at,omitempty"`
	// ExperimentID / ExperimentSalt / ExperimentControlPercent：实验平台化抽样
	ExperimentID             string `gorm:"size:64;index" json:"experiment_id,omitempty"`
	ExperimentSalt           string `gorm:"size:128" json:"experiment_salt,omitempty"`
	ExperimentControlPercent int    `gorm:"not null;default:0" json:"experiment_control_percent,omitempty"`
	// MaxFallback fallback 模式下最大降级次数（不含首渠）；0=不限制
	MaxFallback int `gorm:"not null;default:0" json:"max_fallback,omitempty"`
	// ChannelRoutesJSON 条件路由规则（channel_mode=conditional）
	ChannelRoutesJSON *string `gorm:"type:json;column:channel_routes" json:"channel_routes,omitempty"`
	// ChannelCostsJSON 渠道成本（channel_mode=cost_priority），如 {"sms":10,"inbox":1}
	ChannelCostsJSON *string `gorm:"type:json;column:channel_costs" json:"channel_costs,omitempty"`
	// SplitOwner / SplitLeaseAt：拆分租约，防止 pending→running 后崩溃导致永久卡单
	SplitOwner   string     `gorm:"size:64;index" json:"split_owner,omitempty"`
	SplitLeaseAt *time.Time `gorm:"index" json:"split_lease_at,omitempty"`
	ScheduledAt  *time.Time `json:"scheduled_at,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (MainTask) TableName() string { return "main_tasks" }

// ChannelRoutes 条件路由规则
func (t *MainTask) ChannelRoutes() []ChannelRouteRule {
	if t == nil {
		return nil
	}
	return ParseChannelRoutesJSON(JSONColumnValue(t.ChannelRoutesJSON, ""))
}

// ChannelCosts 渠道成本表
func (t *MainTask) ChannelCosts() map[ChannelType]int {
	if t == nil {
		return nil
	}
	return ParseChannelCostsJSON(JSONColumnValue(t.ChannelCostsJSON, ""))
}

// SendWindows 解析分时窗
func (t *MainTask) SendWindows() []SendWindow {
	if t.SendWindowsJSON == "" || t.SendWindowsJSON == "null" {
		return nil
	}
	var list []SendWindow
	if err := json.Unmarshal([]byte(t.SendWindowsJSON), &list); err != nil {
		return nil
	}
	return list
}

// ChannelList 解析渠道链
func (t *MainTask) ChannelList() []ChannelType {
	if t.Channels == "" || t.Channels == "null" {
		if t.Channel != "" {
			return []ChannelType{t.Channel}
		}
		return nil
	}
	var list []ChannelType
	if err := json.Unmarshal([]byte(t.Channels), &list); err != nil || len(list) == 0 {
		if t.Channel != "" {
			return []ChannelType{t.Channel}
		}
		return nil
	}
	return list
}

// EffectiveChannelMode 有效渠道模式
func (t *MainTask) EffectiveChannelMode() ChannelMode {
	mode := t.ChannelMode.Normalize()
	if mode == ChannelModeConditional || mode == ChannelModeCostPriority {
		return mode
	}
	chs := t.ChannelList()
	if len(chs) <= 1 {
		return ChannelModeSingle
	}
	if mode == ChannelModeSingle {
		return ChannelModeFallback
	}
	return mode
}

// ContentsMap 解析模板分渠道快照
func (t *MainTask) ContentsMap() map[string]ChannelContent {
	if t == nil {
		return nil
	}
	return ParseContentsJSON(JSONColumnValue(t.TemplateContents, ""))
}

// LocalesMap 解析多语言快照
func (t *MainTask) LocalesMap() map[string]LocaleContent {
	if t == nil {
		return nil
	}
	return ParseLocalesJSON(JSONColumnValue(t.TemplateLocales, ""))
}

// SubTask 子任务：按用户分片，支持多 worker 并发认领
type SubTask struct {
	ID           uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	MainTaskID   uint64     `gorm:"index;not null" json:"main_task_id"`
	ShardIndex   int        `gorm:"not null" json:"shard_index"`
	UserIDs      string     `gorm:"type:mediumtext;not null" json:"user_ids"` // JSON 数组
	TotalCount   int        `gorm:"not null;default:0" json:"total_count"`
	SuccessCount int        `gorm:"not null;default:0" json:"success_count"`
	FailCount    int        `gorm:"not null;default:0" json:"fail_count"`
	Status       TaskStatus `gorm:"size:32;not null;index;default:pending" json:"status"`
	RetryCount   int        `gorm:"not null;default:0" json:"retry_count"`
	WorkerID     string     `gorm:"size:64;index" json:"worker_id,omitempty"` // 认领者，便于水平扩展抢占
	ClaimedAt    *time.Time `json:"claimed_at,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	LastError    string     `gorm:"size:512" json:"last_error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (SubTask) TableName() string { return "sub_tasks" }

// PushRecord 推送流水。唯一键 (main_task_id, user_id, channel) 防重复投递。
// 渠道回执定位：(provider, channel, provider_id)；provider_id 为空时不占唯一约束（MySQL NULL 可多行）。
type PushRecord struct {
	ID         uint64      `gorm:"primaryKey;autoIncrement" json:"id"`
	MainTaskID uint64      `gorm:"uniqueIndex:uk_task_user_channel;not null" json:"main_task_id"`
	SubTaskID  uint64      `gorm:"index;not null" json:"sub_task_id"`
	UserID     string      `gorm:"size:64;uniqueIndex:uk_task_user_channel;not null" json:"user_id"`
	Channel    ChannelType `gorm:"size:32;uniqueIndex:uk_task_user_channel;uniqueIndex:uk_provider_ref;not null" json:"channel"`
	Content    string      `gorm:"type:text" json:"content"`
	Status     PushStatus  `gorm:"size:32;not null;index;default:queued" json:"status"`
	// Provider 渠道供应商标识（默认与 channel 同名，HTTP 适配器可覆盖）
	Provider string `gorm:"size:64;uniqueIndex:uk_provider_ref" json:"provider,omitempty"`
	// ProviderID 渠道侧消息 ID；未发送时为 NULL，避免空串撞唯一键
	ProviderID *string    `gorm:"size:128;uniqueIndex:uk_provider_ref" json:"provider_id,omitempty"`
	ErrorMsg   string     `gorm:"size:512" json:"error_msg,omitempty"`
	IsTest     bool       `gorm:"not null;default:false;index" json:"is_test"`
	// ExperimentGroup control|treatment（实验看板聚合）
	ExperimentGroup string     `gorm:"size:32;index" json:"experiment_group,omitempty"`
	SentAt          *time.Time `json:"sent_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (PushRecord) TableName() string { return "push_records" }

// ExperimentAssignment 实验分组落库（含对照组，便于指标看板）
type ExperimentAssignment struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	MainTaskID   uint64    `gorm:"uniqueIndex:uk_exp_task_user;index;not null" json:"main_task_id"`
	UserID       string    `gorm:"size:64;uniqueIndex:uk_exp_task_user;not null" json:"user_id"`
	ExperimentID string    `gorm:"size:64;index" json:"experiment_id,omitempty"`
	GroupName    string    `gorm:"size:32;not null;index;column:group_name" json:"group"` // control|treatment
	CreatedAt    time.Time `json:"created_at"`
}

func (ExperimentAssignment) TableName() string { return "experiment_assignments" }

// ProviderIDValue 安全读取 provider_id
func (r *PushRecord) ProviderIDValue() string {
	if r == nil || r.ProviderID == nil {
		return ""
	}
	return *r.ProviderID
}

// PushReceipt 回执；(push_record_id, event) 唯一，保证幂等
type PushReceipt struct {
	ID           uint64       `gorm:"primaryKey;autoIncrement" json:"id"`
	PushRecordID uint64       `gorm:"uniqueIndex:uk_receipt_record_event;not null" json:"push_record_id"`
	MainTaskID   uint64       `gorm:"index;not null" json:"main_task_id"`
	SubTaskID    uint64       `gorm:"index;not null" json:"sub_task_id"`
	UserID       string       `gorm:"size:64;index;not null" json:"user_id"`
	Channel      ChannelType  `gorm:"size:32;not null" json:"channel"`
	Event        ReceiptEvent `gorm:"size:32;uniqueIndex:uk_receipt_record_event;not null;index" json:"event"`
	RawPayload   string       `gorm:"type:text" json:"raw_payload,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
}

func (PushReceipt) TableName() string { return "push_receipts" }

// ExportJob 异步导出任务（本地文件，生产可换对象存储）
type ExportJob struct {
	ID         uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	MainTaskID uint64     `gorm:"index;not null" json:"main_task_id"`
	Kind       string     `gorm:"size:32;not null;index" json:"kind"`                   // records | failures
	Status     string     `gorm:"size:32;not null;index;default:pending" json:"status"` // pending|running|success|failed
	FilterJSON *string    `gorm:"type:json" json:"filter_json,omitempty"`
	FilePath   string     `gorm:"size:512" json:"file_path,omitempty"`
	FileURL    string     `gorm:"size:512" json:"file_url,omitempty"`
	RowCount   int64      `gorm:"not null;default:0" json:"row_count"`
	ErrorMsg   string     `gorm:"size:512" json:"error_msg,omitempty"`
	CreatedBy  string     `gorm:"size:64" json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (ExportJob) TableName() string { return "export_jobs" }

const (
	ExportStatusPending = "pending"
	ExportStatusRunning = "running"
	ExportStatusSuccess = "success"
	ExportStatusFailed  = "failed"
	ExportKindRecords   = "records"
	ExportKindFailures  = "failures"
)
