import { useCallback, useEffect, useMemo, useState } from 'react'
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
  PanelTitle,
  TableWrap,
  Td,
  Th,
  Toast,
} from '../components/ui'

type RoleInfo = {
  role: string
  name: string
  description: string
  permissions: string[]
  builtin?: boolean
}

type PermInfo = { code: string; name: string; group: string; kind: string }

const permPageSize = 200
/** 兜底上限，避免后端 total 不准时死循环 */
const permMaxPages = 25

/** 角色弹窗要能勾选全部权限码，必须翻完所有分页，不能只拿第一页 */
async function fetchAllPermissions(): Promise<PermInfo[]> {
  const out: PermInfo[] = []
  for (let page = 1; page <= permMaxPages; page += 1) {
    const res = await api.rbacPermissions({ page, page_size: permPageSize })
    const items = res.items ?? []
    out.push(...items)
    if (items.length < permPageSize || out.length >= (res.total ?? 0)) break
  }
  return out
}

export function SettingsRolesPage() {
  const [roles, setRoles] = useState<RoleInfo[]>([])
  const [perms, setPerms] = useState<PermInfo[]>([])
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')

  const [editing, setEditing] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [checked, setChecked] = useState<Record<string, boolean>>({})
  const [newRoleID, setNewRoleID] = useState('')

  const groups = useMemo(() => {
    const m = new Map<string, PermInfo[]>()
    for (const p of perms) {
      const list = m.get(p.group) || []
      list.push(p)
      m.set(p.group, list)
    }
    return [...m.entries()]
  }, [perms])

  const load = useCallback(async () => {
    setBusy(true)
    setErr('')
    try {
      const [r, p] = await Promise.all([api.rbacRoles(), fetchAllPermissions()])
      setRoles(r.items ?? [])
      setPerms(p)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载角色失败')
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  function openEdit(role: string) {
    const cur = roles.find((x) => x.role === role)
    if (!cur) return
    setCreating(false)
    setEditing(role)
    setName(cur.name)
    setDescription(cur.description)
    const next: Record<string, boolean> = {}
    for (const code of cur.permissions) next[code] = true
    setChecked(next)
  }

  function openCreate() {
    setEditing(null)
    setCreating(true)
    setNewRoleID('')
    setName('')
    setDescription('')
    setChecked({})
  }

  function closeModal() {
    if (busy) return
    setEditing(null)
    setCreating(false)
  }

  async function save() {
    if (!editing) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const permissions = Object.keys(checked).filter((k) => checked[k])
      const res = await api.updateRole(editing, { name, description, permissions })
      setMsg(res.persisted ? `已保存角色 ${editing}` : `已更新 ${editing}（${res.warning || '未持久化'}）`)
      setEditing(null)
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  async function createRole() {
    const id = newRoleID.trim()
    if (!id) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const permissions = Object.keys(checked).filter((k) => checked[k])
      const res = await api.createRole({
        role: id,
        name: name || id,
        description,
        permissions,
      })
      setMsg(res.persisted ? `已创建角色 ${id}` : `已创建 ${id}（${res.warning || '未持久化'}）`)
      setCreating(false)
      setNewRoleID('')
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '创建失败')
    } finally {
      setBusy(false)
    }
  }

  const modalOpen = creating || !!editing

  return (
    <div>
      <PageHead
        title="角色配置"
        description="查看与编辑角色权限集合；可新建自定义角色。admin 角色会强制保留系统管理权限。"
        actions={
          <Button type="button" variant="primary" disabled={busy} onClick={openCreate}>
            新建角色
          </Button>
        }
      />
      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <PanelTitle className="mb-0">角色列表</PanelTitle>
          <Button type="button" variant="ghost" disabled={busy} onClick={() => void load()}>
            刷新
          </Button>
        </div>
        <TableWrap>
          <thead>
            <tr>
              <Th>名称</Th>
              <Th>角色 ID</Th>
              <Th>类型</Th>
              <Th>权限数</Th>
              <Th>说明</Th>
              <Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {roles.map((r) => (
              <tr key={r.role} className="hover:bg-white/50">
                <Td>{r.name}</Td>
                <Td>
                  <Mono>{r.role}</Mono>
                </Td>
                <Td>{r.builtin ? '内置' : '自定义'}</Td>
                <Td>{r.permissions?.length ?? 0}</Td>
                <Td className="max-w-[16rem] truncate text-muted" title={r.description || undefined}>
                  {r.description || '—'}
                </Td>
                <Td>
                  <Button
                    type="button"
                    variant="ghost"
                    className="px-3 py-1 text-xs"
                    onClick={() => openEdit(r.role)}
                  >
                    编辑
                  </Button>
                </Td>
              </tr>
            ))}
          </tbody>
        </TableWrap>
        {roles.length === 0 ? <Empty>暂无角色</Empty> : null}
      </Panel>

      <Modal
        open={modalOpen}
        wide
        title={
          creating ? (
            '新建角色'
          ) : (
            <>
              编辑角色 {editing ? <Mono className="text-base">{editing}</Mono> : null}
            </>
          )
        }
        onClose={closeModal}
      >
        <div className="mb-3 grid items-end gap-3 sm:grid-cols-2">
          {creating ? (
            <Field
              label="角色 ID"
              noMargin
              className="sm:col-span-2"
              hint="小写字母开头，字母数字下划线"
            >
              <Input
                value={newRoleID}
                onChange={(e) => setNewRoleID(e.target.value)}
                placeholder="ops_lead"
                className="font-mono text-sm"
              />
            </Field>
          ) : null}
          <Field label="显示名称" noMargin className={creating ? undefined : 'sm:col-span-2'}>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="说明" noMargin className={creating ? undefined : 'sm:col-span-2'}>
            <Input value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
        </div>
        <div className="mb-4 grid max-h-[min(28rem,50vh)] gap-3 overflow-auto pr-1">
          {groups.map(([group, list]) => (
            <div key={group} className="rounded-lg border border-line bg-white/50 px-3 py-2">
              <div className="mb-2 text-sm font-semibold">{group}</div>
              <div className="grid gap-1.5 sm:grid-cols-2">
                {list.map((p) => (
                  <label key={p.code} className="flex cursor-pointer items-start gap-2 text-sm">
                    <input
                      type="checkbox"
                      className="mt-1"
                      checked={!!checked[p.code]}
                      onChange={(e) => setChecked((c) => ({ ...c, [p.code]: e.target.checked }))}
                    />
                    <span>
                      <span className="text-ink-soft">{p.name}</span>
                      <br />
                      <Mono className="text-[11px] text-muted">{p.code}</Mono>
                    </span>
                  </label>
                ))}
              </div>
            </div>
          ))}
          {groups.length === 0 ? <Empty>暂无权限可勾选</Empty> : null}
        </div>
        <BtnRow>
          {creating ? (
            <Button
              type="button"
              variant="primary"
              disabled={busy || !newRoleID.trim()}
              onClick={() => void createRole()}
            >
              创建角色
            </Button>
          ) : (
            <Button type="button" variant="primary" disabled={busy} onClick={() => void save()}>
              保存
            </Button>
          )}
          <Button type="button" variant="ghost" disabled={busy} onClick={closeModal}>
            取消
          </Button>
        </BtnRow>
      </Modal>
    </div>
  )
}
