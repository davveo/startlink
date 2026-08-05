import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { ApiError, api } from '../api/client'
import type { ChannelType, Template, TemplateStatus } from '../api/types'
import { StatusChip } from '../components/StatusChip'

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
      <div className="page-head">
        <div>
          <h1>模板中心</h1>
          <p>草稿 → 提交审核 → 通过后可被活动引用。共 {total} 条。</p>
        </div>
      </div>

      {err ? <div className="toast toast-error">{err}</div> : null}
      {msg ? <div className="toast toast-ok">{msg}</div> : null}

      <div className="grid-2">
        <form className="panel" onSubmit={onSubmitForm}>
          <h2>{editing ? `编辑 ${editing.code}` : '新建模板'}</h2>
          {!editing ? (
            <div className="field">
              <label>code（可选，空则自动生成）</label>
              <input
                value={form.code}
                onChange={(e) => setForm({ ...form, code: e.target.value })}
                placeholder="tpl_welcome"
              />
            </div>
          ) : null}
          <div className="field">
            <label>名称</label>
            <input
              required
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </div>
          <div className="field">
            <label>业务场景</label>
            <input
              value={form.biz_scene}
              onChange={(e) => setForm({ ...form, biz_scene: e.target.value })}
              placeholder="demo / marketing / txn"
            />
          </div>
          <div className="field">
            <label>建议渠道</label>
            <select
              value={form.channel_hint}
              onChange={(e) => setForm({ ...form, channel_hint: e.target.value })}
            >
              <option value="">不限</option>
              <option value="inbox">inbox</option>
              <option value="sms">sms</option>
              <option value="app_push">app_push</option>
              <option value="email">email</option>
              <option value="wecom">wecom</option>
              <option value="dingtalk">dingtalk</option>
            </select>
          </div>
          <div className="field">
            <label>正文（支持 {'{{var}}'}）</label>
            <textarea
              required
              value={form.body}
              onChange={(e) => setForm({ ...form, body: e.target.value })}
            />
          </div>
          <div className="btn-row">
            <button className="btn btn-primary" type="submit" disabled={busy}>
              {editing ? '保存修改' : '创建草稿'}
            </button>
            {editing ? (
              <button
                className="btn btn-ghost"
                type="button"
                onClick={() => {
                  setEditing(null)
                  setForm(emptyForm)
                }}
              >
                取消编辑
              </button>
            ) : null}
          </div>
        </form>

        <div className="panel">
          <h2>筛选</h2>
          <div className="field">
            <label>状态</label>
            <select value={status} onChange={(e) => setStatus(e.target.value as TemplateStatus | '')}>
              <option value="">全部</option>
              <option value="draft">draft</option>
              <option value="pending_review">pending_review</option>
              <option value="approved">approved</option>
              <option value="rejected">rejected</option>
              <option value="disabled">disabled</option>
            </select>
          </div>
          <div className="field">
            <label>关键词</label>
            <input value={keyword} onChange={(e) => setKeyword(e.target.value)} placeholder="名称 / code" />
          </div>
          <button className="btn btn-ink" type="button" onClick={() => void load()} disabled={busy}>
            刷新列表
          </button>
        </div>
      </div>

      <div className="panel">
        <h2>模板列表</h2>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Code</th>
                <th>名称</th>
                <th>场景</th>
                <th>状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((t) => (
                <tr key={t.id}>
                  <td className="mono">{t.id}</td>
                  <td className="mono">{t.code}</td>
                  <td>
                    <div>{t.name}</div>
                    <small className="mono" style={{ color: 'var(--muted)' }}>
                      v{t.version}
                    </small>
                  </td>
                  <td>{t.biz_scene || '-'}</td>
                  <td>
                    <StatusChip status={t.status} />
                    {t.reject_reason ? (
                      <div>
                        <small style={{ color: 'var(--rose)' }}>{t.reject_reason}</small>
                      </div>
                    ) : null}
                  </td>
                  <td>
                    <div className="btn-row">
                      {t.status === 'draft' || t.status === 'rejected' ? (
                        <>
                          <button className="btn btn-ghost" type="button" disabled={busy} onClick={() => startEdit(t)}>
                            编辑
                          </button>
                          <button
                            className="btn btn-primary"
                            type="button"
                            disabled={busy}
                            onClick={() => void run(() => api.submitTemplate(t.id), '已提交审核')}
                          >
                            提交
                          </button>
                          <button
                            className="btn btn-danger"
                            type="button"
                            disabled={busy}
                            onClick={() => void run(() => api.deleteTemplate(t.id), '已删除')}
                          >
                            删除
                          </button>
                        </>
                      ) : null}
                      {t.status === 'pending_review' ? (
                        <>
                          <button
                            className="btn btn-primary"
                            type="button"
                            disabled={busy}
                            onClick={() => void run(() => api.approveTemplate(t.id), '已通过')}
                          >
                            通过
                          </button>
                          <button
                            className="btn btn-danger"
                            type="button"
                            disabled={busy}
                            onClick={() => {
                              const reason = window.prompt('驳回原因', '文案需修改')
                              if (!reason) return
                              void run(() => api.rejectTemplate(t.id, reason), '已驳回')
                            }}
                          >
                            驳回
                          </button>
                        </>
                      ) : null}
                      {t.status === 'approved' ? (
                        <button
                          className="btn btn-ghost"
                          type="button"
                          disabled={busy}
                          onClick={() => void run(() => api.disableTemplate(t.id), '已停用')}
                        >
                          停用
                        </button>
                      ) : null}
                      {t.status === 'disabled' ? (
                        <button
                          className="btn btn-ghost"
                          type="button"
                          disabled={busy}
                          onClick={() => void run(() => api.enableTemplate(t.id), '已重新启用（待审）')}
                        >
                          启用
                        </button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {items.length === 0 ? <div className="empty">暂无模板</div> : null}
        </div>
      </div>
    </div>
  )
}
