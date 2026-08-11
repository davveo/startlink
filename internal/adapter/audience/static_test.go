package audience

import (
	"context"
	"testing"

	"github.com/starlink/push/internal/domain"
)

type memStore struct {
	rows []domain.AudienceSegmentMember
}

func (m *memStore) BulkUpsert(context.Context, []domain.AudienceSegmentMember) (int64, error) {
	return 0, nil
}
func (m *memStore) DeleteBySegment(context.Context, string) error { return nil }
func (m *memStore) List(context.Context, string, domain.ListSegmentMemberQuery) ([]domain.AudienceSegmentMember, int64, error) {
	return m.rows, int64(len(m.rows)), nil
}
func (m *memStore) Count(context.Context, string) (int64, error) { return int64(len(m.rows)), nil }
func (m *memStore) ListPage(_ context.Context, _ string, offset, limit int) ([]domain.AudienceSegmentMember, error) {
	if offset >= len(m.rows) {
		return nil, nil
	}
	end := offset + limit
	if end > len(m.rows) {
		end = len(m.rows)
	}
	return m.rows[offset:end], nil
}

func TestStaticProviderResolve(t *testing.T) {
	store := &memStore{rows: []domain.AudienceSegmentMember{
		{SegmentCode: "s1", UserID: "phone:1", Phone: "1"},
		{SegmentCode: "s1", UserID: "phone:2", Phone: "2"},
		{SegmentCode: "s1", UserID: "e@x.com", Email: "e@x.com"},
	}}
	p := NewStaticProvider(store)
	if !p.Supports("static") || p.Supports("demo") {
		t.Fatal("supports")
	}
	page, err := p.Resolve(context.Background(), domain.AudienceQuery{
		AudienceRef: "s1", BizScene: "static", PageSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 2 || !page.HasMore || page.TotalHint != 3 || page.Users[0].Extra["phone"] != "1" {
		t.Fatalf("%+v", page)
	}
	page2, err := p.Resolve(context.Background(), domain.AudienceQuery{
		AudienceRef: "s1", BizScene: "static", PageSize: 2, PageToken: page.NextPageToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Users) != 1 || page2.HasMore {
		t.Fatalf("%+v", page2)
	}
}
