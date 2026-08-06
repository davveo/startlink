import type { ReactNode } from 'react'
import { useAuth } from './AuthContext'

/** 无权限时不渲染；可用于按钮 / 菜单项。 */
export function Can({
  perm,
  children,
  fallback = null,
}: {
  perm: string | string[]
  children: ReactNode
  fallback?: ReactNode
}) {
  const { can } = useAuth()
  if (!can(perm)) return <>{fallback}</>
  return <>{children}</>
}
