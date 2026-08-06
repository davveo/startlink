import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { ApiError, api } from '../api/client'

export type AuthUser = {
  username: string
  role: string
  roles: string[]
  permissions: string[]
}

type AuthContextValue = {
  user: AuthUser | null
  loading: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
  can: (perm: string | string[]) => boolean
}

const AuthContext = createContext<AuthContextValue | null>(null)

function normalizeUser(raw: {
  username: string
  role?: string
  roles?: string[]
  permissions?: string[]
}): AuthUser {
  const role = raw.role || raw.roles?.[0] || 'viewer'
  const roles = raw.roles?.length ? raw.roles : [role]
  return {
    username: raw.username,
    role,
    roles,
    permissions: raw.permissions ?? [],
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      const me = await api.me()
      setUser(normalizeUser(me))
    } catch (e) {
      if (e instanceof ApiError && (e.code === 40101 || e.code === 401)) {
        setUser(null)
      } else {
        setUser(null)
      }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const login = useCallback(async (username: string, password: string) => {
    const res = await api.login(username, password)
    setUser(normalizeUser(res))
  }, [])

  const logout = useCallback(async () => {
    try {
      await api.logout()
    } catch {
      // 会话已过期时仍清本地状态
    }
    setUser(null)
  }, [])

  const can = useCallback(
    (perm: string | string[]) => {
      if (!user) return false
      const need = Array.isArray(perm) ? perm : [perm]
      if (need.length === 0) return true
      const set = new Set(user.permissions)
      if (set.has('*')) return true
      return need.every((p) => set.has(p))
    },
    [user],
  )

  const value = useMemo(
    () => ({ user, loading, login, logout, refresh, can }),
    [user, loading, login, logout, refresh, can],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return ctx
}

/** 是否拥有指定权限（单个或全部）。 */
export function usePermission(perm: string | string[]) {
  const { can } = useAuth()
  return can(perm)
}
