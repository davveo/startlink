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
	"github.com/starlink/push/internal/push"
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

type PreviewResult struct {
	RenderedTitle   string                    `json:"rendered_title,omitempty"`
	RenderedContent string                    `json:"rendered_content"`
	MissingVars     []string                  `json:"missing_vars,omitempty"`
	SchemaErrors    []string                  `json:"schema_errors,omitempty"`
	Channel         domain.ChannelType        `json:"channel,omitempty"`
	Locale          string                    `json:"locale,omitempty"`
	Policy          domain.MissingVarPolicy   `json:"missing_var_policy"`
	ByChannel       map[string]ChannelPreview `json:"by_channel,omitempty"`
}

type ChannelPreview struct {
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
}

func (s *Service) Create(ctx context.Context, in domain.CreateTemplateInput) (*domain.Template, error) {
	if in.Name == "" {
		return nil, errcode.InvalidParam
	}
	if !domain.TemplateHasBody(in.Body, in.Contents) {
		return nil, errcode.New(40001, "body or contents required")
	}
	if in.ChannelHint != "" && !in.ChannelHint.Valid() {
		return nil, errcode.InvalidParam
	}
	if !in.MissingVarPolicy.Valid() {
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

	policy := in.MissingVarPolicy.Normalize()
	tpl := &domain.Template{
		Code:             in.Code,
		Name:             in.Name,
		Body:             in.Body,
		Contents:         in.Contents,
		VarSchema:        in.VarSchema,
		MissingVarPolicy: policy,
		DefaultLocale:    in.DefaultLocale,
		Locales:          in.Locales,
		BizScene:         in.BizScene,
		ChannelHint:      in.ChannelHint,
		Status:           domain.TemplateStatusDraft,
		Version:          0,
		Revision:         0,
		CreatedBy:        in.CreatedBy,
		UpdatedBy:        in.CreatedBy,
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

func (s *Service) saveSnapshot(ctx context.Context, tpl *domain.Template, note, operator string) error {
	ver := domain.SnapshotFromTemplate(tpl, note, operator)
	return s.templates.CreateVersion(ctx, ver)
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
		tpl.Body = *in.Body
	}
	if in.Contents != nil {
		tpl.Contents = *in.Contents
	}
	if in.VarSchema != nil {
		tpl.VarSchema = *in.VarSchema
	}
	if in.MissingVarPolicy != nil {
		if !in.MissingVarPolicy.Valid() {
			return nil, errcode.InvalidParam
		}
		tpl.MissingVarPolicy = in.MissingVarPolicy.Normalize()
	}
	if in.DefaultLocale != nil {
		tpl.DefaultLocale = *in.DefaultLocale
	}
	if in.Locales != nil {
		tpl.Locales = *in.Locales
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
	if !domain.TemplateHasBody(tpl.Body, tpl.Contents) {
		return nil, errcode.New(40001, "body or contents required")
	}
	if in.UpdatedBy != "" {
		tpl.UpdatedBy = in.UpdatedBy
	}
	tpl.RejectReason = ""
	tpl.Revision++
	if err := s.updateCAS(ctx, tpl, in.Version); err != nil {
		return nil, err
	}
	_ = s.saveSnapshot(ctx, tpl, "update", tpl.UpdatedBy)
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
	tpl.Revision++
	if err := s.updateCAS(ctx, tpl, in.Version); err != nil {
		return nil, err
	}
	_ = s.saveSnapshot(ctx, tpl, "approve", in.ReviewedBy)
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

func (s *Service) ListVersions(ctx context.Context, id uint64) ([]domain.TemplateVersion, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.templates.ListVersions(ctx, id, 50)
}

func (s *Service) Rollback(ctx context.Context, id uint64, in domain.RollbackTemplateInput) (*domain.Template, error) {
	tpl, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	ver, err := s.templates.GetVersion(ctx, id, in.Revision)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	tpl.Name = ver.Name
	tpl.Body = ver.Body
	tpl.Contents = ver.Contents
	tpl.VarSchema = ver.VarSchema
	tpl.MissingVarPolicy = ver.MissingVarPolicy.Normalize()
	tpl.DefaultLocale = ver.DefaultLocale
	tpl.Locales = ver.Locales
	tpl.BizScene = ver.BizScene
	tpl.ChannelHint = ver.ChannelHint
	tpl.Status = domain.TemplateStatusDraft
	tpl.RejectReason = ""
	if in.UpdatedBy != "" {
		tpl.UpdatedBy = in.UpdatedBy
	}
	tpl.Revision++
	if err := s.updateCAS(ctx, tpl, in.Version); err != nil {
		return nil, err
	}
	_ = s.saveSnapshot(ctx, tpl, fmt.Sprintf("rollback:%d", in.Revision), tpl.UpdatedBy)
	return tpl, nil
}

func (s *Service) Preview(ctx context.Context, in domain.PreviewTemplateInput) (*PreviewResult, error) {
	var (
		body      string
		contents  map[string]domain.ChannelContent
		schema    []domain.VarDef
		policy    domain.MissingVarPolicy
		defLocale string
		locales   map[string]domain.LocaleContent
	)
	if in.TemplateID != "" {
		tpl, err := s.GetByCode(ctx, in.TemplateID)
		if err != nil {
			return nil, err
		}
		body = tpl.Body
		contents = tpl.Contents
		schema = tpl.VarSchema
		policy = tpl.MissingVarPolicy
		defLocale = tpl.DefaultLocale
		locales = tpl.Locales
	} else {
		body = in.Body
		contents = in.Contents
		schema = in.VarSchema
		policy = in.MissingVarPolicy
		defLocale = in.DefaultLocale
		locales = in.Locales
	}
	if in.MissingVarPolicy != "" {
		policy = in.MissingVarPolicy
	}
	if !policy.Valid() {
		return nil, errcode.InvalidParam
	}
	policy = policy.Normalize()
	if !domain.TemplateHasBody(body, contents) && len(locales) == 0 {
		return nil, errcode.InvalidParam
	}

	vars := in.Vars
	if vars == nil {
		vars = map[string]string{}
	}
	vars, schemaErrs := domain.ValidateVarsAgainstSchema(schema, vars)
	defaults := domain.DefaultsFromSchema(schema)

	body, contents = domain.ResolveLocaleContent(body, contents, defLocale, locales, in.Locale)

	chs := []domain.ChannelType{}
	if in.Channel != "" {
		chs = append(chs, in.Channel)
	} else {
		for k := range contents {
			chs = append(chs, domain.ChannelType(k))
		}
		if len(chs) == 0 {
			chs = []domain.ChannelType{""}
		}
	}

	out := &PreviewResult{
		MissingVars:  push.MissingVars(body, vars),
		SchemaErrors: schemaErrs,
		Locale:       in.Locale,
		Policy:       policy,
		ByChannel:    map[string]ChannelPreview{},
	}

	renderOne := func(title, content string) (string, string, error) {
		rt, err1 := push.RenderTemplateWithPolicy(title, vars, policy, defaults)
		rc, err2 := push.RenderTemplateWithPolicy(content, vars, policy, defaults)
		if err1 != nil {
			return rt, rc, err1
		}
		return rt, rc, err2
	}

	primaryCh := in.Channel
	if primaryCh == "" && len(chs) > 0 {
		primaryCh = chs[0]
	}
	title, content, extra := domain.ResolveChannelContent(primaryCh, in.Title, body, contents)
	_ = extra
	rt, rc, err := renderOne(title, content)
	if err != nil {
		return nil, errcode.New(40001, err.Error())
	}
	out.RenderedTitle = rt
	out.RenderedContent = rc
	out.Channel = primaryCh
	out.MissingVars = uniqueStrings(append(out.MissingVars, push.MissingVars(content, vars)...))

	for _, ch := range chs {
		t, c, _ := domain.ResolveChannelContent(ch, in.Title, body, contents)
		rt2, rc2, err := renderOne(t, c)
		if err != nil {
			return nil, errcode.New(40001, err.Error())
		}
		key := string(ch)
		if key == "" {
			key = "_default"
		}
		out.ByChannel[key] = ChannelPreview{Title: rt2, Content: rc2}
	}
	if len(schemaErrs) > 0 && policy == domain.MissingVarError {
		return nil, errcode.New(40001, "var_schema: "+strings.Join(schemaErrs, "; "))
	}
	return out, nil
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func FormatCode(id uint64) string {
	return fmt.Sprintf("tpl_%d", id)
}
