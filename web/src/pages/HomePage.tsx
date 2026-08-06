import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { OverviewView } from '../api/types'
import { Can } from '../auth/Can'
import { Perm } from '../auth/permissions'
import { StatusChip } from '../components/StatusChip'
import {
  BtnRow,
  ButtonLink,
  Chip,
  Empty,
  Mono,
  Panel,
  PanelTitle,
  Stat,
  TableWrap,
  Td,
  Th,
  Toast,
} from '../components/ui'
import { channelLabel, taskStatusLabel } from '../lib/labels'

function formatTime(v?: string) {
  if (!v) return '-'
  try {
    return new Date(v).toLocaleString()
  } catch {
    return v
  }
}

function pct(rate: number) {
  if (!Number.isFinite(rate) || rate <= 0) return '0%'
  return `${(rate * 100).toFixed(1)}%`
}

const STATUS_ORDER = [
  'running',
  'pending',
  'paused',
  'retrying',
  'success',
  'partial',
  'failed',
  'cancelled',
  'draft',
] as const

export function HomePage() {
  const [overview, setOverview] = useState<OverviewView | null>(null)
  const [health, setHealth] = useState('checking')
  const [channels, setChannels] = useState<string[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    let alive = true
    ;(async () => {
      try {
        const [h, ch, ov] = await Promise.all([api.healthz(), api.listChannels(), api.getOverview()])
        if (!alive) return
        setHealth(h.status)
        setChannels(ch.channels ?? [])
        setOverview(ov)
      } catch (e) {
        if (!alive) return
        setHealth('down')
        setError(e instanceof ApiError ? e.message : '无法加载概览数据')
      }
    })()
    return () => {
      alive = false
    }
  }, [])

  const sends = overview?.recent_sends
  const byStatus = overview?.by_status ?? {}

  return (
    <div>
      <section className="grid animate-rise gap-3 py-2 pb-5">
        <h1 className="max-w-[16ch] text-[clamp(2.2rem,4.5vw,3.2rem)] font-extrabold leading-[0.98] tracking-tight">
          运营概览
        </h1>
        <p className="m-0 max-w-[48ch] text-[1.02rem] text-muted">
          基于真实活动与流水统计：任务分布、近 {sends?.window_hours ?? 24} 小时发送表现，以及最近活动。
        </p>
        <BtnRow className="mt-1">
          <Can perm={Perm.MenuTasks}>
            <ButtonLink to="/tasks" variant="primary">
              任务列表
            </ButtonLink>
          </Can>
          <Can perm={Perm.CampaignCreate}>
            <ButtonLink to="/campaigns" variant="ink">
              创建活动
            </ButtonLink>
          </Can>
          <Can perm={Perm.MenuNotifications}>
            <ButtonLink to="/notifications" variant="ghost">
              通知中心
            </ButtonLink>
          </Can>
        </BtnRow>
      </section>

      {error ? <Toast kind="error">{error}</Toast> : null}

      <div className="grid gap-3 md:grid-cols-4">
        <Stat label="活动总量">{overview?.campaign_total ?? '—'}</Stat>
        <Stat label="进行中">{overview?.active_count ?? '—'}</Stat>
        <Stat label={`近 ${sends?.window_hours ?? 24}h 发送`}>{sends?.total ?? '—'}</Stat>
        <Stat label="近窗成功率">{sends ? pct(sends.success_rate) : '—'}</Stat>
      </div>

      <div className="mt-3 grid gap-3 md:grid-cols-3">
        <div className="rounded-lg border border-line bg-white/60 px-4 py-3">
          <div className="text-xs text-muted">近窗成功 / 失败（流水）</div>
          <div className="mt-1 font-display text-xl font-semibold">
            <span className="text-ok">{sends?.success ?? 0}</span>
            <span className="mx-2 text-muted">/</span>
            <span className="text-rose">{sends?.failed ?? 0}</span>
          </div>
        </div>
        <div className="rounded-lg border border-line bg-white/60 px-4 py-3">
          <div className="text-xs text-muted">累计用户成功 / 失败</div>
          <div className="mt-1 font-display text-xl font-semibold">
            <span className="text-ok">{overview?.lifetime_success_users ?? 0}</span>
            <span className="mx-2 text-muted">/</span>
            <span className="text-rose">{overview?.lifetime_fail_users ?? 0}</span>
          </div>
        </div>
        <div className="rounded-lg border border-line bg-white/60 px-4 py-3">
          <div className="text-xs text-muted">实验活动 · API · 渠道</div>
          <div className="mt-1 flex flex-wrap items-baseline gap-x-3 gap-y-1 font-display text-xl font-semibold">
            <span>{overview?.experiment_tasks ?? 0}</span>
            <span className="text-sm font-medium text-muted">实验</span>
            <Mono className="text-sm text-muted">{health}</Mono>
            <span className="text-sm font-medium text-muted">{channels.length} 渠</span>
          </div>
        </div>
      </div>

      <Panel className="mt-4">
        <PanelTitle>任务状态分布</PanelTitle>
        {!overview || overview.campaign_total === 0 ? (
          <Empty>暂无活动数据，去创建第一个投放活动吧。</Empty>
        ) : (
          <div className="flex flex-wrap gap-2">
            {STATUS_ORDER.filter((k) => (byStatus[k] ?? 0) > 0).map((k) => (
              <Chip
                key={k}
                tone={
                  k === 'success'
                    ? 'ok'
                    : k === 'failed'
                      ? 'danger'
                      : k === 'partial' || k === 'paused'
                        ? 'warn'
                        : k === 'running' || k === 'retrying'
                          ? 'teal'
                          : 'muted'
                }
              >
                {taskStatusLabel(k)} · {byStatus[k]}
              </Chip>
            ))}
            {Object.entries(byStatus)
              .filter(([k]) => !(STATUS_ORDER as readonly string[]).includes(k))
              .map(([k, n]) => (
                <Chip key={k} tone="muted">
                  {taskStatusLabel(k)} · {n}
                </Chip>
              ))}
          </div>
        )}
        <p className="mt-3 mb-0 text-sm text-muted">
          终态：成功 {overview?.success_count ?? 0} · 部分成功 {overview?.partial_count ?? 0} · 失败{' '}
          {overview?.failed_count ?? 0} · 取消 {overview?.cancelled_count ?? 0}
          {overview && overview.draft_count > 0 ? ` · 草稿 ${overview.draft_count}` : null}
        </p>
      </Panel>

      <Panel className="mt-4">
        <div className="mb-4 flex flex-wrap items-end justify-between gap-2">
          <PanelTitle className="mb-0">最近活动</PanelTitle>
          <ButtonLink to="/tasks" variant="ghost" className="px-3 py-1.5 text-xs">
            查看全部
          </ButtonLink>
        </div>
        {!overview?.recent_campaigns?.length ? (
          <Empty>暂无最近活动</Empty>
        ) : (
          <TableWrap>
            <thead>
              <tr>
                <Th>活动</Th>
                <Th>状态</Th>
                <Th>渠道</Th>
                <Th>成功/失败</Th>
                <Th>时间</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {overview.recent_campaigns.map((c) => (
                <tr key={c.id} className="border-b border-line/70 last:border-0">
                  <Td>
                    <div className="font-semibold">{c.title || c.biz_id}</div>
                    <div className="mt-0.5 text-xs text-muted">
                      <Mono>#{c.id}</Mono> · {c.biz_scene}
                      {c.experiment_id ? (
                        <span className="ml-1.5 text-teal-deep">实验 {c.experiment_id}</span>
                      ) : null}
                    </div>
                  </Td>
                  <Td>
                    <StatusChip status={c.status} />
                  </Td>
                  <Td>
                    <span className="text-sm">{channelLabel(c.channel)}</span>
                  </Td>
                  <Td>
                    <Mono>
                      {c.success_count}/{c.fail_count}
                    </Mono>
                  </Td>
                  <Td>
                    <span className="text-sm text-muted">{formatTime(c.created_at)}</span>
                  </Td>
                  <Td>
                    <div className="flex flex-wrap gap-2">
                      <Link className="text-sm font-semibold text-teal-deep hover:underline" to={`/progress?task=${c.id}`}>
                        进度
                      </Link>
                      <Link className="text-sm font-semibold text-ink-soft hover:underline" to={`/ops/${c.id}`}>
                        分析
                      </Link>
                    </div>
                  </Td>
                </tr>
              ))}
            </tbody>
          </TableWrap>
        )}
      </Panel>

      {channels.length > 0 ? (
        <div className="mt-4 flex flex-wrap items-center gap-2 text-sm text-muted">
          <span>已注册渠道</span>
          {channels.map((c) => (
            <Chip key={c} tone="teal">
              <Mono>{c}</Mono>
            </Chip>
          ))}
        </div>
      ) : null}
    </div>
  )
}
