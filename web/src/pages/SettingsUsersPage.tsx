import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { ApiError, api } from '../api/client'
import { useAuth } from '../auth/AuthContext'
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
  Select,
  TableWrap,
  Td,
  Th,
  Toast,
} from '../components/ui'

type Account = {
  username: string
  display_name?: string
  role: string
  enabled: boolean
  source: string
  permission_count: number
}

type RoleInfo = { role: string; name: string }

export function SettingsUsersPage() {
  const { user, refresh } = useAuth()
  const [items, setItems] = useState<Account[]>([])
  const [roles, setRoles] = useState<RoleInfo[]>([])
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')

  const [showCreate, setShowCreate] = useState(false)
  const [createForm, setCreateForm] = useState({
    username: '',
    password: '',
    role: 'viewer',
    display_name: '',
  })
  const [editing, setEditing] = useState<string | null>(null)
  const [editForm, setEditForm] = useState({
    role: '',
    enabled: true,
    display_name: '',
  })

  const [resetUser, setResetUser] = useState<string | null>(null)
  const [resetPassword, setResetPassword] = useState('')

  const load = useCallback(async () => {
    setBusy(true)
    setErr('')
    try {
      const [u, r] = await Promise.all([api.rbacUsers(), api.rbacRoles()])
      setItems(u.items ?? [])
      setRoles((r.items ?? []).map((x) => ({ role: x.role, name: x.name })))
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载用户失败')
    } finally {
      setBusy(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const res = await api.createUser(createForm)
      setMsg(`已创建用户 ${res.username}`)
      setCreateForm({ username: '', password: '', role: 'viewer', display_name: '' })
      setShowCreate(false)
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '创建失败')
    } finally {
      setBusy(false)
    }
  }

  function startEdit(a: Account) {
    setEditing(a.username)
    setEditForm({
      role: a.role,
      enabled: a.enabled,
      display_name: a.display_name || '',
    })
  }

  async function onSaveEdit() {
    if (!editing) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      await api.updateUser(editing, {
        role: editForm.role,
        enabled: editForm.enabled,
        display_name: editForm.display_name,
      })
      setMsg(`已更新 ${editing}`)
      setEditing(null)
      await load()
      if (user?.username === editing) await refresh()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  async function onResetPassword(e: FormEvent) {
    e.preventDefault()
    if (!resetUser) return
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      await api.resetUserPassword(resetUser, resetPassword)
      setMsg(`已重置 ${resetUser} 的密码，原有登录会话已失效`)
      setResetUser(null)
      setResetPassword('')
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '重置失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <PageHead
        title="用户管理"
        description="创建与编辑运营账号。登录密码只保存 bcrypt 哈希，无法查看明文。"
        actions={
          <Button
            type="button"
            variant="primary"
            disabled={busy}
            onClick={() => {
              setCreateForm({
                username: '',
                password: '',
                role: roles[0]?.role || 'viewer',
                display_name: '',
              })
              setShowCreate(true)
            }}
          >
            创建用户
          </Button>
        }
      />
      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <PanelTitle className="mb-0">账号列表</PanelTitle>
          <Button type="button" variant="ghost" disabled={busy} onClick={() => void load()}>
            刷新
          </Button>
        </div>
        <TableWrap>
          <thead>
            <tr>
              <Th>用户名</Th>
              <Th>显示名</Th>
              <Th>角色</Th>
              <Th>状态</Th>
              <Th>权限数</Th>
              <Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {items.map((a) => (
              <tr key={a.username} className="hover:bg-white/50">
                <Td>
                  <Mono>{a.username}</Mono>
                  {user?.username === a.username ? (
                    <span className="ml-1 text-xs text-muted">（当前）</span>
                  ) : null}
                </Td>
                <Td>{a.display_name || '—'}</Td>
                <Td>
                  <Mono>{a.role}</Mono>
                </Td>
                <Td>{a.enabled ? '启用' : '禁用'}</Td>
                <Td>{a.permission_count}</Td>
                <Td>
                  <div className="flex flex-wrap gap-1">
                    <Button
                      type="button"
                      variant="ghost"
                      className="px-2 py-1 text-xs"
                      onClick={() => startEdit(a)}
                    >
                      编辑
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      className="px-2 py-1 text-xs"
                      onClick={() => {
                        setResetUser(a.username)
                        setResetPassword('')
                      }}
                    >
                      重置密码
                    </Button>
                  </div>
                </Td>
              </tr>
            ))}
          </tbody>
        </TableWrap>
        {items.length === 0 ? <Empty>暂无用户</Empty> : null}
      </Panel>

      <Modal
        open={showCreate}
        title="创建用户"
        onClose={() => {
          if (!busy) setShowCreate(false)
        }}
      >
        <form onSubmit={onCreate}>
          <div className="mb-1 grid items-end gap-3 sm:grid-cols-2">
            <Field label="用户名" noMargin>
              <Input
                required
                className="font-mono text-sm"
                value={createForm.username}
                onChange={(e) => setCreateForm({ ...createForm, username: e.target.value })}
                title="2–32 位字母数字 . _ -"
              />
            </Field>
            <Field label="密码" noMargin>
              <Input
                required
                type="password"
                value={createForm.password}
                onChange={(e) => setCreateForm({ ...createForm, password: e.target.value })}
                minLength={10}
                title="至少 10 位"
              />
            </Field>
            <Field label="显示名（可选）" noMargin>
              <Input
                value={createForm.display_name}
                onChange={(e) => setCreateForm({ ...createForm, display_name: e.target.value })}
              />
            </Field>
            <Field label="角色" noMargin>
              <Select
                value={createForm.role}
                onChange={(e) => setCreateForm({ ...createForm, role: e.target.value })}
              >
                {roles.map((r) => (
                  <option key={r.role} value={r.role}>
                    {r.name}（{r.role}）
                  </option>
                ))}
              </Select>
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
            编辑用户 {editing ? <Mono className="text-base">{editing}</Mono> : null}
          </>
        }
        onClose={() => {
          if (!busy) setEditing(null)
        }}
      >
        <div className="mb-1 grid items-end gap-3 sm:grid-cols-2">
          <Field label="角色" noMargin>
            <Select value={editForm.role} onChange={(e) => setEditForm({ ...editForm, role: e.target.value })}>
              {roles.map((r) => (
                <option key={r.role} value={r.role}>
                  {r.name}（{r.role}）
                </option>
              ))}
            </Select>
          </Field>
          <Field label="显示名" noMargin>
            <Input
              value={editForm.display_name}
              onChange={(e) => setEditForm({ ...editForm, display_name: e.target.value })}
            />
          </Field>
          <Field label="状态" noMargin>
            <Select
              value={editForm.enabled ? '1' : '0'}
              onChange={(e) => setEditForm({ ...editForm, enabled: e.target.value === '1' })}
            >
              <option value="1">启用</option>
              <option value="0">禁用</option>
            </Select>
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

      <Modal
        open={!!resetUser}
        title={
          <>
            重置密码 {resetUser ? <Mono className="text-base">{resetUser}</Mono> : null}
          </>
        }
        onClose={() => {
          if (!busy) {
            setResetUser(null)
            setResetPassword('')
          }
        }}
      >
        <form onSubmit={onResetPassword}>
          <Field label="新密码" noMargin hint="至少 10 位；重置后该用户的旧会话立即失效">
            <Input
              required
              type="password"
              minLength={10}
              value={resetPassword}
              onChange={(e) => setResetPassword(e.target.value)}
              autoFocus
            />
          </Field>
          <BtnRow className="mt-3">
            <Button type="submit" variant="primary" disabled={busy}>
              确认重置
            </Button>
            <Button
              type="button"
              variant="ghost"
              disabled={busy}
              onClick={() => {
                setResetUser(null)
                setResetPassword('')
              }}
            >
              取消
            </Button>
          </BtnRow>
        </form>
      </Modal>
    </div>
  )
}
