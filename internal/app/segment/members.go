package segment

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/pkg/errcode"
)

const (
	maxImportBatch      = 20_000
	maxImportSampleErrs = 8
	maxPhoneLen         = 32
	maxEmailLen         = 128
)

// MemberListResult 静态成员列表
type MemberListResult struct {
	Items    []domain.AudienceSegmentMember `json:"items"`
	Total    int64                          `json:"total"`
	Page     int                            `json:"page"`
	PageSize int                            `json:"page_size"`
}

// ListMembers 分页查看静态人群成员
func (s *Service) ListMembers(ctx context.Context, code string, q domain.ListSegmentMemberQuery) (*MemberListResult, error) {
	seg, err := s.mustGet(ctx, code)
	if err != nil {
		return nil, err
	}
	if !seg.IsStatic() {
		return nil, errcode.New(40001, "仅静态人群段支持查看成员列表")
	}
	if s.members == nil {
		return nil, errcode.New(50001, "静态人群存储未配置")
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 200 {
		q.PageSize = 200
	}
	list, total, err := s.members.List(ctx, seg.Code, q)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []domain.AudienceSegmentMember{}
	}
	return &MemberListResult{Items: list, Total: total, Page: q.Page, PageSize: q.PageSize}, nil
}

// ClearMembers 清空静态人群成员
func (s *Service) ClearMembers(ctx context.Context, code, operator string) (*domain.ImportSegmentMembersResult, error) {
	seg, err := s.mustGet(ctx, code)
	if err != nil {
		return nil, err
	}
	if !seg.IsStatic() {
		return nil, errcode.New(40001, "仅静态人群段支持清空成员")
	}
	if s.members == nil {
		return nil, errcode.New(50001, "静态人群存储未配置")
	}
	if err := s.members.DeleteBySegment(ctx, seg.Code); err != nil {
		return nil, err
	}
	if err := s.syncStaticMemberCount(ctx, seg.Code, operator); err != nil {
		return nil, err
	}
	return &domain.ImportSegmentMembersResult{MemberCount: 0, Replaced: true}, nil
}

// ImportMembersJSON 批量导入静态成员（JSON）
func (s *Service) ImportMembersJSON(ctx context.Context, code string, in domain.ImportSegmentMembersInput) (*domain.ImportSegmentMembersResult, error) {
	seg, err := s.mustGet(ctx, code)
	if err != nil {
		return nil, err
	}
	if !seg.IsStatic() {
		return nil, errcode.New(40001, "仅静态人群段支持导入成员；请创建 source=static 的人群段")
	}
	return s.importMembers(ctx, seg, in.Mode, in.Members, in.Operator)
}

// ImportMembersCSV 从 CSV 流导入。表头支持：user_id, phone, email（大小写不敏感）。
// 至少需有 phone 或 email 之一；无 user_id 时用 phone: / email: 前缀生成。
func (s *Service) ImportMembersCSV(ctx context.Context, code, mode, operator string, r io.Reader) (*domain.ImportSegmentMembersResult, error) {
	seg, err := s.mustGet(ctx, code)
	if err != nil {
		return nil, err
	}
	if !seg.IsStatic() {
		return nil, errcode.New(40001, "仅静态人群段支持导入成员；请创建 source=static 的人群段")
	}
	members, invalid, samples, err := parseMembersCSV(r)
	if err != nil {
		return nil, err
	}
	out, err := s.importMembers(ctx, seg, mode, members, operator)
	if err != nil {
		return nil, err
	}
	out.InvalidRows = invalid
	out.SampleErrors = samples
	return out, nil
}

func (s *Service) importMembers(
	ctx context.Context,
	seg *domain.AudienceSegment,
	mode string,
	raw []domain.SegmentMemberInput,
	operator string,
) (*domain.ImportSegmentMembersResult, error) {
	if s.members == nil {
		return nil, errcode.New(50001, "静态人群存储未配置")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "append"
	}
	if mode != "append" && mode != "replace" {
		return nil, errcode.New(40001, "mode 只能是 append 或 replace")
	}
	if len(raw) == 0 {
		return nil, errcode.New(40001, "members 不能为空")
	}
	if len(raw) > maxImportBatch {
		return nil, errcode.New(40001, fmt.Sprintf("单次最多导入 %d 条", maxImportBatch))
	}

	normalized := make([]domain.AudienceSegmentMember, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	var skipped int
	var samples []string
	for i, row := range raw {
		m, errMsg := normalizeMemberInput(seg.Code, row)
		if errMsg != "" {
			skipped++
			if len(samples) < maxImportSampleErrs {
				samples = append(samples, fmt.Sprintf("行%d: %s", i+1, errMsg))
			}
			continue
		}
		if _, dup := seen[m.UserID]; dup {
			skipped++
			if len(samples) < maxImportSampleErrs {
				samples = append(samples, fmt.Sprintf("行%d: 重复 user_id %s", i+1, m.UserID))
			}
			continue
		}
		seen[m.UserID] = struct{}{}
		normalized = append(normalized, m)
	}
	if len(normalized) == 0 {
		return nil, errcode.New(40001, "没有可导入的有效成员："+strings.Join(samples, "; "))
	}

	replaced := mode == "replace"
	if replaced {
		if err := s.members.DeleteBySegment(ctx, seg.Code); err != nil {
			return nil, err
		}
	}
	touched, err := s.members.BulkUpsert(ctx, normalized)
	if err != nil {
		return nil, err
	}
	_ = touched
	if err := s.syncStaticMemberCount(ctx, seg.Code, operator); err != nil {
		return nil, err
	}
	count, err := s.members.Count(ctx, seg.Code)
	if err != nil {
		return nil, err
	}
	return &domain.ImportSegmentMembersResult{
		Submitted:    len(raw),
		Accepted:     len(normalized),
		Skipped:      skipped,
		MemberCount:  count,
		Replaced:     replaced,
		SampleErrors: samples,
	}, nil
}

func (s *Service) syncStaticMemberCount(ctx context.Context, code, operator string) error {
	count, err := s.members.Count(ctx, code)
	if err != nil {
		return err
	}
	now := time.Now()
	extra := map[string]any{
		"total_hint": count,
		"total":      count,
	}
	fields := map[string]any{
		"member_count":   count,
		"counted_at":     now,
		"refresh_error":  "",
		"audience_extra": domain.MarshalJSONColumn(extra, false),
	}
	if operator != "" {
		fields["updated_by"] = operator
	}
	return s.segments.Update(ctx, code, fields)
}

func normalizeMemberInput(segmentCode string, in domain.SegmentMemberInput) (domain.AudienceSegmentMember, string) {
	phone := strings.TrimSpace(in.Phone)
	email := strings.TrimSpace(strings.ToLower(in.Email))
	uid := strings.TrimSpace(in.UserID)

	if phone == "" && email == "" {
		return domain.AudienceSegmentMember{}, "phone 与 email 至少填一个"
	}
	if phone != "" && utf8.RuneCountInString(phone) > maxPhoneLen {
		return domain.AudienceSegmentMember{}, "phone 过长"
	}
	if email != "" && utf8.RuneCountInString(email) > maxEmailLen {
		return domain.AudienceSegmentMember{}, "email 过长"
	}
	if email != "" && !strings.Contains(email, "@") {
		return domain.AudienceSegmentMember{}, "email 格式无效"
	}

	if uid == "" {
		switch {
		case phone != "":
			uid = "phone:" + phone
		default:
			uid = "email:" + email
		}
	}
	if utf8.RuneCountInString(uid) > maxUserIDLen {
		return domain.AudienceSegmentMember{}, "user_id 过长"
	}
	for _, r := range uid {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_' || r == '-' || r == ':' || r == '.' || r == '@' || r == '+':
		default:
			return domain.AudienceSegmentMember{}, "user_id 含非法字符"
		}
	}

	m := domain.AudienceSegmentMember{
		SegmentCode: segmentCode,
		UserID:      uid,
		Phone:       phone,
		Email:       email,
	}
	if len(in.Vars) > 0 {
		m.VarsJSON = domain.MarshalJSONColumn(in.Vars, false)
	}
	return m, ""
}

func parseMembersCSV(r io.Reader) (members []domain.SegmentMemberInput, invalid int, samples []string, err error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		if err == io.EOF {
			return nil, 0, nil, errcode.New(40001, "CSV 为空")
		}
		return nil, 0, nil, errcode.New(40001, "解析 CSV 失败: "+err.Error())
	}
	// 去掉 UTF-8 BOM
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}
	idx := map[string]int{}
	for i, h := range header {
		key := strings.ToLower(strings.TrimSpace(h))
		switch key {
		case "user_id", "userid", "uid", "用户id", "用户":
			idx["user_id"] = i
		case "phone", "mobile", "tel", "手机号", "手机", "电话":
			idx["phone"] = i
		case "email", "mail", "邮箱":
			idx["email"] = i
		}
	}
	if _, ok := idx["phone"]; !ok {
		if _, ok2 := idx["email"]; !ok2 {
			return nil, 0, nil, errcode.New(40001, "CSV 表头需包含 phone 或 email 列（可选 user_id）")
		}
	}

	rowNo := 1
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		rowNo++
		if err != nil {
			invalid++
			if len(samples) < maxImportSampleErrs {
				samples = append(samples, fmt.Sprintf("行%d: %s", rowNo, err.Error()))
			}
			continue
		}
		get := func(key string) string {
			i, ok := idx[key]
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		in := domain.SegmentMemberInput{
			UserID: get("user_id"),
			Phone:  get("phone"),
			Email:  get("email"),
		}
		if in.UserID == "" && in.Phone == "" && in.Email == "" {
			continue // 空行跳过，不计入 invalid
		}
		if _, msg := normalizeMemberInput("_", in); msg != "" {
			invalid++
			if len(samples) < maxImportSampleErrs {
				samples = append(samples, fmt.Sprintf("行%d: %s", rowNo, msg))
			}
			continue
		}
		members = append(members, in)
		if len(members) > maxImportBatch {
			return nil, invalid, samples, errcode.New(40001, fmt.Sprintf("单次最多导入 %d 条", maxImportBatch))
		}
	}
	if len(members) == 0 {
		return nil, invalid, samples, errcode.New(40001, "CSV 中没有可导入的有效行")
	}
	return members, invalid, samples, nil
}
