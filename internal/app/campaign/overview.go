package campaign

import (
	"context"
	"time"

	"github.com/starlink/push/internal/domain"
)

// OverviewView 运营台概览
type OverviewView struct {
	CampaignTotal   int64              `json:"campaign_total"`
	ByStatus        map[string]int64   `json:"by_status"`
	ActiveCount     int64              `json:"active_count"` // pending/running/paused/retrying
	SuccessCount    int64              `json:"success_count"`
	PartialCount    int64              `json:"partial_count"`
	FailedCount     int64              `json:"failed_count"`
	CancelledCount  int64              `json:"cancelled_count"`
	DraftCount      int64              `json:"draft_count"`
	LifetimeSuccess int64              `json:"lifetime_success_users"`
	LifetimeFail    int64              `json:"lifetime_fail_users"`
	ExperimentTasks int64              `json:"experiment_tasks"`
	RecentSends     RecentSendStats    `json:"recent_sends"`
	RecentCampaigns []OverviewCampaign `json:"recent_campaigns"`
}

type RecentSendStats struct {
	WindowHours int     `json:"window_hours"`
	Total       int64   `json:"total"`
	Success     int64   `json:"success"`
	Failed      int64   `json:"failed"`
	SuccessRate float64 `json:"success_rate"`
}

type OverviewCampaign struct {
	ID           uint64               `json:"id"`
	BizID        string               `json:"biz_id"`
	BizScene     string               `json:"biz_scene"`
	Title        string               `json:"title"`
	Channel      domain.ChannelType   `json:"channel"`
	Channels     []domain.ChannelType `json:"channels,omitempty"`
	Status       domain.TaskStatus    `json:"status"`
	Priority     domain.Priority      `json:"priority"`
	TotalCount   int64                `json:"total_count"`
	SuccessCount int64                `json:"success_count"`
	FailCount    int64                `json:"fail_count"`
	ExperimentID string               `json:"experiment_id,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
	FinishedAt   *time.Time           `json:"finished_at,omitempty"`
}

// Overview 聚合运营概览（真实库表数据，不造假）
func (s *Service) Overview(ctx context.Context) (*OverviewView, error) {
	byStatus, err := s.tasks.CountMainTasksByStatus(ctx)
	if err != nil {
		return nil, err
	}
	view := &OverviewView{
		ByStatus: make(map[string]int64, len(byStatus)),
	}
	for st, n := range byStatus {
		view.ByStatus[string(st)] = n
		view.CampaignTotal += n
		switch st {
		case domain.TaskStatusPending, domain.TaskStatusRunning, domain.TaskStatusPaused, domain.TaskStatusRetrying:
			view.ActiveCount += n
		case domain.TaskStatusSuccess:
			view.SuccessCount = n
		case domain.TaskStatusPartial:
			view.PartialCount = n
		case domain.TaskStatusFailed:
			view.FailedCount = n
		case domain.TaskStatusCancelled:
			view.CancelledCount = n
		case domain.TaskStatusDraft:
			view.DraftCount = n
		}
	}

	succ, fail, err := s.tasks.SumMainTaskUserCounts(ctx)
	if err != nil {
		return nil, err
	}
	view.LifetimeSuccess = succ
	view.LifetimeFail = fail

	const windowHours = 24
	view.RecentSends.WindowHours = windowHours
	if s.pushRepo != nil {
		stats, err := s.pushRepo.CountRecentSends(ctx, time.Now().Add(-windowHours*time.Hour))
		if err != nil {
			return nil, err
		}
		view.RecentSends.Total = stats.Total
		view.RecentSends.Success = stats.Success
		view.RecentSends.Failed = stats.Failed
		den := stats.Success + stats.Failed
		if den > 0 {
			view.RecentSends.SuccessRate = float64(stats.Success) / float64(den)
		}
	}

	list, _, err := s.tasks.ListMainTasks(ctx, domain.ListCampaignQuery{Page: 1, PageSize: 8})
	if err != nil {
		return nil, err
	}
	recent := make([]OverviewCampaign, 0, len(list))
	for i := range list {
		t := list[i]
		recent = append(recent, OverviewCampaign{
			ID:           t.ID,
			BizID:        t.BizID,
			BizScene:     t.BizScene,
			Title:        t.Title,
			Channel:      t.Channel,
			Channels:     t.ChannelList(),
			Status:       t.Status,
			Priority:     t.Priority.Normalize(),
			TotalCount:   t.TotalCount,
			SuccessCount: t.SuccessCount,
			FailCount:    t.FailCount,
			ExperimentID: t.ExperimentID,
			CreatedAt:    t.CreatedAt,
			FinishedAt:   t.FinishedAt,
		})
	}
	view.RecentCampaigns = recent

	if n, err := s.tasks.CountMainTasksWithExperiment(ctx); err != nil {
		return nil, err
	} else {
		view.ExperimentTasks = n
	}

	return view, nil
}
