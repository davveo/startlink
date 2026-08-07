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

type paceTasks struct {
	port.TaskRepository
	mu      sync.Mutex
	renewed int
	renewOK bool
	renewAt int // 第几次续租开始返回失败
}

func (p *paceTasks) RenewSubTaskClaim(context.Context, uint64, string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.renewed++
	if p.renewAt > 0 && p.renewed >= p.renewAt {
		return false, nil
	}
	return p.renewOK, nil
}

func (p *paceTasks) renewCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.renewed
}

type countingPublisher struct {
	mu sync.Mutex
	n  int
}

func (c *countingPublisher) Publish(_ context.Context, msgs []domain.PushMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n += len(msgs)
	return nil
}

func (c *countingPublisher) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func paceMsgs(n int) []domain.PushMessage {
	msgs := make([]domain.PushMessage, n)
	for i := range msgs {
		msgs[i] = domain.PushMessage{UserID: "u", Channel: domain.ChannelSMS}
	}
	return msgs
}

// pace 限速下单个子任务入队可远超 60s 回收阈值，不续租必被另一 worker 抢占重发整个分片
func TestPublishMsgsRenewsClaimDuringPace(t *testing.T) {
	tasks := &paceTasks{renewOK: true}
	mq := &countingPublisher{}
	w := &Worker{tasks: tasks, mq: mq}
	st := &domain.SubTask{ID: 5, WorkerID: "w-1", Status: domain.TaskStatusRunning}
	main := &domain.MainTask{ID: 1, PaceQPS: 1000}

	if err := w.publishMsgs(context.Background(), main, st, paceMsgs(2*renewClaimEvery+1)); err != nil {
		t.Fatal(err)
	}
	if got := tasks.renewCount(); got != 2 {
		t.Fatalf("expected 2 renewals for %d messages, got %d", 2*renewClaimEvery+1, got)
	}
	if got := mq.count(); got != 2*renewClaimEvery+1 {
		t.Fatalf("published %d messages", got)
	}
}

func TestPublishMsgsAbortsWhenClaimLost(t *testing.T) {
	tasks := &paceTasks{renewOK: true, renewAt: 1}
	mq := &countingPublisher{}
	w := &Worker{tasks: tasks, mq: mq}
	st := &domain.SubTask{ID: 5, WorkerID: "w-1", Status: domain.TaskStatusRunning}
	main := &domain.MainTask{ID: 1, PaceQPS: 1000}

	err := w.publishMsgs(context.Background(), main, st, paceMsgs(3*renewClaimEvery))
	if !errors.Is(err, errClaimLost) {
		t.Fatalf("expected claim-lost abort, got %v", err)
	}
	if got := mq.count(); got > renewClaimEvery+1 {
		t.Fatalf("must stop publishing once the claim is lost, published %d", got)
	}
}

// pace=0 时整批一次投递，不需要续租
func TestPublishMsgsWithoutPaceSkipsRenew(t *testing.T) {
	tasks := &paceTasks{renewOK: true}
	mq := &countingPublisher{}
	w := &Worker{tasks: tasks, mq: mq}
	st := &domain.SubTask{ID: 5, WorkerID: "w-1"}

	if err := w.publishMsgs(context.Background(), &domain.MainTask{ID: 1}, st, paceMsgs(200)); err != nil {
		t.Fatal(err)
	}
	if tasks.renewCount() != 0 || mq.count() != 200 {
		t.Fatalf("renew=%d published=%d", tasks.renewCount(), mq.count())
	}
}

func TestIsExpired(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	var zero time.Time

	if !isExpired(&domain.MainTask{ExpireAt: &past}) {
		t.Fatal("past expire_at should be expired")
	}
	if isExpired(&domain.MainTask{ExpireAt: &future}) {
		t.Fatal("future expire_at should not be expired")
	}
	if isExpired(&domain.MainTask{}) || isExpired(&domain.MainTask{ExpireAt: &zero}) || isExpired(nil) {
		t.Fatal("missing/zero expire_at means no expiry")
	}
}
