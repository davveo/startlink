import { useCallback, useEffect, useState } from 'react'
import { ApiError, api } from '../api/client'
import type { AuditLog } from '../api/types'
import {
  BtnRow,
  Button,
  Empty,
  Field,
  Input,
  Mono,
  PageHead,
  Panel,
  Select,
  TableWrap,
  Td,
  Th,
  Toast,
} from '../components/ui'
import { auditActionLabel } from '../lib/labels'

function formatTime(v?: string) {
  if (!v) return '-'
  try {
    return new Date(v).toLocaleString()
  } catch {
    return v
  }
}

export function AuditLogsPage() {
  const [items, setItems] = useState<AuditLog[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [operator, setOperator] = useState('')
  const [action, setAction] = useState('')
  const [successFilter, setSuccessFilter] = useState<'' | '1' | '0'>('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const pageSize = 20

  const load = useCallback(async () => {
    setBusy(true)
    setErr('')
    try {
      const res = await api.listAuditLogs({
        operator: operator || undefined,
        action: action || undefined,
        success: successFilter === '' ? undefined : successFilter === '1',
        page,
        page_size: pageSize,
      })
      setItems(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载审计日志失败')
    } finally {
      setBusy(false)
    }
  }, [action, operator, page, successFilter])

  useEffect(() => {
    void load()
  }, [load])

  const pages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div>
      <PageHead title="审计日志" description="记录运营台写操作：谁、何时、做了什么、结果如何。查询本身不写入审计。" />

      {err ? <Toast kind="error">{err}</Toast> : null}

      <Panel>
        <div className="flex flex-wrap items-end gap-3">
          <Field label="操作者" noMargin className="min-w-[9rem] flex-[1_1_9rem]">
            <Input value={operator} onChange={(e) => setOperator(e.target.value)} placeholder="用户名" />
          </Field>
          <Field label="动作" noMargin className="min-w-[11rem] flex-[1_1_11rem]">
            <Input
              value={action}
              onChange={(e) => setAction(e.target.value)}
              placeholder="campaign.create"
              title="可填前缀，如 campaign / auth"
            />
          </Field>
          <Field label="结果" noMargin className="min-w-[8rem] flex-[1_1_8rem]">
            <Select
              value={successFilter}
              onChange={(e) => {
                setPage(1)
                setSuccessFilter(e.target.value as '' | '1' | '0')
              }}
            >
              <option value="">全部</option>
              <option value="1">成功</option>
              <option value="0">失败</option>
            </Select>
          </Field>
          <BtnRow className="shrink-0">
            <Button
              variant="ink"
              type="button"
              disabled={busy}
              onClick={() => {
                setPage(1)
                void load()
              }}
            >
              查询
            </Button>
          </BtnRow>
        </div>
        <p className="mt-1.5 text-xs text-muted">动作可填前缀，如 campaign / auth</p>
      </Panel>

      <Panel className="mt-4">
        <TableWrap>
          <thead>
            <tr>
              <Th>时间</Th>
              <Th>操作者</Th>
              <Th>动作</Th>
              <Th>资源</Th>
              <Th>请求</Th>
              <Th>结果</Th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr>
                <Td colSpan={6}>
                  <Empty>暂无审计记录</Empty>
                </Td>
              </tr>
            ) : (
              items.map((row) => (
                <tr key={row.id} className="hover:bg-white/50">
                  <Td className="whitespace-nowrap text-sm">{formatTime(row.created_at)}</Td>
                  <Td>{row.operator || '-'}</Td>
                  <Td>
                    <span title={row.action}>{auditActionLabel(row.action)}</span>
                  </Td>
                  <Td className="text-sm">
                    {row.resource_type || '-'}
                    {row.resource_id ? (
                      <>
                        {' '}
                        <Mono>#{row.resource_id}</Mono>
                      </>
                    ) : null}
                  </Td>
                  <Td className="max-w-[220px] truncate text-xs text-muted" title={`${row.method} ${row.path}`}>
                    {row.method} {row.path}
                  </Td>
                  <Td>{row.success ? '成功' : '失败'}</Td>
                </tr>
              ))
            )}
          </tbody>
        </TableWrap>
        <BtnRow className="mt-4">
          <Button variant="ghost" type="button" disabled={busy || page <= 1} onClick={() => setPage((p) => p - 1)}>
            上一页
          </Button>
          <span className="text-sm text-muted">
            {page} / {pages}（共 {total}）
          </span>
          <Button
            variant="ghost"
            type="button"
            disabled={busy || page >= pages}
            onClick={() => setPage((p) => p + 1)}
          >
            下一页
          </Button>
        </BtnRow>
      </Panel>
    </div>
  )
}
