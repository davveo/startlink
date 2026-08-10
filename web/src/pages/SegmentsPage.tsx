import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { ApiError } from '../api/client'
import { segmentApi, type Segment, type SegmentInput, type SegmentKind } from '../api/segments'
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
  TableWrap,
  Td,
  Textarea,
  Th,
  Toast,
} from '../components/ui'
import { useClampPage, useDebounced, useRequestSeq } from '../lib/async'

const pageSize = 20

const emptyForm: SegmentInput & { audience_extra_text: string } = {
  code: '',
  name: '',
  kind: 'include',
  biz_scene: '',
  audience_ref: '',
  description: '',
  status: 'active',
  audience_extra_text: '',
}

function formatTime(v?: string) {
  if (!v) return '-'
  try {
    return new Date(v).toLocaleString()
  } catch {
    return v
  }
}

export function SegmentsPage() {
  const { can } = useAuth()
  const canManage = can(Perm.SegmentManage)

  const [items, setItems] = useState<Segment[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [kind, setKind] = useState('')
  const [bizScene, setBizScene] = useState('')
  const [status, setStatus] = useState('')
  const [keyword, setKeyword] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [reloadTick, setReloadTick] = useState(0)

  // editing=null 表示新建；否则是被编辑人群段的 code
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const [form, setForm] = useState(emptyForm)
  const [confirmDelete, setConfirmDelete] = useState<Segment | null>(null)

  const seq = useRequestSeq()
  const bizSceneQ = useDebounced(bizScene)
  const keywordQ = useDebounced(keyword)

  const load = useCallback(async () => {
    const s = seq.next()
    setBusy(true)
    setErr('')
    try {
      const res = await segmentApi.list({
        kind: kind || undefined,
        biz_scene: bizSceneQ.trim() || undefined,
        status: status || undefined,
        keyword: keywordQ.trim() || undefined,
        page,
        page_size: pageSize,
      })
      if (!seq.isLatest(s)) return
      setItems(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      if (!seq.isLatest(s)) return
      setErr(e instanceof ApiError ? e.message : '加载人群段失败')
    } finally {
      if (seq.isLatest(s)) setBusy(false)
    }
  }, [bizSceneQ, keywordQ, kind, page, seq, status])

  useEffect(() => {
    void load()
  }, [load, reloadTick])

  const pages = Math.max(1, Math.ceil(total / pageSize))
  useClampPage(page, total, pageSize, setPage)

  function startCreate() {
    setEditing(null)
    setForm(emptyForm)
    setShowForm(true)
  }

  function startEdit(seg: Segment) {
    setEditing(seg.code)
    setForm({
      code: seg.code,
      name: seg.name,
      kind: seg.kind,
      biz_scene: seg.biz_scene,
      audience_ref: seg.audience_ref,
      description: seg.description ?? '',
      status: seg.status,
      audience_extra_text: '',
    })
    setShowForm(true)
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setErr('')
    setMsg('')

    let extra: Record<string, unknown> | undefined
    const raw = form.audience_extra_text.trim()
    if (raw) {
      try {
        const parsed: unknown = JSON.parse(raw)
        if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
          throw new Error('not an object')
        }
        extra = parsed as Record<string, unknown>
      } catch {
        setErr('圈人参数必须是合法的 JSON 对象')
        return
      }
    }

    const payload: SegmentInput = {
      name: form.name.trim(),
      kind: form.kind,
      biz_scene: form.biz_scene.trim(),
      audience_ref: form.audience_ref.trim(),
      description: form.description?.trim() || undefined,
      status: form.status,
      audience_extra: extra,
    }

    setBusy(true)
    try {
      if (editing) {
        await segmentApi.update(editing, payload)
        setMsg(`已更新人群段 ${editing}`)
      } else {
        const created = await segmentApi.create({ ...payload, code: form.code?.trim() || undefined })
        setMsg(`已创建人群段 ${created.code}`)
      }
      setShowForm(false)
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  async function onRefresh(seg: Segment) {
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const res = await segmentApi.refresh(seg.code)
      setMsg(
        res.estimated
          ? `${seg.code} 成员数约 ${res.member_count}（已达统计上限，为估算下界）`
          : `${seg.code} 成员数已刷新为 ${res.member_count}`,
      )
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '刷新成员数失败')
    } finally {
      setBusy(false)
    }
  }

  async function onDelete() {
    if (!confirmDelete) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      await segmentApi.remove(confirmDelete.code)
      setMsg(`已删除人群段 ${confirmDelete.code}`)
      setConfirmDelete(null)
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '删除失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <PageHead
        title="人群段"
        description="把一次性的圈人参数沉淀成可复用资产：活动直接引用 code，成员数走缓存快照，不必每次预检都重新圈人。"
        actions={
          canManage ? (
            <Button type="button" variant="primary" disabled={busy} onClick={startCreate}>
              新建人群段
            </Button>
          ) : null
        }
      />

      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel>
        <div className="flex flex-wrap items-end gap-3">
          <Field label="用途" noMargin className="min-w-[8rem] flex-[1_1_8rem]">
            <Select
              value={kind}
              onChange={(e) => {
                setPage(1)
                setKind(e.target.value)
              }}
            >
              <option value="">全部</option>
              <option value="include">投放目标</option>
              <option value="exclude">排除名单</option>
            </Select>
          </Field>
          <Field label="业务场景" noMargin className="min-w-[9rem] flex-[1_1_9rem]">
            <Input
              placeholder="全部"
              value={bizScene}
              onChange={(e) => {
                setPage(1)
                setBizScene(e.target.value)
              }}
            />
          </Field>
          <Field label="状态" noMargin className="min-w-[8rem] flex-[1_1_8rem]">
            <Select
              value={status}
              onChange={(e) => {
                setPage(1)
                setStatus(e.target.value)
              }}
            >
              <option value="">全部</option>
              <option value="active">启用</option>
              <option value="disabled">停用</option>
            </Select>
          </Field>
          <Field label="关键词" noMargin className="min-w-[10rem] flex-[1_1_10rem]">
            <Input
              placeholder="code / 名称"
              value={keyword}
              onChange={(e) => {
                setPage(1)
                setKeyword(e.target.value)
              }}
            />
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
              <Th>Code</Th>
              <Th>名称</Th>
              <Th>用途</Th>
              <Th>业务场景</Th>
              <Th>人群标识</Th>
              <Th>成员数</Th>
              <Th>状态</Th>
              <Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr>
                <Td colSpan={8}>
                  <Empty>暂无人群段</Empty>
                </Td>
              </tr>
            ) : (
              items.map((seg) => (
                <tr key={seg.code} className="hover:bg-white/50">
                  <Td>
                    <Mono>{seg.code}</Mono>
                  </Td>
                  <Td>
                    {seg.name}
                    {seg.description ? (
                      <div className="text-xs text-muted" title={seg.description}>
                        {seg.description}
                      </div>
                    ) : null}
                  </Td>
                  <Td>
                    <Chip tone={seg.kind === 'exclude' ? 'warn' : 'teal'}>
                      {seg.kind === 'exclude' ? '排除名单' : '投放目标'}
                    </Chip>
                  </Td>
                  <Td>{seg.biz_scene}</Td>
                  <Td className="max-w-[180px] truncate text-xs" title={seg.audience_ref}>
                    <Mono>{seg.audience_ref}</Mono>
                  </Td>
                  <Td>
                    <div>{seg.member_count.toLocaleString()}</div>
                    <div className="text-xs text-muted">{formatTime(seg.counted_at)}</div>
                    {seg.refresh_error ? (
                      <div className="text-xs text-rose" title={seg.refresh_error}>
                        刷新异常
                      </div>
                    ) : null}
                  </Td>
                  <Td>
                    <Chip tone={seg.status === 'disabled' ? 'muted' : 'ok'}>
                      {seg.status === 'disabled' ? '停用' : '启用'}
                    </Chip>
                  </Td>
                  <Td>
                    {canManage ? (
                      <BtnRow>
                        <Button
                          type="button"
                          variant="ghost"
                          className="px-3 py-1 text-xs"
                          disabled={busy}
                          onClick={() => startEdit(seg)}
                        >
                          编辑
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          className="px-3 py-1 text-xs"
                          disabled={busy}
                          onClick={() => void onRefresh(seg)}
                        >
                          刷新成员数
                        </Button>
                        <Button
                          type="button"
                          variant="danger"
                          className="px-3 py-1 text-xs"
                          disabled={busy}
                          onClick={() => setConfirmDelete(seg)}
                        >
                          删除
                        </Button>
                      </BtnRow>
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
        open={showForm}
        wide
        title={editing ? <>编辑人群段 <Mono className="text-base">{editing}</Mono></> : '新建人群段'}
        onClose={() => {
          if (!busy) setShowForm(false)
        }}
      >
        <form onSubmit={onSubmit}>
          <div className="mb-1 grid items-end gap-3 sm:grid-cols-2">
            {editing ? null : (
              <Field label="Code（可选）" noMargin className="sm:col-span-2" hint="留空按名称自动生成；仅字母数字下划线短横线">
                <Input
                  className="font-mono text-sm"
                  value={form.code ?? ''}
                  onChange={(e) => setForm({ ...form, code: e.target.value })}
                  placeholder="vip_users"
                />
              </Field>
            )}
            <Field label="名称" noMargin>
              <Input required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </Field>
            <Field label="用途" noMargin>
              <Select
                value={form.kind}
                onChange={(e) => setForm({ ...form, kind: e.target.value as SegmentKind })}
              >
                <option value="include">投放目标</option>
                <option value="exclude">排除名单</option>
              </Select>
            </Field>
            <Field label="业务场景" noMargin>
              <Input
                required
                value={form.biz_scene}
                onChange={(e) => setForm({ ...form, biz_scene: e.target.value })}
                placeholder="demo"
              />
            </Field>
            <Field label="人群标识" noMargin hint="传给业务 Provider 的 audience_ref">
              <Input
                required
                className="font-mono text-sm"
                value={form.audience_ref}
                onChange={(e) => setForm({ ...form, audience_ref: e.target.value })}
              />
            </Field>
            <Field label="状态" noMargin>
              <Select
                value={form.status}
                onChange={(e) => setForm({ ...form, status: e.target.value as 'active' | 'disabled' })}
              >
                <option value="active">启用</option>
                <option value="disabled">停用</option>
              </Select>
            </Field>
            <Field label="说明" noMargin>
              <Input
                value={form.description ?? ''}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
              />
            </Field>
            <Field
              label="圈人参数（JSON，可选）"
              noMargin
              className="sm:col-span-2"
              hint={editing ? '留空表示保持原有参数不变' : '透传给 Provider，如 {"total": 5000}'}
            >
              <Textarea
                value={form.audience_extra_text}
                onChange={(e) => setForm({ ...form, audience_extra_text: e.target.value })}
                placeholder='{"total": 5000}'
              />
            </Field>
          </div>
          <BtnRow className="mt-3">
            <Button type="submit" variant="primary" disabled={busy}>
              {editing ? '保存' : '创建'}
            </Button>
            <Button type="button" variant="ghost" disabled={busy} onClick={() => setShowForm(false)}>
              取消
            </Button>
          </BtnRow>
        </form>
      </Modal>

      <Modal
        open={!!confirmDelete}
        title="删除人群段"
        onClose={() => {
          if (!busy) setConfirmDelete(null)
        }}
      >
        <p className="text-sm text-ink-soft">
          确认删除 <Mono>{confirmDelete?.code}</Mono>（{confirmDelete?.name}）？
        </p>
        <p className="mt-2 text-sm text-muted">仍被活动引用的人群段无法删除，后端会拒绝并告知引用数量。</p>
        <BtnRow className="mt-4">
          <Button type="button" variant="danger" disabled={busy} onClick={() => void onDelete()}>
            确认删除
          </Button>
          <Button type="button" variant="ghost" disabled={busy} onClick={() => setConfirmDelete(null)}>
            取消
          </Button>
        </BtnRow>
      </Modal>
    </div>
  )
}
