#!/usr/bin/env bash
# 验证偏好中心与排除名单在真实投递链路上生效：
# 发布一个小活动，检查被退订/被排除的用户是否真的没有发出去。
# 依赖已启动的本地栈（api + scheduler + pusher）。
set -uo pipefail

BASE="${BASE:-http://127.0.0.1:8080/api/v1}"
ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASS="${ADMIN_PASS:-admin12345}"
TEMPLATE="${TEMPLATE:-tpl_1}"
MYSQL_CONTAINER="${MYSQL_CONTAINER:-starlink-mysql}"
AUDIENCE_SIZE="${AUDIENCE_SIZE:-8}"

JAR="$(mktemp -t starlink-delivery)"
PASS=0
FAIL=0
trap 'rm -f "$JAR"' EXIT

req() {
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

sql() { docker exec -i "$MYSQL_CONTAINER" mysql -uroot -proot -N -B starlink -e "$1" 2>/dev/null; }

SUFFIX="$(date +%s)-$$"
REF="smoke$SUFFIX"
BIZ="delivery-$SUFFIX"
OPTOUT_USER="u_${REF}_1"
NORMAL_USER="u_${REF}_2"

printf '\n== 准备 ==\n'
body='{"username":"'"$ADMIN_USER"'","password":"'"$ADMIN_PASS"'"}'
if printf '%s' "$(req POST /auth/login "$body")" | grep -q '"code":0'; then
  ok "登录"
else
  no "登录" "无法登录，后续用例无意义"
  exit 1
fi

# 让 1 号用户退订营销
body='{"marketing_opt_out":true}'
if printf '%s' "$(req PUT "/preferences/$OPTOUT_USER" "$body")" | grep -q '"code":0'; then
  ok "为 $OPTOUT_USER 设置营销退订"
else
  no "设置营销退订" "写入偏好失败"
fi

printf '\n== 发布活动并等待投递 ==\n'
body='{"biz_id":"'"$BIZ"'","title":"投递冒烟","template_id":"'"$TEMPLATE"'","biz_scene":"demo","audience_ref":"'"$REF"'","audience_extra":{"total":'"$AUDIENCE_SIZE"'},"topic":"promotion","channel":"inbox","priority":"normal"}'
resp="$(req POST /campaigns "$body")"
if printf '%s' "$resp" | grep -q '"code":0'; then
  ok "创建并发布活动"
else
  no "创建并发布活动" "$resp"
  exit 1
fi

TASK_ID="$(printf '%s' "$resp" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["task_id"])')"
printf '  活动 task_id=%s，人群 %s 人\n' "$TASK_ID" "$AUDIENCE_SIZE"

for _ in $(seq 1 30); do
  total="$(sql "SELECT COUNT(*) FROM push_records WHERE main_task_id=$TASK_ID;")"
  [ "${total:-0}" -ge 1 ] && sleep 2 && break
  sleep 1
done

printf '\n== 校验投递结果 ==\n'
total="$(sql "SELECT COUNT(*) FROM push_records WHERE main_task_id=$TASK_ID;")"
printf '  共产生 %s 条流水\n' "${total:-0}"
if [ "${total:-0}" -ge 1 ]; then
  ok "活动已产生投递流水"
else
  no "活动已产生投递流水" "等待 30s 仍无流水，scheduler/pusher 可能未运行"
  exit 1
fi

status="$(sql "SELECT status FROM push_records WHERE main_task_id=$TASK_ID AND user_id='$OPTOUT_USER' LIMIT 1;")"
errmsg="$(sql "SELECT COALESCE(error_msg,'') FROM push_records WHERE main_task_id=$TASK_ID AND user_id='$OPTOUT_USER' LIMIT 1;")"
if [ "$status" = "suppressed" ]; then
  ok "营销退订用户被拦截 (status=suppressed)"
else
  no "营销退订用户被拦截" "期望 suppressed，实际 status='$status'"
fi
if printf '%s' "$errmsg" | grep -q 'marketing'; then
  ok "拦截原因写入流水: $errmsg"
else
  no "拦截原因写入流水" "error_msg='$errmsg'，无法区分是哪一维度拦的"
fi

nstatus="$(sql "SELECT status FROM push_records WHERE main_task_id=$TASK_ID AND user_id='$NORMAL_USER' LIMIT 1;")"
if [ "$nstatus" = "suppressed" ]; then
  no "未退订用户正常送达" "未退订用户也被拦了，status=$nstatus"
elif [ -n "$nstatus" ]; then
  ok "未退订用户正常送达 (status=$nstatus)"
else
  no "未退订用户正常送达" "查无流水"
fi

printf '\n  状态分布:\n'
sql "SELECT status, COUNT(*) FROM push_records WHERE main_task_id=$TASK_ID GROUP BY status;" |
  while IFS=$'\t' read -r st cnt; do printf '    %-14s %s\n' "$st" "$cnt"; done

printf '\n== 排除名单在拆分阶段生效 ==\n'
# demo provider 按 audience_ref 生成 u_<ref>_1..N，
# 所以「同一 ref、total=2」的排除段刚好覆盖前两个用户。
EXC_REF="excl$SUFFIX"
EXC_CODE="exc-$SUFFIX"
body='{"code":"'"$EXC_CODE"'","name":"投递排除","kind":"exclude","biz_scene":"demo","audience_ref":"'"$EXC_REF"'","audience_extra":{"total":2}}'
if printf '%s' "$(req POST /segments "$body")" | grep -q '"code":0'; then
  ok "创建排除名单"
else
  no "创建排除名单" "创建失败"
fi

BIZ2="delivery-exc-$SUFFIX"
body='{"biz_id":"'"$BIZ2"'","title":"排除冒烟","template_id":"'"$TEMPLATE"'","biz_scene":"demo","audience_ref":"'"$EXC_REF"'","audience_extra":{"total":6},"exclude_segment_code":"'"$EXC_CODE"'","channel":"inbox","priority":"normal"}'
resp2="$(req POST /campaigns "$body")"
if printf '%s' "$resp2" | grep -q '"code":0'; then
  ok "创建带排除名单的活动"
  TASK2="$(printf '%s' "$resp2" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["task_id"])')"
  for _ in $(seq 1 30); do
    n="$(sql "SELECT COUNT(*) FROM push_records WHERE main_task_id=$TASK2;")"
    [ "${n:-0}" -ge 1 ] && sleep 2 && break
    sleep 1
  done
  n="$(sql "SELECT COUNT(*) FROM push_records WHERE main_task_id=$TASK2;")"
  printf '  人群 6 人，排除 2 人，实际产生 %s 条流水\n' "${n:-0}"
  if [ "${n:-0}" -eq 4 ]; then
    ok "排除名单剔除了 2 个用户"
  else
    no "排除名单剔除了 2 个用户" "期望 4 条流水，实际 ${n:-0}"
  fi
  excluded="$(sql "SELECT COUNT(*) FROM push_records WHERE main_task_id=$TASK2 AND user_id IN ('u_${EXC_REF}_1','u_${EXC_REF}_2');")"
  if [ "${excluded:-1}" -eq 0 ]; then
    ok "被排除用户没有产生任何投递"
  else
    no "被排除用户没有产生任何投递" "仍有 ${excluded} 条属于被排除用户"
  fi
else
  no "创建带排除名单的活动" "$resp2"
fi

printf '\n---------------------------------\n通过 %d  失败 %d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
