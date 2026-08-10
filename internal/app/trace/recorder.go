package trace

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// Recorder 全链路事件写入器。
// 热路径必须 fail-open：写事件失败只打日志，绝不能拖垮投递。
type Recorder struct {
	repo    port.TraceRepository
	service string

	// 异步缓冲：用户级异常事件走这里，避免 gateway 热路径同步写库
	ch     chan *domain.TraceEvent
	once   sync.Once
	closed chan struct{}
	wg     sync.WaitGroup
}

// NewRecorder service 写入每条事件（api / scheduler / pusher）
func NewRecorder(repo port.TraceRepository, service string) *Recorder {
	r := &Recorder{
		repo:    repo,
		service: service,
		ch:      make(chan *domain.TraceEvent, 4096),
		closed:  make(chan struct{}),
	}
	r.once.Do(func() {
		r.wg.Add(1)
		go r.loop()
	})
	return r
}

func (r *Recorder) Close() {
	if r == nil {
		return
	}
	select {
	case <-r.closed:
		return
	default:
		close(r.closed)
	}
	r.wg.Wait()
}

func (r *Recorder) loop() {
	defer r.wg.Done()
	for {
		select {
		case <-r.closed:
			// 排空缓冲
			for {
				select {
				case ev := <-r.ch:
					r.write(context.Background(), ev)
				default:
					return
				}
			}
		case ev := <-r.ch:
			r.write(context.Background(), ev)
		}
	}
}

func (r *Recorder) write(ctx context.Context, ev *domain.TraceEvent) {
	if r == nil || r.repo == nil || ev == nil {
		return
	}
	if strings.TrimSpace(ev.TraceID) == "" {
		return
	}
	if ev.Service == "" {
		ev.Service = r.service
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now()
	}
	if err := r.repo.Append(ctx, ev); err != nil {
		slog.Warn("trace append failed",
			"trace_id", ev.TraceID,
			"event", ev.Event,
			"err", err,
		)
	}
}

// Record 同步写入（活动/子任务级低量事件用）
func (r *Recorder) Record(ctx context.Context, ev *domain.TraceEvent) {
	r.write(ctx, ev)
}

// RecordAsync 异步写入（用户级异常事件用）；缓冲满则降级为丢弃并打日志
func (r *Recorder) RecordAsync(ev *domain.TraceEvent) {
	if r == nil || ev == nil || strings.TrimSpace(ev.TraceID) == "" {
		return
	}
	if ev.Service == "" {
		ev.Service = r.service
	}
	select {
	case r.ch <- ev:
	default:
		slog.Warn("trace async buffer full, drop event",
			"trace_id", ev.TraceID,
			"event", ev.Event,
		)
	}
}

// Event 构造辅助，减少调用方样板代码
type Event struct {
	TraceID    string
	BizID      string
	MainTaskID uint64
	SubTaskID  uint64
	MsgID      string
	RecordID   uint64
	UserID     string
	Channel    string
	Stage      string
	Event      string
	Level      string
	Message    string
	Detail     map[string]any
}

func (r *Recorder) Emit(ctx context.Context, in Event) {
	r.Record(ctx, buildEvent(in))
}

func (r *Recorder) EmitAsync(in Event) {
	r.RecordAsync(buildEvent(in))
}

func buildEvent(in Event) *domain.TraceEvent {
	level := in.Level
	if level == "" {
		level = domain.TraceLevelInfo
	}
	ev := &domain.TraceEvent{
		TraceID:    strings.TrimSpace(in.TraceID),
		BizID:      strings.TrimSpace(in.BizID),
		MainTaskID: in.MainTaskID,
		SubTaskID:  in.SubTaskID,
		MsgID:      strings.TrimSpace(in.MsgID),
		RecordID:   in.RecordID,
		UserID:     strings.TrimSpace(in.UserID),
		Channel:    strings.TrimSpace(in.Channel),
		Stage:      in.Stage,
		Event:      in.Event,
		Level:      level,
		Message:    clampMsg(in.Message, 512),
		CreatedAt:  time.Now(),
	}
	if len(in.Detail) > 0 {
		ev.DetailJSON = domain.MarshalJSONColumn(in.Detail, false)
	}
	return ev
}

func clampMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// FromMain 从主任务填公共字段
func FromMain(main *domain.MainTask) Event {
	if main == nil {
		return Event{}
	}
	return Event{
		TraceID:    main.TraceID,
		BizID:      main.BizID,
		MainTaskID: main.ID,
	}
}

// FromMsg 从推送消息填公共字段
func FromMsg(msg domain.PushMessage) Event {
	return Event{
		TraceID:    msg.TraceID,
		BizID:      msg.BizID,
		MainTaskID: msg.MainTaskID,
		SubTaskID:  msg.SubTaskID,
		MsgID:      msg.MsgID,
		UserID:     msg.UserID,
		Channel:    string(msg.Channel),
	}
}
