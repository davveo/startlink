#!/usr/bin/env bash
# 写入全链路追踪演示数据（main_tasks + trace_events），供运营台「全链路日志」页查看。
#
# 用法：
#   ./scripts/seed_traces.sh
#   MYSQL_CONTAINER=starlink-mysql ./scripts/seed_traces.sh
#
# 依赖：已启动 docker 栈（make up），且 api 至少启动过一次（完成 AutoMigrate）。
set -euo pipefail

MYSQL_CONTAINER="${MYSQL_CONTAINER:-starlink-mysql}"
MYSQL_HOST="${MYSQL_HOST:-127.0.0.1}"
MYSQL_PORT="${MYSQL_PORT:-3306}"
MYSQL_USER="${MYSQL_USER:-root}"
MYSQL_PASS="${MYSQL_PASS:-root}"
MYSQL_DB="${MYSQL_DB:-starlink}"

SUFFIX="$(date +%s)"
TR1="tr_demo_ok_${SUFFIX}"
TR2="tr_demo_fail_${SUFFIX}"
TR3="tr_demo_suppress_${SUFFIX}"
BIZ1="demo-trace-ok-${SUFFIX}"
BIZ2="demo-trace-fail-${SUFFIX}"
BIZ3="demo-trace-suppress-${SUFFIX}"

mysql_exec() {
  # 优先走宿主机端口（不依赖 docker.sock）；失败再退回 docker exec
  if command -v mysql >/dev/null 2>&1; then
    if mysql -h"$MYSQL_HOST" -P"$MYSQL_PORT" -u"$MYSQL_USER" -p"$MYSQL_PASS" "$MYSQL_DB" \
      -Nse "SELECT 1" >/dev/null 2>&1; then
      mysql -h"$MYSQL_HOST" -P"$MYSQL_PORT" -u"$MYSQL_USER" -p"$MYSQL_PASS" "$MYSQL_DB" \
        --default-character-set=utf8mb4
      return
    fi
  fi
  docker exec -i "$MYSQL_CONTAINER" mysql -u"$MYSQL_USER" -p"$MYSQL_PASS" "$MYSQL_DB" --default-character-set=utf8mb4
}

echo "== 检查 MySQL ($MYSQL_HOST:$MYSQL_PORT / 容器 $MYSQL_CONTAINER) =="

# 等 trace_events 表就绪（api 首次迁移会创建）
ready=0
for i in $(seq 1 40); do
  if command -v mysql >/dev/null 2>&1 && \
    mysql -h"$MYSQL_HOST" -P"$MYSQL_PORT" -u"$MYSQL_USER" -p"$MYSQL_PASS" "$MYSQL_DB" \
      -Nse "SHOW TABLES LIKE 'trace_events'" 2>/dev/null | grep -q trace_events; then
    ready=1
    break
  fi
  if docker exec "$MYSQL_CONTAINER" mysql -u"$MYSQL_USER" -p"$MYSQL_PASS" "$MYSQL_DB" \
    -Nse "SHOW TABLES LIKE 'trace_events'" 2>/dev/null | grep -q trace_events; then
    ready=1
    break
  fi
  if [ "$i" -eq 40 ]; then
    echo "trace_events 表不存在：请先启动 api（make up / make rebuild）完成迁移"
    exit 1
  fi
  sleep 1
done
test "$ready" = 1

echo "== 写入 3 条演示活动 + 事件时间线 =="
mysql_exec <<SQL
SET NAMES utf8mb4;
SET @now = NOW(3);

-- 成功终态活动
INSERT INTO main_tasks (
  biz_id, trace_id, biz_scene, priority, title, channel, channels, channel_mode,
  template_id, template_body, missing_var_policy, audience_ref, audience_extra,
  topic, total_count, success_count, fail_count, sub_task_total, sub_task_done,
  status, version, created_by, created_at, updated_at, started_at, finished_at
) VALUES (
  '$BIZ1', '$TR1', 'demo', 'normal', '【演示】春季促销推送', 'inbox', '["inbox"]', 'single',
  'tpl_1', '你好 {{name}}', 'empty', 'demo:all', '{"total":20}',
  'promotion', 20, 18, 2, 2, 2,
  'partial', 1, 'seed', @now, @now, DATE_SUB(@now, INTERVAL 8 MINUTE), DATE_SUB(@now, INTERVAL 2 MINUTE)
);
SET @mid1 = LAST_INSERT_ID();

-- 拆分失败活动
INSERT INTO main_tasks (
  biz_id, trace_id, biz_scene, priority, title, channel, channels, channel_mode,
  template_id, template_body, missing_var_policy, audience_ref, audience_extra,
  topic, total_count, success_count, fail_count, sub_task_total, sub_task_done,
  status, version, created_by, created_at, updated_at, started_at, finished_at
) VALUES (
  '$BIZ2', '$TR2', 'demo', 'normal', '【演示】人群为空失败样例', 'sms', '["sms"]', 'single',
  'tpl_1', '验证码 {{code}}', 'empty', 'demo:empty', '{}',
  'txn', 0, 0, 0, 0, 0,
  'failed', 1, 'seed', @now, @now, DATE_SUB(@now, INTERVAL 15 MINUTE), DATE_SUB(@now, INTERVAL 14 MINUTE)
);
SET @mid2 = LAST_INSERT_ID();

-- 含用户抑制/限流异常的活动
INSERT INTO main_tasks (
  biz_id, trace_id, biz_scene, priority, title, channel, channels, channel_mode,
  template_id, template_body, missing_var_policy, audience_ref, audience_extra,
  topic, total_count, success_count, fail_count, sub_task_total, sub_task_done,
  status, version, created_by, created_at, updated_at, started_at, finished_at
) VALUES (
  '$BIZ3', '$TR3', 'demo', 'normal', '【演示】偏好抑制与限流样例', 'app_push', '["app_push","sms"]', 'fallback',
  'tpl_1', '限时优惠 {{offer}}', 'empty', 'demo:all', '{"total":50}',
  'promotion', 50, 40, 3, 3, 3,
  'partial', 1, 'seed', @now, @now, DATE_SUB(@now, INTERVAL 30 MINUTE), DATE_SUB(@now, INTERVAL 5 MINUTE)
);
SET @mid3 = LAST_INSERT_ID();

-- ===== TR1 成功偏部分失败时间线 =====
INSERT INTO trace_events (trace_id, biz_id, main_task_id, sub_task_id, msg_id, record_id, user_id, channel, stage, event, level, service, message, detail, created_at) VALUES
('$TR1', '$BIZ1', @mid1, 0, '', 0, '', '', 'campaign', 'campaign.created', 'info', 'api', '活动已创建', '{"draft":false}', DATE_SUB(@now, INTERVAL 10 MINUTE)),
('$TR1', '$BIZ1', @mid1, 0, '', 0, '', '', 'campaign', 'campaign.published', 'info', 'api', '草稿已发布，等待拆分', NULL, DATE_SUB(@now, INTERVAL 9 MINUTE)),
('$TR1', '$BIZ1', @mid1, 0, '', 0, '', '', 'split', 'split.started', 'info', 'scheduler', '开始拆分人群', NULL, DATE_SUB(@now, INTERVAL 8 MINUTE)),
('$TR1', '$BIZ1', @mid1, 0, '', 0, '', '', 'split', 'split.shard_created', 'info', 'scheduler', '分片 #0 已创建，本页 10 人', '{"shard":0,"users":10,"total":10}', DATE_SUB(@now, INTERVAL 7 MINUTE) + INTERVAL 10 SECOND),
('$TR1', '$BIZ1', @mid1, 0, '', 0, '', '', 'split', 'split.shard_created', 'info', 'scheduler', '分片 #1 已创建，本页 10 人', '{"shard":1,"users":10,"total":20}', DATE_SUB(@now, INTERVAL 7 MINUTE) + INTERVAL 20 SECOND),
('$TR1', '$BIZ1', @mid1, 0, '', 0, '', '', 'split', 'split.done', 'info', 'scheduler', '拆分完成：20 人 / 2 分片', '{"total":20,"shards":2}', DATE_SUB(@now, INTERVAL 7 MINUTE)),
('$TR1', '$BIZ1', @mid1, 101, '', 0, '', '', 'worker', 'subtask.claimed', 'info', 'scheduler', '子任务已认领，待入队 10 人', '{"users":10,"shard":0}', DATE_SUB(@now, INTERVAL 6 MINUTE)),
('$TR1', '$BIZ1', @mid1, 101, '', 0, '', '', 'worker', 'subtask.enqueued', 'info', 'scheduler', '已入队 10 条推送消息', '{"enqueued":10}', DATE_SUB(@now, INTERVAL 6 MINUTE) + INTERVAL 15 SECOND),
('$TR1', '$BIZ1', @mid1, 102, '', 0, '', '', 'worker', 'subtask.claimed', 'info', 'scheduler', '子任务已认领，待入队 10 人', '{"users":10,"shard":1}', DATE_SUB(@now, INTERVAL 5 MINUTE)),
('$TR1', '$BIZ1', @mid1, 102, '', 0, '', '', 'worker', 'subtask.enqueued', 'info', 'scheduler', '已入队 10 条推送消息', '{"enqueued":10}', DATE_SUB(@now, INTERVAL 5 MINUTE) + INTERVAL 15 SECOND),
('$TR1', '$BIZ1', @mid1, 101, '101-u_demo_3', 0, 'u_demo_3', 'inbox', 'pusher', 'push.failed', 'error', 'pusher', 'channel unreachable', '{"channel":"inbox","reason":"stub_fail"}', DATE_SUB(@now, INTERVAL 4 MINUTE)),
('$TR1', '$BIZ1', @mid1, 102, '102-u_demo_12', 0, 'u_demo_12', 'inbox', 'pusher', 'push.throttled', 'warn', 'pusher', '渠道限流，稍后重试', '{"channel":"inbox"}', DATE_SUB(@now, INTERVAL 3 MINUTE) + INTERVAL 30 SECOND),
('$TR1', '$BIZ1', @mid1, 101, '', 0, '', '', 'aggregator', 'subtask.aggregated', 'info', 'scheduler', '子任务聚合完成 success=10 fail=0 done=1/2', '{"success":10,"fail":0,"done":1,"total":2}', DATE_SUB(@now, INTERVAL 3 MINUTE)),
('$TR1', '$BIZ1', @mid1, 102, '', 0, '', '', 'aggregator', 'subtask.aggregated', 'info', 'scheduler', '子任务聚合完成 success=10 fail=0 done=2/2', '{"success":10,"fail":0,"done":2,"total":2}', DATE_SUB(@now, INTERVAL 2 MINUTE) + INTERVAL 30 SECOND),
('$TR1', '$BIZ1', @mid1, 0, '', 0, '', '', 'aggregator', 'campaign.finalized', 'warn', 'scheduler', '活动终态 partial（入队成功 20 / 失败 0）', '{"status":"partial","pipeline_success":20,"pipeline_fail":0}', DATE_SUB(@now, INTERVAL 2 MINUTE)),
('$TR1', '$BIZ1', @mid1, 101, '', 9001, 'u_demo_3', 'inbox', 'callback', 'receipt.applied', 'error', 'api', 'vendor rejected: invalid token', '{"receipt_event":"failed"}', DATE_SUB(@now, INTERVAL 1 MINUTE));

-- ===== TR2 拆分失败 =====
INSERT INTO trace_events (trace_id, biz_id, main_task_id, sub_task_id, msg_id, record_id, user_id, channel, stage, event, level, service, message, detail, created_at) VALUES
('$TR2', '$BIZ2', @mid2, 0, '', 0, '', '', 'campaign', 'campaign.created', 'info', 'api', '活动已创建', NULL, DATE_SUB(@now, INTERVAL 16 MINUTE)),
('$TR2', '$BIZ2', @mid2, 0, '', 0, '', '', 'campaign', 'campaign.published', 'info', 'api', '草稿已发布，等待拆分', NULL, DATE_SUB(@now, INTERVAL 15 MINUTE) + INTERVAL 20 SECOND),
('$TR2', '$BIZ2', @mid2, 0, '', 0, '', '', 'split', 'split.started', 'info', 'scheduler', '开始拆分人群', NULL, DATE_SUB(@now, INTERVAL 15 MINUTE)),
('$TR2', '$BIZ2', @mid2, 0, '', 0, '', '', 'split', 'split.failed', 'error', 'scheduler', 'audience empty', '{"error":"audience empty"}', DATE_SUB(@now, INTERVAL 14 MINUTE)),
('$TR2', '$BIZ2', @mid2, 0, '', 0, '', '', 'aggregator', 'campaign.finalized', 'error', 'scheduler', '活动终态 failed（入队成功 0 / 失败 0）', '{"status":"failed"}', DATE_SUB(@now, INTERVAL 14 MINUTE) + INTERVAL 5 SECOND);

-- ===== TR3 抑制 / 免打扰 / 限流 =====
INSERT INTO trace_events (trace_id, biz_id, main_task_id, sub_task_id, msg_id, record_id, user_id, channel, stage, event, level, service, message, detail, created_at) VALUES
('$TR3', '$BIZ3', @mid3, 0, '', 0, '', '', 'campaign', 'campaign.created', 'info', 'api', '活动已创建', NULL, DATE_SUB(@now, INTERVAL 32 MINUTE)),
('$TR3', '$BIZ3', @mid3, 0, '', 0, '', '', 'campaign', 'campaign.published', 'info', 'api', '草稿已发布，等待拆分', NULL, DATE_SUB(@now, INTERVAL 31 MINUTE)),
('$TR3', '$BIZ3', @mid3, 0, '', 0, '', '', 'split', 'split.started', 'info', 'scheduler', '开始拆分人群', NULL, DATE_SUB(@now, INTERVAL 30 MINUTE)),
('$TR3', '$BIZ3', @mid3, 0, '', 0, '', '', 'split', 'split.done', 'info', 'scheduler', '拆分完成：50 人 / 3 分片', '{"total":50,"shards":3}', DATE_SUB(@now, INTERVAL 28 MINUTE)),
('$TR3', '$BIZ3', @mid3, 201, '', 0, '', '', 'worker', 'subtask.enqueued', 'info', 'scheduler', '已入队 20 条推送消息', '{"enqueued":20}', DATE_SUB(@now, INTERVAL 26 MINUTE)),
('$TR3', '$BIZ3', @mid3, 202, '', 0, '', '', 'worker', 'subtask.enqueued', 'info', 'scheduler', '已入队 20 条推送消息', '{"enqueued":20}', DATE_SUB(@now, INTERVAL 25 MINUTE)),
('$TR3', '$BIZ3', @mid3, 203, '', 0, '', '', 'worker', 'subtask.enqueued', 'info', 'scheduler', '已入队 10 条推送消息', '{"enqueued":10}', DATE_SUB(@now, INTERVAL 24 MINUTE)),
('$TR3', '$BIZ3', @mid3, 201, '201-u_optout_1', 0, 'u_optout_1', 'app_push', 'pusher', 'push.suppressed', 'warn', 'pusher', 'preference: marketing disabled', '{"channel":"app_push","reason":"preference"}', DATE_SUB(@now, INTERVAL 20 MINUTE)),
('$TR3', '$BIZ3', @mid3, 201, '201-u_optout_2', 0, 'u_optout_2', 'sms', 'pusher', 'push.suppressed', 'warn', 'pusher', 'unsubscribed: channel=sms', '{"channel":"sms"}', DATE_SUB(@now, INTERVAL 19 MINUTE)),
('$TR3', '$BIZ3', @mid3, 202, '202-u_quiet_1', 0, 'u_quiet_1', 'app_push', 'pusher', 'push.deferred', 'warn', 'pusher', '用户免打扰时段', NULL, DATE_SUB(@now, INTERVAL 18 MINUTE)),
('$TR3', '$BIZ3', @mid3, 202, '202-u_throttle_1', 0, 'u_throttle_1', 'app_push', 'pusher', 'push.throttled', 'warn', 'pusher', '渠道限流，稍后重试', '{"channel":"app_push"}', DATE_SUB(@now, INTERVAL 12 MINUTE)),
('$TR3', '$BIZ3', @mid3, 203, '203-u_expire_1', 0, 'u_expire_1', 'app_push', 'pusher', 'push.expired', 'warn', 'pusher', '活动已过期', NULL, DATE_SUB(@now, INTERVAL 8 MINUTE)),
('$TR3', '$BIZ3', @mid3, 203, '203-u_fail_1', 0, 'u_fail_1', 'sms', 'pusher', 'push.failed', 'error', 'pusher', 'send failed [sms]: vendor timeout', '{"channel":"sms"}', DATE_SUB(@now, INTERVAL 7 MINUTE)),
('$TR3', '$BIZ3', @mid3, 0, '', 0, '', '', 'aggregator', 'campaign.finalized', 'warn', 'scheduler', '活动终态 partial（入队成功 50 / 失败 0）', '{"status":"partial"}', DATE_SUB(@now, INTERVAL 5 MINUTE));

SELECT @mid1 AS task_ok, @mid2 AS task_fail, @mid3 AS task_suppress;
SELECT COUNT(*) AS events FROM trace_events WHERE trace_id IN ('$TR1','$TR2','$TR3');
SQL

echo
echo "演示数据已写入："
echo "  1) $TR1  $BIZ1  （部分成功 + 回执失败）"
echo "  2) $TR2  $BIZ2  （拆分失败）"
echo "  3) $TR3  $BIZ3  （抑制/免打扰/限流）"
echo
echo "打开运营台：侧栏「全链路日志」或 /traces"
echo "直接打开时间线：/traces/$TR1"
echo "事件检索可试：用户 u_optout_1 / 级别 error"
