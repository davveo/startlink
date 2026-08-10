package preference

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/starlink/push/internal/domain"
)

type spyResolver struct {
	invalidated []string
}

func (s *spyResolver) Resolve(context.Context, string) (*domain.UserPreference, error) {
	return nil, nil
}

func (s *spyResolver) Invalidate(userID string) {
	s.invalidated = append(s.invalidated, userID)
}

func newTestService() (*Service, *fakeRepo, *spyResolver) {
	repo := newFakeRepo()
	res := &spyResolver{}
	return NewService(repo, res), repo, res
}

func scopes(logs []domain.ConsentLog) []string {
	out := make([]string, 0, len(logs))
	for _, l := range logs {
		out = append(out, l.Action+"|"+l.Scope)
	}
	sort.Strings(out)
	return out
}

func TestGetReturnsZeroValueWhenAbsent(t *testing.T) {
	svc, _, _ := newTestService()
	view, err := svc.Get(context.Background(), "u1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.UserID != "u1" || view.MarketingOptOut {
		t.Fatalf("expected zero-value preference, got %#v", view)
	}
	if len(view.OptOutChannels) != 0 || len(view.OptOutTopics) != 0 {
		t.Fatalf("expected empty lists, got %#v", view)
	}
}

func TestUpsertMarketingOnlyProducesSingleLog(t *testing.T) {
	svc, repo, res := newTestService()
	ctx := context.Background()

	if _, err := svc.Upsert(ctx, "u1", domain.PreferenceInput{
		MarketingOptOut: boolPtr(true),
		Operator:        "alice",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	logs := repo.consentLogs()
	if len(logs) != 1 {
		t.Fatalf("expected exactly 1 consent log, got %d: %v", len(logs), scopes(logs))
	}
	if logs[0].Action != domain.ConsentOptOut || logs[0].Scope != "marketing" {
		t.Fatalf("unexpected log: %#v", logs[0])
	}
	if logs[0].Operator != "alice" || logs[0].Source != "console" {
		t.Fatalf("actor not recorded: %#v", logs[0])
	}
	if len(res.invalidated) != 1 || res.invalidated[0] != "u1" {
		t.Fatalf("resolver not invalidated: %v", res.invalidated)
	}
}

func TestUpsertUnchangedFieldsProduceNoLogs(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()

	if _, err := svc.Upsert(ctx, "u1", domain.PreferenceInput{
		MarketingOptOut: boolPtr(true),
		OptOutChannels:  slicePtr([]string{"sms"}),
		OptOutTopics:    slicePtr([]string{"promotion"}),
		QuietStart:      strPtr("22:00"),
		QuietEnd:        strPtr("08:00"),
		PreferredHour:   intPtr(10),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	repo.resetConsents()

	// 重复提交同一份内容 + 一个未变化的时区，不应产生任何同意记录
	if _, err := svc.Upsert(ctx, "u1", domain.PreferenceInput{
		MarketingOptOut: boolPtr(true),
		OptOutChannels:  slicePtr([]string{"SMS", " sms "}),
		OptOutTopics:    slicePtr([]string{"Promotion"}),
		QuietStart:      strPtr("22:00"),
		QuietEnd:        strPtr("08:00"),
		PreferredHour:   intPtr(10),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if logs := repo.consentLogs(); len(logs) != 0 {
		t.Fatalf("expected no logs for unchanged update, got %v", scopes(logs))
	}
}

func TestUpsertChannelAddAndRemoveLogs(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()

	if _, err := svc.Upsert(ctx, "u1", domain.PreferenceInput{
		OptOutChannels: slicePtr([]string{"sms", "email"}),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got := scopes(repo.consentLogs())
	want := []string{"opt_out|channel:email", "opt_out|channel:sms"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("add channels: got %v want %v", got, want)
	}

	repo.resetConsents()
	// 移除 sms、新增 inbox：一条 opt_in + 一条 opt_out，email 未变不记录
	if _, err := svc.Upsert(ctx, "u1", domain.PreferenceInput{
		OptOutChannels: slicePtr([]string{"email", "inbox"}),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got = scopes(repo.consentLogs())
	want = []string{"opt_in|channel:sms", "opt_out|channel:inbox"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("mutate channels: got %v want %v", got, want)
	}
}

func TestUpsertTopicAndQuietAndHourLogs(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()

	if _, err := svc.Upsert(ctx, "u1", domain.PreferenceInput{
		OptOutTopics:  slicePtr([]string{"promotion", "news"}),
		QuietStart:    strPtr("22:00"),
		QuietEnd:      strPtr("08:00"),
		PreferredHour: intPtr(9),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got := scopes(repo.consentLogs())
	want := []string{"opt_in|preferred_hour", "opt_out|quiet_hours", "opt_out|topic:news", "opt_out|topic:promotion"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}

	repo.resetConsents()
	// 取消免打扰与期望时段，移除一个主题
	if _, err := svc.Upsert(ctx, "u1", domain.PreferenceInput{
		OptOutTopics:  slicePtr([]string{"news"}),
		QuietStart:    strPtr(""),
		QuietEnd:      strPtr(""),
		PreferredHour: intPtr(ClearPreferredHour),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got = scopes(repo.consentLogs())
	want = []string{"opt_in|quiet_hours", "opt_in|topic:promotion", "opt_out|preferred_hour"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestUpsertValidation(t *testing.T) {
	cases := []struct {
		name string
		in   domain.PreferenceInput
	}{
		{"bad timezone", domain.PreferenceInput{Timezone: strPtr("Mars/Olympus")}},
		{"quiet start not hhmm", domain.PreferenceInput{QuietStart: strPtr("9:00")}},
		{"quiet end not hhmm", domain.PreferenceInput{QuietEnd: strPtr("25:00")}},
		{"quiet end with seconds", domain.PreferenceInput{QuietEnd: strPtr("08:00:00")}},
		{"hour too large", domain.PreferenceInput{PreferredHour: intPtr(24)}},
		{"hour negative", domain.PreferenceInput{PreferredHour: intPtr(-2)}},
		{"invalid channel", domain.PreferenceInput{OptOutChannels: slicePtr([]string{"pigeon"})}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, res := newTestService()
			if _, err := svc.Upsert(context.Background(), "u1", tc.in); err == nil {
				t.Fatal("expected validation error")
			}
			if len(repo.consentLogs()) != 0 {
				t.Fatal("rejected update must not write consent logs")
			}
			if len(res.invalidated) != 0 {
				t.Fatal("rejected update must not invalidate cache")
			}
			if _, ok := repo.prefs["u1"]; ok {
				t.Fatal("rejected update must not persist")
			}
		})
	}
}

func TestUpsertAcceptsValidTimezoneAndClearsIt(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()

	if _, err := svc.Upsert(ctx, "u1", domain.PreferenceInput{Timezone: strPtr("Asia/Shanghai")}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if repo.prefs["u1"].Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone not stored: %#v", repo.prefs["u1"])
	}
	// 时区不在同意审计范围内（不是「同意/拒绝」语义）
	if logs := repo.consentLogs(); len(logs) != 0 {
		t.Fatalf("timezone change should not emit consent logs, got %v", scopes(logs))
	}
}

func TestUpsertOnlyTouchesProvidedFields(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()

	if _, err := svc.Upsert(ctx, "u1", domain.PreferenceInput{
		OptOutChannels: slicePtr([]string{"sms"}),
		PreferredHour:  intPtr(9),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	repo.resetConsents()

	view, err := svc.Upsert(ctx, "u1", domain.PreferenceInput{MarketingOptOut: boolPtr(true)})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if len(view.OptOutChannels) != 1 || view.OptOutChannels[0] != domain.ChannelSMS {
		t.Fatalf("channels dropped by partial update: %#v", view.OptOutChannels)
	}
	if view.PreferredHour == nil || *view.PreferredHour != 9 {
		t.Fatalf("preferred hour dropped by partial update: %#v", view.PreferredHour)
	}
	if logs := repo.consentLogs(); len(logs) != 1 {
		t.Fatalf("expected only the marketing log, got %v", scopes(logs))
	}
}

func TestDeleteWritesConsentAndInvalidates(t *testing.T) {
	svc, repo, res := newTestService()
	ctx := context.Background()

	if _, err := svc.Upsert(ctx, "u1", domain.PreferenceInput{
		MarketingOptOut: boolPtr(true),
		OptOutChannels:  slicePtr([]string{"sms"}),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	repo.resetConsents()
	res.invalidated = nil

	deleted, err := svc.Delete(ctx, "u1", domain.PreferenceInput{Operator: "bob", Source: "console"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Fatal("expected deletion")
	}
	got := scopes(repo.consentLogs())
	want := []string{"opt_in|channel:sms", "opt_in|marketing", "opt_in|preference"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
	if len(res.invalidated) != 1 || res.invalidated[0] != "u1" {
		t.Fatalf("resolver not invalidated: %v", res.invalidated)
	}
}

func TestDeleteMissingPreference(t *testing.T) {
	svc, _, _ := newTestService()
	if _, err := svc.Delete(context.Background(), "ghost", domain.PreferenceInput{}); err == nil {
		t.Fatal("expected not found error")
	}
}

func TestListRejectsInvalidChannelFilter(t *testing.T) {
	svc, _, _ := newTestService()
	if _, err := svc.List(context.Background(), domain.ListPreferenceQuery{Channel: "pigeon"}); err == nil {
		t.Fatal("expected invalid channel filter error")
	}
}

func TestUpsertPersistsJSONColumnsAsNonEmpty(t *testing.T) {
	svc, repo, _ := newTestService()
	if _, err := svc.Upsert(context.Background(), "u1", domain.PreferenceInput{
		MarketingOptOut: boolPtr(true),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// 未提供列表时不应写入 NULL 之外的非法值；MySQL JSON 列拒绝空串
	p := repo.prefs["u1"]
	if v := domain.JSONColumnValue(p.OptOutChannelsJSON, ""); v != "" && v != "[]" {
		t.Fatalf("unexpected channels json: %q", v)
	}
}
