import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { SubTaskListResult, SubTaskView, TaskStatus } from '../api/types'
import { StatusChip } from '../components/StatusChip'
import {
  BtnRow,
  Button,
  ButtonLink,
  Empty,
  Field,
  Mono,
  PageHead,
  Panel,
  Select,
  Stat,
  TableWrap,
  Td,
  Th,
  Toast,
} from '../components/ui'

function formatTime(v?: string) {
  if (!v) return '-'
  try {
    return new Date(v).toLocaleString()
  } catch {
    return v
  }
}

export function SubTasksPage() {
  const { id } = useParams()
  const mainTaskId = Number(id)
  const [data, setData] = useState<SubTaskListResult | null>(null)
  const [status, setStatus] = useState<TaskStatus | ''>('')
  const [page, setPage] = useState(1)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [selected, setSelected] = useState<SubTaskView | null>(null)
  const pageSize = 50

  const load = useCallback(async () => {
    if (!Number.isFinite(mainTaskId) || mainTaskId <= 0) {
      setErr('无效的主任务 ID')
      return
    }
    setBusy(true)
    setErr('')
    try {
      const res = await api.listSubTasks(mainTaskId, {
        status,
        page,
        page_size: pageSize,
      })
      setData(res)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载子任务失败')
    } finally {
      setBusy(false)
    }
  }, [mainTaskId, page, status])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (
      !data ||
      data.status === 'success' ||
      data.status === 'partial' ||
      data.status === 'failed' ||
      data.status === 'cancelled'
    ) {
      return
    }
    const timer = window.setInterval(() => {
      void load()
    }, 3000)
    return () => window.clearInterval(timer)
  }, [data, load])

  const totalPages = Math.max(1, Math.ceil((data?.total ?? 0) / pageSize))

  return (
    <div>
      <PageHead
        title="子任务"
        description={
          data ? (
            <>
              主任务 #{data.main_task_id} · {data.title} · <Mono>{data.biz_id}</Mono>
            </>
          ) : (
            <>主任务 #{mainTaskId}</>
          )
        }
        actions={
          <>
            <ButtonLink to="/tasks" variant="ghost">
              返回列表
            </ButtonLink>
            <ButtonLink to={`/progress?task=${mainTaskId}`} variant="ink">
              活动进度
            </ButtonLink>
          </>
        }
      />

      {err ? <Toast kind="error">{err}</Toast> : null}

      {data ? (
        <div className="mb-4 grid gap-3.5 md:grid-cols-3">
          <Stat label="主任务状态">
            <div className="mt-1">
              <StatusChip status={data.status} />
            </div>
          </Stat>
          <Stat label="子任务总数">{data.total}</Stat>
          <Stat label="当前页">
            <span className="text-[1.2rem]">
              {page} / {totalPages}
            </span>
          </Stat>
        </div>
      ) : null}

      <Panel>
        <div className="grid gap-4 md:grid-cols-2">
          <Field label="子任务状态筛选">
            <Select
              value={status}
              onChange={(e) => {
                setPage(1)
                setStatus(e.target.value as TaskStatus | '')
              }}
            >
              <option value="">全部</option>
              <option value="pending">pending</option>
              <option value="running">running</option>
              <option value="success">success</option>
              <option value="failed">failed</option>
              <option value="cancelled">cancelled</option>
              <option value="retrying">retrying</option>
            </Select>
          </Field>
          <div className="flex items-end pb-3.5">
            <Button variant="ink" type="button" disabled={busy} onClick={() => void load()}>
              刷新
            </Button>
          </div>
        </div>

        <TableWrap>
          <thead>
            <tr>
              <Th>子任务 ID</Th>
              <Th>分片</Th>
              <Th>状态</Th>
              <Th>用户数</Th>
              <Th>成功/失败</Th>
              <Th>重试</Th>
              <Th>Worker</Th>
              <Th>时间</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {(data?.items ?? []).map((st) => (
              <tr key={st.id} className="hover:bg-white/50">
                <Td>
                  <Mono>{st.id}</Mono>
                </Td>
                <Td>
                  <Mono>{st.shard_index}</Mono>
                </Td>
                <Td>
                  <StatusChip status={st.status} />
                  {st.last_error ? <div className="mt-1 text-xs text-rose">{st.last_error}</div> : null}
                </Td>
                <Td>
                  <Mono>{st.total_count}</Mono>
                </Td>
                <Td>
                  <Mono>
                    {st.success_count}/{st.fail_count}
                  </Mono>
                </Td>
                <Td>
                  <Mono>{st.retry_count}</Mono>
                </Td>
                <Td>
                  <Mono>{st.worker_id || '-'}</Mono>
                </Td>
                <Td>
                  <div className="text-xs">开始 {formatTime(st.started_at)}</div>
                  <div className="text-xs">结束 {formatTime(st.finished_at)}</div>
                </Td>
                <Td>
                  <Button variant="ghost" type="button" onClick={() => setSelected(st)}>
                    详情
                  </Button>
                </Td>
              </tr>
            ))}
          </tbody>
        </TableWrap>
        {(data?.items?.length ?? 0) === 0 ? <Empty>暂无子任务（可能尚未拆分完成）</Empty> : null}

        <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
          <span className="text-sm text-muted">
            第 {page} / {totalPages} 页
          </span>
          <BtnRow>
            <Button
              variant="ghost"
              type="button"
              disabled={busy || page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            >
              上一页
            </Button>
            <Button
              variant="ghost"
              type="button"
              disabled={busy || page >= totalPages}
              onClick={() => setPage((p) => p + 1)}
            >
              下一页
            </Button>
          </BtnRow>
        </div>
      </Panel>

      {selected ? (
        <Panel className="mt-4">
          <h2 className="mb-4 text-lg font-semibold">子任务 #{selected.id}</h2>
          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <StatusChip status={selected.status} />
              <p className="font-mono text-sm">
                shard={selected.shard_index} · users={selected.total_count} · ok=
                {selected.success_count} · fail={selected.fail_count} · retry={selected.retry_count}
              </p>
              <p className="font-mono text-sm text-muted">worker={selected.worker_id || '-'}</p>
              {selected.last_error ? <p className="text-rose">错误：{selected.last_error}</p> : null}
            </div>
            <div className="space-y-2 text-sm text-muted">
              <p>认领：{formatTime(selected.claimed_at)}</p>
              <p>开始：{formatTime(selected.started_at)}</p>
              <p>结束：{formatTime(selected.finished_at)}</p>
              <p>更新：{formatTime(selected.updated_at)}</p>
              <Button variant="ghost" type="button" onClick={() => setSelected(null)}>
                关闭
              </Button>
            </div>
          </div>
        </Panel>
      ) : null}
    </div>
  )
}
