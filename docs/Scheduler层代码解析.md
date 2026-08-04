# 异步 Scheduler 层 · 精细化代码解析

本文面向源码阅读，拆解 `cmd/scheduler` 如何把 API 落库的 `pending` 主任务，推进到「子任务入队 MQ + 主任务终态聚合」。配套总览见 [`创建活动主流程.md`](创建活动主流程.md)。

---

## 1. 在系统中的位置

```text
API 写入 main_tasks(pending)
        │
        ▼
┌──────────────── cmd/scheduler ────────────────┐
│  loopSplit  → Splitter.Split → sub_tasks      │
│  loopClaim × N → processSubTask → MQ.Publish  │
│  Aggregator.OnSubFinished → 终态 + Webhook    │
└───────────────────────────────────────────────┘
        │
        ▼
   Pusher 消费（不在本文范围）
```

**Scheduler 不管渠道发送**；它只负责：圈人拆片、把每个用户变成 `PushMessage` 丢进可插拔 MQ、按「子任务入队结果」聚合主任务状态。

---

## 2. 进程启动与依赖装配

入口：[`cmd/scheduler/main.go`](../cmd/scheduler/main.go)

| 步骤 | 代码 | 说明 |
|------|------|------|
| 1 | `bootstrap.NewInfra` | DB / Redis / MQ / Audience / Tasks |
| 2 | `MQ.EnsureReady` | 建好 high/normal Consumer Group（或等价物） |
| 3 | `Redis.Ping` | 聚合计数依赖 Redis |
| 4 | `NewAggregator` / `NewSplitter` / `NewWorker` | 三件套注入 |
| 5 | `signal.NotifyContext` + `worker.Run` | 优雅退出靠 ctx 取消 |

配置项（[`configs/config.yaml`](../configs/config.yaml) → `scheduler`）：

| 配置 | 用途 | 默认 |
|------|------|------|
| `batch_size` | 每个子任务用户数 = 圈人 `PageSize` | 200 |
| `worker_concurrency` | `loopClaim` 协程数 | 8 |
| `poll_interval_ms` | 拆分轮询 / 空闲认领休眠 | 500 |
| `claim_timeout_sec` | running 子任务可被他人接管的超时 | 60 |
| `split_lease_sec` | 拆分租约超时；卡单可被重拆 | 90 |

---

## 3. 协程模型：`Worker.Run`

文件：[`internal/scheduler/worker.go`](../internal/scheduler/worker.go)

```text
Run(ctx)
 ├── go loopSplit(ctx)          // 1 条：拆主任务
 └── go loopClaim(ctx, 0..N-1)  // N 条：认领子任务（N=worker_concurrency）
      wg.Wait()                 // 阻塞到 ctx 取消
```

- **拆分与认领解耦**：拆完立刻有 pending 子任务；认领协程不必等拆分循环同一拍。
- **水平扩展**：多起几个 scheduler 进程即可；抢占靠 DB，不靠选主。
- `worker.id` = `"scheduler-" + uuid[:8]`；认领时再用 `"%s-%d"` 拼上协程下标，写入 `sub_tasks.worker_id`。

---

## 4. 流水线 A：`loopSplit` —— 主任务拆分

### 4.1 循环骨架

```74:120:internal/scheduler/worker.go
func (w *Worker) loopSplit(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	// 每拍：
	// 1) splitPending：ListPending → MarkMainTaskRunning(owner, lease) → Split
	// 2) recoverStaleSplits：ListStaleSplit → ClaimStaleSplit → Split
}
```

每拍最多处理 **10** 条到期 pending，并最多回收 **10** 条拆分卡单。
`split_concurrency`（默认 2）控制同实例并行拆分数；拆分为**流式落库**（边圈人边 `CreateSubTasks`），拆分租约未清前不可 Claim；卡单重拆会先删半成品子任务。
入队前可读 `channel_quota` 利用率，高压时拉长 `pace`（`enqueue_slowdown`）；拆分结束对 `admission=enforce` 渠道做超容量 warn/pause。

### 4.2 哪些主任务会被列出

[`ListPendingMainTasks`](../internal/adapter/repo/task.go)：

```sql
status = 'pending'
AND (scheduled_at IS NULL OR scheduled_at <= NOW())
ORDER BY id ASC
LIMIT ?
```

定时任务未到点会一直待在 pending，拆分协程看得到但 `MarkMainTaskRunning` 前已被时间条件过滤掉。

### 4.3 多实例抢占：`MarkMainTaskRunning`

```sql
UPDATE main_tasks
SET status='running', started_at=NOW(),
    split_owner=?, split_lease_at=NOW()
WHERE id=? AND status='pending'
```

- `RowsAffected > 0` → 本实例独占拆分权并持有租约。
- `= 0` → 别的实例已抢走，或任务已被取消/改状态 → **静默跳过**。

### 4.4 拆分租约与卡单恢复

| 机制 | 说明 |
|------|------|
| 心跳 | `Splitter` 每圈人一页调用 `RenewSplitLease(id, owner)`；丢租约则中止拆分 |
| 结束清理 | 拆分成功/失败终态后 `ClearSplitLease` |
| 卡单定义 | `running` + `sub_task_total=0` + **无** `sub_tasks` 行 + 租约过期（或 lease 为空） |
| 回收 | `ClaimStaleSplitMainTask` 事务内锁行校验后改写 `split_owner`，再 `Split` |
| 配置 | `scheduler.split_lease_sec`（默认 90） |

> 已有子任务的 running 任务**不会**被重拆，避免重复分片。若崩溃发生在 `CreateSubTasks` 之后、`PatchMainMeta` 之前，需另做元数据修复（当前不自动重拆）。

### 4.5 抢占后再读一遍

防止：抢到后瞬间被 cancel/pause。若已取消则清子任务并 `ClearSplitLease`；若暂停则清租约并跳过。

---

## 5. 流水线 A 核心：`Splitter.Split`

文件：[`internal/scheduler/splitter.go`](../internal/scheduler/splitter.go)

### 5.1 输入 / 输出

| 入 | 出 |
|----|----|
| 已抢占的 `*MainTask`（running） | 批量 `sub_tasks` + 回写 `total_count` / `sub_task_total` |

### 5.2 圈人分页循环（逐步）

```text
pageToken = ""
shard = 0
loop:
  ① GetMainTask → 若 cancelled → 取消未完成子任务并 return error
  ② audience.Resolve(AudienceQuery{
       AudienceRef, BizScene, Extra←AudienceExtra,
       PageToken, PageSize=batch_size
     })
  ③ 空页且 !HasMore → break
  ④ 有用户 → 组装一个 SubTask：
       UserIDs JSON = {"user_ids":[...], "vars":{uid:{k:v}}}
       ShardIndex = shard++
       Status = pending
  ⑤ HasMore → pageToken = NextPageToken；否则 break
```

人群实现经 SPI：`AudienceResolver` ← bootstrap 注入的 `audience.Registry`（先 Provider，再 Filter）。

**死循环风险**：`HasMore=true` 但 `NextPageToken` 不变 → Split 永不结束。对接真实人群时必须保证 token 前进（README 二次开发已强调）。

### 5.3 子任务 payload 形态

```json
{
  "user_ids": ["u1", "u2", "..."],
  "vars": {
    "u1": {"name": "张三", "score": "100"}
  }
}
```

圈人页处理：`ab_sample_percent`（AudienceExtra）抽样 → `IntersectChannels(任务渠道链, 用户可达渠道)` → 求交为空则跳过用户 → 子任务 payload 可含 per-user `channels`。

### 5.4 取消窗口（拆分途中多次检查）

| 检查点 | 时机 | 动作 |
|--------|------|------|
| 循环内 | 每页圈人前 | 取消则清子任务；暂停则中止；`RenewSplitLease` 失败则丢租约退出 |
| Create 前 | 批量插入前 | 已取消 / 丢租约则不写库 |
| Create 后 | 插入成功后 | 取消则立刻批量 cancel 刚写入的子任务 |
| 成功结束 | meta 写完 | `ClearSplitLease` |

设计意图：拆分可能很久，运营随时可能点取消。

### 5.5 回写主任务元数据

1. `UpdateMainTaskStats(..., status=running)` —— 计数无锁递增；非终态不覆盖 `paused`/`cancelled`/终态。  
   - 失败：若已 cancelled → 清子任务并返回错误；否则打 warn。
2. `PatchMainMeta(total, subTotal)` —— 接口方法，只写 `total_count` / `sub_task_total`，**不改 status**；排除 `paused`/`cancelled`/终态。

> 拆分收尾不再把并发 `pause` 盖回 `running`。

空人群：返回 `errcode.AudienceEmpty` → `loopSplit` 将主任务标 **failed**。

---

## 6. 流水线 B：`loopClaim` —— 认领与投递

### 6.1 认领循环

```123:148:internal/scheduler/worker.go
st, err := w.tasks.ClaimSubTask(ctx, workerID, claimTimeoutSec)
if st == nil { sleep; continue }
err := w.processSubTask(ctx, st)
// cancelled/paused → continue（已在 process 内处理）
// 其他错误 → 子任务 failed + OnSubFinished(0, total)
```

空闲时 `Sleep(pollInterval)`，避免空转打爆 DB。

### 6.2 `ClaimSubTask`：水平扩展核心 SQL

文件：[`internal/adapter/repo/task.go`](../internal/adapter/repo/task.go) `ClaimSubTask`

事务内：

1. `SELECT ... FOR UPDATE SKIP LOCKED`  
   - JOIN `main_tasks`，要求 **主任务 status = running**（paused/cancelled 的子任务不会被认领）。  
   - 子任务条件：  
     - `pending` / `retrying`，或  
     - `running` 且 `claimed_at < now - claim_timeout`（超时接管）。  
   - `ORDER BY id ASC LIMIT 1`
2. 再 `UPDATE` 该行：`status=running, worker_id=?, claimed_at=?, started_at=?`  
   - 条件仍限制在 pending/retrying/running，防并发丢更新。

`SKIP LOCKED`：行被其他事务锁住则跳过，多实例不会堵在同一行上。

返回：

- 认领成功 → `*SubTask`
- 没有可认领 → `nil, nil`（不是错误）

### 6.3 `processSubTask` 逐步解析

#### Step 1：主任务门禁（投递前）

| 状态 | 行为 |
|------|------|
| `cancelled` | 子任务 → cancelled；返回 `errMainCancelled`（上层不记 fail 聚合） |
| `paused` | `ReleaseSubTask`：子任务回到 pending，清空 worker_id/claimed_at；返回 `errMainPaused` |
| 其他 | 继续 |

暂停设计：已认领但不发 MQ，放回池子，恢复后可再被认领。

#### Step 2：解析用户列表

`json.Unmarshal(st.UserIDs)` → `user_ids` + 可选 `vars`。

#### Step 3：组装 `[]PushMessage`

从 **主任务快照**拷贝（非实时读模板表）：

| 字段 | 来源 |
|------|------|
| `MsgID` | `{mainID}-{subID}-{userID}` |
| `Channel` / `Channels` / `ChannelMode` | `main.Channel*`（任务级策略） |
| `Priority` | `main.Priority` → 决定进 high/normal 队列 |
| `Body` | `main.TemplateBody`（创建时快照） |
| `Vars` | 该用户个性化变量 |

一个子任务 → **一批**消息（最多约 `batch_size` 条），一次 `Publish`。

#### Step 4：投递前二次门禁

圈人/组包与 Publish 之间再查一次主任务，防止这段时间被取消/暂停。

#### Step 5：`mq.Publish(msgs)`

走 [`PriorityRouter.Publish`](../internal/adapter/mq/priority_router.go)：按每条 `Priority` 拆到 high/normal。失败则整子任务失败（见下）。

#### Step 6：成功路径

```go
updated, err := UpdateSubTaskResult(subID, workerID, success=len(msgs), fail=0, status=success)
if !updated { /* 丢认领，不聚合 */ return }
agg.OnSubFinished(mainID, subID, success=len(msgs), fail=0)
```

#### Step 7：失败路径（`loopClaim` 捕获）

```go
updated, _ := UpdateSubTaskResult(subID, workerID, 0, TotalCount, failed, err)
if updated {
  OnSubFinished(mainID, subID, 0, TotalCount)
}
```

> **语义警告**：这里的 success/fail 是「是否成功写入 MQ」，不是渠道送达。主任务 `success_count` 当前也沿用该口径。

---

## 7. 流水线 C：`Aggregator.OnSubFinished`

文件：[`internal/scheduler/aggregator.go`](../internal/scheduler/aggregator.go)

### 7.1 算法

```text
1. TryMarkSubFinished(main, subID)：Redis SADD；非首次直接 return（幂等）
2. Redis HINCRBY：success / fail / done(+1)
3. 读 MainTask
4. 若已终态 → return
5. 无锁原子递增 DB `sub_task_done`（用户 success/fail 不在此累加入队增量）
6. 若 paused → return（不判终态）
7. 若 done < SubTaskTotal → return
8. 若 done >= SubTaskTotal：
     读 Redis 累计 success/fail → success / failed / partial（流水线入队口径）
     CAS 仅写终态 + version；冲突则重读并以增量 0 重试
9. 有 push_records 时 SyncMainUserCounts（渠道口径校准展示计数）
10. 异步 Webhook
```

### 7.2 `UpdateMainTaskStats`

- 计数：`UPDATE ... SET success_count=success_count+? ... WHERE id=?`（无 version）
- 终态：`WHERE id=? AND version=? AND status NOT IN (终态, paused)` 写 status / finished_at / version+1
- 非终态 `running`：仅允许从 pending/running/retrying 软更新，不盖 `paused`

### 7.3 Webhook

`notifyFinished` 开新 goroutine + 10s timeout，失败只打日志，**不回滚**主任务状态。URL：任务上的 `webhook_url`（可覆盖全局默认，由 webhook Client 处理）。Webhook 中的 `success_count`/`fail_count` 在终态校准后为渠道用户口径。

### 7.4 已知正确性坑（读代码时要对齐）

| 问题 | 原因 |
|------|------|
| 重复完成导致 done 虚高 | ~~已修复~~：`UpdateSubTaskResult` 校验 `worker_id`+状态；`OnSubFinished` 按 sub_id Redis SET 去重；重推 `SetSubDone` 清空集合 |
| CAS 冲突会丢计数 | ~~已修复~~：计数无锁递增；终态 CAS 重试增量置 0 |
| 暂停期间 done 已满 | 恢复后不会自动再走「刚好达总量」那次终态判定，除非后续还有 OnSubFinished |

---

## 8. 状态机（Scheduler 视角）

### 8.1 主任务

```text
pending ──MarkRunning──► running ──Split 成功──► running（有 sub）
                │                      │
                │                      ├─ 全部子任务入队成功口径聚合 ──► success/partial/failed
                │                      └─ Split 失败（非取消）──► failed
                │
paused ◄── API pause ── running/pending
paused ── resume ──► running（已有子任务）或 pending

cancelled ◄── API cancel（任意可取消态）
```

### 8.2 子任务

```text
pending ──Claim──► running ──Publish OK──► success
                      │
                      ├─ 主任务 paused ──Release──► pending
                      ├─ 主任务 cancelled ─────────► cancelled
                      ├─ Publish/解析失败 ─────────► failed
                      └─ 超时仍 running ──可被其他实例 Claim──► running（新 worker）
```

---

## 9. 建议断点 / 日志阅读顺序

| 顺序 | 位置 | 看什么 |
|------|------|--------|
| 1 | `cmd/scheduler/main.go` | 依赖是否齐全 |
| 2 | `Worker.Run` | 协程拓扑 |
| 3 | `loopSplit`：pending 抢占 + stale 回收 | 租约字段、重拆是否触发 |
| 4 | `Splitter.Split` 分页循环 | 人群页、`RenewSplitLease`、取消检查 |
| 5 | `CreateSubTasks` / `PatchMainMeta` | DB 子任务与总量 |
| 6 | `ClaimSubTask` SQL | SKIP LOCKED、超时接管 |
| 7 | `processSubTask` | PushMessage 字段、Publish、success 口径 |
| 8 | `OnSubFinished` | Redis done vs SubTaskTotal、终态与 Webhook |

本地跟读时可开多个 scheduler 实例，观察同一 `main_task` 是否只被拆一次、子任务 `worker_id` 是否分散。

---

## 10. 与上下游的契约小结

| 上游（API）交给 Scheduler | Scheduler 交给下游（Pusher） |
|---------------------------|------------------------------|
| `main_tasks` pending + 模板快照 + 渠道/优先级 + 人群引用 | `PushMessage`（含 Body/Vars/Channels/Priority）进入 MQ |
| cancel / pause / resume 改主任务状态 | 认领条件依赖 `main.status=running`；paused 释放子任务 |
| — | 主任务终态 Webhook（渠道结果不在此闭环） |

---

## 11. 文件索引

| 文件 | 职责 |
|------|------|
| [`cmd/scheduler/main.go`](../cmd/scheduler/main.go) | 进程入口 |
| [`internal/scheduler/worker.go`](../internal/scheduler/worker.go) | Run / loopSplit / loopClaim / processSubTask |
| [`internal/scheduler/splitter.go`](../internal/scheduler/splitter.go) | 圈人分页拆子任务 |
| [`internal/scheduler/aggregator.go`](../internal/scheduler/aggregator.go) | 子任务完成聚合 + Webhook |
| [`internal/adapter/repo/task.go`](../internal/adapter/repo/task.go) | ListPending / MarkRunning / Claim / Update* |
| [`internal/adapter/repo/task.go`](../internal/adapter/repo/task.go) | TaskRepo / PatchMainMeta / UpdateMainTaskStats |
| [`internal/adapter/mq/priority_router.go`](../internal/adapter/mq/priority_router.go) | Publish 按优先级分流 |
| [`internal/adapter/audience/registry.go`](../internal/adapter/audience/registry.go) | 人群 SPI 路由 |

---

*读完本文后，建议对照 [`创建活动主流程.md`](创建活动主流程.md) 把「同步创建」与「异步调度」两段拼成完整时间线；渠道发送细节见 `internal/push/gateway.go`。*
