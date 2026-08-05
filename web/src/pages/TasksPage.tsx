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
  const [err, setErr] = useState('')
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
        page,
        page_size: pageSize,
      })
      setItems(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载任务列表失败')
    } finally {
      setBusy(false)
    }
  }, [bizScene, keyword, page, status])

  useEffect(() => {
    void load()
  }, [load])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div>
      <PageHead
        title="任务列表"
        description={`查看主任务状态，进入子任务页追踪每个分片执行情况。共 ${total} 条。`}
        actions={
          <ButtonLink to="/campaigns" variant="primary">
            创建活动
          </ButtonLink>
        }
      />

      {err ? <Toast kind="error">{err}</Toast> : null}

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
          <Button variant="ghost" type="button" disabled={busy} onClick={() => void load()}>
            刷新
          </Button>
        </BtnRow>
      </Panel>

      <Panel className="mt-4">
        <TableWrap>
          <thead>
            <tr>
              <Th>ID</Th>
              <Th>标题 / biz_id</Th>
              <Th>场景</Th>
              <Th>渠道</Th>
              <Th>状态</Th>
              <Th>用户</Th>
              <Th>子任务</Th>
              <Th>创建时间</Th>
              <Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {items.map((t) => (
              <tr key={t.id} className="hover:bg-white/50">
                <Td>
                  <Mono>{t.id}</Mono>
                </Td>
                <Td>
                  <div>{t.title}</div>
                  <Mono className="text-muted">{t.biz_id}</Mono>
                </Td>
                <Td>{t.biz_scene}</Td>
                <Td>
                  <Mono>
                    {(t.channels && t.channels.length > 0 ? t.channels : [t.channel]).join(', ')}
                  </Mono>
                  <div>
                    <span className="text-xs text-muted">
                      {t.channel_mode} · {t.priority}
                    </span>
                  </div>
                </Td>
                <Td>
                  <StatusChip status={t.status} />
                </Td>
                <Td>
                  <Mono>
                    {t.success_count}/{t.fail_count}/{t.total_count}
                  </Mono>
                  <div>
                    <span className="text-xs text-muted">成功/失败/总量</span>
                  </div>
                </Td>
                <Td>
                  <Mono>
                    {t.sub_task_done}/{t.sub_task_total}
                  </Mono>
                </Td>
                <Td>
                  <span className="text-xs">{formatTime(t.created_at)}</span>
                </Td>
                <Td>
                  <BtnRow>
                    <ButtonLink to={`/tasks/${t.id}/subtasks`} variant="primary">
                      子任务
                    </ButtonLink>
                    <ButtonLink to={`/campaigns?task=${t.id}`} variant="ghost">
                      进度
                    </ButtonLink>
                  </BtnRow>
                </Td>
              </tr>
            ))}
          </tbody>
        </TableWrap>
        {items.length === 0 ? <Empty>暂无任务</Empty> : null}

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
    </div>
  )
}
