import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { ExportJob, PushRecord } from '../api/types'
import { useAuth } from '../auth/AuthContext'
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
  Select,
  TableWrap,
  Td,
  Th,
  Toast,
} from '../components/ui'

const pageSize = 20

export function RecordsPage() {
  const { user } = useAuth()
  const { id: idParam } = useParams()
  const navigate = useNavigate()

  const taskFromRoute = idParam && /^\d+$/.test(idParam) ? idParam : ''
  const [taskId, setTaskId] = useState(taskFromRoute)
  const [userFilter, setUserFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState('')
  const [channelFilter, setChannelFilter] = useState('')
  const [page, setPage] = useState(1)
  const [query, setQuery] = useState({
    taskId: taskFromRoute,
    user_id: '',
    status: '',
    channel: '',
  })

  const [records, setRecords] = useState<PushRecord[]>([])
  const [total, setTotal] = useState(0)
  const [exportJob, setExportJob] = useState<ExportJob | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')

  useEffect(() => {
    if (taskFromRoute) {
      setTaskId(taskFromRoute)
      setQuery((q) => ({ ...q, taskId: taskFromRoute }))
      setPage(1)
    }
  }, [taskFromRoute])

  const load = useCallback(async () => {
    const id = Number(query.taskId)
    if (!Number.isFinite(id) || id <= 0) return
    setBusy(true)
    setErr('')
    try {
      const res = await api.listRecords(id, {
        user_id: query.user_id || undefined,
        status: query.status || undefined,
        channel: query.channel || undefined,
        page,
        page_size: pageSize,
      })
      setRecords(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载流水失败')
    } finally {
      setBusy(false)
    }
  }, [query, page])

  useEffect(() => {
    if (Number(query.taskId) > 0) void load()
  }, [load, query.taskId])

  function onQuery() {
    const id = Number(taskId)
    if (!Number.isFinite(id) || id <= 0) {
      setErr('请输入有效的主任务 ID')
      return
    }
    setErr('')
    setPage(1)
    setQuery({
      taskId: String(id),
      user_id: userFilter.trim(),
      status: statusFilter,
      channel: channelFilter.trim(),
    })
    if (taskFromRoute !== String(id)) {
      navigate(`/ops/${id}/records`)
    }
  }

  async function startExport() {
    const id = Number(query.taskId || taskId)
    if (!id) return
    setBusy(true)
    setErr('')
    try {
      const job = await api.createExport(id, 'records', user?.username)
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

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const activeTask = Number(query.taskId || taskId)

  return (
    <div>
      <PageHead
        title="用户流水"
        description={`主任务 #${activeTask || '-'} · 共 ${total} 条。分页查看，避免分析页一次拉全量。`}
        actions={
          <BtnRow>
            {activeTask > 0 ? (
              <ButtonLink to={`/ops/${activeTask}`} variant="ghost">
                返回分析
              </ButtonLink>
            ) : null}
            <ButtonLink to="/tasks" variant="ghost">
              任务列表
            </ButtonLink>
          </BtnRow>
        }
      />

      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel>
        <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
          <Field label="主任务 ID" hint="从分析页「用户流水」进入时会自动带上。">
            <Input
              className="font-mono text-sm"
              value={taskId}
              onChange={(e) => setTaskId(e.target.value)}
              placeholder="task id"
            />
          </Field>
          <Field label="用户 ID" hint="精确匹配 push_records.user_id。">
            <Input
              className="font-mono text-sm"
              value={userFilter}
              onChange={(e) => setUserFilter(e.target.value)}
              placeholder="u_demo_1"
            />
          </Field>
          <Field label="状态">
            <Select value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)}>
              <option value="">全部</option>
              <option value="queued">queued</option>
              <option value="sending">sending</option>
              <option value="sent">sent</option>
              <option value="delivered">delivered</option>
              <option value="clicked">clicked</option>
              <option value="failed">failed</option>
              <option value="suppressed">suppressed</option>
              <option value="unreachable">unreachable</option>
            </Select>
          </Field>
          <Field label="渠道" hint="如 inbox / sms / app_push。">
            <Input
              className="font-mono text-sm"
              value={channelFilter}
              onChange={(e) => setChannelFilter(e.target.value)}
              placeholder="inbox"
            />
          </Field>
        </div>
        <BtnRow className="mt-1">
          <Button variant="ink" type="button" disabled={busy} onClick={onQuery}>
            查询
          </Button>
          <Button variant="ghost" type="button" disabled={busy || !activeTask} onClick={() => void startExport()}>
            异步导出
          </Button>
          {activeTask > 0 ? (
            <a
              className="inline-flex items-center justify-center rounded-full border border-line px-4 py-2 text-sm font-semibold"
              href={api.exportSyncUrl(activeTask, 'records')}
            >
              同步 CSV
            </a>
          ) : null}
        </BtnRow>
        {exportJob ? (
          <p className="mt-3 text-sm text-muted">
            导出 #{exportJob.id} <StatusChip status={exportJob.status} />{' '}
            {exportJob.file_url ? (
              <a className="text-teal-deep underline" href={exportJob.file_url}>
                下载
              </a>
            ) : null}
          </p>
        ) : null}
      </Panel>

      <Panel className="mt-4">
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
              <tr key={r.id} className="hover:bg-white/50">
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
        <BtnRow className="mt-4">
          <Button
            variant="ghost"
            type="button"
            disabled={busy || page <= 1}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
          >
            上一页
          </Button>
          <span className="text-sm text-muted">
            第 {page} / {totalPages} 页
          </span>
          <Button
            variant="ghost"
            type="button"
            disabled={busy || page >= totalPages}
            onClick={() => setPage((p) => p + 1)}
          >
            下一页
          </Button>
        </BtnRow>
      </Panel>
    </div>
  )
}
