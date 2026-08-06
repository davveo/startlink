import { useEffect, useState } from 'react'
import { ApiError, api } from '../api/client'
import type { ChannelType } from '../api/types'
import { BtnRow, ButtonLink, Chip, Empty, Mono, Panel, PanelTitle, Stat, Toast } from '../components/ui'

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
      <section className="grid animate-rise gap-4 py-6 pb-6">
        <h1 className="max-w-[14ch] text-[clamp(2.4rem,5vw,3.6rem)] font-extrabold leading-[0.98] tracking-tight">
          STARLINK 推送运营台
        </h1>
        <p className="m-0 max-w-[42ch] text-[1.05rem] text-muted">
          管理模板审核、创建投放活动、追踪进度与失败重推。对接现有 /api/v1，适合本地联调与运营试跑。
        </p>
        <BtnRow className="mt-2">
          <ButtonLink to="/tasks" variant="primary">
            任务列表
          </ButtonLink>
          <ButtonLink to="/templates" variant="ink">
            模板中心
          </ButtonLink>
        </BtnRow>
      </section>

      {error ? <Toast kind="error">{error}</Toast> : null}

      <div className="grid gap-3.5 md:grid-cols-3">
        <Stat label="API 健康">{health}</Stat>
        <Stat label="已注册渠道">{channels.length}</Stat>
        <Stat label="默认队列">
          <span className="text-[1.15rem]">high / normal</span>
        </Stat>
      </div>

      <Panel className="mt-4">
        <PanelTitle>渠道清单</PanelTitle>
        {channels.length === 0 ? (
          <Empty>暂无渠道数据</Empty>
        ) : (
          <BtnRow>
            {channels.map((c) => (
              <Chip key={c} tone="teal">
                <Mono>{c}</Mono>
              </Chip>
            ))}
          </BtnRow>
        )}
      </Panel>
    </div>
  )
}
