import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { ChannelMode, ChannelRouteRule, ChannelType, Priority, Template } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import {
  BtnRow,
  Button,
  ButtonLink,
  Field,
  Input,
  PageHead,
  Panel,
  PanelTitle,
  Select,
  Textarea,
  Toast,
} from '../components/ui'
import { channelLabel } from '../lib/channels'
import { BIZ_SCENE_OPTIONS } from '../lib/labels'

const DEFAULT_ROUTES_JSON = `[
  {"when":{"var":"vip","op":"eq","value":"true"},"channels":["sms","app_push"]},
  {"channels":["inbox"]}
]`

const DEFAULT_COSTS_JSON = `{
  "inbox": 1,
  "app_push": 2,
  "email": 3,
  "sms": 10
}`

function isStrategyMode(mode: ChannelMode) {
  return mode === 'conditional' || mode === 'cost_priority'
}

export function CampaignsPage() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const [templates, setTemplates] = useState<Template[]>([])
  const [channels, setChannels] = useState<ChannelType[]>([])
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')

  const [form, setForm] = useState({
    biz_id: `camp-${Date.now()}`,
    biz_scene: 'demo',
    title: 'Demo 投放',
    template_id: '',
    audience_ref: 'demo',
    audience_total: '20',
    channel: 'inbox' as ChannelType,
    extraChannels: [] as ChannelType[],
    channel_mode: 'single' as ChannelMode,
    priority: 'normal' as Priority,
    pace_qps: '',
    expire_at: '',
    max_fallback: '',
    channel_routes_json: DEFAULT_ROUTES_JSON,
    channel_costs_json: DEFAULT_COSTS_JSON,
    experiment_id: '',
    experiment_salt: '',
    experiment_control_percent: '',
    created_by: '',
    as_draft: false,
  })
  const [preflightText, setPreflightText] = useState('')
  const [dryRunText, setDryRunText] = useState('')
  const [estimateText, setEstimateText] = useState('')

  useEffect(() => {
    if (user?.username) {
      setForm((f) => (f.created_by ? f : { ...f, created_by: user.username }))
    }
  }, [user?.username])

  useEffect(() => {
    void (async () => {
      try {
        const [tpl, ch] = await Promise.all([
          api.listTemplates({ status: 'approved', page_size: 100 }),
          api.listChannels(),
        ])
        const list = ch.channels ?? []
        setTemplates(tpl.items ?? [])
        setChannels(list)
        setForm((f) => ({
          ...f,
          template_id: f.template_id || tpl.items?.[0]?.code || '',
          channel: list.includes(f.channel) ? f.channel : list[0] || f.channel,
        }))
      } catch (e) {
        setErr(e instanceof ApiError ? e.message : '初始化失败')
      }
    })()
  }, [])

  const channelOptions = useMemo(() => channels, [channels])
  const strategyMode = isStrategyMode(form.channel_mode)

  function toggleExtraChannel(c: ChannelType) {
    setForm((f) => {
      const has = f.extraChannels.includes(c)
      const next = has ? f.extraChannels.filter((x) => x !== c) : [...f.extraChannels, c]
      let channel_mode = f.channel_mode
      if (!isStrategyMode(channel_mode)) {
        channel_mode =
          next.length > 1 && channel_mode === 'single'
            ? 'fallback'
            : next.length <= 1
              ? 'single'
              : channel_mode
      }
      return { ...f, extraChannels: next, channel_mode }
    })
  }

  function buildChannelPayload() {
    const multi = form.extraChannels
    const mode = form.channel_mode
    let channel_routes: ChannelRouteRule[] | undefined
    let channel_costs: Partial<Record<ChannelType, number>> | undefined

    if (mode === 'conditional') {
      try {
        channel_routes = JSON.parse(form.channel_routes_json) as ChannelRouteRule[]
        if (!Array.isArray(channel_routes)) throw new Error('channel_routes 须为数组')
      } catch (e) {
        throw new Error(e instanceof Error ? `channel_routes JSON 无效: ${e.message}` : 'channel_routes JSON 无效')
      }
    }
    if (mode === 'cost_priority') {
      try {
        channel_costs = JSON.parse(form.channel_costs_json) as Partial<Record<ChannelType, number>>
        if (!channel_costs || typeof channel_costs !== 'object' || Array.isArray(channel_costs)) {
          throw new Error('channel_costs 须为对象')
        }
      } catch (e) {
        throw new Error(e instanceof Error ? `channel_costs JSON 无效: ${e.message}` : 'channel_costs JSON 无效')
      }
    }

    const useMulti = multi.length > 0 || strategyMode
    let channel_mode: ChannelMode = mode
    if (!strategyMode) {
      channel_mode = multi.length > 1 ? mode || 'fallback' : 'single'
    }

    return {
      channel: useMulti && multi.length ? undefined : form.channel,
      channels: useMulti && multi.length ? multi : useMulti ? [form.channel] : undefined,
      channel_mode,
      channel_routes,
      channel_costs,
    }
  }

  async function onCreate(e: FormEvent) {
    e.preventDefault()

    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const ch = buildChannelPayload()
      const res = await api.createCampaign({
        biz_id: form.biz_id,
        biz_scene: form.biz_scene,
        title: form.title,
        template_id: form.template_id,
        audience_ref: form.audience_ref,
        ...ch,
        priority: form.priority || 'normal',
        audience_extra: {
          total: Number(form.audience_total) || 20,
        },
        pace_qps: form.pace_qps ? Number(form.pace_qps) : undefined,
        expire_at: form.expire_at ? new Date(form.expire_at).toISOString() : undefined,
        max_fallback: form.max_fallback ? Number(form.max_fallback) : undefined,
        experiment_id: form.experiment_id || undefined,
        experiment_salt: form.experiment_salt || undefined,
        experiment_control_percent: form.experiment_control_percent
          ? Number(form.experiment_control_percent)
          : undefined,
        created_by: form.created_by || undefined,
        as_draft: form.as_draft,
      })
      setMsg(`活动已创建 task_id=${res.task_id} status=${res.status}`)
      setForm((f) => ({ ...f, biz_id: `camp-${Date.now()}` }))
      if (res.status === 'draft') {
        navigate('/tasks?status=draft')
      } else {
        navigate(`/progress?task=${res.task_id}`)
      }
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : e instanceof Error ? e.message : '创建失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <PageHead
        title="创建活动"
        description="创建营销/事务活动；创建成功后可跳转进度页查看执行情况。"
      />

      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel className="max-w-2xl">
        <form onSubmit={onCreate}>
          <PanelTitle>活动参数</PanelTitle>
          <Field
            label="业务幂等键"
            hint="全局唯一。相同键重复提交会返回已有活动，避免误建重复投放。"
          >
            <Input
              required
              className="font-mono text-sm"
              value={form.biz_id}
              onChange={(e) => setForm({ ...form, biz_id: e.target.value })}
              placeholder="camp-20260806-welcome"
            />
          </Field>
          <Field label="活动标题" hint="运营侧展示用名称；当前不会作为推送文案发给用户。">
            <Input
              required
              value={form.title}
              onChange={(e) => setForm({ ...form, title: e.target.value })}
              placeholder="新用户欢迎投放"
            />
          </Field>
          <Field
            label="业务场景"
            hint={
              BIZ_SCENE_OPTIONS.find((o) => o.value === form.biz_scene)?.hint ||
              '决定人群 Provider 路由，并可能映射到高优队列。也可手动输入自定义场景。'
            }
          >
            <Select
              required
              value={
                BIZ_SCENE_OPTIONS.some((o) => o.value === form.biz_scene)
                  ? form.biz_scene
                  : '__custom__'
              }
              onChange={(e) => {
                const v = e.target.value
                if (v === '__custom__') {
                  setForm({ ...form, biz_scene: '' })
                  return
                }
                setForm({ ...form, biz_scene: v })
              }}
            >
              {BIZ_SCENE_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
              <option value="__custom__">自定义…</option>
            </Select>
            {!BIZ_SCENE_OPTIONS.some((o) => o.value === form.biz_scene) ? (
              <Input
                className="mt-2"
                required
                value={form.biz_scene}
                onChange={(e) => setForm({ ...form, biz_scene: e.target.value })}
                placeholder="输入自定义场景"
              />
            ) : null}
          </Field>
          <Field
            label="模板编码"
            hint={
              templates.length === 0
                ? '暂无已通过模板，请先到模板中心创建并审核通过。'
                : '必须选择已审核通过的模板；实际推送内容取该模板快照。'
            }
          >
            <Select
              required
              value={form.template_id}
              onChange={(e) => setForm({ ...form, template_id: e.target.value })}
            >
              <option value="" disabled>
                选择模板
              </option>
              {templates.map((t) => (
                <option key={t.id} value={t.code}>
                  {t.code} · {t.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field
            label="人群引用"
            hint="传给人群服务的人群标识。Demo 源下任意非空字符串即可，常与场景配合使用。"
          >
            <Input
              required
              value={form.audience_ref}
              onChange={(e) => setForm({ ...form, audience_ref: e.target.value })}
              placeholder="demo"
            />
          </Field>
          <Field
            label="Demo 人群总量"
            hint="仅内置 Demo 人群生效：生成多少虚拟用户。生产人群源通常忽略此字段，改用真实圈选结果。"
          >
            <Input
              type="number"
              min={1}
              value={form.audience_total}
              onChange={(e) => setForm({ ...form, audience_total: e.target.value })}
            />
          </Field>
          <Field
            label="主渠道"
            hint="单渠道投放时使用。若下方勾选了多渠道，则以多渠道列表为准。"
          >
            <Select
              value={form.channel}
              onChange={(e) => setForm({ ...form, channel: e.target.value as ChannelType })}
              disabled={form.extraChannels.length > 0}
            >
              {channelOptions.map((c) => (
                <option key={c} value={c}>
                  {channelLabel(c)}
                </option>
              ))}
            </Select>
          </Field>
          <Field
            label="多渠道列表"
            hint="可多选。勾选后优先于「主渠道」；条件路由/成本优先建议勾选候选渠道作兜底链。"
          >
            <div className="flex flex-wrap gap-2 rounded-lg border border-line bg-white px-3 py-2.5">
              {channelOptions.length === 0 ? (
                <span className="text-sm text-muted">暂无已注册渠道</span>
              ) : (
                channelOptions.map((c) => {
                  const checked = form.extraChannels.includes(c)
                  return (
                    <label
                      key={c}
                      className={`inline-flex cursor-pointer items-center gap-1.5 rounded-full border px-3 py-1.5 text-sm transition ${
                        checked
                          ? 'border-teal bg-teal/10 text-teal-deep'
                          : 'border-line text-ink-soft hover:border-teal/40'
                      }`}
                    >
                      <input
                        type="checkbox"
                        className="sr-only"
                        checked={checked}
                        onChange={() => toggleExtraChannel(c)}
                      />
                      {channelLabel(c)}
                    </label>
                  )
                })
              )}
            </div>
            {form.extraChannels.length > 0 ? (
              <p className="mt-1.5 text-xs text-muted">
                已选：{form.extraChannels.map(channelLabel).join('、')}
              </p>
            ) : null}
          </Field>
          <Field
            label="渠道模式"
            hint="降级/并行需多渠道；条件路由按用户变量选渠；成本优先按成本升序降级。"
          >
            <Select
              value={form.channel_mode}
              onChange={(e) => setForm({ ...form, channel_mode: e.target.value as ChannelMode })}
            >
              <option value="single" disabled={form.extraChannels.length > 1}>
                单渠道
              </option>
              <option value="fallback" disabled={form.extraChannels.length <= 1}>
                降级
              </option>
              <option value="parallel" disabled={form.extraChannels.length <= 1}>
                并行（任一成功）
              </option>
              <option value="all_success" disabled={form.extraChannels.length <= 1}>
                全成功（并行须全成）
              </option>
              <option value="conditional">条件路由</option>
              <option value="cost_priority">成本优先</option>
            </Select>
          </Field>
          {form.channel_mode === 'conditional' ? (
            <Field
              label="条件路由规则"
              hint="JSON 数组。按序匹配 when；无 when 为默认兜底。op: eq|ne|in|gt|gte|lt|lte|exists|not_exists。"
            >
              <Textarea
                className="min-h-36 font-mono text-xs"
                value={form.channel_routes_json}
                onChange={(e) => setForm({ ...form, channel_routes_json: e.target.value })}
              />
            </Field>
          ) : null}
          {form.channel_mode === 'cost_priority' ? (
            <Field
              label="渠道成本"
              hint="JSON 对象，数值越低越优先。未填写的渠道用系统默认成本。"
            >
              <Textarea
                className="min-h-28 font-mono text-xs"
                value={form.channel_costs_json}
                onChange={(e) => setForm({ ...form, channel_costs_json: e.target.value })}
              />
            </Field>
          ) : null}
          <Field
            label="最大降级次数"
            hint="仅降级 / 成本优先 / 条件路由多渠降级。不含首渠；0 或不填表示不限制。"
          >
            <Input
              type="number"
              min={0}
              value={form.max_fallback}
              onChange={(e) => setForm({ ...form, max_fallback: e.target.value })}
              placeholder="0 = 不限制"
            />
          </Field>
          <Field label="过期时间" hint="可选。超时消息标记为已过期，不再调渠道。">
            <Input
              type="datetime-local"
              value={form.expire_at}
              onChange={(e) => setForm({ ...form, expire_at: e.target.value })}
            />
          </Field>
          <Field label="实验 ID" hint="可选。启用后按对照组比例跳过对照组用户；分析页可看分组指标。">
            <Input
              value={form.experiment_id}
              onChange={(e) => setForm({ ...form, experiment_id: e.target.value })}
              placeholder="exp_welcome_v1"
            />
          </Field>
          <Field label="实验盐" hint="抽样哈希盐；空则仅用用户 ID。">
            <Input
              value={form.experiment_salt}
              onChange={(e) => setForm({ ...form, experiment_salt: e.target.value })}
            />
          </Field>
          <Field label="对照组比例 %" hint="0–100；落入对照组的用户不发送。">
            <Input
              type="number"
              min={0}
              max={100}
              value={form.experiment_control_percent}
              onChange={(e) => setForm({ ...form, experiment_control_percent: e.target.value })}
              placeholder="0"
            />
          </Field>
          <Field
            label="优先级"
            hint="高优适合事务/验证码；普通为营销队列。不选时也可能按业务场景自动映射。"
          >
            <Select
              value={form.priority}
              onChange={(e) => setForm({ ...form, priority: e.target.value as Priority })}
            >
              <option value="normal">普通</option>
              <option value="high">高优</option>
            </Select>
          </Field>
          <Field
            label="入队节奏"
            hint="可选。限制本活动向消息队列入队的大致 QPS，用于削峰；留空则用系统默认节奏。"
          >
            <Input
              type="number"
              min={0}
              value={form.pace_qps}
              onChange={(e) => setForm({ ...form, pace_qps: e.target.value })}
              placeholder="例如 50"
            />
          </Field>
          <Field
            label="创建人"
            hint="审计字段，写入活动记录，便于列表按负责人筛选。默认取当前登录用户。"
          >
            <Input
              value={form.created_by}
              onChange={(e) => setForm({ ...form, created_by: e.target.value })}
            />
          </Field>
          <label className="mb-3 grid gap-1.5 text-sm">
            <span className="flex items-center gap-2 font-medium">
              <input
                type="checkbox"
                checked={form.as_draft}
                onChange={(e) => setForm({ ...form, as_draft: e.target.checked })}
              />
              保存为草稿
            </span>
            <small className="text-muted">
              勾选后状态为 draft，不进入调度与配额准入；可在任务列表稍后发布。
            </small>
          </label>
          <BtnRow className="mb-3">
            <Button
              variant="ghost"
              type="button"
              disabled={busy}
              title="估算目标人数与命中情况（原始→过滤→AB→可达），不创建活动、不发送。可能未扫完全部人群。"
              onClick={() => {
                void (async () => {
                  setBusy(true)
                  setErr('')
                  try {
                    const multi = form.extraChannels
                    const est = await api.estimateAudience({
                      biz_scene: form.biz_scene,
                      audience_ref: form.audience_ref,
                      channel: multi.length ? undefined : form.channel,
                      channels: multi.length ? multi : undefined,
                      audience_extra: { total: Number(form.audience_total) || 20 },
                      sample_limit: 10,
                    })
                    setEstimateText(JSON.stringify(est, null, 2))
                    setMsg('人群试算完成')
                  } catch (e) {
                    setErr(e instanceof ApiError ? e.message : '试算失败')
                  } finally {
                    setBusy(false)
                  }
                })()
              }}
            >
              人群试算
            </Button>
            <Button
              variant="ghost"
              type="button"
              disabled={busy || !form.template_id}
              title="检查模板可用性、变量 Schema、内嵌人群试算与容量/费用提示，不创建活动、不发送。"
              onClick={() => {
                void (async () => {
                  setBusy(true)
                  setErr('')
                  try {
                    const ch = buildChannelPayload()
                    const pf = await api.preflight({
                      biz_id: form.biz_id,
                      biz_scene: form.biz_scene,
                      title: form.title,
                      template_id: form.template_id,
                      audience_ref: form.audience_ref,
                      ...ch,
                      audience_extra: { total: Number(form.audience_total) || 20 },
                    })
                    setPreflightText(JSON.stringify(pf, null, 2))
                    setMsg('预检完成')
                  } catch (e) {
                    setErr(e instanceof ApiError ? e.message : e instanceof Error ? e.message : '预检失败')
                  } finally {
                    setBusy(false)
                  }
                })()
              }}
            >
              预检
            </Button>
            <Button
              variant="ghost"
              type="button"
              disabled={busy || !form.template_id}
              title="按样本变量渲染最终标题/正文（含分渠内容），不调渠道、不写流水。"
              onClick={() => {
                void (async () => {
                  setBusy(true)
                  try {
                    const multi = form.extraChannels
                    const dr = await api.dryRun({
                      template_id: form.template_id,
                      title: form.title,
                      channel: multi.length ? undefined : form.channel,
                      channels: multi.length ? multi : undefined,
                      vars: { name: 'Starlink' },
                      send: false,
                    })
                    setDryRunText(JSON.stringify(dr, null, 2))
                    setMsg('Dry-run 完成')
                  } catch (e) {
                    setErr(e instanceof ApiError ? e.message : 'dry-run 失败')
                  } finally {
                    setBusy(false)
                  }
                })()
              }}
            >
              Dry-run 渲染
            </Button>
            <Button
              variant="ghost"
              type="button"
              disabled={busy || !form.template_id}
              title="向测试用户真实调用渠道发一条，写测试流水（不计入活动统计），用于验证通路。"
              onClick={() => {
                void (async () => {
                  setBusy(true)
                  try {
                    const multi = form.extraChannels
                    const dr = await api.dryRun({
                      template_id: form.template_id,
                      title: form.title,
                      channel: multi.length ? undefined : form.channel,
                      channels: multi.length ? multi : undefined,
                      user_id: 'test_user_console',
                      vars: { name: 'Starlink' },
                      send: true,
                    })
                    setDryRunText(JSON.stringify(dr, null, 2))
                    setMsg(dr.sent ? '测试发送已发出（测试流水）' : '测试发送未成功')
                  } catch (e) {
                    setErr(e instanceof ApiError ? e.message : '测试发送失败')
                  } finally {
                    setBusy(false)
                  }
                })()
              }}
            >
              测试发送
            </Button>
          </BtnRow>
          <p className="mb-3 text-xs text-muted">
            悬停按钮可看说明：试算/预检/渲染均不真正投放；测试发送会真实调渠道并记测试流水。
          </p>
          {estimateText ? (
            <pre className="mb-3 max-h-40 overflow-auto rounded-lg bg-paper-deep p-3 text-xs">{estimateText}</pre>
          ) : null}
          {preflightText ? (
            <pre className="mb-3 max-h-40 overflow-auto rounded-lg bg-paper-deep p-3 text-xs">{preflightText}</pre>
          ) : null}
          {dryRunText ? (
            <pre className="mb-3 max-h-40 overflow-auto rounded-lg bg-paper-deep p-3 text-xs">{dryRunText}</pre>
          ) : null}
          <BtnRow>
            <Button variant="primary" type="submit" disabled={busy || !form.template_id}>
              {form.as_draft ? '保存草稿' : '创建并开始拆分'}
            </Button>
            <ButtonLink to="/progress" variant="ghost">
              去查进度
            </ButtonLink>
          </BtnRow>
        </form>
      </Panel>
    </div>
  )
}
