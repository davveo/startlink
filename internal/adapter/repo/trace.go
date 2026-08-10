package repo

import (
	"context"
	"strings"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"gorm.io/gorm"
)

var _ port.TraceRepository = (*TraceRepo)(nil)

type TraceRepo struct{ db *gorm.DB }

func NewTraceRepo(db *gorm.DB) *TraceRepo { return &TraceRepo{db: db} }

func (r *TraceRepo) Append(ctx context.Context, ev *domain.TraceEvent) error {
	if ev == nil || strings.TrimSpace(ev.TraceID) == "" {
		return nil
	}
	if ev.Level == "" {
		ev.Level = domain.TraceLevelInfo
	}
	return r.db.WithContext(ctx).Create(ev).Error
}

func (r *TraceRepo) List(ctx context.Context, q domain.ListTraceQuery) ([]domain.TraceEvent, int64, error) {
	page, size := normalizePage(q.Page, q.PageSize, 100)
	db := r.db.WithContext(ctx).Model(&domain.TraceEvent{})
	db = applyTraceFilters(db, q)

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := "id DESC"
	if strings.EqualFold(strings.TrimSpace(q.Order), "asc") {
		order = "id ASC"
	}
	var items []domain.TraceEvent
	err := db.Order(order).Offset((page - 1) * size).Limit(size).Find(&items).Error
	return items, total, err
}

func (r *TraceRepo) ListByTraceID(ctx context.Context, traceID string, limit int) ([]domain.TraceEvent, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 5000 {
		limit = 2000
	}
	var items []domain.TraceEvent
	err := r.db.WithContext(ctx).
		Where("trace_id = ?", traceID).
		Order("id ASC").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *TraceRepo) Summaries(ctx context.Context, q domain.ListTraceQuery) ([]domain.TraceSummary, int64, error) {
	page, size := normalizePage(q.Page, q.PageSize, 100)

	// 以 main_tasks 为主表：即使事件尚未写出也能按业务键找到活动
	db := r.db.WithContext(ctx).Table("main_tasks AS m").
		Select(`
			m.trace_id AS trace_id,
			m.biz_id AS biz_id,
			m.id AS main_task_id,
			m.title AS title,
			m.status AS status,
			COALESCE(e.event_count, 0) AS event_count,
			COALESCE(e.error_count, 0) AS error_count,
			COALESCE(e.warn_count, 0) AS warn_count,
			e.first_at AS first_at,
			e.last_at AS last_at,
			e.last_event AS last_event,
			e.last_message AS last_message`).
		Joins(`LEFT JOIN (
			SELECT
				trace_id,
				COUNT(*) AS event_count,
				SUM(CASE WHEN level = 'error' THEN 1 ELSE 0 END) AS error_count,
				SUM(CASE WHEN level = 'warn' THEN 1 ELSE 0 END) AS warn_count,
				MIN(created_at) AS first_at,
				MAX(created_at) AS last_at,
				SUBSTRING_INDEX(GROUP_CONCAT(event ORDER BY id DESC), ',', 1) AS last_event,
				SUBSTRING_INDEX(GROUP_CONCAT(IFNULL(message,'') ORDER BY id DESC SEPARATOR '\x1e'), '\x1e', 1) AS last_message
			FROM trace_events
			GROUP BY trace_id
		) e ON e.trace_id = m.trace_id`).
		Where("m.trace_id <> '' AND m.trace_id IS NOT NULL")

	if tid := strings.TrimSpace(q.TraceID); tid != "" {
		db = db.Where("m.trace_id = ?", tid)
	}
	if biz := strings.TrimSpace(q.BizID); biz != "" {
		db = db.Where("m.biz_id = ?", biz)
	}
	if q.MainTaskID > 0 {
		db = db.Where("m.id = ?", q.MainTaskID)
	}
	if uid := strings.TrimSpace(q.UserID); uid != "" {
		db = db.Where("EXISTS (SELECT 1 FROM trace_events te WHERE te.trace_id = m.trace_id AND te.user_id = ?)", uid)
	}
	if lvl := strings.TrimSpace(q.Level); lvl != "" {
		db = db.Where("EXISTS (SELECT 1 FROM trace_events te WHERE te.trace_id = m.trace_id AND te.level = ?)", lvl)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []domain.TraceSummary
	err := db.Order("COALESCE(e.last_at, m.created_at) DESC").
		Offset((page - 1) * size).Limit(size).
		Scan(&items).Error
	return items, total, err
}

func (r *TraceRepo) StatsByTraceID(ctx context.Context, traceID string) (*domain.TraceStats, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return &domain.TraceStats{}, nil
	}
	type row struct {
		Stage string
		Level string
		Cnt   int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&domain.TraceEvent{}).
		Select("stage, level, COUNT(*) AS cnt").
		Where("trace_id = ?", traceID).
		Group("stage, level").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := &domain.TraceStats{}
	byStage := map[string]*domain.TraceStageStat{}
	stageOrder := []string{}
	for _, rw := range rows {
		out.EventCount += rw.Cnt
		switch rw.Level {
		case domain.TraceLevelError:
			out.ErrorCount += rw.Cnt
		case domain.TraceLevelWarn:
			out.WarnCount += rw.Cnt
		}
		st := byStage[rw.Stage]
		if st == nil {
			st = &domain.TraceStageStat{Stage: rw.Stage}
			byStage[rw.Stage] = st
			stageOrder = append(stageOrder, rw.Stage)
		}
		st.Count += rw.Cnt
		switch rw.Level {
		case domain.TraceLevelError:
			st.Error += rw.Cnt
		case domain.TraceLevelWarn:
			st.Warn += rw.Cnt
		}
	}
	for _, s := range stageOrder {
		out.Stages = append(out.Stages, *byStage[s])
	}
	return out, nil
}

func (r *TraceRepo) GetMainTaskByTraceID(ctx context.Context, traceID string) (*domain.MainTask, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var t domain.MainTask
	err := r.db.WithContext(ctx).Where("trace_id = ?", traceID).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func applyTraceFilters(db *gorm.DB, q domain.ListTraceQuery) *gorm.DB {
	if tid := strings.TrimSpace(q.TraceID); tid != "" {
		db = db.Where("trace_id = ?", tid)
	}
	if biz := strings.TrimSpace(q.BizID); biz != "" {
		db = db.Where("biz_id = ?", biz)
	}
	if q.MainTaskID > 0 {
		db = db.Where("main_task_id = ?", q.MainTaskID)
	}
	if q.SubTaskID > 0 {
		db = db.Where("sub_task_id = ?", q.SubTaskID)
	}
	if uid := strings.TrimSpace(q.UserID); uid != "" {
		db = db.Where("user_id = ?", uid)
	}
	if st := strings.TrimSpace(q.Stage); st != "" {
		db = db.Where("stage = ?", st)
	}
	if ev := strings.TrimSpace(q.Event); ev != "" {
		db = db.Where("event = ?", ev)
	}
	if lvl := strings.TrimSpace(q.Level); lvl != "" {
		db = db.Where("level = ?", lvl)
	} else if q.AnomalyOnly {
		db = db.Where("level IN ?", []string{domain.TraceLevelError, domain.TraceLevelWarn})
	}
	return db
}

func normalizePage(page, size, maxSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > maxSize {
		size = maxSize
	}
	return page, size
}
