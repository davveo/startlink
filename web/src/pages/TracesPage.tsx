import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { ApiError } from '../api/client'
import { stageLabel, traceApi, type TraceEventView, type TraceSummary } from '../api/traces'
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
  TableWrap,
  Td,
  Th,
  Toast,
} from '../components/ui'
import { cn } from '../lib/cn'
import { useClampPage, useDebounced, useRequestSeq } from '../lib/async'

const pageSize = 20

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

type Tab = 'campaigns' | 'events'

export function TracesPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  // 兼容旧链接 /traces?trace_id=xxx → 独立时间线页
  useEffect(() => {
    const tid = searchParams.get('trace_id')
    if (tid) navigate(`/traces/${encodeURIComponent(tid)}`, { replace: true })
  }, [navigate, searchParams])

  const [tab, setTab] = useState<Tab>('campaigns')
  const [items, setItems] = useState<TraceSummary[]>([])
  const [events, setEvents] = useState<TraceEventView[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [traceId, setTraceId] = useState('')
  const [bizId, setBizId] = useState('')
  const [taskId, setTaskId] = useState('')
  const [userId, setUserId] = useState('')
  const [level, setLevel] = useState('')
  const [stage, setStage] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [reloadTick, setReloadTick] = useState(0)

  const seq = useRequestSeq()
  const traceQ = useDebounced(traceId)
  const bizQ = useDebounced(bizId)
  const taskQ = useDebounced(taskId)
  const userQ = useDebounced(userId)

  const loadCampaigns = useCallback(async () => {
    const s = seq.next()
    setBusy(true)
    setErr('')
    try {
      const mainTaskID = taskQ.trim() ? Number(taskQ.trim()) : undefined
      const res = await traceApi.list({
        trace_id: traceQ.trim() || undefined,
        biz_id: bizQ.trim() || undefined,
        main_task_id: mainTaskID && Number.isFinite(mainTaskID) ? mainTaskID : undefined,
        user_id: userQ.trim() || undefined,
        level: level || undefined,
        page,
        page_size: pageSize,
      })
      if (!seq.isLatest(s)) return
      setItems(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      if (!seq.isLatest(s)) return
      setErr(e instanceof ApiError ? e.message : '加载链路列表失败')
    } finally {
      if (seq.isLatest(s)) setBusy(false)
    }
  }, [bizQ, level, page, seq, taskQ, traceQ, userQ])

  const loadEvents = useCallback(async () => {
    const s = seq.next()
    setBusy(true)
    setErr('')
    try {
      const mainTaskID = taskQ.trim() ? Number(taskQ.trim()) : undefined
      if (
        !traceQ.trim() &&
        !bizQ.trim() &&
        !(mainTaskID && Number.isFinite(mainTaskID)) &&
        !userQ.trim()
      ) {
        setEvents([])
        setTotal(0)
        setErr('事件检索至少填写 Trace ID / 业务 ID / 活动 ID / 用户 ID 之一')
        return
      }
      const res = await traceApi.listEvents({
        trace_id: traceQ.trim() || undefined,
        biz_id: bizQ.trim() || undefined,
        main_task_id: mainTaskID && Number.isFinite(mainTaskID) ? mainTaskID : undefined,
        user_id: userQ.trim() || undefined,
        stage: stage || undefined,
        level: level || undefined,
        page,
        page_size: pageSize,
      })
      if (!seq.isLatest(s)) return
      setEvents(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      if (!seq.isLatest(s)) return
      setErr(e instanceof ApiError ? e.message : '加载事件失败')
    } finally {
      if (seq.isLatest(s)) setBusy(false)
    }
  }, [bizQ, level, page, seq, stage, taskQ, traceQ, userQ])

  useEffect(() => {
    if (tab === 'campaigns') void loadCampaigns()
    else void loadEvents()
  }, [tab, loadCampaigns, loadEvents, reloadTick])

  useClampPage(page, total, pageSize, setPage)
  const pages = Math.max(1, Math.ceil(total / pageSize))

  const openTimeline = (id: string) => {
    if (!id) return
    navigate(`/traces/${encodeURIComponent(id)}`)
  }

  const switchTab = (next: Tab) => {
    setTab(next)
    setPage(1)
    setErr('')
  }

  return (
    <div>
      <PageHead
        title="全链路日志"
        description="按活动级 Trace ID 检索链路摘要与事件。时间线已独立成页并分页加载，避免事件过多时页面过长。"
      />

      {err ? <Toast kind="error">{err}</Toast> : null}

      <div className="mb-3 flex gap-2">
        <button
          type="button"
          className={cn(
            'rounded-lg px-3 py-1.5 text-sm font-medium transition',
            tab === 'campaigns' ? 'bg-teal/15 text-teal' : 'text-ink/60 hover:bg-ink/5',
          )}
          onClick={() => switchTab('campaigns')}
        >
          活动链路
        </button>
        <button
          type="button"
          className={cn(
            'rounded-lg px-3 py-1.5 text-sm font-medium transition',
            tab === 'events' ? 'bg-teal/15 text-teal' : 'text-ink/60 hover:bg-ink/5',
          )}
          onClick={() => switchTab('events')}
        >
          事件检索
        </button>
      </div>

      <Panel>
        <div className="flex flex-wrap items-end gap-3">
          <Field label="Trace ID" noMargin className="min-w-[14rem] flex-[2_1_14rem]">
            <Input
              value={traceId}
              onChange={(e) => {
                setPage(1)
                setTraceId(e.target.value)
              }}
              placeholder="tr_..."
            />
          </Field>
          <Field label="业务 ID" noMargin className="min-w-[10rem] flex-[1_1_10rem]">
            <Input
              value={bizId}
              onChange={(e) => {
                setPage(1)
                setBizId(e.target.value)
              }}
              placeholder="biz_id"
            />
          </Field>
          <Field label="活动 ID" noMargin className="min-w-[8rem] flex-[1_1_8rem]">
            <Input
              value={taskId}
              onChange={(e) => {
                setPage(1)
                setTaskId(e.target.value)
              }}
              placeholder="main_task_id"
            />
          </Field>
          <Field label="用户 ID" noMargin className="min-w-[9rem] flex-[1_1_9rem]">
            <Input
              value={userId}
              onChange={(e) => {
                setPage(1)
                setUserId(e.target.value)
              }}
              placeholder="user_id"
            />
          </Field>
          <Field label="级别" noMargin className="min-w-[7rem] flex-[0_1_7rem]">
            <Select
              value={level}
              onChange={(e) => {
                setPage(1)
                setLevel(e.target.value)
              }}
            >
              <option value="">全部</option>
              <option value="error">error</option>
              <option value="warn">warn</option>
              <option value="info">info</option>
            </Select>
          </Field>
          {tab === 'events' ? (
            <Field label="阶段" noMargin className="min-w-[8rem] flex-[0_1_8rem]">
              <Select
                value={stage}
                onChange={(e) => {
                  setPage(1)
                  setStage(e.target.value)
                }}
              >
                <option value="">全部</option>
                {Object.entries(stageLabel).map(([k, v]) => (
                  <option key={k} value={k}>
                    {v}
                  </option>
                ))}
              </Select>
            </Field>
          ) : null}
          <BtnRow>
            <Button type="button" onClick={() => setReloadTick((n) => n + 1)} disabled={busy}>
              {busy ? '查询中…' : '查询'}
            </Button>
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                setTraceId('')
                setBizId('')
                setTaskId('')
                setUserId('')
                setLevel('')
                setStage('')
                setPage(1)
                setReloadTick((n) => n + 1)
              }}
            >
              清空
            </Button>
          </BtnRow>
        </div>
      </Panel>

      <Panel className="mt-4">
        {tab === 'campaigns' ? (
          items.length === 0 && !busy ? (
            <Empty>暂无链路数据。可运行 scripts/seed_traces.sh 造演示数据。</Empty>
          ) : (
            <TableWrap>
              <thead>
                <tr>
                  <Th>Trace ID</Th>
                  <Th>活动</Th>
                  <Th>状态</Th>
                  <Th>事件</Th>
                  <Th>异常</Th>
                  <Th>最近事件</Th>
                  <Th>更新时间</Th>
                  <Th />
                </tr>
              </thead>
              <tbody>
                {items.map((it) => (
                  <tr key={it.trace_id || String(it.main_task_id)}>
                    <Td>
                      <Mono className="text-xs">{it.trace_id || '-'}</Mono>
                    </Td>
                    <Td>
                      <div className="font-medium">{it.title || '-'}</div>
                      <div className="text-xs text-ink/50">
                        #{it.main_task_id || '-'} · {it.biz_id || '-'}
                      </div>
                    </Td>
                    <Td>{it.status || '-'}</Td>
                    <Td>{it.event_count}</Td>
                    <Td>
                      {it.error_count > 0 || it.warn_count > 0 ? (
                        <span className="text-sm">
                          <span className="text-rose-700">{it.error_count} err</span>
                          {' / '}
                          <span className="text-amber-700">{it.warn_count} warn</span>
                        </span>
                      ) : (
                        '0'
                      )}
                    </Td>
                    <Td>
                      <div className="text-sm">{it.last_event || '-'}</div>
                      <div className="max-w-[16rem] truncate text-xs text-ink/50" title={it.last_message}>
                        {it.last_message || ''}
                      </div>
                    </Td>
                    <Td className="whitespace-nowrap text-xs">{formatTime(it.last_at)}</Td>
                    <Td>
                      <BtnRow>
                        {it.trace_id ? (
                          <Button type="button" variant="ghost" onClick={() => openTimeline(it.trace_id)}>
                            时间线
                          </Button>
                        ) : null}
                        {it.main_task_id ? (
                          <Link className="text-sm text-teal underline" to={`/ops/${it.main_task_id}/records`}>
                            流水
                          </Link>
                        ) : null}
                      </BtnRow>
                    </Td>
                  </tr>
                ))}
              </tbody>
            </TableWrap>
          )
        ) : events.length === 0 && !busy ? (
          <Empty>无匹配事件。请填写筛选条件后查询。</Empty>
        ) : (
          <TableWrap>
            <thead>
              <tr>
                <Th>时间</Th>
                <Th>级别</Th>
                <Th>主/子任务</Th>
                <Th>阶段 / 事件</Th>
                <Th>说明</Th>
                <Th>用户 / 渠道</Th>
                <Th>Trace</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {events.map((ev) => (
                <tr key={ev.id}>
                  <Td className="whitespace-nowrap text-xs">{formatTime(ev.created_at)}</Td>
                  <Td>{levelChip(ev.level)}</Td>
                  <Td className="whitespace-nowrap font-mono text-xs">
                    <div>主 #{ev.main_task_id || '-'}</div>
                    <div className="text-teal">子 #{ev.sub_task_id || '-'}</div>
                  </Td>
                  <Td>
                    <div className="text-sm font-medium">
                      {stageLabel[ev.stage] || ev.stage} · {ev.event}
                    </div>
                    <div className="text-xs text-ink/45">{ev.service || '-'}</div>
                  </Td>
                  <Td>
                    <div className="max-w-[18rem] truncate text-sm" title={ev.message}>
                      {ev.message || '-'}
                    </div>
                  </Td>
                  <Td className="text-xs">
                    {ev.user_id || '-'}
                    {ev.channel ? ` / ${ev.channel}` : ''}
                  </Td>
                  <Td>
                    <Mono className="text-xs">{ev.trace_id}</Mono>
                  </Td>
                  <Td>
                    <Button type="button" variant="ghost" onClick={() => openTimeline(ev.trace_id)}>
                      时间线
                    </Button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </TableWrap>
        )}

        <div className="mt-3 flex items-center justify-between text-sm text-ink/60">
          <span>
            共 {total} 条 · 第 {page}/{pages} 页
          </span>
          <BtnRow>
            <Button type="button" variant="ghost" disabled={page <= 1 || busy} onClick={() => setPage((p) => p - 1)}>
              上一页
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={page >= pages || busy}
              onClick={() => setPage((p) => p + 1)}
            >
              下一页
            </Button>
          </BtnRow>
        </div>
      </Panel>
    </div>
  )
}
