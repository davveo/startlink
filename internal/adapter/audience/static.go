package audience

import (
	"context"
	"strconv"
	"strings"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
)

// StaticProvider 从 DB 读静态人群段成员；仅支持 biz_scene=static。
// audience_ref 约定为人群段 code。
type StaticProvider struct {
	members port.SegmentMemberRepository
}

func NewStaticProvider(members port.SegmentMemberRepository) *StaticProvider {
	return &StaticProvider{members: members}
}

func (p *StaticProvider) Name() string { return "static" }

func (p *StaticProvider) Supports(bizScene string) bool {
	return strings.EqualFold(strings.TrimSpace(bizScene), domain.BizSceneStatic)
}

func (p *StaticProvider) Resolve(ctx context.Context, query domain.AudienceQuery) (*domain.AudiencePage, error) {
	if p.members == nil {
		return nil, errcode.New(50001, "静态人群存储未配置")
	}
	code := strings.TrimSpace(query.AudienceRef)
	if code == "" {
		return nil, errcode.New(40001, "静态人群 audience_ref（人群段 code）不能为空")
	}

	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	offset := 0
	if query.PageToken != "" {
		n, err := strconv.Atoi(query.PageToken)
		if err != nil || n < 0 {
			return nil, errcode.New(40001, "非法 page_token")
		}
		offset = n
	}

	total, err := p.members.Count(ctx, code)
	if err != nil {
		return nil, err
	}
	if int64(offset) >= total {
		return &domain.AudiencePage{HasMore: false, TotalHint: total}, nil
	}

	rows, err := p.members.ListPage(ctx, code, offset, pageSize)
	if err != nil {
		return nil, err
	}
	users := make([]domain.TargetUser, 0, len(rows))
	for i := range rows {
		users = append(users, rows[i].ToTargetUser())
	}
	nextOffset := offset + len(rows)
	hasMore := int64(nextOffset) < total
	next := ""
	if hasMore {
		next = strconv.Itoa(nextOffset)
	}
	return &domain.AudiencePage{
		Users:         users,
		NextPageToken: next,
		TotalHint:     total,
		HasMore:       hasMore,
	}, nil
}
