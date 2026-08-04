package audience

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
)

// Registry 按 BizScene 路由到业务人群 Provider，支持多业务线快速对接
type Registry struct {
	mu        sync.RWMutex
	providers []port.AudienceProvider
	filters   []port.AudienceFilter
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(p port.AudienceProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = append(r.providers, p)
}

func (r *Registry) RegisterFilter(f port.AudienceFilter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.filters = append(r.filters, f)
}

func (r *Registry) Resolve(ctx context.Context, query domain.AudienceQuery) (*domain.AudiencePage, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var provider port.AudienceProvider
	for _, p := range r.providers {
		if p.Supports(query.BizScene) {
			provider = p
			break
		}
	}
	if provider == nil {
		return nil, errcode.UnsupportedScene
	}

	page, err := provider.Resolve(ctx, query)
	if err != nil {
		return nil, err
	}

	users := page.Users
	for _, f := range r.filters {
		users, err = f.Filter(ctx, query.BizScene, users)
		if err != nil {
			return nil, err
		}
	}
	page.Users = users
	return page, nil
}

// DemoProvider 联调假人群：仅支持配置的 demo scenes，不再兜底所有 biz_scene
type DemoProvider struct {
	scenes map[string]struct{}
}

func NewDemoProvider(scenes []string) *DemoProvider {
	m := make(map[string]struct{}, len(scenes))
	for _, s := range scenes {
		s = strings.TrimSpace(strings.ToLower(s))
		if s != "" {
			m[s] = struct{}{}
		}
	}
	if len(m) == 0 {
		m["demo"] = struct{}{}
		m["dev"] = struct{}{}
	}
	return &DemoProvider{scenes: m}
}

func (p *DemoProvider) Name() string { return "demo" }

func (p *DemoProvider) Supports(bizScene string) bool {
	_, ok := p.scenes[strings.ToLower(strings.TrimSpace(bizScene))]
	return ok
}

func (p *DemoProvider) Resolve(ctx context.Context, query domain.AudienceQuery) (*domain.AudiencePage, error) {
	total := 500
	if v, ok := query.Extra["total"].(float64); ok {
		total = int(v)
	} else if v, ok := query.Extra["total"].(int); ok {
		total = v
	}

	pageSize := query.PageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	offset := 0
	if query.PageToken != "" {
		offset, _ = strconv.Atoi(query.PageToken)
	}
	if offset >= total {
		return &domain.AudiencePage{HasMore: false, TotalHint: int64(total)}, nil
	}

	end := offset + pageSize
	if end > total {
		end = total
	}
	users := make([]domain.TargetUser, 0, end-offset)
	for i := offset; i < end; i++ {
		uid := fmt.Sprintf("u_%s_%d", query.AudienceRef, i+1)
		// 演示用户可达渠道：偶数用户仅 inbox，奇数全渠道空（走任务默认）
		var chs []domain.ChannelType
		if (i+1)%4 == 0 {
			chs = []domain.ChannelType{domain.ChannelInbox}
		}
		users = append(users, domain.TargetUser{
			UserID:   uid,
			Channels: chs,
			Vars: map[string]string{
				"name":  fmt.Sprintf("User%d", i+1),
				"score": strconv.Itoa(i + 1),
			},
		})
	}
	next := ""
	hasMore := end < total
	if hasMore {
		next = strconv.Itoa(end)
	}
	return &domain.AudiencePage{
		Users:         users,
		NextPageToken: next,
		TotalHint:     int64(total),
		HasMore:       hasMore,
	}, nil
}

// HTTPProvider 业务人群 HTTP SPI：POST AudienceQuery JSON，响应 AudiencePage JSON
type HTTPProvider struct {
	url       string
	scenes    map[string]struct{}
	acceptAll bool
	client    *http.Client
}

func NewHTTPProvider(url string, scenes []string, timeoutSec int) *HTTPProvider {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	m := make(map[string]struct{}, len(scenes))
	for _, s := range scenes {
		s = strings.TrimSpace(strings.ToLower(s))
		if s != "" {
			m[s] = struct{}{}
		}
	}
	return &HTTPProvider{
		url:       url,
		scenes:    m,
		acceptAll: len(m) == 0,
		client:    &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
	}
}

func (p *HTTPProvider) Name() string { return "http" }

func (p *HTTPProvider) Supports(bizScene string) bool {
	if p.url == "" {
		return false
	}
	if p.acceptAll {
		return bizScene != ""
	}
	_, ok := p.scenes[strings.ToLower(strings.TrimSpace(bizScene))]
	return ok
}

func (p *HTTPProvider) Resolve(ctx context.Context, query domain.AudienceQuery) (*domain.AudiencePage, error) {
	raw, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("audience http %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var page domain.AudiencePage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ComplianceFilter 示例：过滤 user_id 以 _block 结尾的用户
type ComplianceFilter struct{}

func NewComplianceFilter() *ComplianceFilter { return &ComplianceFilter{} }

func (f *ComplianceFilter) Filter(ctx context.Context, bizScene string, users []domain.TargetUser) ([]domain.TargetUser, error) {
	out := make([]domain.TargetUser, 0, len(users))
	for _, u := range users {
		if len(u.UserID) >= 6 && u.UserID[len(u.UserID)-6:] == "_block" {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

// BlacklistFilter Redis SET 黑名单
type BlacklistFilter struct {
	rdb *redis.Client
	key string
}

func NewBlacklistFilter(rdb *redis.Client, key string) *BlacklistFilter {
	return &BlacklistFilter{rdb: rdb, key: key}
}

func (f *BlacklistFilter) Filter(ctx context.Context, bizScene string, users []domain.TargetUser) ([]domain.TargetUser, error) {
	if f.rdb == nil || f.key == "" || len(users) == 0 {
		return users, nil
	}
	out := make([]domain.TargetUser, 0, len(users))
	for _, u := range users {
		ok, err := f.rdb.SIsMember(ctx, f.key, u.UserID).Result()
		if err != nil {
			return nil, err
		}
		if ok {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

// UnsubscribeFilter 按渠道退订：从用户 Channels 剔除；若剔除后无渠道则丢弃用户。
// 用户 Channels 为空时保留（由任务默认渠道决定，发送前 Gateway 再检）。
type UnsubscribeFilter struct {
	rdb    *redis.Client
	prefix string
}

func NewUnsubscribeFilter(rdb *redis.Client, prefix string) *UnsubscribeFilter {
	return &UnsubscribeFilter{rdb: rdb, prefix: prefix}
}

func (f *UnsubscribeFilter) Filter(ctx context.Context, bizScene string, users []domain.TargetUser) ([]domain.TargetUser, error) {
	if f.rdb == nil || f.prefix == "" || len(users) == 0 {
		return users, nil
	}
	out := make([]domain.TargetUser, 0, len(users))
	for _, u := range users {
		if len(u.Channels) == 0 {
			out = append(out, u)
			continue
		}
		kept := make([]domain.ChannelType, 0, len(u.Channels))
		for _, ch := range u.Channels {
			ok, err := f.rdb.SIsMember(ctx, f.prefix+string(ch), u.UserID).Result()
			if err != nil {
				return nil, err
			}
			if ok {
				continue
			}
			kept = append(kept, ch)
		}
		if len(kept) == 0 {
			continue
		}
		u.Channels = kept
		out = append(out, u)
	}
	return out, nil
}

// ABSampleFilter 按 AudienceQuery.Extra["ab_sample_percent"]（0-100）抽样；未配置则全量。
type ABSampleFilter struct{}

func NewABSampleFilter() *ABSampleFilter { return &ABSampleFilter{} }

func (f *ABSampleFilter) Filter(ctx context.Context, bizScene string, users []domain.TargetUser) ([]domain.TargetUser, error) {
	// Extra 不在 Filter 签名中；AB 抽样在 Splitter 侧用 AudienceExtra 处理更合适。
	// 此处保留钩子：用户 Extra["ab_keep"]=false 时丢弃。
	out := make([]domain.TargetUser, 0, len(users))
	for _, u := range users {
		if u.Extra != nil {
			if keep, ok := u.Extra["ab_keep"].(bool); ok && !keep {
				continue
			}
		}
		out = append(out, u)
	}
	return out, nil
}

// SampleByPercent 按 userID 稳定哈希抽样（供 Splitter 使用）
func SampleByPercent(userID string, percent int) bool {
	if percent >= 100 {
		return true
	}
	if percent <= 0 {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(userID))
	return int(h.Sum32()%100) < percent
}
