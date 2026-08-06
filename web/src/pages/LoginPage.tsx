import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { Button, Field, Input, Toast } from '../components/ui'

export function LoginPage() {
  const { user, loading, login } = useAuth()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const next = params.get('next') || '/'

  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  if (!loading && user) {
    return <Navigate to={next.startsWith('/') ? next : '/'} replace />
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await login(username.trim(), password)
      navigate(next.startsWith('/') ? next : '/', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '登录失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid min-h-screen place-items-center px-4 py-10">
      <div className="w-full max-w-md animate-rise">
        <div className="mb-8 text-center">
          <div className="font-display text-[2rem] font-extrabold tracking-wide text-ink">
            STAR<span className="text-teal">LINK</span>
          </div>
          <p className="mt-2 text-muted">推送运营台登录</p>
        </div>

        <form
          onSubmit={onSubmit}
          className="rounded-[10px] border border-line/90 bg-white/72 p-6 shadow-panel backdrop-blur-sm"
        >
          {error ? (
            <div className="mb-4">
              <Toast kind="error">{error}</Toast>
            </div>
          ) : null}

          <Field label="用户名">
            <Input
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="admin"
              required
            />
          </Field>
          <Field label="密码">
            <Input
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
              required
            />
          </Field>

          <Button type="submit" variant="primary" className="mt-2 w-full" disabled={busy || loading}>
            {busy ? '登录中…' : '登录'}
          </Button>
          <p className="mt-3 text-center text-[11px] leading-snug text-muted">
            示例：admin（全权限）/ operator（运营）/ viewer（只读）
          </p>
        </form>
      </div>
    </div>
  )
}
