package preference

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/starlink/push/internal/domain"
)

// fakeRepo 内存版 PreferenceRepository：本项目没有引入 sqlite/dockertest，
// service 与 resolver 的行为用假实现验证，SQL 正确性靠集成环境覆盖。
type fakeRepo struct {
	mu       sync.Mutex
	prefs    map[string]domain.UserPreference
	consents []domain.ConsentLog
	getCalls int
	getErr   error
	nextID   uint64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{prefs: map[string]domain.UserPreference{}}
}

func (f *fakeRepo) Get(_ context.Context, userID string) (*domain.UserPreference, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	p, ok := f.prefs[userID]
	if !ok {
		return nil, nil
	}
	cp := p
	return &cp, nil
}

func (f *fakeRepo) GetMany(_ context.Context, userIDs []string) (map[string]*domain.UserPreference, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]*domain.UserPreference{}
	for _, id := range userIDs {
		if p, ok := f.prefs[id]; ok {
			cp := p
			out[id] = &cp
		}
	}
	return out, nil
}

func (f *fakeRepo) Upsert(_ context.Context, pref *domain.UserPreference) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if pref == nil {
		return errors.New("nil pref")
	}
	cp := *pref
	if cp.ID == 0 {
		f.nextID++
		cp.ID = f.nextID
	}
	f.prefs[cp.UserID] = cp
	return nil
}

func (f *fakeRepo) List(_ context.Context, q domain.ListPreferenceQuery) ([]domain.UserPreference, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.UserPreference
	for _, p := range f.prefs {
		if q.UserID != "" && p.UserID != q.UserID {
			continue
		}
		if q.MarketingOptOut != nil && p.MarketingOptOut != *q.MarketingOptOut {
			continue
		}
		if q.Channel != "" && !p.IsChannelOptedOut(domain.ChannelType(strings.ToLower(q.Channel))) {
			continue
		}
		if q.Topic != "" && !p.IsTopicOptedOut(q.Topic) {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out, int64(len(out)), nil
}

func (f *fakeRepo) Delete(_ context.Context, userID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.prefs[userID]; !ok {
		return false, nil
	}
	delete(f.prefs, userID)
	return true, nil
}

func (f *fakeRepo) AppendConsent(_ context.Context, logs []domain.ConsentLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.consents = append(f.consents, logs...)
	return nil
}

func (f *fakeRepo) ListConsent(_ context.Context, q domain.ListConsentLogQuery) ([]domain.ConsentLog, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.ConsentLog
	for _, l := range f.consents {
		if q.UserID != "" && l.UserID != q.UserID {
			continue
		}
		if q.Action != "" && l.Action != q.Action {
			continue
		}
		if q.Scope != "" && l.Scope != q.Scope {
			continue
		}
		out = append(out, l)
	}
	return out, int64(len(out)), nil
}

func (f *fakeRepo) consentLogs() []domain.ConsentLog {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.ConsentLog, len(f.consents))
	copy(out, f.consents)
	return out
}

func (f *fakeRepo) resetConsents() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.consents = nil
}

func (f *fakeRepo) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getCalls
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func intPtr(i int) *int       { return &i }
func slicePtr(v []string) *[]string {
	return &v
}
