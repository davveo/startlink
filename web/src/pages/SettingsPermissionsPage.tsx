import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { ApiError, api } from '../api/client'
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

type PermInfo = {
  code: string
  name: string
  group: string
  kind: string
  description?: string
  is_system?: boolean
}

const pageSize = 20

export function SettingsPermissionsPage() {
  const [items, setItems] = useState<PermInfo[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const [keywordInput, setKeywordInput] = useState('')
  const [group, setGroup] = useState('')
  const [kind, setKind] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')

  const [showCreate, setShowCreate] = useState(false)
  const [createForm, setCreateForm] = useState({
    code: '',
    name: '',
    group: '自定义',
    kind: 'action',
    description: '',
  })
  const [editing, setEditing] = useState<string | null>(null)
  const [editForm, setEditForm] = useState({
    name: '',
    group: '',
    kind: 'action',
    description: '',
  })

  const seq = useRequestSeq()
  const groupQ = useDebounced(group)

  const load = useCallback(async () => {
    const s = seq.next()
    setBusy(true)
    setErr('')
    try {
      const res = await api.rbacPermissions({
        page,
        page_size: pageSize,
        keyword: keyword || undefined,
        group: groupQ.trim() || undefined,
        kind: kind || undefined,
      })
      if (!seq.isLatest(s)) return
      setItems(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      if (!seq.isLatest(s)) return
      setErr(e instanceof ApiError ? e.message : '加载权限目录失败')
    } finally {
      if (seq.isLatest(s)) setBusy(false)
    }
  }, [groupQ, kind, keyword, page, seq])

  useEffect(() => {
    void load()
  }, [load])

  const pages = Math.max(1, Math.ceil(total / pageSize))

  useClampPage(page, total, pageSize, setPage)

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      await api.createPermission(createForm)
      setMsg(`已注册权限 ${createForm.code}`)
      setShowCreate(false)
      setCreateForm({ code: '', name: '', group: '自定义', kind: 'action', description: '' })
      setPage(1)
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '创建失败')
    } finally {
      setBusy(false)
    }
  }

  function startEdit(p: PermInfo) {
    setEditing(p.code)
    setEditForm({
      name: p.name,
      group: p.group,
      kind: p.kind,
      description: p.description || '',
    })
  }

  async function onSaveEdit() {
    if (!editing) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      await api.updatePermission(editing, editForm)
      setMsg(`已更新 ${editing}`)
      setEditing(null)
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <PageHead
        title="权限管理"
        description="注册与维护权限码（菜单 / 按钮）。角色勾选从本表读取；前端挂码与后端 RequirePermission 使用同一权限码即可生效。"
      />
      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel>
        <div className="mb-4">
          <div className="flex flex-wrap items-end gap-3">
            <Field label="关键词" noMargin className="min-w-[10rem] flex-[1_1_10rem]">
              <Input
                placeholder="码 / 名称"
                value={keywordInput}
                onChange={(e) => setKeywordInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    setPage(1)
                    setKeyword(keywordInput.trim())
                  }
                }}
              />
            </Field>
            <Field label="分组" noMargin className="min-w-[8rem] flex-[1_1_8rem]">
              <Input
                placeholder="全部"
                value={group}
                onChange={(e) => {
                  setPage(1)
                  setGroup(e.target.value)
                }}
              />
            </Field>
            <Field label="类型" noMargin className="min-w-[8rem] flex-[1_1_8rem]">
              <Select
                value={kind}
                onChange={(e) => {
                  setPage(1)
                  setKind(e.target.value)
                }}
              >
                <option value="">全部</option>
                <option value="menu">菜单</option>
                <option value="action">按钮/操作</option>
              </Select>
            </Field>
          </div>
          <div className="mt-3 flex flex-wrap items-end gap-3">
            <BtnRow className="shrink-0">
              <Button
                type="button"
                variant="ghost"
                disabled={busy}
                onClick={() => {
                  setPage(1)
                  setKeyword(keywordInput.trim())
                }}
              >
                查询
              </Button>
              <Button type="button" variant="ghost" disabled={busy} onClick={() => void load()}>
                刷新
              </Button>
              <Button
                type="button"
                variant="primary"
                disabled={busy}
                onClick={() => {
                  setCreateForm({ code: '', name: '', group: '自定义', kind: 'action', description: '' })
                  setShowCreate(true)
                }}
              >
                新建权限
              </Button>
            </BtnRow>
          </div>
        </div>

        <TableWrap>
          <thead>
            <tr>
              <Th>分组</Th>
              <Th>类型</Th>
              <Th>中文名</Th>
              <Th>权限码</Th>
              <Th>系统</Th>
              <Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {items.map((p) => (
              <tr key={p.code} className="hover:bg-white/50">
                <Td>{p.group}</Td>
                <Td>{p.kind === 'menu' ? '菜单' : '按钮/操作'}</Td>
                <Td>{p.name}</Td>
                <Td>
                  <Mono>{p.code}</Mono>
                </Td>
                <Td>{p.is_system ? '是' : '否'}</Td>
                <Td>
                  <Button
                    type="button"
                    variant="ghost"
                    className="px-3 py-1 text-xs"
                    onClick={() => startEdit(p)}
                  >
                    编辑
                  </Button>
                </Td>
              </tr>
            ))}
          </tbody>
        </TableWrap>
        {items.length === 0 ? <Empty>无匹配权限</Empty> : null}
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
        open={showCreate}
        title="新建权限"
        onClose={() => {
          if (!busy) setShowCreate(false)
        }}
      >
        <form onSubmit={onCreate}>
          <div className="mb-1 grid items-end gap-3 sm:grid-cols-2">
            <Field
              label="权限码"
              noMargin
              className="sm:col-span-2"
              hint="小写字母开头，如 menu.reports / report.export"
            >
              <Input
                required
                className="font-mono text-sm"
                value={createForm.code}
                onChange={(e) => setCreateForm({ ...createForm, code: e.target.value })}
                placeholder="menu.reports"
              />
            </Field>
            <Field label="中文名" noMargin>
              <Input
                required
                value={createForm.name}
                onChange={(e) => setCreateForm({ ...createForm, name: e.target.value })}
              />
            </Field>
            <Field label="分组" noMargin>
              <Input
                value={createForm.group}
                onChange={(e) => setCreateForm({ ...createForm, group: e.target.value })}
              />
            </Field>
            <Field label="类型" noMargin>
              <Select
                value={createForm.kind}
                onChange={(e) => setCreateForm({ ...createForm, kind: e.target.value })}
              >
                <option value="menu">菜单</option>
                <option value="action">按钮/操作</option>
              </Select>
            </Field>
            <Field label="说明（可选）" noMargin className="sm:col-span-2">
              <Input
                value={createForm.description}
                onChange={(e) => setCreateForm({ ...createForm, description: e.target.value })}
              />
            </Field>
          </div>
          <BtnRow className="mt-3">
            <Button type="submit" variant="primary" disabled={busy}>
              创建
            </Button>
            <Button type="button" variant="ghost" disabled={busy} onClick={() => setShowCreate(false)}>
              取消
            </Button>
          </BtnRow>
        </form>
      </Modal>

      <Modal
        open={!!editing}
        title={
          <>
            编辑权限 {editing ? <Mono className="text-base">{editing}</Mono> : null}
          </>
        }
        onClose={() => {
          if (!busy) setEditing(null)
        }}
      >
        <div className="mb-1 grid items-end gap-3 sm:grid-cols-2">
          <Field label="中文名" noMargin>
            <Input
              value={editForm.name}
              onChange={(e) => setEditForm({ ...editForm, name: e.target.value })}
            />
          </Field>
          <Field label="分组" noMargin>
            <Input
              value={editForm.group}
              onChange={(e) => setEditForm({ ...editForm, group: e.target.value })}
            />
          </Field>
          <Field label="类型" noMargin>
            <Select
              value={editForm.kind}
              onChange={(e) => setEditForm({ ...editForm, kind: e.target.value })}
            >
              <option value="menu">菜单</option>
              <option value="action">按钮/操作</option>
            </Select>
          </Field>
          <Field label="说明" noMargin>
            <Input
              value={editForm.description}
              onChange={(e) => setEditForm({ ...editForm, description: e.target.value })}
            />
          </Field>
        </div>
        <BtnRow className="mt-3">
          <Button type="button" variant="primary" disabled={busy} onClick={() => void onSaveEdit()}>
            保存
          </Button>
          <Button type="button" variant="ghost" disabled={busy} onClick={() => setEditing(null)}>
            取消
          </Button>
        </BtnRow>
      </Modal>
    </div>
  )
}
