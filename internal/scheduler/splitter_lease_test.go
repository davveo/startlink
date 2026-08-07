package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

type leaseTasks struct {
	port.TaskRepository
	mu          sync.Mutex
	main        *domain.MainTask
	leaseOK     bool
	created     int
	plainCreate int
}

func (l *leaseTasks) GetMainTask(context.Context, uint64) (*domain.MainTask, error) {
	return l.main, nil
}

func (l *leaseTasks) RenewSplitLease(context.Context, uint64, string) (bool, error) {
	return true, nil
}

func (l *leaseTasks) CreateSubTasksWithLease(_ context.Context, _ uint64, _ string, tasks []domain.SubTask) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.leaseOK {
		return false, nil
	}
	l.created += len(tasks)
	return true, nil
}

func (l *leaseTasks) CreateSubTasks(_ context.Context, tasks []domain.SubTask) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.plainCreate += len(tasks)
	return nil
}

func (l *leaseTasks) PatchMainMeta(context.Context, uint64, int64, int) error { return nil }
func (l *leaseTasks) ClearSplitLease(context.Context, uint64) error           { return nil }
func (l *leaseTasks) UpdateMainTaskStats(context.Context, uint64, int64, int64, int64, int, domain.TaskStatus) (bool, error) {
	return true, nil
}
func (l *leaseTasks) CancelUnfinishedSubTasks(context.Context, uint64) (int64, error) {
	return 0, nil
}

func (l *leaseTasks) createdCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.created
}

type stubAudience struct {
	mu    sync.Mutex
	calls int
	pages []domain.AudiencePage
}

func (s *stubAudience) Resolve(context.Context, domain.AudienceQuery) (*domain.AudiencePage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.calls
	s.calls++
	if idx >= len(s.pages) {
		return &domain.AudiencePage{}, nil
	}
	page := s.pages[idx]
	return &page, nil
}

func (s *stubAudience) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func onePageAudience() *stubAudience {
	return &stubAudience{pages: []domain.AudiencePage{{
		Users: []domain.TargetUser{{UserID: "u1"}, {UserID: "u2"}},
	}}}
}

// 慢分页期间租约可能已被抢走：写入必须带租约条件，
// 否则会插入已作废的分片，留下永远 pending 的孤儿子任务。
func TestSplitAbortsWhenLeaseLostBeforeWrite(t *testing.T) {
	tasks := &leaseTasks{
		main:    &domain.MainTask{ID: 1, Status: domain.TaskStatusRunning, Channel: domain.ChannelSMS},
		leaseOK: false,
	}
	aud := onePageAudience()
	s := NewSplitter(tasks, aud, nil, 200)

	err := s.Split(context.Background(), tasks.main, "worker-1")
	if err == nil {
		t.Fatal("expected split to abort on lost lease")
	}
	if tasks.createdCount() != 0 {
		t.Fatalf("no subtask should be written after losing the lease, got %d", tasks.createdCount())
	}
}

func TestSplitWritesSubTasksWhileLeaseHeld(t *testing.T) {
	tasks := &leaseTasks{
		main:    &domain.MainTask{ID: 1, Status: domain.TaskStatusRunning, Channel: domain.ChannelSMS},
		leaseOK: true,
	}
	s := NewSplitter(tasks, onePageAudience(), nil, 200)

	if err := s.Split(context.Background(), tasks.main, "worker-1"); err != nil {
		t.Fatal(err)
	}
	if tasks.createdCount() != 1 {
		t.Fatalf("expected 1 shard, got %d", tasks.createdCount())
	}
}

// 过期活动圈人 → 建子任务 → 全量入队全是无效写入，入口必须短路
func TestSplitShortCircuitsExpiredCampaign(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	tasks := &leaseTasks{
		main: &domain.MainTask{
			ID: 1, Status: domain.TaskStatusRunning, Channel: domain.ChannelSMS, ExpireAt: &past,
		},
		leaseOK: true,
	}
	aud := onePageAudience()
	s := NewSplitter(tasks, aud, nil, 200)

	err := s.Split(context.Background(), tasks.main, "worker-1")
	if !errors.Is(err, domain.ErrCampaignExpired) {
		t.Fatalf("expected campaign-expired, got %v", err)
	}
	if aud.callCount() != 0 {
		t.Fatalf("expired campaign must not resolve audience, calls=%d", aud.callCount())
	}
	if tasks.createdCount() != 0 {
		t.Fatalf("expired campaign must not create subtasks, got %d", tasks.createdCount())
	}
}
