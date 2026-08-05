import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { ChannelType } from '../api/types'

export function HomePage() {
  const [channels, setChannels] = useState<ChannelType[]>([])
  const [health, setHealth] = useState<string>('checking')
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    ;(async () => {
      try {
        const [h, ch] = await Promise.all([api.healthz(), api.listChannels()])
        if (!alive) return
        setHealth(h.status)
        setChannels(ch.channels ?? [])
      } catch (e) {
        if (!alive) return
        setHealth('down')
        setError(e instanceof ApiError ? e.message : '无法连接 API')
      }
    })()
    return () => {
      alive = false
    }
  }, [])

  return (
    <div>
      <section className="hero">
        <h1>STARLINK 推送运营台</h1>
        <p>管理模板审核、创建投放活动、追踪进度与失败重推。对接现有 /api/v1，适合本地联调与运营试跑。</p>
        <div className="hero-actions">
          <Link className="btn btn-primary" to="/campaigns">
            创建活动
          </Link>
          <Link className="btn btn-ink" to="/tasks">
            任务列表
          </Link>
          <Link className="btn btn-ghost" to="/templates">
            模板中心
          </Link>
        </div>
      </section>

      {error ? <div className="toast toast-error">{error}</div> : null}

      <div className="grid-3">
        <div className="stat">
          <div className="label">API 健康</div>
          <div className="value">{health}</div>
        </div>
        <div className="stat">
          <div className="label">已注册渠道</div>
          <div className="value">{channels.length}</div>
        </div>
        <div className="stat">
          <div className="label">默认队列</div>
          <div className="value" style={{ fontSize: '1.15rem', marginTop: '0.55rem' }}>
            high / normal
          </div>
        </div>
      </div>

      <div className="panel" style={{ marginTop: '1rem' }}>
        <h2>渠道清单</h2>
        {channels.length === 0 ? (
          <div className="empty">暂无渠道数据</div>
        ) : (
          <div className="btn-row">
            {channels.map((c) => (
              <span key={c} className="chip chip-teal mono">
                {c}
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
