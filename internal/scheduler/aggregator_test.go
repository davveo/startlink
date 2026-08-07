package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

type memAggCache struct {
	mu      sync.Mutex
	fin     map[string]struct{}
	done    int64
	incrErr error
}

func (m *memAggCache) TryMarkSubFinished(_ context.Context, mainTaskID, subTaskID uint64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fin == nil {
		m.fin = map[string]struct{}{}
	}
	k := fmt.Sprintf("%d:%d", mainTaskID, subTaskID)
	if _, ok := m.fin[k]; ok {
		return false, nil
	}
	m.fin[k] = struct{}{}
	return true, nil
}

func (m *memAggCache) UnmarkSubFinished(_ context.Context, mainTaskID, subTaskID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.fin, fmt.Sprintf("%d:%d", mainTaskID, subTaskID))
	return nil
}

func (m *memAggCache) IncrSubDone(_ context.Context, _ uint64, _, _ int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.incrErr != nil {
		return 0, m.incrErr
	}
	m.done++
	return m.done, nil
}

func (m *memAggCache) marked(mainTaskID, subTaskID uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.fin[fmt.Sprintf("%d:%d", mainTaskID, subTaskID)]
	return ok
}

func (m *memAggCache) GetSubDone(context.Context, uint64) (int64, int64, int64, error) {
	return 0, 0, m.done, nil
}
func (m *memAggCache) SetSubDone(context.Context, uint64, int64, int64, int64) error {
	return nil
}
func (m *memAggCache) Allow(context.Context, string, int, int) (bool, error) { return true, nil }
func (m *memAggCache) AllowAll(context.Context, []port.FrequencyLimit) (bool, error) {
	return true, nil
}
func (m *memAggCache) HasDelivered(context.Context, uint64, string, domain.ChannelType) (bool, error) {
	return false, nil
}
func (m *memAggCache) MarkDelivered(context.Context, uint64, string, domain.ChannelType, int) error {
	return nil
}
func (m *memAggCache) ClearDelivered(context.Context, uint64, string, domain.ChannelType) error {
	return nil
}

type stubTasks struct {
	port.TaskRepository
	mu        sync.Mutex
	main      *domain.MainTask
	statsErr  error
	finals    []domain.TaskStatus
	subDoneUp int
}

func (s *stubTasks) GetMainTask(context.Context, uint64) (*domain.MainTask, error) {
	return s.main, nil
}

func (s *stubTasks) UpdateMainTaskStats(_ context.Context, _ uint64, _ int64, _, _ int64, subDoneDelta int, status domain.TaskStatus) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.statsErr != nil {
		return false, s.statsErr
	}
	s.subDoneUp += subDoneDelta
	if status.IsTerminal() {
		s.finals = append(s.finals, status)
	}
	return true, nil
}

func (s *stubTasks) finalStatuses() []domain.TaskStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.TaskStatus(nil), s.finals...)
}

func TestOnSubFinishedIdempotent(t *testing.T) {
	cache := &memAggCache{}
	tasks := &stubTasks{main: &domain.MainTask{
		ID: 1, Status: domain.TaskStatusRunning, SubTaskTotal: 10, Version: 1,
	}}
	agg := NewAggregator(tasks, cache, nil, nil, nil)

	ctx := context.Background()
	if err := agg.OnSubFinished(ctx, 1, 42, 10, 0); err != nil {
		t.Fatal(err)
	}
	if err := agg.OnSubFinished(ctx, 1, 42, 10, 0); err != nil {
		t.Fatal(err)
	}
	if cache.done != 1 {
		t.Fatalf("done=%d want 1 (duplicate should be skipped)", cache.done)
	}
}

// Redis 计数可能因重启/逐出/超 TTL 归零，主任务终态判定必须以 DB 的 sub_task_done 为权威，
// 否则活动永远停在 running：不写 finished_at、不发 Webhook、不发站内信。
func TestOnSubFinishedFallsBackToDBCounter(t *testing.T) {
	cache := &memAggCache{}
	tasks := &stubTasks{main: &domain.MainTask{
		ID: 7, Status: domain.TaskStatusRunning, SubTaskTotal: 3, SubTaskDone: 2, Version: 1,
	}}
	agg := NewAggregator(tasks, cache, nil, nil, nil)

	// Redis 视角只看到本次这一个完成（done=1），DB 视角是第 3 个（2+1）
	if err := agg.OnSubFinished(context.Background(), 7, 42, 10, 0); err != nil {
		t.Fatal(err)
	}
	if got := tasks.finalStatuses(); len(got) != 1 || got[0] != domain.TaskStatusSuccess {
		t.Fatalf("expected terminal transition from db counter, got %v", got)
	}
}

func TestOnSubFinishedWaitsWhenCountersDisagreeBelowTotal(t *testing.T) {
	cache := &memAggCache{}
	tasks := &stubTasks{main: &domain.MainTask{
		ID: 7, Status: domain.TaskStatusRunning, SubTaskTotal: 5, SubTaskDone: 1, Version: 1,
	}}
	agg := NewAggregator(tasks, cache, nil, nil, nil)

	if err := agg.OnSubFinished(context.Background(), 7, 42, 10, 0); err != nil {
		t.Fatal(err)
	}
	if got := tasks.finalStatuses(); len(got) != 0 {
		t.Fatalf("must not finish early, got %v", got)
	}
}

// 幂等标记打在聚合成功之前，后续任一步失败若不回滚，该子任务的聚合永久丢失，
// 调用方（worker）此时已把子任务置终态，没有任何重放路径。
func TestOnSubFinishedRollsBackMarkWhenStatsFail(t *testing.T) {
	cache := &memAggCache{}
	boom := errors.New("db down")
	tasks := &stubTasks{
		main:     &domain.MainTask{ID: 7, Status: domain.TaskStatusRunning, SubTaskTotal: 3, Version: 1},
		statsErr: boom,
	}
	agg := NewAggregator(tasks, cache, nil, nil, nil)
	ctx := context.Background()

	if err := agg.OnSubFinished(ctx, 7, 42, 10, 0); !errors.Is(err, boom) {
		t.Fatalf("expected db error, got %v", err)
	}
	if cache.marked(7, 42) {
		t.Fatal("idempotency mark must be rolled back so the aggregation can be replayed")
	}

	tasks.mu.Lock()
	tasks.statsErr = nil
	tasks.mu.Unlock()

	if err := agg.OnSubFinished(ctx, 7, 42, 10, 0); err != nil {
		t.Fatal(err)
	}
	if tasks.subDoneUp != 1 {
		t.Fatalf("replay should increment sub_task_done once, got %d", tasks.subDoneUp)
	}
}

// Redis 递增失败不能回滚标记（DB 已递增），否则重放会把 sub_task_done 加两次
func TestOnSubFinishedKeepsMarkWhenRedisIncrFails(t *testing.T) {
	cache := &memAggCache{incrErr: errors.New("redis down")}
	tasks := &stubTasks{main: &domain.MainTask{
		ID: 7, Status: domain.TaskStatusRunning, SubTaskTotal: 9, Version: 1,
	}}
	agg := NewAggregator(tasks, cache, nil, nil, nil)

	if err := agg.OnSubFinished(context.Background(), 7, 42, 10, 0); err != nil {
		t.Fatalf("redis counter loss must not fail the aggregation: %v", err)
	}
	if !cache.marked(7, 42) {
		t.Fatal("mark must survive: db counter already advanced")
	}
	if tasks.subDoneUp != 1 {
		t.Fatalf("sub_task_done should advance once, got %d", tasks.subDoneUp)
	}
}

func TestFinalizeStaleRecoversStuckMainTask(t *testing.T) {
	cache := &memAggCache{}
	tasks := &stubTasks{main: &domain.MainTask{
		ID: 7, Status: domain.TaskStatusRunning, SubTaskTotal: 3, SubTaskDone: 3, Version: 1,
	}}
	agg := NewAggregator(tasks, cache, nil, nil, nil)

	if err := agg.FinalizeStale(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if got := tasks.finalStatuses(); len(got) != 1 {
		t.Fatalf("reaper should push the terminal status once, got %v", got)
	}
}

func TestFinalizeStaleSkipsUnfinishedAndTerminal(t *testing.T) {
	cache := &memAggCache{}
	unfinished := &stubTasks{main: &domain.MainTask{
		ID: 7, Status: domain.TaskStatusRunning, SubTaskTotal: 3, SubTaskDone: 1, Version: 1,
	}}
	if err := NewAggregator(unfinished, cache, nil, nil, nil).FinalizeStale(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if got := unfinished.finalStatuses(); len(got) != 0 {
		t.Fatalf("unfinished task must stay running, got %v", got)
	}

	already := &stubTasks{main: &domain.MainTask{
		ID: 8, Status: domain.TaskStatusSuccess, SubTaskTotal: 3, SubTaskDone: 3, Version: 1,
	}}
	if err := NewAggregator(already, cache, nil, nil, nil).FinalizeStale(context.Background(), 8); err != nil {
		t.Fatal(err)
	}
	if got := already.finalStatuses(); len(got) != 0 {
		t.Fatalf("terminal task must be left alone, got %v", got)
	}
}

var _ port.AggregatorCache = (*memAggCache)(nil)
