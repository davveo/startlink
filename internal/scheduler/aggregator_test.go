package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

type memAggCache struct {
	mu   sync.Mutex
	fin  map[string]struct{}
	done int64
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

func (m *memAggCache) IncrSubDone(_ context.Context, _ uint64, _, _ int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.done++
	return m.done, nil
}

func (m *memAggCache) GetSubDone(context.Context, uint64) (int64, int64, int64, error) {
	return 0, 0, m.done, nil
}
func (m *memAggCache) SetSubDone(context.Context, uint64, int64, int64, int64) error {
	return nil
}
func (m *memAggCache) Allow(context.Context, string, int, int) (bool, error) { return true, nil }
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
	main *domain.MainTask
}

func (s *stubTasks) GetMainTask(context.Context, uint64) (*domain.MainTask, error) {
	return s.main, nil
}
func (s *stubTasks) UpdateMainTaskStats(context.Context, uint64, int64, int64, int64, int, domain.TaskStatus) (bool, error) {
	return true, nil
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

var _ port.AggregatorCache = (*memAggCache)(nil)
