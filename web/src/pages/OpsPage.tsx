import { useCallback, useEffect, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { ExperimentMetrics, FailureAgg, FunnelView } from '../api/types'
import {
  BtnRow,
  Button,
  ButtonLink,
  Empty,
  Field,
  Input,
  Mono,
  PageHead,
  Panel,
  PanelTitle,
  Stat,
  TableWrap,
  Td,
  Th,
  Toast,
} from '../components/ui'

function pct(rate: number) {
  if (!Number.isFinite(rate)) return '-'
  return `${(rate * 100).toFixed(1)}%`
}

export function OpsPage() {
  const { id: idParam } = useParams()
  const [search] = useSearchParams()
  const initialId = idParam || search.get('task') || ''
  const [taskId, setTaskId] = useState(initialId)
  const [funnel, setFunnel] = useState<FunnelView | null>(null)
  const [failures, setFailures] = useState<FailureAgg[]>([])
  const [experiment, setExperiment] = useState<ExperimentMetrics | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    const id = Number(taskId)
    if (!Number.isFinite(id) || id <= 0) return
    setBusy(true)
    setErr('')
    try {
      const [f, fail, exp] = await Promise.all([
        api.getFunnel(id),
        api.getFailures(id),
        api.getExperimentMetrics(id),
      ])
      setFunnel(f)
      setFailures(fail.items ?? [])
      setExperiment(exp)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载失败')
    } finally {
      setBusy(false)
    }
  }, [taskId])

  useEffect(() => {
    if (Number(taskId) > 0) void load()
  }, [load, taskId])

  const p = funnel?.pipeline
  const taskNum = Number(taskId)
  const groups = experiment?.groups ?? []

  return (
    <div>
      <PageHead
        title="投递分析"
        description="漏斗、失败归因与实验分组指标。用户级流水请到独立列表查看。"
        actions={
          <BtnRow>
            {taskNum > 0 ? (
              <ButtonLink to={`/ops/${taskNum}/records`} variant="primary">
                用户流水
              </ButtonLink>
            ) : null}
            <ButtonLink to="/tasks" variant="ghost">
              返回任务
            </ButtonLink>
          </BtnRow>
        }
      />
      {err ? <Toast kind="error">{err}</Toast> : null}

      <Panel>
        <div className="grid gap-3 md:grid-cols-3">
          <Field label="主任务 ID" hint="从任务列表点「分析」会自动带入。">
            <Input value={taskId} onChange={(e) => setTaskId(e.target.value)} placeholder="task id" />
          </Field>
          <div className="flex items-end gap-2 pb-3.5">
            <Button variant="ink" type="button" disabled={busy} onClick={() => void load()}>
              加载分析
            </Button>
            {taskNum > 0 ? (
              <ButtonLink to={`/ops/${taskNum}/records`} variant="ghost">
                查看流水
              </ButtonLink>
            ) : null}
          </div>
        </div>
      </Panel>

      {funnel ? (
        <>
          <div className="mt-4 grid gap-3 md:grid-cols-4">
            <Stat label="原始人群(落库)">{funnel.audience_raw_count}</Stat>
            <Stat label="过滤后">{funnel.audience_filtered_count}</Stat>
            <Stat label="可达">{funnel.audience_reachable_count}</Stat>
            <Stat label="入队流水">{funnel.enqueued_users}</Stat>
          </div>
          <Panel className="mt-4">
            <PanelTitle>投递漏斗（流水状态）</PanelTitle>
            <div className="grid gap-3 md:grid-cols-4">
              <Stat label="已入队">{p?.queued ?? 0}</Stat>
              <Stat label="发送中">{p?.sending ?? 0}</Stat>
              <Stat label="已发送">{p?.sent ?? 0}</Stat>
              <Stat label="已送达">{p?.delivered ?? 0}</Stat>
              <Stat label="已点击">{p?.clicked ?? 0}</Stat>
              <Stat label="失败">{p?.failed ?? 0}</Stat>
              <Stat label="已抑制">{p?.suppressed ?? 0}</Stat>
              <Stat label="不可达">{p?.unreachable ?? 0}</Stat>
            </div>
          </Panel>
        </>
      ) : null}

      <Panel className="mt-4">
        <PanelTitle>
          实验指标看板
          {experiment?.experiment_id ? (
            <span className="ml-2 text-sm font-normal text-muted">
              实验 <Mono>{experiment.experiment_id}</Mono>
            </span>
          ) : null}
        </PanelTitle>
        <TableWrap>
          <thead>
            <tr>
              <Th>分组</Th>
              <Th>分配用户</Th>
              <Th>有流水</Th>
              <Th>成功</Th>
              <Th>失败</Th>
              <Th>抑制</Th>
              <Th>已发送</Th>
              <Th>送达</Th>
              <Th>点击</Th>
              <Th>失败流水</Th>
              <Th>成功率</Th>
            </tr>
          </thead>
          <tbody>
            {groups.map((g) => (
              <tr key={g.group}>
                <Td>
                  <Mono>{g.group}</Mono>
                </Td>
                <Td>{g.assigned_users}</Td>
                <Td>{g.reach_users}</Td>
                <Td>{g.success_users}</Td>
                <Td>{g.fail_users}</Td>
                <Td>{g.suppressed_users}</Td>
                <Td>{g.sent_records}</Td>
                <Td>{g.delivered_records}</Td>
                <Td>{g.clicked_records}</Td>
                <Td>{g.failed_records}</Td>
                <Td>{pct(g.success_rate)}</Td>
              </tr>
            ))}
          </tbody>
        </TableWrap>
        {groups.length === 0 ? (
          <Empty>暂无实验分组数据（创建活动时填写 experiment_id / 对照组比例后拆分会写入 assignment）</Empty>
        ) : null}
      </Panel>

      <Panel className="mt-4">
        <PanelTitle>失败分析</PanelTitle>
        <TableWrap>
          <thead>
            <tr>
              <Th>渠道</Th>
              <Th>供应商</Th>
              <Th>错误</Th>
              <Th>次数</Th>
            </tr>
          </thead>
          <tbody>
            {failures.map((f, i) => (
              <tr key={`${f.channel}-${i}`}>
                <Td>
                  <Mono>{f.channel}</Mono>
                </Td>
                <Td>
                  <Mono>{f.provider || '-'}</Mono>
                </Td>
                <Td>{f.error_msg || '-'}</Td>
                <Td>{f.count}</Td>
              </tr>
            ))}
          </tbody>
        </TableWrap>
        {failures.length === 0 ? <Empty>暂无失败汇总</Empty> : null}
        {taskNum > 0 ? (
          <BtnRow className="mt-4">
            <ButtonLink to={`/ops/${taskNum}/records`} variant="ink">
              查看用户流水
            </ButtonLink>
            <ButtonLink to={`/tasks/${taskNum}/subtasks`} variant="ghost">
              查看子任务
            </ButtonLink>
            <ButtonLink to={`/progress?task=${taskNum}`} variant="ghost">
              活动进度
            </ButtonLink>
          </BtnRow>
        ) : null}
      </Panel>
    </div>
  )
}
