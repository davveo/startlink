#!/usr/bin/env bash
# 人群资产化 + 用户偏好中心的端到端冒烟，跑在已启动的本地栈上（make up / make rebuild）。
#
#   ./scripts/smoke_segments_preferences.sh
#   BASE=http://127.0.0.1:8080/api/v1 ADMIN_PASS=xxx ./scripts/smoke_segments_preferences.sh
set -uo pipefail

BASE="${BASE:-http://127.0.0.1:8080/api/v1}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASS="${ADMIN_PASS:-admin12345}"
VIEWER_USER="${VIEWER_USER:-viewer}"
VIEWER_PASS="${VIEWER_PASS:-viewer12345}"
REDIS_CONTAINER="${REDIS_CONTAINER:-starlink-redis}"
# 需要一个已审核通过的模板；本地栈默认 seed 了 tpl_1
TEMPLATE="${TEMPLATE:-tpl_1}"

JAR="$(mktemp -t starlink-smoke)"
PASS=0
FAIL=0
trap 'rm -f "$JAR"' EXIT

req() { # method path [body]
  local method="$1" path="$2"
  if [ "$#" -ge 3 ]; then
    curl -sS -b "$JAR" -c "$JAR" -X "$method" "$BASE$path" \
      -H 'Content-Type: application/json' --data-binary "$3"
  else
    curl -sS -b "$JAR" -c "$JAR" -X "$method" "$BASE$path"
  fi
}

ok() { PASS=$((PASS + 1)); printf '  PASS  %s\n' "$1"; }
no() { FAIL=$((FAIL + 1)); printf '  FAIL  %s\n        %s\n' "$1" "$2"; }

# has 断言响应包含某个片段
has() { # name want actual
  if printf '%s' "$3" | grep -q -- "$2"; then ok "$1"; else no "$1" "期望包含 $2，实际: $3"; fi
}

# rejected 断言「被业务规则拒绝」。直接匹配 '"code":4' 会把 40101 unauthorized
# 也算通过，那样会话一失效整个用例集就全绿，等于没测。
rejected() { # name actual
  if printf '%s' "$2" | grep -q '"code":0'; then
    no "$1" "不应成功，实际: $2"
  elif printf '%s' "$2" | grep -q '"code":40101'; then
    no "$1" "会话失效而非业务拒绝: $2"
  else
    ok "$1"
  fi
}

# denied 断言权限不足（403），区别于会话失效（401）
denied() { # name actual
  if printf '%s' "$2" | grep -qE '"code":403|forbidden'; then ok "$1"; else no "$1" "期望 403，实际: $2"; fi
}

section() { printf '\n== %s ==\n' "$1"; }

SUFFIX="$(date +%s)-$$"
SEG="smoke-inc-$SUFFIX"
EXC="smoke-exc-$SUFFIX"
USR="smoke-user-$SUFFIX"
BIZ="smoke-camp-$SUFFIX"

section "登录"
body='{"username":"'"$ADMIN_USER"'","password":"'"$ADMIN_PASS"'"}'
has "admin 登录成功" '"code":0' "$(req POST /auth/login "$body")"

section "人群段 CRUD"
body='{"code":"'"$SEG"'","name":"冒烟人群","kind":"include","biz_scene":"demo","audience_ref":"demo:all","audience_extra":{"total":30}}'
has "创建 include 人群段" '"code":0' "$(req POST /segments "$body")"
rejected "重复 code 被拒绝" "$(req POST /segments "$body")"
has "查询单个人群段" "\"code\":\"$SEG\"" "$(req GET "/segments/$SEG")"
has "列表可见新建人群段" "$SEG" "$(req GET '/segments?page=1&page_size=50')"
body='{"name":"冒烟人群改名","kind":"include","biz_scene":"demo","audience_ref":"demo:all"}'
has "更新人群段名称" '"code":0' "$(req PUT "/segments/$SEG" "$body")"
has "更新已生效" '冒烟人群改名' "$(req GET "/segments/$SEG")"
rejected "缺少必填字段的更新被拒" "$(req PUT "/segments/$SEG" '{"name":"只给名字"}')"
has "刷新成员数" '"member_count"' "$(req POST "/segments/$SEG/refresh" '{}')"

STATIC="smoke-static-$SUFFIX"
section "静态 CSV 人群"
body='{"code":"'"$STATIC"'","name":"静态短信名单","kind":"include","source":"static"}'
has "创建 static 人群段" '"code":0' "$(req POST /segments "$body")"
has "static 锁定 biz_scene" '"biz_scene":"static"' "$(req GET "/segments/$STATIC")"
body='{"mode":"replace","members":[{"phone":"13800138001"},{"email":"smoke@example.com","user_id":"u-mail"},{"phone":""}]}'
has "JSON 导入成员" '"accepted":2' "$(req POST "/segments/$STATIC/members/import" "$body")"
has "成员列表可见" '13800138001' "$(req GET "/segments/$STATIC/members?page=1&page_size=10")"
has "刷新静态成员数" '"member_count":2' "$(req POST "/segments/$STATIC/refresh" '{}')"
body='{"biz_id":"'"$BIZ"'-sms","title":"静态短信","template_id":"'"$TEMPLATE"'","segment_code":"'"$STATIC"'","channel":"sms","priority":"normal","as_draft":true}'
has "用静态人群创建短信活动" '"code":0' "$(req POST /campaigns "$body")"
rejected "动态段不可导入成员" "$(req POST "/segments/$SEG/members/import" '{"members":[{"phone":"1"}]}')"

body='{"code":"'"$EXC"'","name":"冒烟排除","kind":"exclude","biz_scene":"demo","audience_ref":"demo:all","audience_extra":{"total":5}}'
has "创建 exclude 排除名单" '"code":0' "$(req POST /segments "$body")"

section "人群段与活动联动"
body='{"biz_id":"'"$BIZ"'","title":"冒烟活动","template_id":"'"$TEMPLATE"'","segment_code":"'"$SEG"'","exclude_segment_code":"'"$EXC"'","topic":"promotion","channel":"inbox","priority":"normal","as_draft":true}'
has "用 segment_code 创建活动" '"code":0' "$(req POST /campaigns "$body")"

body='{"biz_id":"'"$BIZ"'-x","title":"坏活动","template_id":"'"$TEMPLATE"'","segment_code":"no-such-seg","channel":"inbox","priority":"normal","as_draft":true}'
rejected "引用不存在的人群段被拒" "$(req POST /campaigns "$body")"

body='{"biz_id":"'"$BIZ"'-y","title":"坏活动2","template_id":"'"$TEMPLATE"'","segment_code":"'"$SEG"'","exclude_segment_code":"'"$SEG"'","channel":"inbox","priority":"normal","as_draft":true}'
rejected "include 段不能当排除名单用" "$(req POST /campaigns "$body")"

rejected "被活动引用的人群段不可删除" "$(req DELETE "/segments/$SEG")"

section "黑名单 / 退订名单"
body='{"kind":"blacklist","user_ids":["'"$USR"'","'"$USR"'-2"],"reason":"smoke"}'
has "批量加入黑名单" '"code":0' "$(req POST /suppressions "$body")"
body='{"kind":"blacklist","user_ids":["'"$USR"'"],"reason":"smoke"}'
has "重复导入幂等" '"added":0' "$(req POST /suppressions "$body")"
body='{"kind":"unsubscribe","channel":"sms","user_ids":["'"$USR"'"]}'
has "加入渠道退订" '"code":0' "$(req POST /suppressions "$body")"
body='{"kind":"unsubscribe","user_ids":["'"$USR"'"]}'
rejected "退订缺少渠道被拒" "$(req POST /suppressions "$body")"
has "名单列表可见" "$USR" "$(req GET "/suppressions?kind=blacklist&user_id=$USR")"
has "名单统计" '"blacklist"' "$(req GET /suppressions/stats)"
has "移除黑名单" '"code":0' "$(req DELETE "/suppressions?kind=blacklist&user_id=$USR")"
has "移除后列表为空" '"total":0' "$(req GET "/suppressions?kind=blacklist&user_id=$USR")"

section "名单同步到 Redis"
has "Redis 黑名单命中" "$USR-2" \
  "$(docker exec "$REDIS_CONTAINER" redis-cli SMEMBERS starlink:blacklist 2>/dev/null)"
has "Redis 退订集合命中" "$USR" \
  "$(docker exec "$REDIS_CONTAINER" redis-cli SMEMBERS starlink:unsub:sms 2>/dev/null)"
if docker exec "$REDIS_CONTAINER" redis-cli SISMEMBER starlink:blacklist "$USR" 2>/dev/null | grep -q '^0$'; then
  ok "移除黑名单同步到 Redis"
else
  no "移除黑名单同步到 Redis" "Redis 中仍存在 $USR"
fi

section "用户偏好中心"
body='{"timezone":"Asia/Shanghai","marketing_opt_out":true,"opt_out_channels":["sms"],"opt_out_topics":["promotion"],"quiet_start":"22:00","quiet_end":"08:00"}'
has "写入用户偏好" '"code":0' "$(req PUT "/preferences/$USR" "$body")"
has "读回偏好" '"marketing_opt_out":true' "$(req GET "/preferences/$USR")"
has "偏好列表可筛选" "$USR" "$(req GET "/preferences?user_id=$USR")"
rejected "非法时区被拒" "$(req PUT "/preferences/$USR" '{"timezone":"Not/AZone"}')"
rejected "非法免打扰格式被拒" "$(req PUT "/preferences/$USR" '{"quiet_start":"9:00","quiet_end":"18:00"}')"
rejected "非法 preferred_hour 被拒" "$(req PUT "/preferences/$USR" '{"preferred_hour":25}')"
rejected "非法渠道被拒" "$(req PUT "/preferences/$USR" '{"opt_out_channels":["carrier-pigeon"]}')"
has "同意变更审计有记录" "$USR" "$(req GET "/consent-logs?user_id=$USR")"

BEFORE="$(req GET "/consent-logs?user_id=$USR" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["total"])' 2>/dev/null || echo -1)"
req PUT "/preferences/$USR" '{"marketing_opt_out":true}' >/dev/null
AFTER="$(req GET "/consent-logs?user_id=$USR" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["total"])' 2>/dev/null || echo -2)"
if [ "$BEFORE" = "$AFTER" ]; then
  ok "重复提交不产生新的同意记录 (total=$AFTER)"
else
  no "重复提交不产生新的同意记录" "提交前 $BEFORE 条，提交后 $AFTER 条"
fi

has "删除偏好" '"code":0' "$(req DELETE "/preferences/$USR")"

section "权限校验"
body='{"username":"'"$VIEWER_USER"'","password":"'"$VIEWER_PASS"'"}'
has "viewer 登录成功" '"code":0' "$(req POST /auth/login "$body")"
body='{"code":"viewer-should-fail","name":"x","kind":"include","biz_scene":"demo","audience_ref":"demo:all"}'
denied "viewer 不能创建人群段" "$(req POST /segments "$body")"
denied "viewer 不能读用户偏好" "$(req GET '/preferences?page=1')"
denied "viewer 不能改黑名单" "$(req POST /suppressions '{"kind":"blacklist","user_ids":["x"]}')"
has "viewer 可以读人群段" '"code":0' "$(req GET '/segments?page=1')"

printf '\n---------------------------------\n通过 %d  失败 %d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
