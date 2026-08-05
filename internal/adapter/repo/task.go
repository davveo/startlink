package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
	"github.com/starlink/push/pkg/applog"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type TaskRepo struct {
	db *gorm.DB
}

func NewDB(dsn string, maxIdle, maxOpen int, gormLogger logger.Interface) (*gorm.DB, error) {
	if gormLogger == nil {
		gormLogger = applog.NewGormLogger("warn")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetConnMaxLifetime(time.Hour)
	return db, nil
}

func AutoMigrate(db *gorm.DB) error {
	// 多进程（api/scheduler/pusher）并发启动时串行化 DDL，避免 Error 1050
	const lockName = "starlink_schema_migrate"
	var got int
	if err := db.Raw("SELECT GET_LOCK(?, 120)", lockName).Scan(&got).Error; err != nil {
		return fmt.Errorf("acquire migrate lock: %w", err)
	}
	if got != 1 {
		return fmt.Errorf("acquire migrate lock timed out")
	}
	defer func() {
		_ = db.Exec("SELECT RELEASE_LOCK(?)", lockName).Error
	}()

	if db.Migrator().HasTable(&domain.PushRecord{}) {
		// 建唯一索引前清理历史重复流水，保留同维度最新一条
		_ = db.Exec(`
DELETE pr FROM push_records pr
INNER JOIN (
  SELECT main_task_id, user_id, channel, MAX(id) AS keep_id
  FROM push_records
  GROUP BY main_task_id, user_id, channel
  HAVING COUNT(*) > 1
) d ON pr.main_task_id = d.main_task_id
   AND pr.user_id = d.user_id
   AND pr.channel = d.channel
   AND pr.id <> d.keep_id
`).Error

		if !db.Migrator().HasColumn(&domain.PushRecord{}, "Provider") {
			_ = db.Migrator().AddColumn(&domain.PushRecord{}, "Provider")
		}
		_ = db.Exec(`UPDATE push_records SET provider_id = NULL WHERE provider_id = ''`).Error
		_ = db.Exec(`UPDATE push_records SET provider = channel WHERE (provider IS NULL OR provider = '') AND channel IS NOT NULL AND channel <> ''`).Error

		_ = db.Exec(`
DELETE pr FROM push_records pr
INNER JOIN (
  SELECT provider, channel, provider_id, MAX(id) AS keep_id
  FROM push_records
  WHERE provider_id IS NOT NULL AND provider_id <> ''
  GROUP BY provider, channel, provider_id
  HAVING COUNT(*) > 1
) d ON IFNULL(pr.provider,'') = IFNULL(d.provider,'')
   AND pr.channel = d.channel
   AND pr.provider_id = d.provider_id
   AND pr.id <> d.keep_id
`).Error
	}

	if db.Migrator().HasTable(&domain.PushReceipt{}) {
		_ = db.Exec(`
DELETE pr FROM push_receipts pr
INNER JOIN (
  SELECT push_record_id, event, MAX(id) AS keep_id
  FROM push_receipts
  GROUP BY push_record_id, event
  HAVING COUNT(*) > 1
) d ON pr.push_record_id = d.push_record_id
   AND pr.event = d.event
   AND pr.id <> d.keep_id
`).Error
	}

	return db.AutoMigrate(
		&domain.MainTask{},
		&domain.SubTask{},
		&domain.PushRecord{},
		&domain.PushReceipt{},
		&domain.Template{},
		&domain.ExportJob{},
	)
}

func NewTaskRepo(db *gorm.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

func (r *TaskRepo) CreateMainTask(ctx context.Context, task *domain.MainTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *TaskRepo) GetMainTask(ctx context.Context, id uint64) (*domain.MainTask, error) {
	var t domain.MainTask
	if err := r.db.WithContext(ctx).First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TaskRepo) GetMainTaskByBizID(ctx context.Context, bizID string) (*domain.MainTask, error) {
	var t domain.MainTask
	if err := r.db.WithContext(ctx).Where("biz_id = ?", bizID).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TaskRepo) MarkMainTaskRunning(ctx context.Context, id uint64, workerID string) (bool, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&domain.MainTask{}).
		Where("id = ? AND status = ?", id, domain.TaskStatusPending).
		Updates(map[string]any{
			"status":         domain.TaskStatusRunning,
			"started_at":     now,
			"split_owner":    workerID,
			"split_lease_at": now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *TaskRepo) RenewSplitLease(ctx context.Context, id uint64, workerID string) (bool, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&domain.MainTask{}).
		Where("id = ? AND status = ? AND split_owner = ?", id, domain.TaskStatusRunning, workerID).
		Updates(map[string]any{"split_lease_at": now})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *TaskRepo) ClearSplitLease(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&domain.MainTask{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"split_owner":    "",
			"split_lease_at": nil,
		}).Error
}

func (r *TaskRepo) ListStaleSplitMainTasks(ctx context.Context, leaseTimeoutSec int, limit int) ([]domain.MainTask, error) {
	if leaseTimeoutSec <= 0 {
		leaseTimeoutSec = 90
	}
	if limit <= 0 {
		limit = 10
	}
	deadline := time.Now().Add(-time.Duration(leaseTimeoutSec) * time.Second)
	var list []domain.MainTask
	// 租约过期且仍持有 split_owner：含无子任务卡单与流式拆分中途崩溃
	err := r.db.WithContext(ctx).
		Where(`status = ? AND split_owner <> '' AND split_owner IS NOT NULL AND (split_lease_at IS NULL OR split_lease_at < ?)`,
			domain.TaskStatusRunning, deadline).
		Order("id ASC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

func (r *TaskRepo) ClaimStaleSplitMainTask(ctx context.Context, id uint64, workerID string, leaseTimeoutSec int) (bool, error) {
	if leaseTimeoutSec <= 0 {
		leaseTimeoutSec = 90
	}
	deadline := time.Now().Add(-time.Duration(leaseTimeoutSec) * time.Second)
	now := time.Now()

	var claimed bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var main domain.MainTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).First(&main).Error; err != nil {
			return err
		}
		if main.Status != domain.TaskStatusRunning || main.SplitOwner == "" {
			return nil
		}
		if main.SplitLeaseAt != nil && !main.SplitLeaseAt.Before(deadline) {
			return nil // 租约仍有效
		}
		res := tx.Model(&domain.MainTask{}).
			Where("id = ? AND status = ? AND split_owner <> '' AND (split_lease_at IS NULL OR split_lease_at < ?)",
				id, domain.TaskStatusRunning, deadline).
			Updates(map[string]any{
				"split_owner":    workerID,
				"split_lease_at": now,
				"sub_task_total": 0,
				"total_count":    0,
			})
		if res.Error != nil {
			return res.Error
		}
		claimed = res.RowsAffected > 0
		return nil
	})
	return claimed, err
}

func (r *TaskRepo) ListPendingMainTasks(ctx context.Context, limit int) ([]domain.MainTask, error) {
	var list []domain.MainTask
	now := time.Now()
	err := r.db.WithContext(ctx).
		Where("status = ? AND (scheduled_at IS NULL OR scheduled_at <= ?)", domain.TaskStatusPending, now).
		Order("id ASC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

func (r *TaskRepo) ListMainTasks(ctx context.Context, q domain.ListCampaignQuery) ([]domain.MainTask, int64, error) {
	page := q.Page
	if page <= 0 {
		page = 1
	}
	size := q.PageSize
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}

	db := r.db.WithContext(ctx).Model(&domain.MainTask{})
	if q.BizScene != "" {
		db = db.Where("biz_scene = ?", q.BizScene)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Channel != "" {
		db = db.Where("channel = ? OR JSON_CONTAINS(channels, JSON_QUOTE(?))", q.Channel, string(q.Channel))
	}
	if q.Priority != "" {
		db = db.Where("priority = ?", q.Priority)
	}
	if q.CreatedBy != "" {
		db = db.Where("created_by = ?", q.CreatedBy)
	}
	if q.CreatedFrom != nil {
		db = db.Where("created_at >= ?", *q.CreatedFrom)
	}
	if q.CreatedTo != nil {
		db = db.Where("created_at <= ?", *q.CreatedTo)
	}
	if q.ScheduledFrom != nil {
		db = db.Where("scheduled_at >= ?", *q.ScheduledFrom)
	}
	if q.ScheduledTo != nil {
		db = db.Where("scheduled_at <= ?", *q.ScheduledTo)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("biz_id LIKE ? OR title LIKE ?", like, like)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []domain.MainTask
	err := db.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

// CancelMainTask 仅取消仍处于可执行状态的主任务
func (r *TaskRepo) CancelMainTask(ctx context.Context, id uint64) (bool, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&domain.MainTask{}).
		Where("id = ? AND status IN ?", id, []domain.TaskStatus{
			domain.TaskStatusPending,
			domain.TaskStatusRunning,
			domain.TaskStatusPaused,
			domain.TaskStatusRetrying,
		}).
		Updates(map[string]any{
			"status":      domain.TaskStatusCancelled,
			"finished_at": now,
			"version":     gorm.Expr("version + 1"),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *TaskRepo) PauseMainTask(ctx context.Context, id uint64) (bool, error) {
	res := r.db.WithContext(ctx).Model(&domain.MainTask{}).
		Where("id = ? AND status IN ?", id, []domain.TaskStatus{
			domain.TaskStatusPending,
			domain.TaskStatusRunning,
			domain.TaskStatusRetrying,
		}).
		Updates(map[string]any{
			"status":  domain.TaskStatusPaused,
			"version": gorm.Expr("version + 1"),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *TaskRepo) ResumeMainTask(ctx context.Context, id uint64, hasSubTasks bool) (bool, error) {
	next := domain.TaskStatusPending
	if hasSubTasks {
		next = domain.TaskStatusRunning
	}
	res := r.db.WithContext(ctx).Model(&domain.MainTask{}).
		Where("id = ? AND status = ?", id, domain.TaskStatusPaused).
		Updates(map[string]any{
			"status":      next,
			"finished_at": nil,
			"version":     gorm.Expr("version + 1"),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *TaskRepo) ReopenMainTask(ctx context.Context, id uint64, addSubTasks int) (bool, error) {
	updates := map[string]any{
		"status":      domain.TaskStatusRunning,
		"finished_at": nil,
		"version":     gorm.Expr("version + 1"),
	}
	if addSubTasks > 0 {
		updates["sub_task_total"] = gorm.Expr("sub_task_total + ?", addSubTasks)
	}
	res := r.db.WithContext(ctx).Model(&domain.MainTask{}).
		Where("id = ? AND status IN ?", id, []domain.TaskStatus{
			domain.TaskStatusFailed,
			domain.TaskStatusPartial,
		}).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// UpdateMainTaskStats 计数无锁原子递增（不丢增量）；终态切换才 CAS version。
// 非终态不会把 paused/cancelled/终态盖回 running。
func (r *TaskRepo) UpdateMainTaskStats(ctx context.Context, id uint64, version int64, successDelta, failDelta int64, subDoneDelta int, status domain.TaskStatus) (bool, error) {
	if successDelta != 0 || failDelta != 0 || subDoneDelta != 0 {
		res := r.db.WithContext(ctx).Model(&domain.MainTask{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"success_count": gorm.Expr("success_count + ?", successDelta),
				"fail_count":    gorm.Expr("fail_count + ?", failDelta),
				"sub_task_done": gorm.Expr("sub_task_done + ?", subDoneDelta),
			})
		if res.Error != nil {
			return false, res.Error
		}
	}

	if !status.IsTerminal() {
		if status == domain.TaskStatusRunning {
			_ = r.db.WithContext(ctx).Model(&domain.MainTask{}).
				Where("id = ? AND status IN ?", id, []domain.TaskStatus{
					domain.TaskStatusPending,
					domain.TaskStatusRunning,
					domain.TaskStatusRetrying,
				}).
				Update("status", domain.TaskStatusRunning)
		}
		return true, nil
	}

	now := time.Now()
	res := r.db.WithContext(ctx).Model(&domain.MainTask{}).
		Where("id = ? AND version = ? AND status NOT IN ?", id, version, []domain.TaskStatus{
			domain.TaskStatusSuccess,
			domain.TaskStatusPartial,
			domain.TaskStatusFailed,
			domain.TaskStatusCancelled,
			domain.TaskStatusPaused,
		}).
		Updates(map[string]any{
			"status":      status,
			"version":     version + 1,
			"finished_at": now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// PatchMainMeta 写入拆分后的总量；不改写 status，排除 paused/cancelled/终态
func (r *TaskRepo) PatchMainMeta(ctx context.Context, id uint64, total int64, subTotal int) error {
	return r.db.WithContext(ctx).Model(&domain.MainTask{}).
		Where("id = ? AND status NOT IN ?", id, []domain.TaskStatus{
			domain.TaskStatusCancelled,
			domain.TaskStatusPaused,
			domain.TaskStatusSuccess,
			domain.TaskStatusPartial,
			domain.TaskStatusFailed,
		}).
		Updates(map[string]any{
			"total_count":    total,
			"sub_task_total": subTotal,
		}).Error
}

func (r *TaskRepo) SyncMainUserCounts(ctx context.Context, id uint64, success, fail int64) error {
	return r.db.WithContext(ctx).Model(&domain.MainTask{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"success_count": success,
			"fail_count":    fail,
		}).Error
}

func (r *TaskRepo) CreateSubTasks(ctx context.Context, tasks []domain.SubTask) error {
	if len(tasks) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(tasks, 100).Error
}

func (r *TaskRepo) DeleteSubTasksByMainTask(ctx context.Context, mainTaskID uint64) (int64, error) {
	res := r.db.WithContext(ctx).Where("main_task_id = ?", mainTaskID).Delete(&domain.SubTask{})
	return res.RowsAffected, res.Error
}

// errClaimEmpty 表示本轮没有可认领子任务（空闲轮询，非故障）
var errClaimEmpty = errors.New("no claimable subtask")

// ClaimSubTask 使用 FOR UPDATE SKIP LOCKED 实现多实例水平扩展认领。
// 仅认领「主任务仍为 running 且拆分租约已清」且「子任务 pending/retrying/超时 running」的记录。
func (r *TaskRepo) ClaimSubTask(ctx context.Context, workerID string, claimTimeoutSec int) (*domain.SubTask, error) {
	var claimed *domain.SubTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var st domain.SubTask
		timeout := time.Now().Add(-time.Duration(claimTimeoutSec) * time.Second)
		// 用 Find 而不是 First：空队列时不产生 ErrRecordNotFound
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Table("sub_tasks").
			Select("sub_tasks.*").
			Joins(`JOIN main_tasks ON main_tasks.id = sub_tasks.main_task_id
				AND main_tasks.status = ?
				AND (main_tasks.split_owner = '' OR main_tasks.split_owner IS NULL)`, domain.TaskStatusRunning).
			Where(`(
				sub_tasks.status IN (?, ?)
				OR (sub_tasks.status = ? AND sub_tasks.claimed_at IS NOT NULL AND sub_tasks.claimed_at < ?)
			)`, domain.TaskStatusPending, domain.TaskStatusRetrying, domain.TaskStatusRunning, timeout).
			Order("sub_tasks.id ASC").
			Limit(1).
			Find(&st).Error
		if err != nil {
			return err
		}
		if st.ID == 0 {
			return errClaimEmpty
		}
		now := time.Now()
		res := tx.Model(&domain.SubTask{}).
			Where("id = ? AND status IN ?", st.ID, []domain.TaskStatus{
				domain.TaskStatusPending,
				domain.TaskStatusRetrying,
				domain.TaskStatusRunning,
			}).
			Updates(map[string]any{
				"status":     domain.TaskStatusRunning,
				"worker_id":  workerID,
				"claimed_at": now,
				"started_at": now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errClaimEmpty
		}
		st.Status = domain.TaskStatusRunning
		st.WorkerID = workerID
		st.ClaimedAt = &now
		st.StartedAt = &now
		claimed = &st
		return nil
	})
	if errors.Is(err, errClaimEmpty) {
		return nil, nil
	}
	return claimed, err
}

// CancelUnfinishedSubTasks 批量取消未完成子任务
func (r *TaskRepo) CancelUnfinishedSubTasks(ctx context.Context, mainTaskID uint64) (int64, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&domain.SubTask{}).
		Where("main_task_id = ? AND status IN ?", mainTaskID, []domain.TaskStatus{
			domain.TaskStatusPending,
			domain.TaskStatusRunning,
			domain.TaskStatusRetrying,
		}).
		Updates(map[string]any{
			"status":      domain.TaskStatusCancelled,
			"finished_at": now,
			"last_error":  "main task cancelled",
		})
	return res.RowsAffected, res.Error
}

func (r *TaskRepo) ReleaseSubTask(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&domain.SubTask{}).
		Where("id = ? AND status = ?", id, domain.TaskStatusRunning).
		Updates(map[string]any{
			"status":     domain.TaskStatusPending,
			"worker_id":  "",
			"claimed_at": nil,
			"started_at": nil,
			"last_error": "released due to pause",
		}).Error
}

func (r *TaskRepo) ResetFailedSubTasks(ctx context.Context, mainTaskID uint64) (int64, error) {
	res := r.db.WithContext(ctx).Model(&domain.SubTask{}).
		Where("main_task_id = ? AND status = ?", mainTaskID, domain.TaskStatusFailed).
		Updates(map[string]any{
			"status":        domain.TaskStatusPending,
			"success_count": 0,
			"fail_count":    0,
			"finished_at":   nil,
			"worker_id":     "",
			"claimed_at":    nil,
			"started_at":    nil,
			"last_error":    "",
			"retry_count":   gorm.Expr("retry_count + 1"),
		})
	return res.RowsAffected, res.Error
}

func (r *TaskRepo) MaxShardIndex(ctx context.Context, mainTaskID uint64) (int, error) {
	var maxIdx *int
	err := r.db.WithContext(ctx).Model(&domain.SubTask{}).
		Select("MAX(shard_index)").
		Where("main_task_id = ?", mainTaskID).
		Scan(&maxIdx).Error
	if err != nil {
		return -1, err
	}
	if maxIdx == nil {
		return -1, nil
	}
	return *maxIdx, nil
}

func (r *TaskRepo) ListSubTasksByStatus(ctx context.Context, mainTaskID uint64, status domain.TaskStatus) ([]domain.SubTask, error) {
	var list []domain.SubTask
	err := r.db.WithContext(ctx).
		Where("main_task_id = ? AND status = ?", mainTaskID, status).
		Find(&list).Error
	return list, err
}

func (r *TaskRepo) ListSubTasks(ctx context.Context, mainTaskID uint64, q domain.ListSubTaskQuery) ([]domain.SubTask, int64, error) {
	page := q.Page
	if page <= 0 {
		page = 1
	}
	size := q.PageSize
	if size <= 0 {
		size = 50
	}
	if size > 200 {
		size = 200
	}

	db := r.db.WithContext(ctx).Model(&domain.SubTask{}).Where("main_task_id = ?", mainTaskID)
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []domain.SubTask
	// 列表不需要巨型 user_ids JSON
	err := db.Select("id, main_task_id, shard_index, total_count, success_count, fail_count, status, retry_count, worker_id, claimed_at, started_at, finished_at, last_error, created_at, updated_at").
		Order("shard_index ASC, id ASC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&list).Error
	return list, total, err
}

func (r *TaskRepo) GetSubTask(ctx context.Context, id uint64) (*domain.SubTask, error) {
	var st domain.SubTask
	if err := r.db.WithContext(ctx).
		Select("id, main_task_id, shard_index, total_count, success_count, fail_count, status, retry_count, worker_id, claimed_at, started_at, finished_at, last_error, created_at, updated_at").
		First(&st, id).Error; err != nil {
		return nil, err
	}
	return &st, nil
}

func (r *TaskRepo) SyncMainCounters(ctx context.Context, id uint64, success, fail int64, subDone, subTotal int) error {
	updates := map[string]any{
		"success_count": success,
		"fail_count":    fail,
		"sub_task_done": subDone,
		"version":       gorm.Expr("version + 1"),
	}
	if subTotal >= 0 {
		updates["sub_task_total"] = subTotal
	}
	return r.db.WithContext(ctx).Model(&domain.MainTask{}).Where("id = ?", id).Updates(updates).Error
}

func (r *TaskRepo) UpdateSubTaskResult(ctx context.Context, id uint64, workerID string, success, fail int, status domain.TaskStatus, lastErr string) (bool, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&domain.SubTask{}).
		Where("id = ? AND worker_id = ? AND status IN ?", id, workerID, []domain.TaskStatus{
			domain.TaskStatusRunning,
			domain.TaskStatusRetrying,
		}).
		Updates(map[string]any{
			"success_count": success,
			"fail_count":    fail,
			"status":        status,
			"last_error":    lastErr,
			"finished_at":   now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *TaskRepo) SummarizeSubTasks(ctx context.Context, mainTaskID uint64) ([]domain.SubTaskStatusSummary, error) {
	type row struct {
		Status      domain.TaskStatus
		SubCount    int
		UserTotal   int64
		UserSuccess int64
		UserFail    int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&domain.SubTask{}).
		Select(`status,
			COUNT(*) AS sub_count,
			COALESCE(SUM(total_count), 0) AS user_total,
			COALESCE(SUM(success_count), 0) AS user_success,
			COALESCE(SUM(fail_count), 0) AS user_fail`).
		Where("main_task_id = ?", mainTaskID).
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]domain.SubTaskStatusSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.SubTaskStatusSummary{
			Status:      r.Status,
			SubCount:    r.SubCount,
			UserTotal:   r.UserTotal,
			UserSuccess: r.UserSuccess,
			UserFail:    r.UserFail,
		})
	}
	return out, nil
}

// PushRepo 推送流水
type PushRepo struct {
	db *gorm.DB
}

func NewPushRepo(db *gorm.DB) *PushRepo {
	return &PushRepo{db: db}
}

func (r *PushRepo) UpdateRecordStatus(ctx context.Context, id uint64, status domain.PushStatus, providerID, errMsg string) error {
	return r.UpdateRecordDelivery(ctx, id, status, "", providerID, errMsg)
}

func (r *PushRepo) UpdateRecordDelivery(ctx context.Context, id uint64, status domain.PushStatus, provider, providerID, errMsg string) error {
	var rec domain.PushRecord
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&rec).Error; err != nil {
		return err
	}
	if !rec.Status.CanTransitTo(status) {
		// 过期/回退事件：幂等忽略，避免冲掉已前进状态
		return nil
	}
	if rec.Status == status && provider == "" && providerID == "" && errMsg == "" {
		return nil
	}

	updates := map[string]any{"status": status}
	if provider != "" {
		updates["provider"] = provider
	}
	if providerID != "" {
		updates["provider_id"] = providerID
	}
	switch {
	case status == domain.PushStatusFailed || status.IsSuppressedLike():
		if errMsg != "" {
			updates["error_msg"] = errMsg
		}
	case status.DeliveredOK() || status == domain.PushStatusSending:
		updates["error_msg"] = errMsg // 成功路径清空；sending 占位通常为空
	case status == domain.PushStatusQueued || status == domain.PushStatusCancelled:
		if errMsg != "" {
			updates["error_msg"] = errMsg
		}
	}
	// 仅首次进入 sent 时写入 sent_at，送达/点击回执不得改写
	if status == domain.PushStatusSent && rec.SentAt == nil {
		now := time.Now()
		updates["sent_at"] = now
	}
	return r.db.WithContext(ctx).Model(&domain.PushRecord{}).Where("id = ?", id).Updates(updates).Error
}

func (r *PushRepo) GetRecordByProviderRef(ctx context.Context, provider string, channel domain.ChannelType, providerID string) (*domain.PushRecord, error) {
	if providerID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var rec domain.PushRecord
	q := r.db.WithContext(ctx).Where("provider_id = ?", providerID)
	if provider != "" {
		q = q.Where("provider = ?", provider)
	}
	if channel != "" {
		q = q.Where("channel = ?", channel)
	}
	if err := q.First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *PushRepo) ApplyReceipt(ctx context.Context, recordID uint64, status domain.PushStatus, errMsg string, receipt *domain.PushReceipt) error {
	if receipt == nil {
		return fmt.Errorf("nil receipt")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec domain.PushRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", recordID).First(&rec).Error; err != nil {
			return err
		}
		if rec.Status.CanTransitTo(status) && !(rec.Status == status && errMsg == "") {
			updates := map[string]any{"status": status}
			switch {
			case status == domain.PushStatusFailed || status.IsSuppressedLike():
				if errMsg != "" {
					updates["error_msg"] = errMsg
				}
			case status.DeliveredOK():
				updates["error_msg"] = errMsg
			}
			if err := tx.Model(&domain.PushRecord{}).Where("id = ?", recordID).Updates(updates).Error; err != nil {
				return err
			}
		}
		receipt.PushRecordID = recordID
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "push_record_id"}, {Name: "event"}},
			DoNothing: true,
		}).Create(receipt).Error
	})
}

func (r *PushRepo) CreateReceipt(ctx context.Context, receipt *domain.PushReceipt) error {
	if receipt == nil {
		return fmt.Errorf("nil receipt")
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "push_record_id"}, {Name: "event"}},
		DoNothing: true,
	}).Create(receipt).Error
	return err
}

func (r *PushRepo) ListFailedUserIDs(ctx context.Context, mainTaskID uint64) ([]string, error) {
	var ids []string
	// 有供应商失败流水、且该活动下无任何渠道成功投递的用户（抑制类不计入失败重推）
	err := r.db.WithContext(ctx).Raw(`
SELECT DISTINCT f.user_id
FROM push_records f
WHERE f.main_task_id = ? AND f.status = ? AND f.is_test = 0
  AND NOT EXISTS (
    SELECT 1 FROM push_records s
    WHERE s.main_task_id = f.main_task_id
      AND s.user_id = f.user_id
      AND s.is_test = 0
      AND s.status IN (?, ?, ?)
  )
`, mainTaskID, domain.PushStatusFailed,
		domain.PushStatusSent, domain.PushStatusDelivered, domain.PushStatusClicked,
	).Scan(&ids).Error
	return ids, err
}

func (r *PushRepo) CountUserOutcomes(ctx context.Context, mainTaskID uint64) (port.UserPushOutcomes, error) {
	var out port.UserPushOutcomes
	var n int64
	if err := r.db.WithContext(ctx).Model(&domain.PushRecord{}).
		Where("main_task_id = ? AND is_test = 0", mainTaskID).
		Count(&n).Error; err != nil {
		return out, err
	}
	out.HasRecords = n > 0
	if !out.HasRecords {
		return out, nil
	}
	if err := r.db.WithContext(ctx).Raw(`
SELECT COUNT(DISTINCT user_id) FROM push_records
WHERE main_task_id = ? AND is_test = 0 AND status IN (?, ?, ?)
`, mainTaskID, domain.PushStatusSent, domain.PushStatusDelivered, domain.PushStatusClicked).
		Scan(&out.SuccessUsers).Error; err != nil {
		return out, err
	}
	failed, err := r.ListFailedUserIDs(ctx, mainTaskID)
	if err != nil {
		return out, err
	}
	out.FailUsers = int64(len(failed))

	countExclusive := func(status domain.PushStatus) (int64, error) {
		var n int64
		err := r.db.WithContext(ctx).Raw(`
SELECT COUNT(DISTINCT u.user_id) FROM push_records u
WHERE u.main_task_id = ? AND u.status = ? AND u.is_test = 0
  AND NOT EXISTS (
    SELECT 1 FROM push_records s
    WHERE s.main_task_id = u.main_task_id AND s.user_id = u.user_id AND s.is_test = 0
      AND s.status IN (?, ?, ?, ?)
  )
`, mainTaskID, status,
			domain.PushStatusSent, domain.PushStatusDelivered, domain.PushStatusClicked, domain.PushStatusFailed,
		).Scan(&n).Error
		return n, err
	}
	if out.SuppressedUsers, err = countExclusive(domain.PushStatusSuppressed); err != nil {
		return out, err
	}
	if out.UnreachableUsers, err = countExclusive(domain.PushStatusUnreachable); err != nil {
		return out, err
	}
	if out.ExpiredUsers, err = countExclusive(domain.PushStatusExpired); err != nil {
		return out, err
	}
	if out.QuotaRejectedUsers, err = countExclusive(domain.PushStatusQuotaRejected); err != nil {
		return out, err
	}
	return out, nil
}

const deliveryInFlightStale = 2 * time.Minute

// ClaimDelivery 按用户+活动+渠道占位发送。
// duplicate：已成功投递，应跳过；inFlight：另一实例正在发送，宜稍后重试。
func (r *PushRepo) ClaimDelivery(ctx context.Context, rec *domain.PushRecord) (id uint64, duplicate, inFlight bool, err error) {
	if rec == nil {
		return 0, false, false, fmt.Errorf("nil push record")
	}
	rec.Status = domain.PushStatusSending
	rec.ErrorMsg = ""
	rec.ProviderID = nil
	if rec.Provider == "" {
		rec.Provider = string(rec.Channel)
	}

	err = r.db.WithContext(ctx).Create(rec).Error
	if err == nil {
		return rec.ID, false, false, nil
	}
	if !isDuplicateKey(err) {
		return 0, false, false, err
	}

	var existing domain.PushRecord
	if err := r.db.WithContext(ctx).
		Where("main_task_id = ? AND user_id = ? AND channel = ?", rec.MainTaskID, rec.UserID, rec.Channel).
		First(&existing).Error; err != nil {
		return 0, false, false, err
	}

	if existing.Status.DeliveredOK() {
		return existing.ID, true, false, nil
	}

	if existing.Status == domain.PushStatusSending && time.Since(existing.UpdatedAt) < deliveryInFlightStale {
		return existing.ID, false, true, nil
	}

	reclaimStatuses := []domain.PushStatus{
		domain.PushStatusFailed,
		domain.PushStatusCancelled,
		domain.PushStatusQueued,
		domain.PushStatusSuppressed,
		domain.PushStatusUnreachable,
		domain.PushStatusExpired,
		domain.PushStatusQuotaRejected,
		domain.PushStatusSending, // 陈旧 sending 可抢占
	}
	res := r.db.WithContext(ctx).Model(&domain.PushRecord{}).
		Where("id = ? AND status IN ?", existing.ID, reclaimStatuses).
		Updates(map[string]any{
			"sub_task_id": rec.SubTaskID,
			"content":     rec.Content,
			"status":      domain.PushStatusSending,
			"provider":    rec.Provider,
			"provider_id": nil,
			"error_msg":   "",
			"sent_at":     nil,
		})
	if res.Error != nil {
		return 0, false, false, res.Error
	}
	if res.RowsAffected == 0 {
		// 竞态：重新读取
		if err := r.db.WithContext(ctx).First(&existing, existing.ID).Error; err != nil {
			return 0, false, false, err
		}
		if existing.Status.DeliveredOK() {
			return existing.ID, true, false, nil
		}
		return existing.ID, false, true, nil
	}
	return existing.ID, false, false, nil
}

func isDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062
	}
	return false
}

// UpdateMainTaskFields 按字段局部更新主任务（草稿编辑 / 发布）
func (r *TaskRepo) UpdateMainTaskFields(ctx context.Context, id uint64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&domain.MainTask{}).Where("id = ?", id).Updates(fields).Error
}

func (r *PushRepo) ListPushRecords(ctx context.Context, mainTaskID uint64, q domain.ListPushRecordQuery) ([]domain.PushRecord, int64, error) {
	page, size := q.Page, q.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	db := r.db.WithContext(ctx).Model(&domain.PushRecord{}).
		Where("main_task_id = ? AND is_test = 0", mainTaskID)
	if q.UserID != "" {
		db = db.Where("user_id = ?", q.UserID)
	}
	if q.Channel != "" {
		db = db.Where("channel = ?", q.Channel)
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		db = db.Where("error_msg LIKE ? OR provider_id LIKE ? OR provider LIKE ?", like, like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []domain.PushRecord
	err := db.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

func (r *PushRepo) AggregateFailures(ctx context.Context, mainTaskID uint64) ([]port.FailureAggRow, error) {
	var rows []port.FailureAggRow
	err := r.db.WithContext(ctx).Raw(`
SELECT channel, IFNULL(provider,'') AS provider,
       LEFT(IFNULL(error_msg,''), 200) AS error_msg,
       COUNT(*) AS count
FROM push_records
WHERE main_task_id = ? AND is_test = 0 AND status = ?
GROUP BY channel, IFNULL(provider,''), LEFT(IFNULL(error_msg,''), 200)
ORDER BY count DESC
LIMIT 200
`, mainTaskID, domain.PushStatusFailed).Scan(&rows).Error
	return rows, err
}

func (r *PushRepo) CountStatusFunnel(ctx context.Context, mainTaskID uint64) (port.FunnelCounts, error) {
	var out port.FunnelCounts
	type row struct {
		Status domain.PushStatus
		N      int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(`
SELECT status, COUNT(*) AS n FROM push_records
WHERE main_task_id = ? AND is_test = 0
GROUP BY status
`, mainTaskID).Scan(&rows).Error
	if err != nil {
		return out, err
	}
	for _, x := range rows {
		switch x.Status {
		case domain.PushStatusQueued:
			out.Queued = x.N
		case domain.PushStatusSending:
			out.Sending = x.N
		case domain.PushStatusSent:
			out.Sent = x.N
		case domain.PushStatusDelivered:
			out.Delivered = x.N
		case domain.PushStatusClicked:
			out.Clicked = x.N
		case domain.PushStatusFailed:
			out.Failed = x.N
		case domain.PushStatusSuppressed:
			out.Suppressed = x.N
		case domain.PushStatusUnreachable:
			out.Unreachable = x.N
		case domain.PushStatusCancelled:
			out.Cancelled = x.N
		case domain.PushStatusExpired:
			out.Expired = x.N
		case domain.PushStatusQuotaRejected:
			out.QuotaRejected = x.N
		}
	}
	return out, nil
}

func (r *PushRepo) CreateTestRecord(ctx context.Context, rec *domain.PushRecord) error {
	rec.IsTest = true
	return r.db.WithContext(ctx).Create(rec).Error
}

func (r *PushRepo) CreateExportJob(ctx context.Context, job *domain.ExportJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *PushRepo) GetExportJob(ctx context.Context, id uint64) (*domain.ExportJob, error) {
	var job domain.ExportJob
	if err := r.db.WithContext(ctx).First(&job, id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *PushRepo) UpdateExportJob(ctx context.Context, id uint64, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&domain.ExportJob{}).Where("id = ?", id).Updates(fields).Error
}

func (r *PushRepo) IterPushRecords(ctx context.Context, mainTaskID uint64, fn func(domain.PushRecord) error) error {
	const batch = 500
	var lastID uint64
	for {
		var list []domain.PushRecord
		err := r.db.WithContext(ctx).
			Where("main_task_id = ? AND is_test = 0 AND id > ?", mainTaskID, lastID).
			Order("id ASC").Limit(batch).Find(&list).Error
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return nil
		}
		for _, rec := range list {
			lastID = rec.ID
			if err := fn(rec); err != nil {
				return err
			}
		}
	}
}
