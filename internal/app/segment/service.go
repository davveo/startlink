package segment

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
	"gorm.io/gorm"
)

// AudienceResolver 人群解析（audience.Registry 已满足）。
// 刷新成员数与排除段展开都走它，避免 service 直接依赖整个 audience 包。
type AudienceResolver interface {
	Resolve(ctx context.Context, q domain.AudienceQuery) (*domain.AudiencePage, error)
}

const (
	// resolvePageSize 翻页拉人群的单页大小
	resolvePageSize = 1000
	// maxRefreshPages / maxRefreshUsers 刷新成员数的翻页上限。
	// 触顶后停止并把结果标为估算值——一次后台刷新不值得把上游圈人服务打穿。
	maxRefreshPages = 50
	maxRefreshUsers = 200_000
	// maxExcludePages / maxExcludeUsers 排除段展开上限。
	// 这里触顶必须报错：只剔除一部分等于对剩下的人误发，比整单失败更糟。
	maxExcludePages = 50
	maxExcludeUsers = 200_000

	maxUserIDLen        = 64
	maxSegmentCodeLen   = 64
	maxSuppressionBatch = 5000
)

type Service struct {
	segments    port.SegmentRepository
	suppression port.SuppressionRepository
	store       port.SuppressionStore
	resolver    AudienceResolver
}

func NewService(
	segments port.SegmentRepository,
	suppression port.SuppressionRepository,
	store port.SuppressionStore,
	resolver AudienceResolver,
) *Service {
	return &Service{segments: segments, suppression: suppression, store: store, resolver: resolver}
}

// SegmentListResult 列表响应体（与其它模块统一 items/total/page/page_size）
type SegmentListResult struct {
	Items    []domain.AudienceSegment `json:"items"`
	Total    int64                    `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"page_size"`
}

// SegmentView 人群段 + 引用计数（列表页要显示「被几个活动引用」以决定能不能删）
type SegmentView struct {
	*domain.AudienceSegment
	CampaignRefs int64 `json:"campaign_refs"`
}

// RefreshResult 刷新成员数结果
type RefreshResult struct {
	Segment *domain.AudienceSegment `json:"segment"`
	// MemberCount 本次统计到的成员数
	MemberCount int64 `json:"member_count"`
	// Estimated true 表示触顶提前结束，成员数是下界估算值
	Estimated bool   `json:"estimated"`
	Error     string `json:"error,omitempty"`
}

func (s *Service) ListSegments(ctx context.Context, q domain.ListSegmentQuery) (*SegmentListResult, error) {
	if q.Kind != "" && !q.Kind.Valid() {
		return nil, errcode.InvalidParam
	}
	if q.Status != "" && !validStatus(q.Status) {
		return nil, errcode.InvalidParam
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 100 {
		q.PageSize = 100
	}
	list, total, err := s.segments.List(ctx, q)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []domain.AudienceSegment{}
	}
	return &SegmentListResult{Items: list, Total: total, Page: q.Page, PageSize: q.PageSize}, nil
}

// GetSegment 返回人群段及其活动引用数
func (s *Service) GetSegment(ctx context.Context, code string) (*SegmentView, error) {
	seg, err := s.mustGet(ctx, code)
	if err != nil {
		return nil, err
	}
	refs, err := s.segments.CountCampaignRefs(ctx, seg.Code)
	if err != nil {
		return nil, err
	}
	return &SegmentView{AudienceSegment: seg, CampaignRefs: refs}, nil
}

// LookupSegment 只取人群段实体，不附带引用计数。
// 创建活动的热路径用它，省掉一次 CountCampaignRefs 的全表统计。
func (s *Service) LookupSegment(ctx context.Context, code string) (*domain.AudienceSegment, error) {
	return s.mustGet(ctx, code)
}

func (s *Service) CreateSegment(ctx context.Context, in domain.SegmentInput) (*domain.AudienceSegment, error) {
	name := strings.TrimSpace(in.Name)
	bizScene := strings.TrimSpace(in.BizScene)
	audienceRef := strings.TrimSpace(in.AudienceRef)
	if name == "" || bizScene == "" || audienceRef == "" {
		return nil, errcode.New(40001, "name / biz_scene / audience_ref 均不能为空")
	}
	if !in.Kind.Valid() {
		return nil, errcode.New(40001, "kind 只能是 include 或 exclude")
	}
	status, err := normalizeStatus(in.Status)
	if err != nil {
		return nil, err
	}

	code := strings.TrimSpace(in.Code)
	if code == "" {
		code = generateCode(name)
	}
	if err := validateCode(code); err != nil {
		return nil, err
	}
	exist, err := s.segments.GetByCode(ctx, code)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if exist != nil {
		return nil, errcode.New(40901, fmt.Sprintf("人群段 code 已存在：%s", code))
	}

	seg := &domain.AudienceSegment{
		Code:        code,
		Name:        name,
		Kind:        in.Kind.Normalize(),
		BizScene:    bizScene,
		AudienceRef: audienceRef,
		Description: strings.TrimSpace(in.Description),
		Status:      status,
		CreatedBy:   in.Operator,
		UpdatedBy:   in.Operator,
	}
	if len(in.AudienceExtra) > 0 {
		seg.AudienceExtraJSON = domain.MarshalJSONColumn(in.AudienceExtra, false)
	}
	if err := s.segments.Create(ctx, seg); err != nil {
		if isDuplicateErr(err) {
			return nil, errcode.New(40901, fmt.Sprintf("人群段 code 已存在：%s", code))
		}
		return nil, err
	}
	return seg, nil
}

func (s *Service) UpdateSegment(ctx context.Context, code string, in domain.SegmentInput) (*domain.AudienceSegment, error) {
	seg, err := s.mustGet(ctx, code)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	bizScene := strings.TrimSpace(in.BizScene)
	audienceRef := strings.TrimSpace(in.AudienceRef)
	if name == "" || bizScene == "" || audienceRef == "" {
		return nil, errcode.New(40001, "name / biz_scene / audience_ref 均不能为空")
	}
	if !in.Kind.Valid() {
		return nil, errcode.New(40001, "kind 只能是 include 或 exclude")
	}
	status, err := normalizeStatus(in.Status)
	if err != nil {
		return nil, err
	}

	fields := map[string]any{
		"name":         name,
		"kind":         in.Kind.Normalize(),
		"biz_scene":    bizScene,
		"audience_ref": audienceRef,
		"description":  strings.TrimSpace(in.Description),
		"status":       status,
	}
	if in.Operator != "" {
		fields["updated_by"] = in.Operator
	}
	// AudienceSegment.AudienceExtraJSON 是 json:"-"，调用方读不回旧值，
	// 所以空 audience_extra 一律按「不改」处理，避免改个名字把圈人参数抹掉。
	if len(in.AudienceExtra) > 0 {
		fields["audience_extra"] = domain.MarshalJSONColumn(in.AudienceExtra, false)
	}
	if err := s.segments.Update(ctx, seg.Code, fields); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	return s.mustGet(ctx, seg.Code)
}

func (s *Service) DeleteSegment(ctx context.Context, code string) error {
	seg, err := s.mustGet(ctx, code)
	if err != nil {
		return err
	}
	refs, err := s.segments.CountCampaignRefs(ctx, seg.Code)
	if err != nil {
		return err
	}
	if refs > 0 {
		return errcode.New(40901, fmt.Sprintf("仍有 %d 个活动引用该人群段，请先解除引用再删除", refs))
	}
	if err := s.segments.Delete(ctx, seg.Code); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.NotFound
		}
		return err
	}
	return nil
}

// RefreshSegment 重新翻页统计成员数。上游圈人失败只写进 RefreshError，
// 不让一次统计失败把整个人群段的管理操作也带崩。
func (s *Service) RefreshSegment(ctx context.Context, code, operator string) (*RefreshResult, error) {
	seg, err := s.mustGet(ctx, code)
	if err != nil {
		return nil, err
	}
	if s.resolver == nil {
		return nil, errcode.New(50001, "未配置人群解析器，无法刷新成员数")
	}

	count, estimated, resolveErr := s.countMembers(ctx, seg)
	now := time.Now()
	fields := map[string]any{"counted_at": now}
	if operator != "" {
		fields["updated_by"] = operator
	}

	out := &RefreshResult{MemberCount: count, Estimated: estimated}
	switch {
	case resolveErr != nil:
		out.Error = truncate(resolveErr.Error(), 500)
		fields["refresh_error"] = out.Error
	case estimated:
		// 估算值没有独立字段可放，借 refresh_error 让运营台看到「这个数是下界」
		note := fmt.Sprintf("已达刷新上限（%d 页 / %d 人），成员数为估算下界", maxRefreshPages, maxRefreshUsers)
		out.Error = note
		fields["member_count"] = count
		fields["refresh_error"] = note
	default:
		fields["member_count"] = count
		fields["refresh_error"] = ""
	}
	if err := s.segments.Update(ctx, seg.Code, fields); err != nil {
		return nil, err
	}
	updated, err := s.mustGet(ctx, seg.Code)
	if err != nil {
		return nil, err
	}
	out.Segment = updated
	return out, nil
}

// ResolveExcludeUserIDs 展开排除段的全部用户 ID，供调度器拆分时剔除。
// 触顶宁可返回错误：只剔一半等于对剩下的人误发。
func (s *Service) ResolveExcludeUserIDs(ctx context.Context, code string) (map[string]struct{}, error) {
	seg, err := s.mustGet(ctx, code)
	if err != nil {
		return nil, err
	}
	if !seg.Active() {
		return nil, errcode.New(40902, fmt.Sprintf("排除人群段 %s 已停用，无法用于剔除", seg.Code))
	}
	if s.resolver == nil {
		return nil, errcode.New(50001, "未配置人群解析器，无法展开排除名单")
	}

	out := make(map[string]struct{})
	query := domain.AudienceQuery{
		AudienceRef: seg.AudienceRef,
		BizScene:    seg.BizScene,
		Extra:       seg.ExtraMap(),
		PageSize:    resolvePageSize,
	}
	for pageNo := 0; ; pageNo++ {
		if pageNo >= maxExcludePages {
			return nil, errcode.New(40005, fmt.Sprintf(
				"排除人群段 %s 超过 %d 页上限，无法完整展开；请缩小人群或改用抑制名单", seg.Code, maxExcludePages))
		}
		page, err := s.resolver.Resolve(ctx, query)
		if err != nil {
			return nil, err
		}
		if page == nil {
			break
		}
		for _, u := range page.Users {
			if u.UserID == "" {
				continue
			}
			out[u.UserID] = struct{}{}
		}
		if len(out) > maxExcludeUsers {
			return nil, errcode.New(40005, fmt.Sprintf(
				"排除人群段 %s 超过 %d 人上限，无法完整展开；请缩小人群或改用抑制名单", seg.Code, maxExcludeUsers))
		}
		if !page.HasMore || page.NextPageToken == "" {
			break
		}
		query.PageToken = page.NextPageToken
	}
	return out, nil
}

// countMembers 翻页累计成员数；estimated=true 表示触顶提前结束
func (s *Service) countMembers(ctx context.Context, seg *domain.AudienceSegment) (count int64, estimated bool, err error) {
	query := domain.AudienceQuery{
		AudienceRef: seg.AudienceRef,
		BizScene:    seg.BizScene,
		Extra:       seg.ExtraMap(),
		PageSize:    resolvePageSize,
	}
	seen := make(map[string]struct{})
	for pageNo := 0; pageNo < maxRefreshPages; pageNo++ {
		page, err := s.resolver.Resolve(ctx, query)
		if err != nil {
			return count, false, err
		}
		if page == nil {
			return count, false, nil
		}
		for _, u := range page.Users {
			if u.UserID == "" {
				continue
			}
			if _, dup := seen[u.UserID]; dup {
				continue
			}
			seen[u.UserID] = struct{}{}
		}
		count = int64(len(seen))
		if count >= maxRefreshUsers {
			return count, true, nil
		}
		if !page.HasMore || page.NextPageToken == "" {
			return count, false, nil
		}
		query.PageToken = page.NextPageToken
	}
	return count, true, nil
}

func (s *Service) mustGet(ctx context.Context, code string) (*domain.AudienceSegment, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errcode.InvalidParam
	}
	seg, err := s.segments.GetByCode(ctx, code)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.NotFound
		}
		return nil, err
	}
	if seg == nil {
		return nil, errcode.NotFound
	}
	return seg, nil
}

func validStatus(status string) bool {
	return status == domain.SegmentStatusActive || status == domain.SegmentStatusDisabled
}

func normalizeStatus(status string) (string, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		return domain.SegmentStatusActive, nil
	}
	if !validStatus(status) {
		return "", errcode.New(40001, "status 只能是 active 或 disabled")
	}
	return status, nil
}

func validateCode(code string) error {
	if len(code) > maxSegmentCodeLen {
		return errcode.New(40001, fmt.Sprintf("code 长度不能超过 %d", maxSegmentCodeLen))
	}
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return errcode.New(40001, "code 只能包含字母、数字、下划线与短横线")
		}
	}
	return nil
}

// generateCode name 转 slug + 短随机后缀；非 ASCII 名称（如中文）退化为纯随机码
func generateCode(name string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('_')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "_")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "_")
	}
	if slug == "" {
		slug = "seg"
	}
	return slug + "_" + randomSuffix(6)
}

const codeAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

func randomSuffix(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败极罕见；退化为时间戳后缀，仍能满足唯一性诉求
		return fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000)
	}
	out := make([]byte, n)
	for i, c := range buf {
		out[i] = codeAlphabet[int(c)%len(codeAlphabet)]
	}
	return string(out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// isDuplicateErr 唯一键冲突（MySQL 1062 / GORM ErrDuplicatedKey）
func isDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate entry") || strings.Contains(msg, "unique constraint")
}
