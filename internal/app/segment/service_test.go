package segment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/errcode"
)

// ---- fakes ----

type fakeSegments struct {
	items    map[string]*domain.AudienceSegment
	refs     map[string]int64
	deleted  []string
	createFn func(*domain.AudienceSegment) error
}

func newFakeSegments(segs ...*domain.AudienceSegment) *fakeSegments {
	f := &fakeSegments{items: map[string]*domain.AudienceSegment{}, refs: map[string]int64{}}
	for _, s := range segs {
		f.items[s.Code] = s
	}
	return f
}

func (f *fakeSegments) Create(_ context.Context, seg *domain.AudienceSegment) error {
	if f.createFn != nil {
		return f.createFn(seg)
	}
	if _, ok := f.items[seg.Code]; ok {
		return errors.New("Error 1062: Duplicate entry")
	}
	f.items[seg.Code] = seg
	return nil
}

func (f *fakeSegments) Update(_ context.Context, code string, fields map[string]any) error {
	seg, ok := f.items[code]
	if !ok {
		return errcode.NotFound
	}
	if v, ok := fields["member_count"].(int64); ok {
		seg.MemberCount = v
	}
	if v, ok := fields["refresh_error"].(string); ok {
		seg.RefreshError = v
	}
	if v, ok := fields["name"].(string); ok {
		seg.Name = v
	}
	if v, ok := fields["status"].(string); ok {
		seg.Status = v
	}
	return nil
}

func (f *fakeSegments) GetByCode(_ context.Context, code string) (*domain.AudienceSegment, error) {
	seg, ok := f.items[code]
	if !ok {
		return nil, nil
	}
	cp := *seg
	return &cp, nil
}

func (f *fakeSegments) Delete(_ context.Context, code string) error {
	if _, ok := f.items[code]; !ok {
		return errcode.NotFound
	}
	delete(f.items, code)
	f.deleted = append(f.deleted, code)
	return nil
}

func (f *fakeSegments) List(_ context.Context, _ domain.ListSegmentQuery) ([]domain.AudienceSegment, int64, error) {
	out := make([]domain.AudienceSegment, 0, len(f.items))
	for _, s := range f.items {
		out = append(out, *s)
	}
	return out, int64(len(out)), nil
}

func (f *fakeSegments) CountCampaignRefs(_ context.Context, code string) (int64, error) {
	return f.refs[code], nil
}

type fakeSuppression struct {
	rows    []domain.SuppressionEntry
	addErr  error
	removed int
}

func (f *fakeSuppression) BulkAdd(_ context.Context, entries []domain.SuppressionEntry) (int64, error) {
	if f.addErr != nil {
		return 0, f.addErr
	}
	var added int64
	for _, e := range entries {
		dup := false
		for _, ex := range f.rows {
			if ex.Kind == e.Kind && ex.UserID == e.UserID && ex.Channel == e.Channel {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		f.rows = append(f.rows, e)
		added++
	}
	return added, nil
}

func (f *fakeSuppression) Remove(_ context.Context, kind domain.SuppressionKind, userID, channel string) (bool, error) {
	for i, e := range f.rows {
		if e.Kind == kind && e.UserID == userID && e.Channel == channel {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			f.removed++
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeSuppression) List(_ context.Context, _ domain.ListSuppressionQuery) ([]domain.SuppressionEntry, int64, error) {
	return f.rows, int64(len(f.rows)), nil
}

func (f *fakeSuppression) CountByKind(_ context.Context) (map[domain.SuppressionKind]int64, error) {
	out := map[domain.SuppressionKind]int64{}
	for _, e := range f.rows {
		out[e.Kind]++
	}
	return out, nil
}

func (f *fakeSuppression) IterAll(_ context.Context, fn func(domain.SuppressionEntry) error) error {
	for _, e := range f.rows {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

type fakeStore struct {
	blacklist []string
	unsub     map[string][]string
	failAdd   bool
}

func newFakeStore() *fakeStore { return &fakeStore{unsub: map[string][]string{}} }

func (f *fakeStore) AddBlacklist(_ context.Context, userIDs []string) error {
	if f.failAdd {
		return errors.New("redis down")
	}
	f.blacklist = append(f.blacklist, userIDs...)
	return nil
}

func (f *fakeStore) RemoveBlacklist(_ context.Context, userID string) error {
	if f.failAdd {
		return errors.New("redis down")
	}
	out := f.blacklist[:0]
	for _, id := range f.blacklist {
		if id != userID {
			out = append(out, id)
		}
	}
	f.blacklist = out
	return nil
}

func (f *fakeStore) AddUnsubscribe(_ context.Context, channel string, userIDs []string) error {
	if f.failAdd {
		return errors.New("redis down")
	}
	f.unsub[channel] = append(f.unsub[channel], userIDs...)
	return nil
}

func (f *fakeStore) RemoveUnsubscribe(_ context.Context, channel, userID string) error {
	if f.failAdd {
		return errors.New("redis down")
	}
	out := f.unsub[channel][:0]
	for _, id := range f.unsub[channel] {
		if id != userID {
			out = append(out, id)
		}
	}
	f.unsub[channel] = out
	return nil
}

// fakeResolver 每页 pageSize 个用户，共 total 个；total<0 表示无限翻页
type fakeResolver struct {
	total    int
	pages    int
	err      error
	pageSize int
}

func (f *fakeResolver) Resolve(_ context.Context, q domain.AudienceQuery) (*domain.AudiencePage, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.pages++
	size := f.pageSize
	if size <= 0 {
		size = q.PageSize
	}
	offset := 0
	if q.PageToken != "" {
		fmt.Sscanf(q.PageToken, "%d", &offset)
	}
	end := offset + size
	infinite := f.total < 0
	if !infinite && end > f.total {
		end = f.total
	}
	users := make([]domain.TargetUser, 0, end-offset)
	for i := offset; i < end; i++ {
		users = append(users, domain.TargetUser{UserID: fmt.Sprintf("u%d", i)})
	}
	hasMore := infinite || end < f.total
	next := ""
	if hasMore {
		next = fmt.Sprintf("%d", end)
	}
	return &domain.AudiencePage{Users: users, NextPageToken: next, HasMore: hasMore}, nil
}

func newTestService(segs *fakeSegments, sup *fakeSuppression, store *fakeStore, res AudienceResolver) *Service {
	// (*fakeStore)(nil) 装箱后接口非 nil，service 的 s.store != nil 分支会误判，这里显式留空接口
	var storeArg port.SuppressionStore
	if store != nil {
		storeArg = store
	}
	return NewService(segs, sup, storeArg, res)
}

// ---- tests ----

func TestDeleteSegmentBlockedByCampaignRefs(t *testing.T) {
	segs := newFakeSegments(&domain.AudienceSegment{Code: "vip", Name: "VIP", Status: domain.SegmentStatusActive})
	segs.refs["vip"] = 3
	svc := newTestService(segs, &fakeSuppression{}, newFakeStore(), nil)

	err := svc.DeleteSegment(context.Background(), "vip")
	if err == nil {
		t.Fatal("expected delete to be rejected while campaigns reference the segment")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Fatalf("error should mention reference count, got %v", err)
	}
	if len(segs.deleted) != 0 {
		t.Fatalf("segment must not be deleted, deleted=%v", segs.deleted)
	}

	segs.refs["vip"] = 0
	if err := svc.DeleteSegment(context.Background(), "vip"); err != nil {
		t.Fatalf("delete without refs: %v", err)
	}
	if len(segs.deleted) != 1 {
		t.Fatalf("expected one delete, got %v", segs.deleted)
	}
}

func TestDeleteSegmentNotFound(t *testing.T) {
	svc := newTestService(newFakeSegments(), &fakeSuppression{}, newFakeStore(), nil)
	if err := svc.DeleteSegment(context.Background(), "ghost"); err != errcode.NotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestCreateSegmentGeneratesCodeAndRejectsDuplicate(t *testing.T) {
	segs := newFakeSegments()
	svc := newTestService(segs, &fakeSuppression{}, newFakeStore(), nil)
	ctx := context.Background()

	seg, err := svc.CreateSegment(ctx, domain.SegmentInput{
		Name: "High Value Users", BizScene: "demo", AudienceRef: "ref-1", Operator: "alice",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(seg.Code, "high_value_users_") {
		t.Fatalf("expected slug-based code, got %s", seg.Code)
	}
	if seg.Kind != domain.SegmentKindInclude || seg.Status != domain.SegmentStatusActive {
		t.Fatalf("defaults not applied: %+v", seg)
	}

	if _, err := svc.CreateSegment(ctx, domain.SegmentInput{
		Code: seg.Code, Name: "dup", BizScene: "demo", AudienceRef: "ref-2",
	}); err == nil {
		t.Fatal("expected duplicate code to be rejected")
	}

	if _, err := svc.CreateSegment(ctx, domain.SegmentInput{
		Name: "bad", BizScene: "demo", AudienceRef: "ref", Kind: domain.SegmentKind("weird"),
	}); err == nil {
		t.Fatal("expected invalid kind to be rejected")
	}

	// 中文名无法产出 slug，仍要拿到合法 code
	cn, err := svc.CreateSegment(ctx, domain.SegmentInput{Name: "高价值用户", BizScene: "demo", AudienceRef: "ref-3"})
	if err != nil {
		t.Fatalf("create cn: %v", err)
	}
	if err := validateCode(cn.Code); err != nil {
		t.Fatalf("generated code invalid: %s (%v)", cn.Code, err)
	}
}

func TestRefreshSegmentCapsAndMarksEstimated(t *testing.T) {
	segs := newFakeSegments(&domain.AudienceSegment{
		Code: "big", Name: "big", BizScene: "demo", AudienceRef: "ref", Status: domain.SegmentStatusActive,
	})
	svc := newTestService(segs, &fakeSuppression{}, newFakeStore(), &fakeResolver{total: -1, pageSize: 5000})

	res, err := svc.RefreshSegment(context.Background(), "big", "alice")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !res.Estimated {
		t.Fatal("expected result to be flagged as estimated after hitting the cap")
	}
	if res.MemberCount < maxRefreshUsers {
		t.Fatalf("expected count to reach the cap, got %d", res.MemberCount)
	}
}

func TestRefreshSegmentResolverErrorIsRecordedNotReturned(t *testing.T) {
	segs := newFakeSegments(&domain.AudienceSegment{
		Code: "s1", Name: "s1", BizScene: "demo", AudienceRef: "ref", Status: domain.SegmentStatusActive,
	})
	svc := newTestService(segs, &fakeSuppression{}, newFakeStore(), &fakeResolver{err: errors.New("upstream 503")})

	res, err := svc.RefreshSegment(context.Background(), "s1", "alice")
	if err != nil {
		t.Fatalf("resolver failure must not fail the whole call: %v", err)
	}
	if !strings.Contains(res.Error, "upstream 503") {
		t.Fatalf("expected refresh error recorded, got %q", res.Error)
	}
	if segs.items["s1"].RefreshError == "" {
		t.Fatal("refresh_error should be persisted")
	}
}

func TestResolveExcludeUserIDs(t *testing.T) {
	segs := newFakeSegments(&domain.AudienceSegment{
		Code: "ex", Name: "ex", BizScene: "demo", AudienceRef: "ref",
		Kind: domain.SegmentKindExclude, Status: domain.SegmentStatusActive,
	})
	svc := newTestService(segs, &fakeSuppression{}, newFakeStore(), &fakeResolver{total: 2500, pageSize: 1000})

	ids, err := svc.ResolveExcludeUserIDs(context.Background(), "ex")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(ids) != 2500 {
		t.Fatalf("expected 2500 ids, got %d", len(ids))
	}
	if _, ok := ids["u0"]; !ok {
		t.Fatal("missing first user")
	}
}

func TestResolveExcludeUserIDsFailsOverLimit(t *testing.T) {
	segs := newFakeSegments(&domain.AudienceSegment{
		Code: "ex", Name: "ex", BizScene: "demo", AudienceRef: "ref",
		Kind: domain.SegmentKindExclude, Status: domain.SegmentStatusActive,
	})
	svc := newTestService(segs, &fakeSuppression{}, newFakeStore(), &fakeResolver{total: -1, pageSize: 10000})

	if _, err := svc.ResolveExcludeUserIDs(context.Background(), "ex"); err == nil {
		t.Fatal("over-limit exclude segment must fail loudly instead of silently truncating")
	}
}

func TestResolveExcludeUserIDsRejectsDisabled(t *testing.T) {
	segs := newFakeSegments(&domain.AudienceSegment{
		Code: "ex", Name: "ex", BizScene: "demo", AudienceRef: "ref",
		Kind: domain.SegmentKindExclude, Status: domain.SegmentStatusDisabled,
	})
	svc := newTestService(segs, &fakeSuppression{}, newFakeStore(), &fakeResolver{total: 10})

	if _, err := svc.ResolveExcludeUserIDs(context.Background(), "ex"); err == nil {
		t.Fatal("disabled exclude segment must not silently resolve to an empty set")
	}
}

func TestAddSuppressionsBlacklistNormalizesChannel(t *testing.T) {
	sup := &fakeSuppression{}
	store := newFakeStore()
	svc := newTestService(newFakeSegments(), sup, store, nil)

	res, err := svc.AddSuppressions(context.Background(), domain.SuppressionInput{
		Kind:    domain.SuppressionBlacklist,
		UserIDs: []string{"u1", "u2"},
		Channel: "sms", // 黑名单是全渠道，传什么都得归一化成 "*"
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if res.Added != 2 {
		t.Fatalf("expected 2 added, got %d", res.Added)
	}
	for _, row := range sup.rows {
		if row.Channel != domain.SuppressionAllChannels {
			t.Fatalf("blacklist channel must be %q, got %q", domain.SuppressionAllChannels, row.Channel)
		}
	}
	if len(store.blacklist) != 2 || len(store.unsub) != 0 {
		t.Fatalf("blacklist must go to the blacklist key only: %+v", store)
	}
}

func TestAddSuppressionsUnsubscribeRequiresValidChannel(t *testing.T) {
	svc := newTestService(newFakeSegments(), &fakeSuppression{}, newFakeStore(), nil)
	ctx := context.Background()

	if _, err := svc.AddSuppressions(ctx, domain.SuppressionInput{
		Kind: domain.SuppressionUnsubscribe, UserIDs: []string{"u1"},
	}); err == nil {
		t.Fatal("unsubscribe without channel must fail")
	}
	if _, err := svc.AddSuppressions(ctx, domain.SuppressionInput{
		Kind: domain.SuppressionUnsubscribe, UserIDs: []string{"u1"}, Channel: "carrier-pigeon",
	}); err == nil {
		t.Fatal("unsubscribe with unknown channel must fail")
	}
	if _, err := svc.AddSuppressions(ctx, domain.SuppressionInput{
		Kind: domain.SuppressionKind("nope"), UserIDs: []string{"u1"},
	}); err == nil {
		t.Fatal("unknown kind must fail")
	}
}

func TestAddSuppressionsDedupesAndTrimsUserIDs(t *testing.T) {
	sup := &fakeSuppression{}
	store := newFakeStore()
	svc := newTestService(newFakeSegments(), sup, store, nil)

	res, err := svc.AddSuppressions(context.Background(), domain.SuppressionInput{
		Kind:    domain.SuppressionUnsubscribe,
		Channel: string(domain.ChannelSMS),
		UserIDs: []string{" u1 ", "u1", "", "   ", "u2", "u1"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if res.Submitted != 2 || res.Added != 2 {
		t.Fatalf("expected 2 unique ids, got submitted=%d added=%d", res.Submitted, res.Added)
	}
	if len(sup.rows) != 2 || sup.rows[0].UserID != "u1" || sup.rows[1].UserID != "u2" {
		t.Fatalf("unexpected rows: %+v", sup.rows)
	}

	// 二次提交同样的名单：全部命中唯一键，added=0 / skipped=2
	res2, err := svc.AddSuppressions(context.Background(), domain.SuppressionInput{
		Kind: domain.SuppressionUnsubscribe, Channel: string(domain.ChannelSMS), UserIDs: []string{"u1", "u2"},
	})
	if err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if res2.Added != 0 || res2.Skipped != 2 {
		t.Fatalf("expected idempotent re-add, got %+v", res2)
	}
}

func TestAddSuppressionsRejectsOversizedUserID(t *testing.T) {
	svc := newTestService(newFakeSegments(), &fakeSuppression{}, newFakeStore(), nil)
	long := strings.Repeat("x", maxUserIDLen+1)
	if _, err := svc.AddSuppressions(context.Background(), domain.SuppressionInput{
		Kind: domain.SuppressionBlacklist, UserIDs: []string{long},
	}); err == nil {
		t.Fatal("user_id longer than the column must be rejected before hitting the DB")
	}
	if _, err := svc.AddSuppressions(context.Background(), domain.SuppressionInput{
		Kind: domain.SuppressionBlacklist, UserIDs: []string{" ", ""},
	}); err == nil {
		t.Fatal("empty user_ids must be rejected")
	}
}

func TestAddSuppressionsKeepsDBRowsWhenRedisFails(t *testing.T) {
	sup := &fakeSuppression{}
	store := newFakeStore()
	store.failAdd = true
	svc := newTestService(newFakeSegments(), sup, store, nil)

	res, err := svc.AddSuppressions(context.Background(), domain.SuppressionInput{
		Kind: domain.SuppressionBlacklist, UserIDs: []string{"u1", "u2"},
	})
	if err == nil {
		t.Fatal("redis failure must surface as an error")
	}
	if res == nil {
		t.Fatal("result must still be returned so the caller knows rows landed in the DB")
	}
	if res.Added != 2 || res.Synced || res.SyncError == "" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(sup.rows) != 2 {
		t.Fatalf("DB rows must not be rolled back, got %d", len(sup.rows))
	}
	if !strings.Contains(err.Error(), "2") {
		t.Fatalf("error should carry the persisted count, got %v", err)
	}
}

func TestRemoveSuppression(t *testing.T) {
	sup := &fakeSuppression{rows: []domain.SuppressionEntry{
		{Kind: domain.SuppressionBlacklist, UserID: "u1", Channel: domain.SuppressionAllChannels},
	}}
	store := newFakeStore()
	store.blacklist = []string{"u1"}
	svc := newTestService(newFakeSegments(), sup, store, nil)

	// blacklist 移除时 channel 同样归一化，传 sms 也要能删掉 "*" 那行
	res, err := svc.RemoveSuppression(context.Background(), domain.SuppressionBlacklist, "u1", "sms")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !res.Removed || !res.Synced {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(sup.rows) != 0 || len(store.blacklist) != 0 {
		t.Fatalf("row should be gone from both DB and redis: %+v %+v", sup.rows, store.blacklist)
	}

	if _, err := svc.RemoveSuppression(context.Background(), domain.SuppressionUnsubscribe, "u1", ""); err == nil {
		t.Fatal("unsubscribe removal without channel must fail")
	}
}

func TestSuppressionStats(t *testing.T) {
	sup := &fakeSuppression{rows: []domain.SuppressionEntry{
		{Kind: domain.SuppressionBlacklist, UserID: "u1", Channel: "*"},
		{Kind: domain.SuppressionUnsubscribe, UserID: "u2", Channel: "sms"},
		{Kind: domain.SuppressionUnsubscribe, UserID: "u3", Channel: "sms"},
	}}
	svc := newTestService(newFakeSegments(), sup, newFakeStore(), nil)

	stats, err := svc.SuppressionStats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Blacklist != 1 || stats.Unsubscribe != 2 || stats.Total != 3 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestAddSuppressionsWithoutStoreStillPersists(t *testing.T) {
	sup := &fakeSuppression{}
	svc := newTestService(newFakeSegments(), sup, nil, nil)

	res, err := svc.AddSuppressions(context.Background(), domain.SuppressionInput{
		Kind: domain.SuppressionBlacklist, UserIDs: []string{"u1"},
	})
	if err != nil {
		t.Fatalf("add without redis store: %v", err)
	}
	if res.Added != 1 || len(sup.rows) != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
}
