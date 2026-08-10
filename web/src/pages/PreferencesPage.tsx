import { useCallback, useEffect, useState } from 'react'
import { ApiError } from '../api/client'
import {
  CLEAR_PREFERRED_HOUR,
  PREFERENCE_CHANNELS,
  preferenceApi,
  type ConsentLog,
  type UserPreference,
} from '../api/preferences'
import { useAuth } from '../auth/AuthContext'
import { Perm } from '../auth/permissions'
import {
  BtnRow,
  Button,
  Empty,
  Field,
  Input,
  Modal,
  Mono,
  PageHead,
  Panel,
  Select,
  TableWrap,
  Td,
  Th,
  Toast,
} from '../components/ui'
import { useClampPage, useDebounced, useRequestSeq } from '../lib/async'
import { channelLabel } from '../lib/labels'

const PAGE_SIZE = 20

function formatTime(v?: string) {
  if (!v) return '-'
  try {
    return new Date(v).toLocaleString()
  } catch {
    return v
  }
}

function quietLabel(p: UserPreference) {
  if (!p.quiet_start || !p.quiet_end) return '—'
  return `${p.quiet_start} - ${p.quiet_end}`
}

function hourLabel(p: UserPreference) {
  if (p.preferred_hour === undefined || p.preferred_hour === null || p.preferred_hour < 0) return '不限'
  return `${String(p.preferred_hour).padStart(2, '0')}:00`
}

function scopeLabel(scope: string) {
  if (scope === 'marketing') return '营销总开关'
  if (scope === 'quiet_hours') return '免打扰时段'
  if (scope === 'preferred_hour') return '期望送达时段'
  if (scope === 'preference') return '偏好记录'
  if (scope.startsWith('channel:')) return `渠道 · ${channelLabel(scope.slice('channel:'.length))}`
  if (scope.startsWith('topic:')) return `主题 · ${scope.slice('topic:'.length)}`
  return scope
}

type EditForm = {
  userID: string
  isNew: boolean
  timezone: string
  quietStart: string
  quietEnd: string
  preferredHour: string
  channels: string[]
  topics: string
  marketingOptOut: boolean
}

function emptyForm(): EditForm {
  return {
    userID: '',
    isNew: true,
    timezone: '',
    quietStart: '',
    quietEnd: '',
    preferredHour: '',
    channels: [],
    topics: '',
    marketingOptOut: false,
  }
}

function toForm(p: UserPreference): EditForm {
  return {
    userID: p.user_id,
    isNew: false,
    timezone: p.timezone ?? '',
    quietStart: p.quiet_start ?? '',
    quietEnd: p.quiet_end ?? '',
    preferredHour:
      p.preferred_hour === undefined || p.preferred_hour === null || p.preferred_hour < 0
        ? ''
        : String(p.preferred_hour),
    channels: p.opt_out_channels ?? [],
    topics: (p.opt_out_topics ?? []).join(', '),
    marketingOptOut: p.marketing_opt_out,
  }
}

function PreferenceTab({ canManage, canView }: { canManage: boolean; canView: boolean }) {
  const [items, setItems] = useState<UserPreference[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [userID, setUserID] = useState('')
  const [channel, setChannel] = useState('')
  const [marketing, setMarketing] = useState<'' | '1' | '0'>('')
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)
  const [reloadTick, setReloadTick] = useState(0)
  const [form, setForm] = useState<EditForm | null>(null)
  const seq = useRequestSeq()

  const userIDQ = useDebounced(userID)

  const load = useCallback(async () => {
    if (!canView) return
    const s = seq.next()
    setBusy(true)
    setErr('')
    try {
      const res = await preferenceApi.list({
        user_id: userIDQ || undefined,
        channel: channel || undefined,
        marketing_opt_out: marketing === '' ? undefined : marketing === '1',
        page,
        page_size: PAGE_SIZE,
      })
      if (!seq.isLatest(s)) return
      setItems(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      if (!seq.isLatest(s)) return
      setErr(e instanceof ApiError ? e.message : '加载用户偏好失败')
    } finally {
      if (seq.isLatest(s)) setBusy(false)
    }
  }, [canView, channel, marketing, page, seq, userIDQ])

  useEffect(() => {
    void load()
  }, [load, reloadTick])

  useClampPage(page, total, PAGE_SIZE, setPage)
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  async function onSave() {
    if (!form) return
    const target = form.userID.trim()
    if (!target) {
      setErr('请填写用户 ID')
      return
    }
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      await preferenceApi.upsert(target, {
        timezone: form.timezone.trim(),
        quiet_start: form.quietStart,
        quiet_end: form.quietEnd,
        preferred_hour: form.preferredHour === '' ? CLEAR_PREFERRED_HOUR : Number(form.preferredHour),
        opt_out_channels: form.channels,
        opt_out_topics: form.topics
          .split(/[,，\s]+/)
          .map((t) => t.trim())
          .filter(Boolean),
        marketing_opt_out: form.marketingOptOut,
      })
      setMsg(`已保存 ${target} 的偏好`)
      setForm(null)
      setReloadTick((t) => t + 1)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  async function onDelete(target: string) {
    if (!window.confirm(`删除 ${target} 的偏好记录？该用户将恢复为「全部允许」，操作会写入同意审计。`)) {
      return
    }
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      await preferenceApi.remove(target)
      setMsg(`已删除 ${target} 的偏好`)
      setReloadTick((t) => t + 1)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '删除失败')
    } finally {
      setBusy(false)
    }
  }

  function toggleChannel(ch: string) {
    setForm((f) =>
      f
        ? {
            ...f,
            channels: f.channels.includes(ch)
              ? f.channels.filter((x) => x !== ch)
              : [...f.channels, ch],
          }
        : f,
    )
  }

  return (
    <>
      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel>
        <div className="flex flex-wrap items-end gap-3">
          <Field label="用户 ID" noMargin className="min-w-[10rem] flex-[1_1_10rem]">
            <Input
              value={userID}
              onChange={(e) => {
                setPage(1)
                setUserID(e.target.value)
              }}
              placeholder="精确匹配"
            />
          </Field>
          <Field label="退订渠道" noMargin className="min-w-[9rem] flex-[1_1_9rem]">
            <Select
              value={channel}
              onChange={(e) => {
                setPage(1)
                setChannel(e.target.value)
              }}
            >
              <option value="">全部</option>
              {PREFERENCE_CHANNELS.map((ch) => (
                <option key={ch} value={ch}>
                  {channelLabel(ch)}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="营销退订" noMargin className="min-w-[8rem] flex-[1_1_8rem]">
            <Select
              value={marketing}
              onChange={(e) => {
                setPage(1)
                setMarketing(e.target.value as '' | '1' | '0')
              }}
            >
              <option value="">全部</option>
              <option value="1">已退订</option>
              <option value="0">未退订</option>
            </Select>
          </Field>
          <BtnRow className="shrink-0">
            <Button
              variant="ink"
              type="button"
              disabled={busy || !canView}
              onClick={() => {
                setPage(1)
                setReloadTick((t) => t + 1)
              }}
            >
              查询
            </Button>
            {canManage ? (
              <Button variant="primary" type="button" disabled={busy} onClick={() => setForm(emptyForm())}>
                新增偏好
              </Button>
            ) : null}
          </BtnRow>
        </div>
      </Panel>

      <Panel className="mt-4">
        <TableWrap>
          <thead>
            <tr>
              <Th>用户 ID</Th>
              <Th>营销退订</Th>
              <Th>退订渠道</Th>
              <Th>退订主题</Th>
              <Th>免打扰时段</Th>
              <Th>期望送达</Th>
              <Th>更新时间</Th>
              <Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr>
                <Td colSpan={8}>
                  <Empty>暂无偏好记录</Empty>
                </Td>
              </tr>
            ) : (
              items.map((p) => (
                <tr key={p.user_id} className="hover:bg-white/50">
                  <Td>
                    <Mono>{p.user_id}</Mono>
                  </Td>
                  <Td>{p.marketing_opt_out ? '已退订' : '—'}</Td>
                  <Td className="text-sm">
                    {p.opt_out_channels?.length
                      ? p.opt_out_channels.map((c) => channelLabel(c)).join('、')
                      : '—'}
                  </Td>
                  <Td className="max-w-[200px] truncate text-sm" title={(p.opt_out_topics ?? []).join(', ')}>
                    {p.opt_out_topics?.length ? p.opt_out_topics.join('、') : '—'}
                  </Td>
                  <Td className="whitespace-nowrap text-sm">{quietLabel(p)}</Td>
                  <Td className="whitespace-nowrap text-sm">{hourLabel(p)}</Td>
                  <Td className="whitespace-nowrap text-sm">{formatTime(p.updated_at)}</Td>
                  <Td>
                    {canManage ? (
                      <div className="flex flex-wrap gap-1">
                        <Button
                          type="button"
                          variant="ghost"
                          className="px-2 py-1 text-xs"
                          onClick={() => setForm(toForm(p))}
                        >
                          编辑
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          className="px-2 py-1 text-xs"
                          disabled={busy}
                          onClick={() => void onDelete(p.user_id)}
                        >
                          删除
                        </Button>
                      </div>
                    ) : (
                      <span className="text-xs text-muted">只读</span>
                    )}
                  </Td>
                </tr>
              ))
            )}
          </tbody>
        </TableWrap>
        <BtnRow className="mt-4">
          <Button variant="ghost" type="button" disabled={busy || page <= 1} onClick={() => setPage((p) => p - 1)}>
            上一页
          </Button>
          <span className="text-sm text-muted">
            {page} / {pages}（共 {total}）
          </span>
          <Button
            variant="ghost"
            type="button"
            disabled={busy || page >= pages}
            onClick={() => setPage((p) => p + 1)}
          >
            下一页
          </Button>
        </BtnRow>
      </Panel>

      <Modal
        open={!!form}
        title={
          form?.isNew ? (
            '新增用户偏好'
          ) : (
            <>
              编辑偏好 {form ? <Mono className="text-base">{form.userID}</Mono> : null}
            </>
          )
        }
        onClose={() => {
          if (!busy) setForm(null)
        }}
      >
        {form ? (
          <div>
            <div className="mb-1 grid items-end gap-3 sm:grid-cols-2">
              <Field label="用户 ID" noMargin>
                <Input
                  className="font-mono text-sm"
                  value={form.userID}
                  disabled={!form.isNew}
                  onChange={(e) => setForm({ ...form, userID: e.target.value })}
                />
              </Field>
              <Field label="营销总开关" noMargin hint="退订后拒收全部普通优先级营销消息">
                <Select
                  value={form.marketingOptOut ? '1' : '0'}
                  onChange={(e) => setForm({ ...form, marketingOptOut: e.target.value === '1' })}
                >
                  <option value="0">接收营销消息</option>
                  <option value="1">已退订营销</option>
                </Select>
              </Field>
              <Field label="时区" noMargin hint="IANA 名称，如 Asia/Shanghai；留空用活动/服务器时区">
                <Input
                  value={form.timezone}
                  onChange={(e) => setForm({ ...form, timezone: e.target.value })}
                  placeholder="Asia/Shanghai"
                />
              </Field>
              <Field label="期望送达小时" noMargin hint="留空表示不限">
                <Select
                  value={form.preferredHour}
                  onChange={(e) => setForm({ ...form, preferredHour: e.target.value })}
                >
                  <option value="">不限</option>
                  {Array.from({ length: 24 }, (_, h) => (
                    <option key={h} value={String(h)}>
                      {String(h).padStart(2, '0')}:00
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label="免打扰开始" noMargin>
                <Input
                  type="time"
                  value={form.quietStart}
                  onChange={(e) => setForm({ ...form, quietStart: e.target.value })}
                />
              </Field>
              <Field label="免打扰结束" noMargin hint="两者都填才生效">
                <Input
                  type="time"
                  value={form.quietEnd}
                  onChange={(e) => setForm({ ...form, quietEnd: e.target.value })}
                />
              </Field>
            </div>

            <Field label="退订渠道" className="mt-3" hint="勾选的渠道不再向该用户投放营销消息">
              <div className="flex flex-wrap gap-3">
                {PREFERENCE_CHANNELS.map((ch) => (
                  <label key={ch} className="flex items-center gap-1.5 text-sm">
                    <input
                      type="checkbox"
                      checked={form.channels.includes(ch)}
                      onChange={() => toggleChannel(ch)}
                    />
                    {channelLabel(ch)}
                  </label>
                ))}
              </div>
            </Field>

            <Field label="退订主题" hint="逗号或空格分隔，如 promotion, coupon">
              <Input
                value={form.topics}
                onChange={(e) => setForm({ ...form, topics: e.target.value })}
                placeholder="promotion, coupon"
              />
            </Field>

            <BtnRow className="mt-3">
              <Button type="button" variant="primary" disabled={busy} onClick={() => void onSave()}>
                保存
              </Button>
              <Button type="button" variant="ghost" disabled={busy} onClick={() => setForm(null)}>
                取消
              </Button>
            </BtnRow>
          </div>
        ) : null}
      </Modal>
    </>
  )
}

function ConsentTab({ canView }: { canView: boolean }) {
  const [items, setItems] = useState<ConsentLog[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [userID, setUserID] = useState('')
  const [action, setAction] = useState('')
  const [scope, setScope] = useState('')
  const [since, setSince] = useState('')
  const [until, setUntil] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [reloadTick, setReloadTick] = useState(0)
  const seq = useRequestSeq()

  const userIDQ = useDebounced(userID)
  const scopeQ = useDebounced(scope)

  const load = useCallback(async () => {
    if (!canView) return
    const s = seq.next()
    setBusy(true)
    setErr('')
    try {
      const res = await preferenceApi.listConsentLogs({
        user_id: userIDQ || undefined,
        action: action || undefined,
        scope: scopeQ || undefined,
        since: since || undefined,
        until: until || undefined,
        page,
        page_size: PAGE_SIZE,
      })
      if (!seq.isLatest(s)) return
      setItems(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      if (!seq.isLatest(s)) return
      setErr(e instanceof ApiError ? e.message : '加载同意审计失败')
    } finally {
      if (seq.isLatest(s)) setBusy(false)
    }
  }, [action, canView, page, scopeQ, seq, since, until, userIDQ])

  useEffect(() => {
    void load()
  }, [load, reloadTick])

  useClampPage(page, total, PAGE_SIZE, setPage)
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <>
      {err ? <Toast kind="error">{err}</Toast> : null}

      <Panel>
        <div className="flex flex-wrap items-end gap-3">
          <Field label="用户 ID" noMargin className="min-w-[10rem] flex-[1_1_10rem]">
            <Input
              value={userID}
              onChange={(e) => {
                setPage(1)
                setUserID(e.target.value)
              }}
              placeholder="精确匹配"
            />
          </Field>
          <Field label="动作" noMargin className="min-w-[8rem] flex-[1_1_8rem]">
            <Select
              value={action}
              onChange={(e) => {
                setPage(1)
                setAction(e.target.value)
              }}
            >
              <option value="">全部</option>
              <option value="opt_out">退订</option>
              <option value="opt_in">恢复接收</option>
            </Select>
          </Field>
          <Field
            label="范围"
            noMargin
            className="min-w-[10rem] flex-[1_1_10rem]"
          >
            <Input
              value={scope}
              onChange={(e) => {
                setPage(1)
                setScope(e.target.value)
              }}
              placeholder="marketing / channel:sms"
              title="填 channel: 或 topic: 可按前缀筛选"
            />
          </Field>
          <Field label="开始时间" noMargin className="min-w-[12rem] flex-[1_1_12rem]">
            <Input
              type="datetime-local"
              value={since}
              onChange={(e) => {
                setPage(1)
                setSince(e.target.value)
              }}
            />
          </Field>
          <Field label="结束时间" noMargin className="min-w-[12rem] flex-[1_1_12rem]">
            <Input
              type="datetime-local"
              value={until}
              onChange={(e) => {
                setPage(1)
                setUntil(e.target.value)
              }}
            />
          </Field>
          <BtnRow className="shrink-0">
            <Button
              variant="ink"
              type="button"
              disabled={busy || !canView}
              onClick={() => {
                setPage(1)
                setReloadTick((t) => t + 1)
              }}
            >
              查询
            </Button>
          </BtnRow>
        </div>
        <p className="mt-1.5 text-xs text-muted">范围可填前缀，如 channel: 查全部渠道变更、topic: 查全部主题变更。</p>
      </Panel>

      <Panel className="mt-4">
        <TableWrap>
          <thead>
            <tr>
              <Th>时间</Th>
              <Th>用户 ID</Th>
              <Th>动作</Th>
              <Th>范围</Th>
              <Th>来源</Th>
              <Th>操作人</Th>
              <Th>详情</Th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr>
                <Td colSpan={7}>
                  <Empty>暂无同意变更记录</Empty>
                </Td>
              </tr>
            ) : (
              items.map((l) => (
                <tr key={l.id} className="hover:bg-white/50">
                  <Td className="whitespace-nowrap text-sm">{formatTime(l.created_at)}</Td>
                  <Td>
                    <Mono>{l.user_id}</Mono>
                  </Td>
                  <Td>{l.action === 'opt_out' ? '退订' : '恢复接收'}</Td>
                  <Td className="text-sm" title={l.scope}>
                    {scopeLabel(l.scope)}
                  </Td>
                  <Td className="text-sm">{l.source || '-'}</Td>
                  <Td className="text-sm">{l.operator || '-'}</Td>
                  <Td className="max-w-[240px] truncate text-xs text-muted" title={l.detail}>
                    {l.detail || '-'}
                  </Td>
                </tr>
              ))
            )}
          </tbody>
        </TableWrap>
        <BtnRow className="mt-4">
          <Button variant="ghost" type="button" disabled={busy || page <= 1} onClick={() => setPage((p) => p - 1)}>
            上一页
          </Button>
          <span className="text-sm text-muted">
            {page} / {pages}（共 {total}）
          </span>
          <Button
            variant="ghost"
            type="button"
            disabled={busy || page >= pages}
            onClick={() => setPage((p) => p + 1)}
          >
            下一页
          </Button>
        </BtnRow>
      </Panel>
    </>
  )
}

export function PreferencesPage() {
  const { can } = useAuth()
  const canView = can(Perm.PreferenceView)
  const canManage = can(Perm.PreferenceManage)
  const [tab, setTab] = useState<'preferences' | 'consents'>('preferences')

  return (
    <div>
      <PageHead
        title="用户偏好中心"
        description="用户自己的选择：渠道退订、主题退订、免打扰与期望送达时段。与抑制名单（运营单方面加黑）分开管理，每次变更都留同意举证。"
      />

      <BtnRow className="mb-4">
        <Button
          type="button"
          variant={tab === 'preferences' ? 'ink' : 'ghost'}
          onClick={() => setTab('preferences')}
        >
          用户偏好
        </Button>
        <Button
          type="button"
          variant={tab === 'consents' ? 'ink' : 'ghost'}
          onClick={() => setTab('consents')}
        >
          同意变更审计
        </Button>
      </BtnRow>

      {!canView ? <Toast kind="error">当前账号没有 preference.view 权限</Toast> : null}

      {tab === 'preferences' ? (
        <PreferenceTab canManage={canManage} canView={canView} />
      ) : (
        <ConsentTab canView={canView} />
      )}
    </div>
  )
}
