package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	ScheduleStatusActive   = "active"
	ScheduleStatusPaused   = "paused"
	ScheduleStatusFinished = "finished"
)

const (
	ScheduleRunSuccess = "success"
	ScheduleRunFailed  = "failed"
	ScheduleRunSkipped = "skipped"
)

// CampaignSchedule 周期性活动：按 cron 规则反复派生一次性活动。
// 本身不投放，只负责在到点时用 Payload 模板创建新的 MainTask。
type CampaignSchedule struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	Code     string `gorm:"size:64;uniqueIndex;not null" json:"code"`
	Name     string `gorm:"size:128;not null" json:"name"`
	Status   string `gorm:"size:16;not null;index;default:active" json:"status"`
	CronExpr string `gorm:"size:128;not null;column:cron_expr" json:"cron_expr"`
	// Timezone cron 求值时区；空为服务器本地时区
	Timezone string `gorm:"size:64" json:"timezone,omitempty"`
	// PayloadJSON CreateCampaignInput 模板；biz_id 由每次触发按 BizIDPrefix 生成
	PayloadJSON string `gorm:"type:json;column:payload;not null" json:"-"`
	// BizIDPrefix 派生活动 biz_id 前缀；空则用 code
	BizIDPrefix string     `gorm:"size:64" json:"biz_id_prefix,omitempty"`
	StartAt     *time.Time `json:"start_at,omitempty"`
	EndAt       *time.Time `json:"end_at,omitempty"`
	// MaxRuns 最大触发次数；0 不限
	MaxRuns   int64      `gorm:"not null;default:0" json:"max_runs,omitempty"`
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	// NextRunAt 预计算的下次触发时刻，调度器据此扫描
	NextRunAt *time.Time `gorm:"index" json:"next_run_at,omitempty"`
	RunCount  int64      `gorm:"not null;default:0" json:"run_count"`
	FailCount int64      `gorm:"not null;default:0" json:"fail_count"`
	LastError string     `gorm:"size:512" json:"last_error,omitempty"`
	// Owner / LeaseAt 触发租约，避免多实例重复派生
	Owner     string     `gorm:"size:64" json:"-"`
	LeaseAt   *time.Time `json:"-"`
	CreatedBy string     `gorm:"size:64" json:"created_by,omitempty"`
	UpdatedBy string     `gorm:"size:64" json:"updated_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (CampaignSchedule) TableName() string { return "campaign_schedules" }

// Location cron 求值时区
func (s *CampaignSchedule) Location() *time.Location {
	if s == nil {
		return time.Local
	}
	return LoadLocation(s.Timezone)
}

// Cron 解析 cron 表达式
func (s *CampaignSchedule) Cron() (*CronSchedule, error) {
	if s == nil {
		return nil, fmt.Errorf("schedule is nil")
	}
	return ParseCron(s.CronExpr)
}

// Payload 解析活动模板
func (s *CampaignSchedule) Payload() (*CreateCampaignInput, error) {
	if s == nil || strings.TrimSpace(s.PayloadJSON) == "" {
		return nil, fmt.Errorf("schedule payload is empty")
	}
	var in CreateCampaignInput
	if err := json.Unmarshal([]byte(s.PayloadJSON), &in); err != nil {
		return nil, fmt.Errorf("解析周期活动模板失败: %w", err)
	}
	return &in, nil
}

// ComputeNext 计算 after 之后的下次触发；超出 end_at / max_runs 返回零值。
func (s *CampaignSchedule) ComputeNext(after time.Time) (time.Time, error) {
	cron, err := s.Cron()
	if err != nil {
		return time.Time{}, err
	}
	loc := s.Location()
	if s.StartAt != nil && after.Before(*s.StartAt) {
		// 首次触发不能早于生效时间
		after = s.StartAt.Add(-time.Minute)
	}
	next := cron.Next(after.In(loc))
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("cron 表达式 %q 无可用触发时刻", s.CronExpr)
	}
	if s.EndAt != nil && next.After(*s.EndAt) {
		return time.Time{}, nil
	}
	if s.MaxRuns > 0 && s.RunCount >= s.MaxRuns {
		return time.Time{}, nil
	}
	return next, nil
}

// Exhausted 是否已达终止条件
func (s *CampaignSchedule) Exhausted(now time.Time) bool {
	if s == nil {
		return true
	}
	if s.MaxRuns > 0 && s.RunCount >= s.MaxRuns {
		return true
	}
	if s.EndAt != nil && now.After(*s.EndAt) {
		return true
	}
	return false
}

// BizIDFor 生成本次触发的活动 biz_id；同一 planned 时刻结果稳定，
// 配合 main_tasks.biz_id 唯一键形成第二道幂等防线。
func (s *CampaignSchedule) BizIDFor(planned time.Time) string {
	prefix := strings.TrimSpace(s.BizIDPrefix)
	if prefix == "" {
		prefix = s.Code
	}
	return fmt.Sprintf("%s-%s", prefix, planned.In(s.Location()).Format("20060102T1504"))
}

// CampaignScheduleRun 周期触发流水。(schedule_id, planned_at) 唯一，
// 是多实例并发下「同一时刻只派生一次」的权威保证。
type CampaignScheduleRun struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ScheduleID uint64    `gorm:"index;not null;uniqueIndex:uk_schedule_planned" json:"schedule_id"`
	PlannedAt  time.Time `gorm:"not null;uniqueIndex:uk_schedule_planned" json:"planned_at"`
	BizID      string    `gorm:"size:64;index" json:"biz_id,omitempty"`
	MainTaskID uint64    `gorm:"index" json:"main_task_id,omitempty"`
	Status     string    `gorm:"size:16;not null;index" json:"status"`
	ErrorMsg   string    `gorm:"size:512" json:"error_msg,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func (CampaignScheduleRun) TableName() string { return "campaign_schedule_runs" }

// ScheduleInput 创建/更新周期活动
type ScheduleInput struct {
	Code        string              `json:"code,omitempty"`
	Name        string              `json:"name" binding:"required"`
	CronExpr    string              `json:"cron_expr" binding:"required"`
	Timezone    string              `json:"timezone,omitempty"`
	BizIDPrefix string              `json:"biz_id_prefix,omitempty"`
	StartAt     *time.Time          `json:"start_at,omitempty"`
	EndAt       *time.Time          `json:"end_at,omitempty"`
	MaxRuns     int64               `json:"max_runs,omitempty"`
	Status      string              `json:"status,omitempty"`
	Payload     CreateCampaignInput `json:"payload"`
	Operator    string              `json:"operator,omitempty"`
}

// ListScheduleQuery 周期活动列表
type ListScheduleQuery struct {
	Status   string `form:"status"`
	Keyword  string `form:"keyword"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// ListScheduleRunQuery 触发流水查询
type ListScheduleRunQuery struct {
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}
