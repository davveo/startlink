import { useCallback, useEffect, useState } from 'react'
import { ApiError, api } from '../api/client'
import type { CampaignListItem, TaskStatus } from '../api/types'
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

function formatTime(v?: string) {
  if (!v) return '-'
  try {
    return new Date(v).toLocaleString()
  } catch {
    return v
  }
}

export function TasksPage() {
  const [items, setItems] = useState<CampaignListItem[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<TaskStatus | ''>('')
  const [keyword, setKeyword] = useState('')
  const [bizScene, setBizScene] = useState('')
  const [channel, setChannel] = useState('')
  const [priority, setPriority] = useState('')
  const [createdBy, setCreatedBy] = useState('')
  const [selected, setSelected] = useState<number[]>([])
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)
  const pageSize = 20

  const load = useCallback(async () => {
    setBusy(true)
    setErr('')
    try {
      const res = await api.listCampaigns({
        status,
        keyword: keyword.trim() || undefined,
        biz_scene: bizScene.trim() || undefined,
        channel: channel.trim() || undefined,
        priority: priority.trim() || undefined,
        created_by: createdBy.trim() || undefined,
        page,
        page_size: pageSize,
      })
      setItems(res.items ?? [])
      setTotal(res.total ?? 0)
      setSelected([])
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载任务列表失败')
    } finally {
      setBusy(false)
    }
  }, [bizScene, channel, createdBy, keyword, page, priority, status])

  useEffect(() => {
    void load()
  }, [load])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  function toggle(id: number) {
    setSelected((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]))
  }

  async function batch(action: 'pause' | 'resume' | 'cancel' | 'retry') {
    if (selected.length === 0) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const res = await api.batchAction(action, selected)
      setMsg(`批量 ${action}：成功 ${res.success} / 失败 ${res.failed}`)
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '批量操作失败')
    } finally {
      setBusy(false)
    }
  }

  async function publish(id: number) {
    setBusy(true)
    try {
      await api.publishCampaign(id)
      setMsg(`草稿 #${id} 已发布`)
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '发布失败')
    } finally {
      setBusy(false)
    }
  }

  async function copyOne(id: number) {
    const biz = window.prompt('新 biz_id', `copy-${Date.now()}`)
    if (!biz) return
    setBusy(true)
    try {
      const res = await api.copyCampaign(id, { biz_id: biz, as_draft: true, created_by: 'console' })
      setMsg(`已复制为草稿 task_id=${res.task_id}`)
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '复制失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <PageHead
        title="任务列表"
        description={`查看主任务状态，支持批量暂停/恢复/取消/重推。共 ${total} 条。`}
        actions={
          <ButtonLink to="/campaigns" variant="primary">
            创建活动
          </ButtonLink>
        }
      />

      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel>
        <div className="grid gap-3.5 md:grid-cols-3">
          <Field label="状态">
            <Select
              value={status}
              onChange={(e) => {
                setPage(1)
                setStatus(e.target.value as TaskStatus | '')
              }}
            >
              <option value="">全部</option>
              <option value="draft">draft</option>
              <option value="pending">pending</option>
              <option value="running">running</option>
              <option value="paused">paused</option>
              <option value="success">success</option>
              <option value="partial">partial</option>
              <option value="failed">failed</option>
              <option value="cancelled">cancelled</option>
              <option value="retrying">retrying</option>
            </Select>
          </Field>
          <Field label="biz_scene">
            <Input value={bizScene} onChange={(e) => setBizScene(e.target.value)} placeholder="demo" />
          </Field>
          <Field label="channel">
            <Input value={channel} onChange={(e) => setChannel(e.target.value)} placeholder="app_push" />
          </Field>
          <Field label="priority">
            <Select
              value={priority}
              onChange={(e) => {
                setPage(1)
                setPriority(e.target.value)
              }}
            >
              <option value="">全部</option>
              <option value="high">high</option>
              <option value="normal">normal</option>
            </Select>
          </Field>
          <Field label="created_by">
            <Input value={createdBy} onChange={(e) => setCreatedBy(e.target.value)} placeholder="console" />
          </Field>
          <Field label="关键词（biz_id / title）">
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder="camp- / Demo"
            />
          </Field>
        </div>
        <BtnRow>
          <Button
            variant="ink"
            type="button"
            disabled={busy}
            onClick={() => {
              setPage(1)
              void load()
            }}
          >
            查询
          </Button>
          <Button variant="ghost" type="button" disabled={busy || !selected.length} onClick={() => void batch('pause')}>
            批量暂停
          </Button>
          <Button variant="ghost" type="button" disabled={busy || !selected.length} onClick={() => void batch('resume')}>
            批量恢复
          </Button>
          <Button variant="danger" type="button" disabled={busy || !selected.length} onClick={() => void batch('cancel')}>
            批量取消
          </Button>
          <Button variant="primary" type="button" disabled={busy || !selected.length} onClick={() => void batch('retry')}>
            批量重推
          </Button>
        </BtnRow>
      </Panel>

      <Panel className="mt-4">
        <TableWrap>
          <thead>
            <tr>
              <Th />
              <Th>ID</Th>
              <Th>标题 / biz_id</Th>
              <Th>场景</Th>
              <Th>渠道</Th>
              <Th>状态</Th>
              <Th>用户</Th>
              <Th>创建时间</Th>
              <Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {items.map((t) => (
              <tr key={t.id} className="hover:bg-white/50">
                <Td>
                  <input type="checkbox" checked={selected.includes(t.id)} onChange={() => toggle(t.id)} />
                </Td>
                <Td>
                  <Mono>{t.id}</Mono>
                </Td>
                <Td>
                  <div>{t.title}</div>
                  <Mono className="text-muted">{t.biz_id}</Mono>
                  {t.created_by ? <div className="text-xs text-muted">by {t.created_by}</div> : null}
                </Td>
                <Td>{t.biz_scene}</Td>
                <Td>
                  <Mono>
                    {(t.channels && t.channels.length > 0 ? t.channels : [t.channel]).join(', ')}
                  </Mono>
                </Td>
                <Td>
                  <StatusChip status={t.status} />
                </Td>
                <Td>
                  <Mono>
                    {t.success_count}/{t.fail_count}/{t.total_count}
                  </Mono>
                </Td>
                <Td>
                  <span className="text-xs">{formatTime(t.created_at)}</span>
                </Td>
                <Td>
                  <BtnRow>
                    {t.status === 'draft' ? (
                      <Button variant="primary" type="button" disabled={busy} onClick={() => void publish(t.id)}>
                        发布
                      </Button>
                    ) : null}
                    <ButtonLink to={`/tasks/${t.id}/subtasks`} variant="ghost">
                      子任务
                    </ButtonLink>
                    <ButtonLink to={`/ops/${t.id}`} variant="ghost">
                      分析
                    </ButtonLink>
                    <ButtonLink to={`/campaigns?task=${t.id}`} variant="ghost">
                      进度
                    </ButtonLink>
                    <Button variant="ghost" type="button" disabled={busy} onClick={() => void copyOne(t.id)}>
                      复制
                    </Button>
                    <a
                      className="inline-flex items-center rounded-full border border-line px-3 py-1 text-xs"
                      href={api.exportSyncUrl(t.id, 'records')}
                    >
                      导出
                    </a>
                  </BtnRow>
                </Td>
              </tr>
            ))}
          </tbody>
        </TableWrap>
        {items.length === 0 ? <Empty>暂无任务</Empty> : null}

        <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
          <span className="text-sm text-muted">
            第 {page} / {totalPages} 页 · 已选 {selected.length}
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
    </div>
  )
}
