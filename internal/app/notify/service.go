package notify

import (
	"context"
	"log/slog"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// Service 站内通知应用服务
type Service struct {
	repo port.NotificationRepository
	hub  *Hub
}

func NewService(repo port.NotificationRepository, hub *Hub) *Service {
	return &Service{repo: repo, hub: hub}
}

func (s *Service) Hub() *Hub {
	if s == nil {
		return nil
	}
	return s.hub
}

type ListResult struct {
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	Items    []domain.Notification `json:"items"`
}

func (s *Service) List(ctx context.Context, q domain.ListNotificationQuery) (*ListResult, error) {
	page := q.Page
	if page <= 0 {
		page = 1
	}
	size := q.PageSize
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	q.Page, q.PageSize = page, size
	list, total, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, err
	}
	return &ListResult{Total: total, Page: page, PageSize: size, Items: list}, nil
}

func (s *Service) UnreadCount(ctx context.Context) (int64, error) {
	return s.repo.CountUnread(ctx)
}

func (s *Service) MarkRead(ctx context.Context, id uint64) error {
	_, err := s.repo.MarkRead(ctx, id)
	return err
}

func (s *Service) MarkAllRead(ctx context.Context) (int64, error) {
	return s.repo.MarkAllRead(ctx)
}

// EmitTaskTerminal 主任务进入终态时写入站内通知（失败仅记日志）
func (s *Service) EmitTaskTerminal(ctx context.Context, task *domain.MainTask, status domain.TaskStatus) {
	if s == nil || s.repo == nil || task == nil {
		return
	}
	n := domain.NewTaskTerminalNotification(task, status)
	if n == nil {
		return
	}
	if err := s.repo.Create(ctx, n); err != nil {
		slog.Warn("create notification failed", "task_id", task.ID, "status", status, "err", err)
	}
}
