import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { ApiError, api } from '../api/client'
import type { ChannelType, Template, TemplateStatus } from '../api/types'
import { StatusChip } from '../components/StatusChip'
import {
  BtnRow,
  Button,
  Empty,
  Field,
  Input,
  Mono,
  PageHead,
  Panel,
  PanelTitle,
  Select,
  TableWrap,
  Td,
  Textarea,
  Th,
  Toast,
} from '../components/ui'

const emptyForm = {
  code: '',
  name: '',
  body: '你好 {{name}}，欢迎使用 Starlink。',
  biz_scene: 'demo',
  channel_hint: '' as ChannelType | '',
}

export function TemplatesPage() {
  const [items, setItems] = useState<Template[]>([])
  const [total, setTotal] = useState(0)
  const [status, setStatus] = useState<TemplateStatus | ''>('')
  const [keyword, setKeyword] = useState('')
  const [form, setForm] = useState(emptyForm)
  const [editing, setEditing] = useState<Template | null>(null)
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

  async function onSubmitForm(e: FormEvent) {
    e.preventDefault()
    if (editing) {
      await run(
        () =>
          api.updateTemplate(editing.id, {
            name: form.name,
            body: form.body,
            biz_scene: form.biz_scene,
            channel_hint: form.channel_hint || undefined,
            version: editing.version,
          }),
        '模板已更新',
      )
      setEditing(null)
      setForm(emptyForm)
      return
    }
    await run(
      () =>
        api.createTemplate({
          code: form.code || undefined,
          name: form.name,
          body: form.body,
          biz_scene: form.biz_scene,
          channel_hint: form.channel_hint || undefined,
          created_by: 'console',
        }),
      '模板已创建',
    )
    setForm(emptyForm)
  }

  function startEdit(t: Template) {
    setEditing(t)
    setForm({
      code: t.code,
      name: t.name,
      body: t.body,
      biz_scene: t.biz_scene,
      channel_hint: t.channel_hint ?? '',
    })
  }

  return (
    <div>
      <PageHead title="模板中心" description={`草稿 → 提交审核 → 通过后可被活动引用。共 ${total} 条。`} />

      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <div className="grid gap-4 md:grid-cols-2">
        <Panel>
          <form onSubmit={onSubmitForm}>
            <PanelTitle>{editing ? `编辑 ${editing.code}` : '新建模板'}</PanelTitle>
            {!editing ? (
              <Field label="code（可选，空则自动生成）">
                <Input
                  value={form.code}
                  onChange={(e) => setForm({ ...form, code: e.target.value })}
                  placeholder="tpl_welcome"
                />
              </Field>
            ) : null}
            <Field label="名称">
              <Input
                required
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
              />
            </Field>
            <Field label="业务场景">
              <Input
                value={form.biz_scene}
                onChange={(e) => setForm({ ...form, biz_scene: e.target.value })}
                placeholder="demo / marketing / txn"
              />
            </Field>
            <Field label="建议渠道">
              <Select
                value={form.channel_hint}
                onChange={(e) => setForm({ ...form, channel_hint: e.target.value as ChannelType | '' })}
              >
                <option value="">不限</option>
                <option value="inbox">inbox</option>
                <option value="sms">sms</option>
                <option value="app_push">app_push</option>
                <option value="email">email</option>
                <option value="wecom">wecom</option>
                <option value="dingtalk">dingtalk</option>
              </Select>
            </Field>
            <Field label={<>正文（支持 {'{{var}}'}）</>}>
              <Textarea
                required
                value={form.body}
                onChange={(e) => setForm({ ...form, body: e.target.value })}
              />
            </Field>
            <BtnRow>
              <Button variant="primary" type="submit" disabled={busy}>
                {editing ? '保存修改' : '创建草稿'}
              </Button>
              {editing ? (
                <Button
                  variant="ghost"
                  type="button"
                  onClick={() => {
                    setEditing(null)
                    setForm(emptyForm)
                  }}
                >
                  取消编辑
                </Button>
              ) : null}
            </BtnRow>
          </form>
        </Panel>

        <Panel>
          <PanelTitle>筛选</PanelTitle>
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
          <Button variant="ink" type="button" onClick={() => void load()} disabled={busy}>
            刷新列表
          </Button>
        </Panel>
      </div>

      <Panel className="mt-4">
        <PanelTitle>模板列表</PanelTitle>
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
                        <Button variant="ghost" type="button" disabled={busy} onClick={() => startEdit(t)}>
                          编辑
                        </Button>
                        <Button
                          variant="primary"
                          type="button"
                          disabled={busy}
                          onClick={() => void run(() => api.submitTemplate(t.id), '已提交审核')}
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
                          onClick={() => void run(() => api.approveTemplate(t.id), '已通过')}
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
                            void run(() => api.rejectTemplate(t.id, reason), '已驳回')
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
