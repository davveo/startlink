import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { ApiError } from '../api/client'
import {
  segmentApi,
  type SuppressionEntry,
  type SuppressionKind,
  type SuppressionStats,
} from '../api/segments'
import { useAuth } from '../auth/AuthContext'
import { Perm } from '../auth/permissions'
import {
  BtnRow,
  Button,
  Chip,
  Empty,
  Field,
  Input,
  Modal,
  Mono,
  PageHead,
  Panel,
  Select,
  Stat,
  TableWrap,
  Td,
  Textarea,
  Th,
  Toast,
} from '../components/ui'
import { useClampPage, useDebounced, useRequestSeq } from '../lib/async'

const pageSize = 20

const channels = ['app_push', 'sms', 'email', 'inbox', 'wecom', 'dingtalk'] as const

const channelLabels: Record<string, string> = {
  app_push: 'App推送',
  sms: '短信',
  email: '邮件',
  inbox: '站内信',
  wecom: '企业微信',
  dingtalk: '钉钉',
  '*': '全渠道',
}

function formatTime(v?: string) {
  if (!v) return '-'
  try {
    return new Date(v).toLocaleString()
  } catch {
    return v
  }
}

/** CSV 字段转义：包含分隔符/引号/换行时加引号 */
function csvCell(v: string) {
  if (/[",\n]/.test(v)) return `"${v.replace(/"/g, '""')}"`
  return v
}

export function SuppressionPage() {
  const { can } = useAuth()
  const canManage = can(Perm.SuppressionManage)

  const [items, setItems] = useState<SuppressionEntry[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [stats, setStats] = useState<SuppressionStats | null>(null)
  const [kind, setKind] = useState('')
  const [userId, setUserId] = useState('')
  const [channel, setChannel] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [reloadTick, setReloadTick] = useState(0)

  const [showAdd, setShowAdd] = useState(false)
  const [addForm, setAddForm] = useState({
    kind: 'blacklist' as SuppressionKind,
    channel: 'sms' as string,
    reason: '',
    userIdsText: '',
  })
  const [confirmRemove, setConfirmRemove] = useState<SuppressionEntry | null>(null)

  const seq = useRequestSeq()
  const userIdQ = useDebounced(userId)

  const load = useCallback(async () => {
    const s = seq.next()
    setBusy(true)
    setErr('')
    try {
      const [list, st] = await Promise.all([
        segmentApi.listSuppressions({
          kind: kind || undefined,
          user_id: userIdQ.trim() || undefined,
          channel: channel || undefined,
          page,
          page_size: pageSize,
        }),
        segmentApi.suppressionStats(),
      ])
      if (!seq.isLatest(s)) return
      setItems(list.items ?? [])
      setTotal(list.total ?? 0)
      setStats(st)
    } catch (e) {
      if (!seq.isLatest(s)) return
      setErr(e instanceof ApiError ? e.message : '加载抑制名单失败')
    } finally {
      if (seq.isLatest(s)) setBusy(false)
    }
  }, [channel, kind, page, seq, userIdQ])

  useEffect(() => {
    void load()
  }, [load, reloadTick])

  const pages = Math.max(1, Math.ceil(total / pageSize))
  useClampPage(page, total, pageSize, setPage)

  async function onAdd(e: FormEvent) {
    e.preventDefault()
    setErr('')
    setMsg('')
    const userIds = addForm.userIdsText
      .split(/[\s,;]+/)
      .map((s) => s.trim())
      .filter(Boolean)
    if (userIds.length === 0) {
      setErr('请至少填写一个 user_id')
      return
    }

    setBusy(true)
    try {
      const res = await segmentApi.addSuppressions({
        kind: addForm.kind,
        user_ids: userIds,
        channel: addForm.kind === 'unsubscribe' ? addForm.channel : undefined,
        reason: addForm.reason.trim() || undefined,
        source: 'console',
      })
      setMsg(`提交 ${res.submitted} 条：新增 ${res.added} 条，已存在 ${res.skipped} 条`)
      setShowAdd(false)
      setAddForm({ ...addForm, userIdsText: '', reason: '' })
    } catch (e) {
      // Redis 同步失败时后端已入库，错误文案里带了条数；列表照常刷新
      setErr(e instanceof ApiError ? e.message : '加入名单失败')
    } finally {
      setBusy(false)
      await load()
    }
  }

  async function onRemove() {
    if (!confirmRemove) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const res = await segmentApi.removeSuppression({
        kind: confirmRemove.kind,
        user_id: confirmRemove.user_id,
        channel: confirmRemove.channel,
      })
      setMsg(res.removed ? `已移除 ${confirmRemove.user_id}` : '该记录已不存在')
      setConfirmRemove(null)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '移除失败')
    } finally {
      setBusy(false)
      await load()
    }
  }

  /** 导出当前筛选条件下的全部记录（分页拉取后前端拼 CSV） */
  async function onExport() {
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const exportPageSize = 200
      const rows: SuppressionEntry[] = []
      for (let p = 1; ; p++) {
        const res = await segmentApi.listSuppressions({
          kind: kind || undefined,
          user_id: userIdQ.trim() || undefined,
          channel: channel || undefined,
          page: p,
          page_size: exportPageSize,
        })
        rows.push(...(res.items ?? []))
        if (rows.length >= (res.total ?? 0) || (res.items ?? []).length === 0) break
        // 名单可能很大，导出封顶 2 万行，避免浏览器卡死
        if (rows.length >= 20000) break
      }
      const header = ['kind', 'user_id', 'channel', 'reason', 'source', 'operator', 'created_at']
      const csv = [
        header.join(','),
        ...rows.map((r) =>
          [r.kind, r.user_id, r.channel, r.reason ?? '', r.source ?? '', r.operator ?? '', r.created_at]
            .map((v) => csvCell(String(v)))
            .join(','),
        ),
      ].join('\n')
      // \ufeff 让 Excel 认出 UTF-8，否则中文原因列全是乱码
      const blob = new Blob([`\ufeff${csv}`], { type: 'text/csv;charset=utf-8' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `suppressions_${new Date().toISOString().slice(0, 10)}.csv`
      a.click()
      URL.revokeObjectURL(url)
      setMsg(`已导出 ${rows.length} 条`)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '导出失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <PageHead
        title="黑名单 / 退订"
        description="发送链路读 Redis 快路径，本页是可查询、可审计的权威副本。加入名单先落库再同步缓存。"
        actions={
          <BtnRow>
            <Button type="button" variant="ghost" disabled={busy} onClick={() => void onExport()}>
              导出 CSV
            </Button>
            {canManage ? (
              <Button type="button" variant="primary" disabled={busy} onClick={() => setShowAdd(true)}>
                批量加入
              </Button>
            ) : null}
          </BtnRow>
        }
      />

      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <div className="mb-4 grid gap-3 sm:grid-cols-3">
        <Stat label="全渠道黑名单">{stats ? stats.blacklist.toLocaleString() : '-'}</Stat>
        <Stat label="按渠道退订">{stats ? stats.unsubscribe.toLocaleString() : '-'}</Stat>
        <Stat label="合计">{stats ? stats.total.toLocaleString() : '-'}</Stat>
      </div>

      <Panel>
        <div className="flex flex-wrap items-end gap-3">
          <Field label="类型" noMargin className="min-w-[9rem] flex-[1_1_9rem]">
            <Select
              value={kind}
              onChange={(e) => {
                setPage(1)
                setKind(e.target.value)
              }}
            >
              <option value="">全部</option>
              <option value="blacklist">黑名单</option>
              <option value="unsubscribe">退订</option>
            </Select>
          </Field>
          <Field label="用户 ID" noMargin className="min-w-[10rem] flex-[1_1_10rem]">
            <Input
              placeholder="精确匹配"
              value={userId}
              onChange={(e) => {
                setPage(1)
                setUserId(e.target.value)
              }}
            />
          </Field>
          <Field label="渠道" noMargin className="min-w-[9rem] flex-[1_1_9rem]">
            <Select
              value={channel}
              onChange={(e) => {
                setPage(1)
                setChannel(e.target.value)
              }}
            >
              <option value="">全部</option>
              <option value="*">全渠道（黑名单）</option>
              {channels.map((c) => (
                <option key={c} value={c}>
                  {channelLabels[c]}
                </option>
              ))}
            </Select>
          </Field>
          <BtnRow className="shrink-0">
            <Button
              type="button"
              variant="ink"
              disabled={busy}
              onClick={() => {
                setPage(1)
                setReloadTick((t) => t + 1)
              }}
            >
              查询
            </Button>
          </BtnRow>
        </div>
      </Panel>

      <Panel className="mt-4">
        <TableWrap>
          <thead>
            <tr>
              <Th>类型</Th>
              <Th>用户 ID</Th>
              <Th>渠道</Th>
              <Th>原因</Th>
              <Th>来源 / 操作人</Th>
              <Th>加入时间</Th>
              <Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr>
                <Td colSpan={7}>
                  <Empty>名单为空</Empty>
                </Td>
              </tr>
            ) : (
              items.map((row) => (
                <tr key={row.id} className="hover:bg-white/50">
                  <Td>
                    <Chip tone={row.kind === 'blacklist' ? 'danger' : 'warn'}>
                      {row.kind === 'blacklist' ? '黑名单' : '退订'}
                    </Chip>
                  </Td>
                  <Td>
                    <Mono>{row.user_id}</Mono>
                  </Td>
                  <Td>{channelLabels[row.channel] ?? row.channel}</Td>
                  <Td className="max-w-[220px] truncate text-sm" title={row.reason}>
                    {row.reason || '-'}
                  </Td>
                  <Td className="text-xs text-muted">
                    {row.source || '-'} / {row.operator || '-'}
                  </Td>
                  <Td className="whitespace-nowrap text-sm">{formatTime(row.created_at)}</Td>
                  <Td>
                    {canManage ? (
                      <Button
                        type="button"
                        variant="danger"
                        className="px-3 py-1 text-xs"
                        disabled={busy}
                        onClick={() => setConfirmRemove(row)}
                      >
                        移除
                      </Button>
                    ) : (
                      <span className="text-xs text-muted">只读</span>
                    )}
                  </Td>
                </tr>
              ))
            )}
          </tbody>
        </TableWrap>
        <div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-sm text-muted">
          <span>
            第 {page} / {pages} 页 · 共 {total} 条
          </span>
          <div className="flex gap-2">
            <Button type="button" variant="ghost" disabled={busy || page <= 1} onClick={() => setPage((p) => p - 1)}>
              上一页
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={busy || page >= pages}
              onClick={() => setPage((p) => p + 1)}
            >
              下一页
            </Button>
          </div>
        </div>
      </Panel>

      <Modal
        open={showAdd}
        wide
        title="批量加入名单"
        onClose={() => {
          if (!busy) setShowAdd(false)
        }}
      >
        <form onSubmit={onAdd}>
          <div className="mb-1 grid items-end gap-3 sm:grid-cols-2">
            <Field label="类型" noMargin>
              <Select
                value={addForm.kind}
                onChange={(e) => setAddForm({ ...addForm, kind: e.target.value as SuppressionKind })}
              >
                <option value="blacklist">黑名单（全渠道拉黑）</option>
                <option value="unsubscribe">退订（按渠道）</option>
              </Select>
            </Field>
            <Field label="渠道" noMargin hint={addForm.kind === 'blacklist' ? '黑名单固定全渠道，无需选择' : undefined}>
              <Select
                value={addForm.channel}
                disabled={addForm.kind === 'blacklist'}
                onChange={(e) => setAddForm({ ...addForm, channel: e.target.value })}
              >
                {channels.map((c) => (
                  <option key={c} value={c}>
                    {channelLabels[c]}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="原因" noMargin className="sm:col-span-2">
              <Input
                value={addForm.reason}
                onChange={(e) => setAddForm({ ...addForm, reason: e.target.value })}
                placeholder="投诉 / 合规要求 / 用户主动退订"
              />
            </Field>
            <Field
              label="用户 ID 列表"
              noMargin
              className="sm:col-span-2"
              hint="支持换行、逗号、空格分隔粘贴；单次最多 5000 个，重复项会自动去重"
            >
              <Textarea
                required
                value={addForm.userIdsText}
                onChange={(e) => setAddForm({ ...addForm, userIdsText: e.target.value })}
                placeholder={'u_1001\nu_1002\nu_1003'}
              />
            </Field>
          </div>
          <BtnRow className="mt-3">
            <Button type="submit" variant="primary" disabled={busy}>
              加入名单
            </Button>
            <Button type="button" variant="ghost" disabled={busy} onClick={() => setShowAdd(false)}>
              取消
            </Button>
          </BtnRow>
        </form>
      </Modal>

      <Modal
        open={!!confirmRemove}
        title="移出名单"
        onClose={() => {
          if (!busy) setConfirmRemove(null)
        }}
      >
        <p className="text-sm text-ink-soft">
          确认将 <Mono>{confirmRemove?.user_id}</Mono> 从
          {confirmRemove?.kind === 'blacklist' ? '黑名单' : `${channelLabels[confirmRemove?.channel ?? ''] ?? confirmRemove?.channel}退订名单`}
          中移除？移除后该用户会重新进入投放范围。
        </p>
        <BtnRow className="mt-4">
          <Button type="button" variant="danger" disabled={busy} onClick={() => void onRemove()}>
            确认移除
          </Button>
          <Button type="button" variant="ghost" disabled={busy} onClick={() => setConfirmRemove(null)}>
            取消
          </Button>
        </BtnRow>
      </Modal>
    </div>
  )
}
