import { useCallback, useEffect, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { ExportJob, FailureAgg, FunnelView, PushRecord } from '../api/types'
import { StatusChip } from '../components/StatusChip'
import {
  BtnRow,
  Button,
  ButtonLink,
  Empty,
  Field,
  Input,
  Mono,
  PageHead,
  Panel,
  PanelTitle,
  Select,
  Stat,
  TableWrap,
  Td,
  Th,
  Toast,
} from '../components/ui'

export function OpsPage() {
  const { id: idParam } = useParams()
  const [search] = useSearchParams()
  const initialId = idParam || search.get('task') || ''
  const [taskId, setTaskId] = useState(initialId)
  const [funnel, setFunnel] = useState<FunnelView | null>(null)
  const [failures, setFailures] = useState<FailureAgg[]>([])
  const [records, setRecords] = useState<PushRecord[]>([])
  const [recordTotal, setRecordTotal] = useState(0)
  const [userFilter, setUserFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [exportJob, setExportJob] = useState<ExportJob | null>(null)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    const id = Number(taskId)
    if (!Number.isFinite(id) || id <= 0) return
    setBusy(true)
    setErr('')
    try {
      const [f, fail, rec] = await Promise.all([
        api.getFunnel(id),
        api.getFailures(id),
        api.listRecords(id, { user_id: userFilter || undefined, status: statusFilter || undefined, page_size: 50 }),
      ])
      setFunnel(f)
      setFailures(fail.items ?? [])
      setRecords(rec.items ?? [])
      setRecordTotal(rec.total ?? 0)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载失败')
    } finally {
      setBusy(false)
    }
  }, [taskId, userFilter, statusFilter])

  useEffect(() => {
    if (Number(taskId) > 0) void load()
  }, [load, taskId])

  async function startExport(kind: string) {
    const id = Number(taskId)
    if (!id) return
    setBusy(true)
    setErr('')
    try {
      const job = await api.createExport(id, kind)
      setExportJob(job)
      setMsg(`导出任务 #${job.id} 已创建`)
      const timer = window.setInterval(async () => {
        try {
          const j = await api.getExport(job.id)
          setExportJob(j)
          if (j.status === 'success' || j.status === 'failed') {
            window.clearInterval(timer)
            if (j.status === 'success') setMsg(`导出完成，共 ${j.row_count} 行`)
            else setErr(j.error_msg || '导出失败')
          }
        } catch {
          window.clearInterval(timer)
        }
      }, 1000)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '导出失败')
    } finally {
      setBusy(false)
    }
  }

  const p = funnel?.pipeline

  return (
    <div>
      <PageHead
        title="投递分析"
        description="漏斗、失败归因、用户流水与导出。不含 DLQ/PEL 运维。"
        actions={
          <ButtonLink to="/tasks" variant="ghost">
            返回任务
          </ButtonLink>
        }
      />
      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel>
        <div className="grid gap-3 md:grid-cols-3">
          <Field label="主任务 ID">
            <Input value={taskId} onChange={(e) => setTaskId(e.target.value)} placeholder="task id" />
          </Field>
          <div className="flex items-end pb-3.5">
            <Button variant="ink" type="button" disabled={busy} onClick={() => void load()}>
              加载分析
            </Button>
          </div>
        </div>
      </Panel>

      {funnel ? (
        <>
          <div className="mt-4 grid gap-3 md:grid-cols-4">
            <Stat label="原始人群(落库)">{funnel.audience_raw_count}</Stat>
            <Stat label="过滤后">{funnel.audience_filtered_count}</Stat>
            <Stat label="可达">{funnel.audience_reachable_count}</Stat>
            <Stat label="入队流水">{funnel.enqueued_users}</Stat>
          </div>
          <Panel className="mt-4">
            <PanelTitle>投递漏斗（流水状态）</PanelTitle>
            <div className="grid gap-3 md:grid-cols-4">
              <Stat label="queued">{p?.queued ?? 0}</Stat>
              <Stat label="sending">{p?.sending ?? 0}</Stat>
              <Stat label="sent">{p?.sent ?? 0}</Stat>
              <Stat label="delivered">{p?.delivered ?? 0}</Stat>
              <Stat label="clicked">{p?.clicked ?? 0}</Stat>
              <Stat label="failed">{p?.failed ?? 0}</Stat>
              <Stat label="suppressed">{p?.suppressed ?? 0}</Stat>
              <Stat label="unreachable">{p?.unreachable ?? 0}</Stat>
            </div>
          </Panel>
        </>
      ) : null}

      <Panel className="mt-4">
        <PanelTitle>失败分析</PanelTitle>
        <TableWrap>
          <thead>
            <tr>
              <Th>渠道</Th>
              <Th>供应商</Th>
              <Th>错误</Th>
              <Th>次数</Th>
            </tr>
          </thead>
          <tbody>
            {failures.map((f, i) => (
              <tr key={`${f.channel}-${i}`}>
                <Td>
                  <Mono>{f.channel}</Mono>
                </Td>
                <Td>
                  <Mono>{f.provider || '-'}</Mono>
                </Td>
                <Td>{f.error_msg || '-'}</Td>
                <Td>{f.count}</Td>
              </tr>
            ))}
          </tbody>
        </TableWrap>
        {failures.length === 0 ? <Empty>暂无失败汇总</Empty> : null}
      </Panel>

      <Panel className="mt-4">
        <PanelTitle>用户流水 · 共 {recordTotal}</PanelTitle>
        <div className="grid gap-3 md:grid-cols-3">
          <Field label="user_id">
            <Input value={userFilter} onChange={(e) => setUserFilter(e.target.value)} />
          </Field>
          <Field label="status">
            <Select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
              <option value="">全部</option>
              <option value="failed">failed</option>
              <option value="sent">sent</option>
              <option value="delivered">delivered</option>
              <option value="suppressed">suppressed</option>
            </Select>
          </Field>
          <div className="flex items-end gap-2 pb-3.5">
            <Button variant="ink" type="button" disabled={busy} onClick={() => void load()}>
              查询
            </Button>
            <Button variant="ghost" type="button" disabled={busy} onClick={() => void startExport('records')}>
              异步导出
            </Button>
            <a
              className="inline-flex items-center justify-center rounded-full border border-line px-4 py-2 text-sm font-semibold"
              href={api.exportSyncUrl(Number(taskId) || 0, 'records')}
            >
              同步 CSV
            </a>
          </div>
        </div>
        {exportJob ? (
          <p className="mb-3 text-sm text-muted">
            导出 #{exportJob.id} <StatusChip status={exportJob.status} />{' '}
            {exportJob.file_url ? (
              <a className="text-teal-deep underline" href={exportJob.file_url}>
                下载
              </a>
            ) : null}
          </p>
        ) : null}
        <TableWrap>
          <thead>
            <tr>
              <Th>ID</Th>
              <Th>用户</Th>
              <Th>渠道</Th>
              <Th>状态</Th>
              <Th>错误</Th>
            </tr>
          </thead>
          <tbody>
            {records.map((r) => (
              <tr key={r.id}>
                <Td>
                  <Mono>{r.id}</Mono>
                </Td>
                <Td>
                  <Mono>{r.user_id}</Mono>
                </Td>
                <Td>
                  <Mono>{r.channel}</Mono>
                </Td>
                <Td>
                  <StatusChip status={r.status} />
                </Td>
                <Td className="max-w-xs truncate text-xs text-rose">{r.error_msg || '-'}</Td>
              </tr>
            ))}
          </tbody>
        </TableWrap>
        {records.length === 0 ? <Empty>暂无流水</Empty> : null}
        <BtnRow className="mt-3">
          <Link className="text-sm text-teal-deep underline" to={`/tasks/${taskId}/subtasks`}>
            查看子任务
          </Link>
          <Link className="text-sm text-teal-deep underline" to={`/campaigns?task=${taskId}`}>
            活动进度
          </Link>
        </BtnRow>
      </Panel>
    </div>
  )
}
