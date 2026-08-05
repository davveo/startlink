import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { SubTaskListResult, SubTaskView, TaskStatus } from '../api/types'
import { StatusChip } from '../components/StatusChip'

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
    if (!data || data.status === 'success' || data.status === 'partial' || data.status === 'failed' || data.status === 'cancelled') {
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
      <div className="page-head">
        <div>
          <h1>子任务</h1>
          <p>
            {data ? (
              <>
                主任务 #{data.main_task_id} · {data.title} ·{' '}
                <span className="mono">{data.biz_id}</span>
              </>
            ) : (
              <>主任务 #{mainTaskId}</>
            )}
          </p>
        </div>
        <div className="btn-row">
          <Link className="btn btn-ghost" to="/tasks">
            返回列表
          </Link>
          <Link className="btn btn-ink" to={`/campaigns?task=${mainTaskId}`}>
            活动进度
          </Link>
        </div>
      </div>

      {err ? <div className="toast toast-error">{err}</div> : null}

      {data ? (
        <div className="grid-3" style={{ marginBottom: '1rem' }}>
          <div className="stat">
            <div className="label">主任务状态</div>
            <div style={{ marginTop: '0.45rem' }}>
              <StatusChip status={data.status} />
            </div>
          </div>
          <div className="stat">
            <div className="label">子任务总数</div>
            <div className="value">{data.total}</div>
          </div>
          <div className="stat">
            <div className="label">当前页</div>
            <div className="value" style={{ fontSize: '1.2rem', marginTop: '0.55rem' }}>
              {page} / {totalPages}
            </div>
          </div>
        </div>
      ) : null}

      <div className="panel">
        <div className="grid-2">
          <div className="field">
            <label>子任务状态筛选</label>
            <select
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
            </select>
          </div>
          <div className="field">
            <label>&nbsp;</label>
            <button className="btn btn-ink" type="button" disabled={busy} onClick={() => void load()}>
              刷新
            </button>
          </div>
        </div>

        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>子任务 ID</th>
                <th>分片</th>
                <th>状态</th>
                <th>用户数</th>
                <th>成功/失败</th>
                <th>重试</th>
                <th>Worker</th>
                <th>时间</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {(data?.items ?? []).map((st) => (
                <tr key={st.id}>
                  <td className="mono">{st.id}</td>
                  <td className="mono">{st.shard_index}</td>
                  <td>
                    <StatusChip status={st.status} />
                    {st.last_error ? (
                      <div>
                        <small style={{ color: 'var(--rose)' }}>{st.last_error}</small>
                      </div>
                    ) : null}
                  </td>
                  <td className="mono">{st.total_count}</td>
                  <td className="mono">
                    {st.success_count}/{st.fail_count}
                  </td>
                  <td className="mono">{st.retry_count}</td>
                  <td className="mono">{st.worker_id || '-'}</td>
                  <td>
                    <small>开始 {formatTime(st.started_at)}</small>
                    <div>
                      <small>结束 {formatTime(st.finished_at)}</small>
                    </div>
                  </td>
                  <td>
                    <button className="btn btn-ghost" type="button" onClick={() => setSelected(st)}>
                      详情
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {(data?.items?.length ?? 0) === 0 ? <div className="empty">暂无子任务（可能尚未拆分完成）</div> : null}
        </div>

        <div className="btn-row" style={{ marginTop: '1rem', justifyContent: 'space-between' }}>
          <span style={{ color: 'var(--muted)' }}>
            第 {page} / {totalPages} 页
          </span>
          <div className="btn-row">
            <button
              className="btn btn-ghost"
              type="button"
              disabled={busy || page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            >
              上一页
            </button>
            <button
              className="btn btn-ghost"
              type="button"
              disabled={busy || page >= totalPages}
              onClick={() => setPage((p) => p + 1)}
            >
              下一页
            </button>
          </div>
        </div>
      </div>

      {selected ? (
        <div className="panel">
          <h2>子任务 #{selected.id}</h2>
          <div className="grid-2">
            <div>
              <p>
                <StatusChip status={selected.status} />
              </p>
              <p className="mono">
                shard={selected.shard_index} · users={selected.total_count} · ok=
                {selected.success_count} · fail={selected.fail_count} · retry={selected.retry_count}
              </p>
              <p className="mono" style={{ color: 'var(--muted)' }}>
                worker={selected.worker_id || '-'}
              </p>
              {selected.last_error ? (
                <p style={{ color: 'var(--rose)' }}>错误：{selected.last_error}</p>
              ) : null}
            </div>
            <div>
              <p>
                <small>认领：{formatTime(selected.claimed_at)}</small>
              </p>
              <p>
                <small>开始：{formatTime(selected.started_at)}</small>
              </p>
              <p>
                <small>结束：{formatTime(selected.finished_at)}</small>
              </p>
              <p>
                <small>更新：{formatTime(selected.updated_at)}</small>
              </p>
              <button className="btn btn-ghost" type="button" onClick={() => setSelected(null)}>
                关闭
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}
