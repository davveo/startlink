package preference

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
)

// ClearPreferredHour 期望送达小时的清除哨兵。
// PreferenceInput.PreferredHour 用 nil 表达「不修改」，因此需要另一个值表达「清空」。
const ClearPreferredHour = -1

// consentDetailMax ConsentLog.Detail 列宽 512，超长直接截断
const consentDetailMax = 512

type Service struct {
	repo     port.PreferenceRepository
	resolver port.PreferenceResolver
}

func NewService(repo port.PreferenceRepository, resolver port.PreferenceResolver) *Service {
	return &Service{repo: repo, resolver: resolver}
}

// PreferenceView 对外视图：UserPreference 的 JSON 列打了 json:"-"，
// 直接返回实体前端拿不到退订渠道/主题。
type PreferenceView struct {
	domain.UserPreference
	OptOutChannels []domain.ChannelType `json:"opt_out_channels"`
	OptOutTopics   []string             `json:"opt_out_topics"`
}

func newView(p *domain.UserPreference) *PreferenceView {
	if p == nil {
		return nil
	}
	chs := p.OptOutChannels()
	if chs == nil {
		chs = []domain.ChannelType{}
	}
	topics := p.OptOutTopics()
	if topics == nil {
		topics = []string{}
	}
	return &PreferenceView{UserPreference: *p, OptOutChannels: chs, OptOutTopics: topics}
}

type ListResult struct {
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Items    []PreferenceView `json:"items"`
}

type ConsentListResult struct {
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Items    []domain.ConsentLog `json:"items"`
}

// Get 未配置偏好时返回零值（= 全部允许），避免前端为「有/无记录」写两套渲染
func (s *Service) Get(ctx context.Context, userID string) (*PreferenceView, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errcode.InvalidParam
	}
	p, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return newView(&domain.UserPreference{UserID: userID}), nil
	}
	return newView(p), nil
}

func (s *Service) List(ctx context.Context, q domain.ListPreferenceQuery) (*ListResult, error) {
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
	if q.Channel != "" && !domain.ChannelType(strings.ToLower(strings.TrimSpace(q.Channel))).Valid() {
		return nil, errcode.New(40001, "invalid channel filter")
	}
	list, total, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, err
	}
	items := make([]PreferenceView, 0, len(list))
	for i := range list {
		items = append(items, *newView(&list[i]))
	}
	return &ListResult{Total: total, Page: page, PageSize: size, Items: items}, nil
}

func (s *Service) ListConsent(ctx context.Context, q domain.ListConsentLogQuery) (*ConsentListResult, error) {
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
	if q.Action != "" && q.Action != domain.ConsentOptIn && q.Action != domain.ConsentOptOut {
		return nil, errcode.New(40001, "invalid consent action")
	}
	list, total, err := s.repo.ListConsent(ctx, q)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []domain.ConsentLog{}
	}
	return &ConsentListResult{Total: total, Page: page, PageSize: size, Items: list}, nil
}

// Upsert 局部更新偏好：只写 in 中非 nil 的字段，并按新旧值差量生成同意审计。
func (s *Service) Upsert(ctx context.Context, userID string, in domain.PreferenceInput) (*PreferenceView, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errcode.InvalidParam
	}

	existing, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	old := existing
	if old == nil {
		old = &domain.UserPreference{UserID: userID}
	}

	next := *old
	next.UserID = userID
	if err := applyInput(&next, in); err != nil {
		return nil, err
	}

	source, operator := normalizeActor(in)
	next.UpdatedBy = operator

	logs := diffConsent(userID, old, &next, source, operator)

	if err := s.repo.Upsert(ctx, &next); err != nil {
		return nil, err
	}
	if len(logs) > 0 {
		if err := s.repo.AppendConsent(ctx, logs); err != nil {
			return nil, err
		}
	}
	s.invalidate(userID)

	saved, err := s.repo.Get(ctx, userID)
	if err != nil || saved == nil {
		// 读回失败不影响写入结果，返回内存中的最新值
		return newView(&next), nil
	}
	return newView(saved), nil
}

// Delete 清空偏好记录 = 恢复默认「全部允许」，同样要留下同意举证
func (s *Service) Delete(ctx context.Context, userID string, in domain.PreferenceInput) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, errcode.InvalidParam
	}
	existing, err := s.repo.Get(ctx, userID)
	if err != nil {
		return false, err
	}
	if existing == nil {
		return false, errcode.NotFound
	}

	source, operator := normalizeActor(in)
	logs := diffConsent(userID, existing, &domain.UserPreference{UserID: userID}, source, operator)
	logs = append(logs, domain.ConsentLog{
		UserID:   userID,
		Action:   domain.ConsentOptIn,
		Scope:    "preference",
		Source:   source,
		Operator: operator,
		Detail:   "preference record deleted",
	})

	removed, err := s.repo.Delete(ctx, userID)
	if err != nil {
		return false, err
	}
	if err := s.repo.AppendConsent(ctx, logs); err != nil {
		return removed, err
	}
	s.invalidate(userID)
	return removed, nil
}

func (s *Service) invalidate(userID string) {
	if s.resolver != nil {
		s.resolver.Invalidate(userID)
	}
}

func normalizeActor(in domain.PreferenceInput) (source, operator string) {
	source = strings.TrimSpace(in.Source)
	if source == "" {
		source = "console"
	}
	return source, strings.TrimSpace(in.Operator)
}

// applyInput 校验并写入非 nil 字段
func applyInput(p *domain.UserPreference, in domain.PreferenceInput) error {
	if in.Timezone != nil {
		tz := strings.TrimSpace(*in.Timezone)
		if tz != "" {
			if _, err := time.LoadLocation(tz); err != nil {
				return errcode.New(40001, "invalid timezone: "+tz)
			}
		}
		p.Timezone = tz
	}
	if in.QuietStart != nil {
		v := strings.TrimSpace(*in.QuietStart)
		if v != "" && !validHHMM(v) {
			return errcode.New(40001, "quiet_start must be HH:MM")
		}
		p.QuietStart = v
	}
	if in.QuietEnd != nil {
		v := strings.TrimSpace(*in.QuietEnd)
		if v != "" && !validHHMM(v) {
			return errcode.New(40001, "quiet_end must be HH:MM")
		}
		p.QuietEnd = v
	}
	if in.PreferredHour != nil {
		h := *in.PreferredHour
		switch {
		case h == ClearPreferredHour:
			p.PreferredHour = nil
		case h < 0 || h > 23:
			return errcode.New(40001, "preferred_hour must be 0-23")
		default:
			v := h
			p.PreferredHour = &v
		}
	}
	if in.OptOutChannels != nil {
		chs, err := normalizeChannels(*in.OptOutChannels)
		if err != nil {
			return err
		}
		p.OptOutChannelsJSON = domain.MarshalJSONColumn(chs, true)
	}
	if in.OptOutTopics != nil {
		topics := domain.NormalizeTopicList(*in.OptOutTopics)
		if topics == nil {
			topics = []string{}
		}
		p.OptOutTopicsJSON = domain.MarshalJSONColumn(topics, true)
	}
	if in.MarketingOptOut != nil {
		p.MarketingOptOut = *in.MarketingOptOut
	}
	return nil
}

// validHHMM 严格 HH:MM，拒绝 "9:00" / "09:00:00" 这类写法，
// 免打扰窗比较是字符串字典序，位数不齐会静默算错
func validHHMM(v string) bool {
	if len(v) != 5 || v[2] != ':' {
		return false
	}
	if _, err := time.Parse("15:04", v); err != nil {
		return false
	}
	return true
}

func normalizeChannels(in []string) ([]string, error) {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		ch := strings.ToLower(strings.TrimSpace(raw))
		if ch == "" {
			continue
		}
		if !domain.ChannelType(ch).Valid() {
			return nil, errcode.New(40001, "invalid channel: "+ch)
		}
		if _, ok := seen[ch]; ok {
			continue
		}
		seen[ch] = struct{}{}
		out = append(out, ch)
	}
	sort.Strings(out)
	return out, nil
}

// diffConsent 逐维度对比新旧偏好生成同意审计。
// 合规举证要能回答「谁在什么时候退订了哪个渠道/主题」，只记一条「改过了」答不出来。
func diffConsent(userID string, oldP, newP *domain.UserPreference, source, operator string) []domain.ConsentLog {
	var logs []domain.ConsentLog
	add := func(action, scope, detail string) {
		if len(detail) > consentDetailMax {
			detail = detail[:consentDetailMax]
		}
		logs = append(logs, domain.ConsentLog{
			UserID:   userID,
			Action:   action,
			Scope:    scope,
			Source:   source,
			Operator: operator,
			Detail:   detail,
		})
	}

	oldMarketing := oldP != nil && oldP.MarketingOptOut
	newMarketing := newP != nil && newP.MarketingOptOut
	if oldMarketing != newMarketing {
		if newMarketing {
			add(domain.ConsentOptOut, "marketing", "marketing opt-out enabled")
		} else {
			add(domain.ConsentOptIn, "marketing", "marketing opt-out cleared")
		}
	}

	oldChannels := channelSet(oldP)
	newChannels := channelSet(newP)
	for _, ch := range sortedKeys(newChannels) {
		if _, ok := oldChannels[ch]; !ok {
			add(domain.ConsentOptOut, "channel:"+ch, "channel opted out")
		}
	}
	for _, ch := range sortedKeys(oldChannels) {
		if _, ok := newChannels[ch]; !ok {
			add(domain.ConsentOptIn, "channel:"+ch, "channel opt-out removed")
		}
	}

	oldTopics := topicSet(oldP)
	newTopics := topicSet(newP)
	for _, t := range sortedKeys(newTopics) {
		if _, ok := oldTopics[t]; !ok {
			add(domain.ConsentOptOut, "topic:"+t, "topic opted out")
		}
	}
	for _, t := range sortedKeys(oldTopics) {
		if _, ok := newTopics[t]; !ok {
			add(domain.ConsentOptIn, "topic:"+t, "topic opt-out removed")
		}
	}

	oldStart, oldEnd := quietPair(oldP)
	newStart, newEnd := quietPair(newP)
	if oldStart != newStart || oldEnd != newEnd {
		detail := fmt.Sprintf("quiet hours %s -> %s", quietLabel(oldStart, oldEnd), quietLabel(newStart, newEnd))
		if newStart != "" && newEnd != "" {
			add(domain.ConsentOptOut, "quiet_hours", detail)
		} else {
			add(domain.ConsentOptIn, "quiet_hours", detail)
		}
	}

	oldHour := hourValue(oldP)
	newHour := hourValue(newP)
	if oldHour != newHour {
		detail := fmt.Sprintf("preferred hour %s -> %s", hourLabel(oldHour), hourLabel(newHour))
		if newHour >= 0 {
			add(domain.ConsentOptIn, "preferred_hour", detail)
		} else {
			add(domain.ConsentOptOut, "preferred_hour", detail)
		}
	}

	return logs
}

func channelSet(p *domain.UserPreference) map[string]struct{} {
	out := map[string]struct{}{}
	if p == nil {
		return out
	}
	for _, ch := range p.OptOutChannels() {
		key := strings.ToLower(strings.TrimSpace(string(ch)))
		if key != "" {
			out[key] = struct{}{}
		}
	}
	return out
}

func topicSet(p *domain.UserPreference) map[string]struct{} {
	out := map[string]struct{}{}
	if p == nil {
		return out
	}
	for _, t := range p.OptOutTopics() {
		key := strings.ToLower(strings.TrimSpace(t))
		if key != "" {
			out[key] = struct{}{}
		}
	}
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func quietPair(p *domain.UserPreference) (string, string) {
	if p == nil {
		return "", ""
	}
	return strings.TrimSpace(p.QuietStart), strings.TrimSpace(p.QuietEnd)
}

func quietLabel(start, end string) string {
	if start == "" || end == "" {
		return "none"
	}
	return start + "-" + end
}

func hourValue(p *domain.UserPreference) int {
	if p == nil || p.PreferredHour == nil {
		return -1
	}
	return *p.PreferredHour
}

func hourLabel(h int) string {
	if h < 0 {
		return "none"
	}
	return fmt.Sprintf("%02d", h)
}
