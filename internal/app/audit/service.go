package audit

import (
	"context"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

type Service struct {
	repo port.AuditLogRepository
}

func NewService(repo port.AuditLogRepository) *Service {
	return &Service{repo: repo}
}

type ListResult struct {
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Items    []domain.AuditLog `json:"items"`
}

func (s *Service) List(ctx context.Context, q domain.ListAuditLogQuery) (*ListResult, error) {
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

func (s *Service) Record(ctx context.Context, log *domain.AuditLog) error {
	if s == nil || s.repo == nil || log == nil {
		return nil
	}
	return s.repo.Create(ctx, log)
}
