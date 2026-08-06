import { useCallback, useEffect, useState } from 'react'
import { ApiError, api } from '../api/client'
import type { Template, TemplateStatus } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { StatusChip } from '../components/StatusChip'
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
  Select,
  TableWrap,
  Td,
  Th,
  Toast,
} from '../components/ui'

export function TemplatesPage() {
  const { user } = useAuth()
  const operator = user?.username || 'admin'
  const [items, setItems] = useState<Template[]>([])
  const [total, setTotal] = useState(0)
  const [status, setStatus] = useState<TemplateStatus | ''>('')
  const [keyword, setKeyword] = useState('')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')

  const load = useCallback(async () => {
    try {
      const res = await api.listTemplates({
        status,
        keyword: keyword.trim() || undefined,
        page: 1,
        page_size: 50,
      })
      setItems(res.items ?? [])
      setTotal(res.total ?? 0)
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '加载模板失败')
    }
  }, [keyword, status])

  useEffect(() => {
    void load()
  }, [load])

  async function run(action: () => Promise<unknown>, ok = '完成') {
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      await action()
      setMsg(ok)
      await load()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '操作失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div>
      <PageHead title="模板中心" description={`草稿 → 提交审核 → 通过后可被活动引用。共 ${total} 条。`} />

      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel>
        <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
          <div className="grid flex-1 gap-3 sm:grid-cols-2 md:grid-cols-3">
            <Field label="状态">
              <Select value={status} onChange={(e) => setStatus(e.target.value as TemplateStatus | '')}>
                <option value="">全部</option>
                <option value="draft">draft</option>
                <option value="pending_review">pending_review</option>
                <option value="approved">approved</option>
                <option value="rejected">rejected</option>
                <option value="disabled">disabled</option>
              </Select>
            </Field>
            <Field label="关键词">
              <Input value={keyword} onChange={(e) => setKeyword(e.target.value)} placeholder="名称 / code" />
            </Field>
          </div>
          <BtnRow>
            <Button variant="ink" type="button" onClick={() => void load()} disabled={busy}>
              刷新
            </Button>
            <ButtonLink to="/templates/new" variant="primary">
              创建模板
            </ButtonLink>
          </BtnRow>
        </div>

        <TableWrap>
          <thead>
            <tr>
              <Th>ID</Th>
              <Th>Code</Th>
              <Th>名称</Th>
              <Th>场景</Th>
              <Th>状态</Th>
              <Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {items.map((t) => (
              <tr key={t.id} className="hover:bg-white/50">
                <Td>
                  <Mono>{t.id}</Mono>
                </Td>
                <Td>
                  <Mono>{t.code}</Mono>
                </Td>
                <Td>
                  <div>{t.name}</div>
                  <Mono className="text-muted">v{t.version}</Mono>
                </Td>
                <Td>{t.biz_scene || '-'}</Td>
                <Td>
                  <StatusChip status={t.status} />
                  {t.reject_reason ? <div className="mt-1 text-xs text-rose">{t.reject_reason}</div> : null}
                </Td>
                <Td>
                  <BtnRow>
                    {t.status === 'draft' || t.status === 'rejected' ? (
                      <>
                        <ButtonLink to={`/templates/${t.id}/edit`} variant="ghost">
                          编辑
                        </ButtonLink>
                        <Button
                          variant="primary"
                          type="button"
                          disabled={busy}
                          onClick={() => void run(() => api.submitTemplate(t.id, operator), '已提交审核')}
                        >
                          提交
                        </Button>
                        <Button
                          variant="danger"
                          type="button"
                          disabled={busy}
                          onClick={() => void run(() => api.deleteTemplate(t.id), '已删除')}
                        >
                          删除
                        </Button>
                      </>
                    ) : null}
                    {t.status === 'pending_review' ? (
                      <>
                        <Button
                          variant="primary"
                          type="button"
                          disabled={busy}
                          onClick={() => void run(() => api.approveTemplate(t.id, operator), '已通过')}
                        >
                          通过
                        </Button>
                        <Button
                          variant="danger"
                          type="button"
                          disabled={busy}
                          onClick={() => {
                            const reason = window.prompt('驳回原因', '文案需修改')
                            if (!reason) return
                            void run(() => api.rejectTemplate(t.id, reason, operator), '已驳回')
                          }}
                        >
                          驳回
                        </Button>
                      </>
                    ) : null}
                    {t.status === 'approved' ? (
                      <Button
                        variant="ghost"
                        type="button"
                        disabled={busy}
                        onClick={() => void run(() => api.disableTemplate(t.id), '已停用')}
                      >
                        停用
                      </Button>
                    ) : null}
                    {t.status === 'disabled' ? (
                      <Button
                        variant="ghost"
                        type="button"
                        disabled={busy}
                        onClick={() => void run(() => api.enableTemplate(t.id), '已重新启用（待审）')}
                      >
                        启用
                      </Button>
                    ) : null}
                  </BtnRow>
                </Td>
              </tr>
            ))}
          </tbody>
        </TableWrap>
        {items.length === 0 ? <Empty>暂无模板</Empty> : null}
      </Panel>
    </div>
  )
}
