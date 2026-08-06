import { useCallback, useEffect, useMemo, useState } from 'react'
import { ApiError, api } from '../api/client'
import {
  BtnRow,
  Button,
  Empty,
  Field,
  Input,
  Mono,
  PageHead,
  Panel,
  PanelTitle,
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

export function SettingsRolesPage() {
  const [roles, setRoles] = useState<RoleInfo[]>([])
  const [perms, setPerms] = useState<PermInfo[]>([])
  const [selected, setSelected] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [checked, setChecked] = useState<Record<string, boolean>>({})
  const [newRoleID, setNewRoleID] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')

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
      const [r, p] = await Promise.all([
        api.rbacRoles(),
        api.rbacPermissions({ page: 1, page_size: 200 }),
      ])
      const items = r.items ?? []
      setRoles(items)
      setPerms(p.items ?? [])
      const pick = selected && items.some((x) => x.role === selected) ? selected : items[0]?.role || ''
      setSelected(pick)
      const cur = items.find((x) => x.role === pick)
      if (cur) {
        setName(cur.name)
        setDescription(cur.description)
        const next: Record<string, boolean> = {}
        for (const code of cur.permissions) next[code] = true
        setChecked(next)
      }
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载角色失败')
    } finally {
      setBusy(false)
    }
  }, [selected])

  useEffect(() => {
    void load()
  }, []) // 首屏加载；切角色用本地 state

  function selectRole(role: string) {
    const cur = roles.find((x) => x.role === role)
    setSelected(role)
    if (!cur) return
    setName(cur.name)
    setDescription(cur.description)
    const next: Record<string, boolean> = {}
    for (const code of cur.permissions) next[code] = true
    setChecked(next)
  }

  async function save() {
    if (!selected) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const permissions = Object.keys(checked).filter((k) => checked[k])
      const res = await api.updateRole(selected, { name, description, permissions })
      setMsg(res.persisted ? `已保存角色 ${selected}` : `已更新 ${selected}（${res.warning || '未持久化'}）`)
      const r = await api.rbacRoles()
      setRoles(r.items ?? [])
      if (res.role) {
        setName(res.role.name)
        setDescription(res.role.description)
        const next: Record<string, boolean> = {}
        for (const code of res.role.permissions) next[code] = true
        setChecked(next)
      }
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
      setNewRoleID('')
      const r = await api.rbacRoles()
      setRoles(r.items ?? [])
      setSelected(id)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '创建失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <PageHead
        title="角色配置"
        description="查看与编辑角色权限集合；可基于勾选新建自定义角色。admin 角色会强制保留系统管理权限。"
      />
      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <div className="grid gap-4 lg:grid-cols-[220px_1fr]">
        <Panel>
          <PanelTitle>角色列表</PanelTitle>
          {roles.length === 0 ? <Empty>暂无角色</Empty> : null}
          <ul className="m-0 flex list-none flex-col gap-1 p-0">
            {roles.map((r) => (
              <li key={r.role}>
                <button
                  type="button"
                  className={`w-full rounded-lg px-3 py-2 text-left text-sm transition ${
                    selected === r.role ? 'bg-teal/15 font-semibold text-ink' : 'text-ink-soft hover:bg-white/60'
                  }`}
                  onClick={() => selectRole(r.role)}
                >
                  {r.name}
                  <div className="text-xs font-normal text-muted">
                    <Mono>{r.role}</Mono>
                    {r.builtin ? ' · 内置' : ' · 自定义'}
                  </div>
                </button>
              </li>
            ))}
          </ul>
        </Panel>

        <Panel>
          <PanelTitle>权限勾选 {selected ? <Mono className="text-sm font-normal">· {selected}</Mono> : null}</PanelTitle>
          {!selected ? (
            <Empty>请选择角色</Empty>
          ) : (
            <>
              <div className="mb-3 grid gap-3 md:grid-cols-2">
                <Field label="显示名称" noMargin>
                  <Input value={name} onChange={(e) => setName(e.target.value)} />
                </Field>
                <Field label="说明" noMargin>
                  <Input value={description} onChange={(e) => setDescription(e.target.value)} />
                </Field>
              </div>
              <div className="mb-4 grid max-h-[28rem] gap-3 overflow-auto pr-1">
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
              </div>
              <BtnRow>
                <Button type="button" variant="primary" disabled={busy} onClick={() => void save()}>
                  保存当前角色
                </Button>
              </BtnRow>
              <div className="mt-5 border-t border-line pt-4">
                <PanelTitle className="mb-2">另存为自定义角色</PanelTitle>
                <div className="flex flex-wrap items-end gap-3">
                  <Field label="角色 ID" noMargin className="min-w-[10rem]">
                    <Input
                      value={newRoleID}
                      onChange={(e) => setNewRoleID(e.target.value)}
                      placeholder="ops_lead"
                      className="font-mono text-sm"
                    />
                  </Field>
                  <Button type="button" variant="ink" disabled={busy || !newRoleID.trim()} onClick={() => void createRole()}>
                    创建角色
                  </Button>
                </div>
                <p className="mt-2 text-xs text-muted">ID 需小写字母开头，字母数字下划线；将使用上方勾选的权限集合。</p>
              </div>
            </>
          )}
        </Panel>
      </div>
    </div>
  )
}
