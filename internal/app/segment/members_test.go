package segment

import (
	"context"
	"strings"
	"testing"

	"github.com/starlink/push/internal/domain"
)

type fakeMembers struct {
	rows map[string][]domain.AudienceSegmentMember
}

func newFakeMembers() *fakeMembers {
	return &fakeMembers{rows: map[string][]domain.AudienceSegmentMember{}}
}

func (f *fakeMembers) BulkUpsert(_ context.Context, members []domain.AudienceSegmentMember) (int64, error) {
	var n int64
	for _, m := range members {
		list := f.rows[m.SegmentCode]
		found := false
		for i := range list {
			if list[i].UserID == m.UserID {
				list[i] = m
				found = true
				break
			}
		}
		if !found {
			list = append(list, m)
			n++
		}
		f.rows[m.SegmentCode] = list
	}
	return n, nil
}

func (f *fakeMembers) DeleteBySegment(_ context.Context, code string) error {
	delete(f.rows, code)
	return nil
}

func (f *fakeMembers) List(_ context.Context, code string, q domain.ListSegmentMemberQuery) ([]domain.AudienceSegmentMember, int64, error) {
	list := f.rows[code]
	return list, int64(len(list)), nil
}

func (f *fakeMembers) Count(_ context.Context, code string) (int64, error) {
	return int64(len(f.rows[code])), nil
}

func (f *fakeMembers) ListPage(_ context.Context, code string, offset, limit int) ([]domain.AudienceSegmentMember, error) {
	list := f.rows[code]
	if offset >= len(list) {
		return nil, nil
	}
	end := offset + limit
	if end > len(list) {
		end = len(list)
	}
	return list[offset:end], nil
}

func TestImportMembersJSON(t *testing.T) {
	segs := newFakeSegments(&domain.AudienceSegment{
		Code: "static_sms", Name: "短信名单", Kind: domain.SegmentKindInclude,
		Source: domain.SegmentSourceStatic, BizScene: domain.BizSceneStatic, AudienceRef: "static_sms",
		Status: domain.SegmentStatusActive,
	})
	mem := newFakeMembers()
	svc := NewService(segs, mem, nil, nil, nil)

	res, err := svc.ImportMembersJSON(context.Background(), "static_sms", domain.ImportSegmentMembersInput{
		Members: []domain.SegmentMemberInput{
			{Phone: "13800138000"},
			{Email: "a@example.com"},
			{UserID: "u1", Phone: "13900139000", Email: "b@example.com"},
			{Phone: ""}, // invalid
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Accepted != 3 || res.Skipped != 1 || res.MemberCount != 3 {
		t.Fatalf("got %+v", res)
	}
	if mem.rows["static_sms"][0].UserID != "phone:13800138000" {
		t.Fatalf("auto uid: %s", mem.rows["static_sms"][0].UserID)
	}
	tu := mem.rows["static_sms"][0].ToTargetUser()
	if tu.Extra["phone"] != "13800138000" {
		t.Fatalf("extra phone: %+v", tu.Extra)
	}
}

func TestImportMembersCSV(t *testing.T) {
	segs := newFakeSegments(&domain.AudienceSegment{
		Code: "csv1", Name: "CSV", Kind: domain.SegmentKindInclude,
		Source: domain.SegmentSourceStatic, BizScene: domain.BizSceneStatic, AudienceRef: "csv1",
		Status: domain.SegmentStatusActive,
	})
	mem := newFakeMembers()
	svc := NewService(segs, mem, nil, nil, nil)

	csv := "user_id,phone,email\n" +
		"u1,13800000001,\n" +
		",,a@test.com\n" +
		",,not-an-email\n"
	res, err := svc.ImportMembersCSV(context.Background(), "csv1", "replace", "admin", strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if res.Accepted != 2 || res.InvalidRows != 1 || res.MemberCount != 2 || !res.Replaced {
		t.Fatalf("got %+v", res)
	}
}

func TestCreateStaticSegment(t *testing.T) {
	segs := newFakeSegments()
	svc := NewService(segs, newFakeMembers(), nil, nil, nil)
	seg, err := svc.CreateSegment(context.Background(), domain.SegmentInput{
		Code: "my_static", Name: "静态", Source: domain.SegmentSourceStatic, Kind: domain.SegmentKindInclude,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seg.BizScene != domain.BizSceneStatic || seg.AudienceRef != "my_static" || seg.Source != domain.SegmentSourceStatic {
		t.Fatalf("%+v", seg)
	}
}

func TestImportRejectsProviderSegment(t *testing.T) {
	segs := newFakeSegments(&domain.AudienceSegment{
		Code: "demo_seg", Name: "Demo", Kind: domain.SegmentKindInclude,
		Source: domain.SegmentSourceProvider, BizScene: "demo", AudienceRef: "demo",
		Status: domain.SegmentStatusActive,
	})
	svc := NewService(segs, newFakeMembers(), nil, nil, nil)
	_, err := svc.ImportMembersJSON(context.Background(), "demo_seg", domain.ImportSegmentMembersInput{
		Members: []domain.SegmentMemberInput{{Phone: "1"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
