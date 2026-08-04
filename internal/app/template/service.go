package template

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/starlink/push/internal/adapter/repo"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
	"gorm.io/gorm"
)

type Service struct {
	templates port.TemplateRepository
}

func NewService(templates port.TemplateRepository) *Service {
	return &Service{templates: templates}
}

type ListResult struct {
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Size  int               `json:"page_size"`
	Items []domain.Template `json:"items"`
}

func (s *Service) Create(ctx context.Context, in domain.CreateTemplateInput) (*domain.Template, error) {
	if in.Name == "" || in.Body == "" {
		return nil, errcode.InvalidParam
	}
	if in.ChannelHint != "" && !in.ChannelHint.Valid() {
		return nil, errcode.InvalidParam
	}
	if in.Code != "" {
		exist, err := s.templates.GetByCode(ctx, in.Code)
		if err == nil && exist != nil {
			return nil, errcode.Conflict
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	tpl := &domain.Template{
		Code:        in.Code,
		Name:        in.Name,
		Body:        in.Body,
		BizScene:    in.BizScene,
		ChannelHint: in.ChannelHint,
		Status:      domain.TemplateStatusDraft,
		Version:     0,
		CreatedBy:   in.CreatedBy,
		UpdatedBy:   in.CreatedBy,
	}
	if err := s.templates.Create(ctx, tpl); err != nil {
		return nil, err
	}
	if in.Code == "" || strings.HasPrefix(tpl.Code, "tmp_") {
		repo.EnsureTemplateCode(tpl)
		ok, err := s.templates.UpdateCAS(ctx, tpl, tpl.Version)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errcode.Conflict
		}
	}
	return tpl, nil
}

func (s *Service) Get(ctx context.Context, id uint64) (*domain.Template, error) {
	tpl, err := s.templates.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	return tpl, nil
}

func (s *Service) GetByCode(ctx context.Context, code string) (*domain.Template, error) {
	tpl, err := s.templates.GetByCode(ctx, code)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	return tpl, nil
}

func (s *Service) GetApprovedByCode(ctx context.Context, code string) (*domain.Template, error) {
	tpl, err := s.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if !tpl.Status.Usable() {
		return nil, errcode.TemplateNotUsable
	}
	return tpl, nil
}

func (s *Service) updateCAS(ctx context.Context, tpl *domain.Template, expected *int64) error {
	ver := tpl.Version
	if expected != nil {
		ver = *expected
	}
	ok, err := s.templates.UpdateCAS(ctx, tpl, ver)
	if err != nil {
		return err
	}
	if !ok {
		return errcode.Conflict
	}
	return nil
}

func (s *Service) Update(ctx context.Context, id uint64, in domain.UpdateTemplateInput) (*domain.Template, error) {
	tpl, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !tpl.Status.Editable() {
		return nil, errcode.InvalidState
	}
	if in.Name != nil {
		if *in.Name == "" {
			return nil, errcode.InvalidParam
		}
		tpl.Name = *in.Name
	}
	if in.Body != nil {
		if *in.Body == "" {
			return nil, errcode.InvalidParam
		}
		tpl.Body = *in.Body
	}
	if in.BizScene != nil {
		tpl.BizScene = *in.BizScene
	}
	if in.ChannelHint != nil {
		if *in.ChannelHint != "" && !in.ChannelHint.Valid() {
			return nil, errcode.InvalidParam
		}
		tpl.ChannelHint = *in.ChannelHint
	}
	if in.UpdatedBy != "" {
		tpl.UpdatedBy = in.UpdatedBy
	}
	tpl.RejectReason = ""
	if err := s.updateCAS(ctx, tpl, in.Version); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s *Service) Delete(ctx context.Context, id uint64) error {
	tpl, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if tpl.Status != domain.TemplateStatusDraft && tpl.Status != domain.TemplateStatusRejected {
		return errcode.InvalidState
	}
	return s.templates.Delete(ctx, id)
}

func (s *Service) List(ctx context.Context, q domain.ListTemplateQuery) (*ListResult, error) {
	if q.Status != "" && !q.Status.Valid() {
		return nil, errcode.InvalidParam
	}
	list, total, err := s.templates.List(ctx, q)
	if err != nil {
		return nil, err
	}
	page := q.Page
	if page <= 0 {
		page = 1
	}
	size := q.PageSize
	if size <= 0 {
		size = 20
	}
	return &ListResult{Total: total, Page: page, Size: size, Items: list}, nil
}

func (s *Service) Submit(ctx context.Context, id uint64, operator string) (*domain.Template, error) {
	tpl, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !tpl.Status.CanSubmit() {
		return nil, errcode.InvalidState
	}
	tpl.Status = domain.TemplateStatusPendingReview
	tpl.RejectReason = ""
	if operator != "" {
		tpl.UpdatedBy = operator
	}
	if err := s.updateCAS(ctx, tpl, nil); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s *Service) Approve(ctx context.Context, id uint64, in domain.ReviewTemplateInput) (*domain.Template, error) {
	tpl, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !tpl.Status.CanReview() {
		return nil, errcode.InvalidState
	}
	now := time.Now()
	tpl.Status = domain.TemplateStatusApproved
	tpl.RejectReason = ""
	tpl.ReviewedBy = in.ReviewedBy
	tpl.ReviewedAt = &now
	if err := s.updateCAS(ctx, tpl, in.Version); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s *Service) Reject(ctx context.Context, id uint64, in domain.ReviewTemplateInput) (*domain.Template, error) {
	tpl, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !tpl.Status.CanReview() {
		return nil, errcode.InvalidState
	}
	if in.RejectReason == "" {
		return nil, errcode.InvalidParam
	}
	now := time.Now()
	tpl.Status = domain.TemplateStatusRejected
	tpl.RejectReason = in.RejectReason
	tpl.ReviewedBy = in.ReviewedBy
	tpl.ReviewedAt = &now
	if err := s.updateCAS(ctx, tpl, in.Version); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s *Service) Disable(ctx context.Context, id uint64, operator string) (*domain.Template, error) {
	tpl, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if tpl.Status != domain.TemplateStatusApproved {
		return nil, errcode.InvalidState
	}
	tpl.Status = domain.TemplateStatusDisabled
	if operator != "" {
		tpl.UpdatedBy = operator
	}
	if err := s.updateCAS(ctx, tpl, nil); err != nil {
		return nil, err
	}
	return tpl, nil
}

func (s *Service) Enable(ctx context.Context, id uint64, operator string) (*domain.Template, error) {
	tpl, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if tpl.Status != domain.TemplateStatusDisabled {
		return nil, errcode.InvalidState
	}
	tpl.Status = domain.TemplateStatusPendingReview
	if operator != "" {
		tpl.UpdatedBy = operator
	}
	if err := s.updateCAS(ctx, tpl, nil); err != nil {
		return nil, err
	}
	return tpl, nil
}

func FormatCode(id uint64) string {
	return fmt.Sprintf("tpl_%d", id)
}
