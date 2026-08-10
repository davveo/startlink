import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ApiError } from '../api/client'
import { stageLabel, traceApi, type TraceDetail, type TraceEventView } from '../api/traces'
import {
  BtnRow,
  Button,
  Chip,
  Empty,
  Field,
  Input,
  Mono,
  PageHead,
  Panel,
  Select,
  Stat,
  Toast,
} from '../components/ui'
import { cn } from '../lib/cn'
import { useDebounced, useRequestSeq } from '../lib/async'

const pageSizeOptions = [30, 50, 100] as const

function formatTime(v?: string) {
  if (!v) return '-'
  try {
    return new Date(v).toLocaleString()
  } catch {
    return v
  }
}

function levelChip(level: string) {
  if (level === 'error') return <Chip tone="danger">error</Chip>
  if (level === 'warn') return <Chip tone="warn">warn</Chip>
  return <Chip>info</Chip>
}

function TaskIDChips({
  mainTaskID,
  subTaskID,
  onFilterSub,
}: {
  mainTaskID?: number
  subTaskID?: number
  onFilterSub?: (subID: number) => void
}) {
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5">
      {mainTaskID ? (
        <Link
          to={`/tasks/${mainTaskID}/subtasks`}
          className="rounded bg-ink/8 px-1.5 py-0.5 font-mono text-xs text-ink hover:bg-teal/15 hover:text-teal"
          title="查看该活动的子任务列表"
        >
          主任务 #{mainTaskID}
        </Link>
      ) : (
        <span className="rounded bg-ink/5 px-1.5 py-0.5 font-mono text-xs text-ink/40">主任务 —</span>
      )}
      {subTaskID ? (
        <button
          type="button"
          className="rounded bg-teal/12 px-1.5 py-0.5 font-mono text-xs text-teal hover:bg-teal/20"
          title="只看该子任务的事件"
          onClick={() => onFilterSub?.(subTaskID)}
        >
          子任务 #{subTaskID}
        </button>
      ) : (
        <span className="rounded bg-ink/5 px-1.5 py-0.5 font-mono text-xs text-ink/40">子任务 —</span>
      )}
    </span>
  )
}

function EventRow({
  ev,
  expanded,
  onToggle,
  onFilterSub,
}: {
  ev: TraceEventView
  expanded: boolean
  onToggle: () => void
  onFilterSub?: (subID: number) => void
}) {
  const hasDetail = !!ev.detail && Object.keys(ev.detail).length > 0
  return (
    <li className="relative pb-3 last:pb-0">
      <span
        className={cn(
          'absolute -left-[1.3rem] top-1.5 h-2.5 w-2.5 rounded-full',
          ev.level === 'error' ? 'bg-rose' : ev.level === 'warn' ? 'bg-amber' : 'bg-teal',
        )}
      />
      <div className="flex flex-wrap items-center gap-2">
        {levelChip(ev.level)}
        <TaskIDChips mainTaskID={ev.main_task_id} subTaskID={ev.sub_task_id} onFilterSub={onFilterSub} />
        <span className="text-sm font-medium">
          {stageLabel[ev.stage] || ev.stage} · {ev.event}
        </span>
        <span className="text-xs text-ink/45">{formatTime(ev.created_at)}</span>
        {ev.service ? <Chip>{ev.service}</Chip> : null}
      </div>
      {ev.message ? (
        <div className="mt-1 line-clamp-2 text-sm text-ink/80" title={ev.message}>
          {ev.message}
        </div>
      ) : null}
      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-ink/50">
        {ev.user_id ? <span>user={ev.user_id}</span> : null}
        {ev.channel ? <span>channel={ev.channel}</span> : null}
        {ev.record_id ? <span>record=#{ev.record_id}</span> : null}
        {ev.msg_id ? (
          <span className="max-w-[12rem] truncate" title={ev.msg_id}>
            msg={ev.msg_id}
          </span>
        ) : null}
        {hasDetail ? (
          <button type="button" className="text-teal underline" onClick={onToggle}>
            {expanded ? '收起详情' : '展开详情'}
          </button>
        ) : null}
      </div>
      {expanded && hasDetail ? (
        <pre className="mt-2 max-h-40 overflow-auto rounded bg-ink/5 p-2 text-xs text-ink/70">
          {JSON.stringify(ev.detail, null, 2)}
        </pre>
      ) : null}
    </li>
  )
}

export function TraceTimelinePage() {
  const { traceId: rawId = '' } = useParams()
  const traceId = decodeURIComponent(rawId)
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const [detail, setDetail] = useState<TraceDetail | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [expanded, setExpanded] = useState<Record<number, boolean>>({})

  const page = Math.max(1, Number(searchParams.get('page') || '1') || 1)
  const pageSize = pageSizeOptions.includes(Number(searchParams.get('page_size')) as (typeof pageSizeOptions)[number])
    ? Number(searchParams.get('page_size'))
    : 50
  const level = searchParams.get('level') || ''
  const stage = searchParams.get('stage') || ''
  const userId = searchParams.get('user_id') || ''
  const subTaskID = searchParams.get('sub_task_id') || ''
  const anomalyOnly = searchParams.get('anomaly_only') === '1'

  const [userDraft, setUserDraft] = useState(userId)
  const [subDraft, setSubDraft] = useState(subTaskID)
  const userQ = useDebounced(userDraft)
  const subQ = useDebounced(subDraft)
  const seq = useRequestSeq()

  useEffect(() => {
    setUserDraft(userId)
  }, [userId])
  useEffect(() => {
    setSubDraft(subTaskID)
  }, [subTaskID])

  useEffect(() => {
    if (userQ === userId) return
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (userQ.trim()) next.set('user_id', userQ.trim())
        else next.delete('user_id')
        next.set('page', '1')
        return next
      },
      { replace: true },
    )
  }, [userQ, userId, setSearchParams])

  useEffect(() => {
    if (subQ === subTaskID) return
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (subQ.trim()) next.set('sub_task_id', subQ.trim())
        else next.delete('sub_task_id')
        next.set('page', '1')
        return next
      },
      { replace: true },
    )
  }, [subQ, subTaskID, setSearchParams])

  const patchQuery = (patch: Record<string, string | undefined>, resetPage = true) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)
      for (const [k, v] of Object.entries(patch)) {
        if (!v) next.delete(k)
        else next.set(k, v)
      }
      if (resetPage) next.set('page', '1')
      return next
    })
  }

  const load = useCallback(async () => {
    if (!traceId) return
    const s = seq.next()
    setBusy(true)
    setErr('')
    try {
      const subN = subTaskID.trim() ? Number(subTaskID.trim()) : undefined
      const res = await traceApi.get(traceId, {
        page,
        page_size: pageSize,
        level: level || undefined,
        stage: stage || undefined,
        user_id: userId || undefined,
        sub_task_id: subN && Number.isFinite(subN) && subN > 0 ? subN : undefined,
        anomaly_only: anomalyOnly && !level,
        order: 'asc',
      })
      if (!seq.isLatest(s)) return
      setDetail(res)
      setExpanded({})
    } catch (e) {
      if (!seq.isLatest(s)) return
      setDetail(null)
      setErr(e instanceof ApiError ? e.message : '加载时间线失败')
    } finally {
      if (seq.isLatest(s)) setBusy(false)
    }
  }, [anomalyOnly, level, page, pageSize, seq, stage, subTaskID, traceId, userId])

  useEffect(() => {
    void load()
  }, [load])

  const filteredTotal = detail?.filtered_total ?? 0
  const pages = Math.max(1, Math.ceil(filteredTotal / pageSize))

  useEffect(() => {
    if (page > pages) patchQuery({ page: String(pages) }, false)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, pages])

  const grouped = useMemo(() => {
    const events = detail?.events ?? []
    const out: { stage: string; items: TraceEventView[] }[] = []
    for (const ev of events) {
      const last = out[out.length - 1]
      if (last && last.stage === ev.stage) last.items.push(ev)
      else out.push({ stage: ev.stage, items: [ev] })
    }
    return out
  }, [detail?.events])

  if (!traceId) {
    return (
      <div>
        <PageHead title="链路时间线" description="缺少 Trace ID" />
        <Empty>
          <Link className="text-teal underline" to="/traces">
            返回全链路日志
          </Link>
        </Empty>
      </div>
    )
  }

  return (
    <div>
      <PageHead
        title="链路时间线"
        description="分页加载事件，避免上万条一次性撑开页面。默认每页 50 条；可用「仅异常」快速定位问题。"
      />

      {err ? <Toast kind="error">{err}</Toast> : null}

      <Panel>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="text-lg font-semibold">{detail?.title || (busy ? '加载中…' : '链路详情')}</div>
            <div className="mt-1 break-all text-sm text-ink/60">
              <Mono>{traceId}</Mono>
              {detail?.biz_id ? ` · ${detail.biz_id}` : ''}
              {detail?.main_task_id ? ` · #${detail.main_task_id}` : ''}
              {detail?.status ? ` · ${detail.status}` : ''}
            </div>
          </div>
          <BtnRow>
            <Button type="button" variant="ghost" onClick={() => navigate('/traces')}>
              返回列表
            </Button>
            {detail?.main_task_id ? (
              <ButtonLinkish to={`/ops/${detail.main_task_id}/records`}>推送流水</ButtonLinkish>
            ) : null}
            <Button type="button" variant="ghost" onClick={() => void load()} disabled={busy}>
              刷新
            </Button>
          </BtnRow>
        </div>

        <div className="mt-4 flex flex-wrap gap-3">
          <Stat label="事件总数">{detail?.event_count ?? '—'}</Stat>
          <Stat label="错误">{detail?.error_count ?? '—'}</Stat>
          <Stat label="警告">{detail?.warn_count ?? '—'}</Stat>
          <Stat label="当前筛选">{filteredTotal}</Stat>
        </div>

        {detail?.stages && detail.stages.length > 0 ? (
          <div className="mt-3 flex flex-wrap gap-2">
            <button
              type="button"
              className={cn(
                'rounded-lg px-2.5 py-1 text-xs font-medium',
                !stage ? 'bg-teal/15 text-teal' : 'bg-ink/5 text-ink/60 hover:bg-ink/10',
              )}
              onClick={() => patchQuery({ stage: undefined })}
            >
              全部阶段
            </button>
            {detail.stages.map((st) => (
              <button
                key={st.stage}
                type="button"
                className={cn(
                  'rounded-lg px-2.5 py-1 text-xs font-medium',
                  stage === st.stage ? 'bg-teal/15 text-teal' : 'bg-ink/5 text-ink/60 hover:bg-ink/10',
                )}
                onClick={() => patchQuery({ stage: st.stage })}
                title={`error ${st.error} / warn ${st.warn}`}
              >
                {stageLabel[st.stage] || st.stage} {st.count}
                {st.error + st.warn > 0 ? (
                  <span className="ml-1 text-rose-700">·{st.error + st.warn}</span>
                ) : null}
              </button>
            ))}
          </div>
        ) : null}
      </Panel>

      <Panel className="mt-4">
        <div className="flex flex-wrap items-end gap-3">
          <Field label="级别" noMargin className="min-w-[7rem]">
            <Select
              value={level}
              onChange={(e) => {
                const v = e.target.value
                patchQuery({
                  level: v || undefined,
                  anomaly_only: v ? undefined : anomalyOnly ? '1' : undefined,
                })
              }}
            >
              <option value="">全部</option>
              <option value="error">error</option>
              <option value="warn">warn</option>
              <option value="info">info</option>
            </Select>
          </Field>
          <Field label="用户 ID" noMargin className="min-w-[10rem] flex-[1_1_10rem]">
            <Input
              value={userDraft}
              onChange={(e) => setUserDraft(e.target.value)}
              placeholder="筛选用户"
            />
          </Field>
          <Field label="子任务 ID" noMargin className="min-w-[8rem] flex-[0_1_8rem]">
            <Input
              value={subDraft}
              onChange={(e) => setSubDraft(e.target.value)}
              placeholder="sub_task_id"
            />
          </Field>
          <Field label="每页" noMargin className="min-w-[6rem]">
            <Select
              value={String(pageSize)}
              onChange={(e) => patchQuery({ page_size: e.target.value })}
            >
              {pageSizeOptions.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </Select>
          </Field>
          <BtnRow>
            <Button
              type="button"
              variant={anomalyOnly && !level ? 'primary' : 'ghost'}
              onClick={() =>
                patchQuery({
                  anomaly_only: anomalyOnly && !level ? undefined : '1',
                  level: undefined,
                })
              }
            >
              仅异常
            </Button>
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                setUserDraft('')
                setSubDraft('')
                setSearchParams({})
              }}
            >
              清空筛选
            </Button>
          </BtnRow>
        </div>
      </Panel>

      <Panel className="mt-4">
        <div className="mb-3 flex items-center justify-between text-sm text-ink/60">
          <span>
            第 {page}/{pages} 页 · 本页 {detail?.events.length ?? 0} 条
            {busy ? ' · 加载中…' : ''}
          </span>
          <BtnRow>
            <Button
              type="button"
              variant="ghost"
              disabled={page <= 1 || busy}
              onClick={() => patchQuery({ page: String(page - 1) }, false)}
            >
              上一页
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={page >= pages || busy}
              onClick={() => patchQuery({ page: String(page + 1) }, false)}
            >
              下一页
            </Button>
          </BtnRow>
        </div>

        {/* 固定视口高度：上千页时也不拉长整页文档 */}
        <div className="max-h-[min(70vh,720px)] overflow-y-auto rounded-lg border border-line/70 bg-white/40 p-4">
          {!detail && busy ? <Empty>加载中…</Empty> : null}
          {detail && detail.events.length === 0 ? (
            <Empty>{anomalyOnly ? '当前筛选下无异常事件' : '本页无事件'}</Empty>
          ) : null}
          {grouped.map((g) => (
            <div key={`${g.stage}-${g.items[0]?.id}`} className="mb-5 last:mb-0">
              <div className="sticky top-0 z-10 mb-2 bg-white/90 py-1 text-xs font-semibold tracking-wide text-ink/55 backdrop-blur">
                {stageLabel[g.stage] || g.stage}
                <span className="ml-2 font-normal text-ink/40">{g.items.length}</span>
              </div>
              <ol className="space-y-3 border-l border-ink/15 pl-4">
                {g.items.map((ev) => (
                  <EventRow
                    key={ev.id}
                    ev={ev}
                    expanded={!!expanded[ev.id]}
                    onToggle={() => setExpanded((m) => ({ ...m, [ev.id]: !m[ev.id] }))}
                    onFilterSub={(id) => {
                      setSubDraft(String(id))
                      patchQuery({ sub_task_id: String(id) })
                    }}
                  />
                ))}
              </ol>
            </div>
          ))}
        </div>

        <div className="mt-3 flex items-center justify-between text-sm text-ink/60">
          <span>筛选命中 {filteredTotal} 条（全链路共 {detail?.event_count ?? 0} 条）</span>
          <BtnRow>
            <Button
              type="button"
              variant="ghost"
              disabled={page <= 1 || busy}
              onClick={() => patchQuery({ page: String(page - 1) }, false)}
            >
              上一页
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={page >= pages || busy}
              onClick={() => patchQuery({ page: String(page + 1) }, false)}
            >
              下一页
            </Button>
          </BtnRow>
        </div>
      </Panel>
    </div>
  )
}

function ButtonLinkish({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link
      to={to}
      className="inline-flex items-center justify-center rounded-lg border border-line bg-transparent px-3 py-2 text-sm font-medium text-ink hover:bg-white/60"
    >
      {children}
    </Link>
  )
}
