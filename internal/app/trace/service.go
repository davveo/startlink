package trace

import (
	"context"
	"errors"
	"strings"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
	"gorm.io/gorm"
)

// Service 运营台查询全链路时间线
type Service struct {
	repo port.TraceRepository
}

func NewService(repo port.TraceRepository) *Service {
	return &Service{repo: repo}
}

type SummaryListResult struct {
	Items    []domain.TraceSummary `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

// DetailResult 时间线页：头信息 + 当前页事件（分页，避免一次拉成千上万条）
type DetailResult struct {
	TraceID    string                  `json:"trace_id"`
	BizID      string                  `json:"biz_id,omitempty"`
	MainTaskID uint64                  `json:"main_task_id,omitempty"`
	Title      string                  `json:"title,omitempty"`
	Status     domain.TaskStatus       `json:"status,omitempty"`
	EventCount int64                   `json:"event_count"`
	ErrorCount int64                   `json:"error_count"`
	WarnCount  int64                   `json:"warn_count"`
	Stages     []domain.TraceStageStat `json:"stages,omitempty"`
	Events     []domain.TraceEventView `json:"events"`
	// FilteredTotal 当前筛选条件下的事件总数（分页用）
	FilteredTotal int64 `json:"filtered_total"`
	Page          int   `json:"page"`
	PageSize      int   `json:"page_size"`
}

func (s *Service) ListSummaries(ctx context.Context, q domain.ListTraceQuery) (*SummaryListResult, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	items, total, err := s.repo.Summaries(ctx, q)
	if err != nil {
		return nil, err
	}
	return &SummaryListResult{
		Items:    items,
		Total:    total,
		Page:     q.Page,
		PageSize: q.PageSize,
	}, nil
}

// Get 兼容旧调用：默认返回第 1 页、每页 50、正序。
func (s *Service) Get(ctx context.Context, traceID string) (*DetailResult, error) {
	return s.GetTimeline(ctx, domain.ListTraceQuery{
		TraceID:  traceID,
		Order:    "asc",
		Page:     1,
		PageSize: 50,
	})
}

// GetTimeline 时间线详情：元数据走聚合 SQL，事件走分页，禁止全量加载。
func (s *Service) GetTimeline(ctx context.Context, q domain.ListTraceQuery) (*DetailResult, error) {
	traceID := strings.TrimSpace(q.TraceID)
	if traceID == "" {
		return nil, errcode.InvalidParam
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 50
	}
	if q.PageSize > 100 {
		q.PageSize = 100
	}
	if strings.TrimSpace(q.Order) == "" {
		q.Order = "asc"
	}
	q.TraceID = traceID

	main, err := s.repo.GetMainTaskByTraceID(ctx, traceID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	stats, err := s.repo.StatsByTraceID(ctx, traceID)
	if err != nil {
		return nil, err
	}
	if main == nil && (stats == nil || stats.EventCount == 0) {
		return nil, errcode.NotFound
	}

	events, filtered, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, err
	}

	out := &DetailResult{
		TraceID:       traceID,
		Events:        make([]domain.TraceEventView, 0, len(events)),
		FilteredTotal: filtered,
		Page:          q.Page,
		PageSize:      q.PageSize,
	}
	if stats != nil {
		out.EventCount = stats.EventCount
		out.ErrorCount = stats.ErrorCount
		out.WarnCount = stats.WarnCount
		out.Stages = stats.Stages
	}
	if main != nil {
		out.BizID = main.BizID
		out.MainTaskID = main.ID
		out.Title = main.Title
		out.Status = main.Status
	} else if len(events) > 0 {
		out.BizID = events[0].BizID
		out.MainTaskID = events[0].MainTaskID
	}
	for _, ev := range events {
		out.Events = append(out.Events, ev.View())
	}
	return out, nil
}

func (s *Service) ListEvents(ctx context.Context, q domain.ListTraceQuery) ([]domain.TraceEventView, int64, error) {
	items, total, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	views := make([]domain.TraceEventView, 0, len(items))
	for _, ev := range items {
		views = append(views, ev.View())
	}
	return views, total, nil
}
