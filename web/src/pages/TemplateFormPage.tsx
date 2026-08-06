import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { ApiError, api } from '../api/client'
import type { ChannelContent, ChannelType, MissingVarPolicy, Template, VarDef } from '../api/types'
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

const CONTENT_CHANNELS: ChannelType[] = ['inbox', 'sms', 'app_push', 'email']

const emptyForm = {
  code: '',
  name: '',
  body: '你好 {{name}}，欢迎使用 Starlink。',
  biz_scene: 'demo',
  channel_hint: '' as ChannelType | '',
  missing_var_policy: 'empty' as MissingVarPolicy,
  default_locale: '',
  contents: {
    inbox: { title: '', body: '' },
    sms: { title: '', body: '' },
    app_push: { title: '', body: '' },
    email: { title: '', body: '' },
  } as Record<string, ChannelContent>,
  var_schema_text: '[\n  {"name":"name","type":"string","required":true,"example":"Ada"}\n]',
}

function contentsFromTemplate(t: Template): Record<string, ChannelContent> {
  const base: Record<string, ChannelContent> = {
    inbox: { title: '', body: '' },
    sms: { title: '', body: '' },
    app_push: { title: '', body: '' },
    email: { title: '', body: '' },
  }
  if (t.contents) {
    for (const k of Object.keys(t.contents)) {
      base[k] = { title: t.contents[k]?.title ?? '', body: t.contents[k]?.body ?? '' }
    }
  }
  return base
}

function compactContents(c: Record<string, ChannelContent>): Record<string, ChannelContent> | undefined {
  const out: Record<string, ChannelContent> = {}
  for (const [k, v] of Object.entries(c)) {
    if ((v.title && v.title.trim()) || (v.body && v.body.trim())) {
      out[k] = { title: v.title || undefined, body: v.body || undefined }
    }
  }
  return Object.keys(out).length ? out : undefined
}

function parseVarSchema(text: string): VarDef[] | undefined {
  const t = text.trim()
  if (!t) return undefined
  const parsed = JSON.parse(t) as VarDef[]
  if (!Array.isArray(parsed)) throw new Error('var_schema 须为数组')
  return parsed
}

export function TemplateFormPage() {
  const { id: idParam } = useParams()
  const editingId = idParam ? Number(idParam) : 0
  const isEdit = Number.isFinite(editingId) && editingId > 0

  const { user } = useAuth()
  const navigate = useNavigate()
  const [form, setForm] = useState(emptyForm)
  const [contentTab, setContentTab] = useState<ChannelType>('inbox')
  const [editing, setEditing] = useState<Template | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [previewText, setPreviewText] = useState('')
  const [previewVars, setPreviewVars] = useState('{"name":"Ada"}')

  useEffect(() => {
    if (!isEdit) {
      setEditing(null)
      setForm(emptyForm)
      return
    }
    let alive = true
    void (async () => {
      setBusy(true)
      setErr('')
      try {
        const t = await api.getTemplate(editingId)
        if (!alive) return
        if (t.status !== 'draft' && t.status !== 'rejected') {
          setErr('仅草稿或已驳回模板可编辑')
          return
        }
        setEditing(t)
        setForm({
          code: t.code,
          name: t.name,
          body: t.body,
          biz_scene: t.biz_scene,
          channel_hint: t.channel_hint ?? '',
          missing_var_policy: t.missing_var_policy || 'empty',
          default_locale: t.default_locale ?? '',
          contents: contentsFromTemplate(t),
          var_schema_text: t.var_schema?.length
            ? JSON.stringify(t.var_schema, null, 2)
            : emptyForm.var_schema_text,
        })
      } catch (e) {
        if (!alive) return
        setErr(e instanceof ApiError ? e.message : '加载模板失败')
      } finally {
        if (alive) setBusy(false)
      }
    })()
    return () => {
      alive = false
    }
  }, [editingId, isEdit])

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setErr('')
    setMsg('')
    try {
      const contents = compactContents(form.contents)
      const var_schema = parseVarSchema(form.var_schema_text)
      if (isEdit && editing) {
        await api.updateTemplate(editing.id, {
          name: form.name,
          body: form.body,
          contents: contents ?? {},
          var_schema: var_schema ?? [],
          missing_var_policy: form.missing_var_policy,
          default_locale: form.default_locale || undefined,
          biz_scene: form.biz_scene,
          channel_hint: form.channel_hint || undefined,
          version: editing.version,
          updated_by: user?.username,
        })
        setMsg('模板已更新')
        navigate('/templates')
        return
      }
      await api.createTemplate({
        code: form.code || undefined,
        name: form.name,
        body: form.body,
        contents,
        var_schema,
        missing_var_policy: form.missing_var_policy,
        default_locale: form.default_locale || undefined,
        biz_scene: form.biz_scene,
        channel_hint: form.channel_hint || undefined,
        created_by: user?.username || 'admin',
      })
      setMsg('模板已创建')
      navigate('/templates')
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : e instanceof Error ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  async function onPreview() {
    setBusy(true)
    setErr('')
    setPreviewText('')
    try {
      let vars: Record<string, string> = {}
      try {
        vars = JSON.parse(previewVars) as Record<string, string>
      } catch {
        throw new Error('预览变量须为 JSON 对象')
      }
      const contents = compactContents(form.contents)
      const var_schema = parseVarSchema(form.var_schema_text)
      const res = await api.previewTemplate({
        template_id: isEdit && editing ? editing.code : undefined,
        body: form.body,
        contents,
        var_schema,
        missing_var_policy: form.missing_var_policy,
        default_locale: form.default_locale || undefined,
        title: '预览标题',
        channel: contentTab,
        vars,
      })
      setPreviewText(
        `title: ${res.rendered_title || '(空)'}\ncontent: ${res.rendered_content}\nmissing: ${(res.missing_vars || []).join(', ') || '-'}\nschema: ${(res.schema_errors || []).join('; ') || '-'}`,
      )
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : e instanceof Error ? e.message : '预览失败')
    } finally {
      setBusy(false)
    }
  }

  const tabContent = form.contents[contentTab] || { title: '', body: '' }

  return (
    <div>
      <PageHead
        title={isEdit ? '编辑模板' : '创建模板'}
        description={isEdit ? `修改草稿或已驳回模板 ${editing?.code ?? ''}` : '创建草稿后，在列表中提交审核。'}
      />

      {err ? <Toast kind="error">{err}</Toast> : null}
      {msg ? <Toast kind="ok">{msg}</Toast> : null}

      <Panel className="max-w-3xl">
        <form onSubmit={onSubmit}>
          <PanelTitle>{isEdit ? `编辑 ${editing?.code ?? ''}` : '模板内容'}</PanelTitle>
          {!isEdit ? (
            <Field
              label="模板编码"
              hint="创建后不可改，活动引用的是编码而非数字 ID。留空则系统自动生成。"
            >
              <Input
                value={form.code}
                onChange={(e) => setForm({ ...form, code: e.target.value })}
                placeholder="tpl_welcome"
              />
            </Field>
          ) : null}
          <Field label="模板名称" hint="列表与运营识别用，不会直接作为推送标题发出。">
            <Input
              required
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
              placeholder="新用户欢迎语"
            />
          </Field>
          <Field label="业务场景" hint="便于按场景筛选模板。">
            <Input
              value={form.biz_scene}
              onChange={(e) => setForm({ ...form, biz_scene: e.target.value })}
              placeholder="demo"
            />
          </Field>
          <Field label="建议渠道" hint="仅作运营提示，不强制活动渠道。">
            <Select
              value={form.channel_hint}
              onChange={(e) => setForm({ ...form, channel_hint: e.target.value as ChannelType | '' })}
            >
              <option value="">不限</option>
              <option value="inbox">站内信</option>
              <option value="sms">短信</option>
              <option value="app_push">App 推送</option>
              <option value="email">邮件</option>
              <option value="wecom">企业微信</option>
              <option value="dingtalk">钉钉</option>
            </Select>
          </Field>
          <Field label="缺失变量策略" hint="报错 / 保留占位符 / 用 Schema 默认值 / 置空。">
            <Select
              value={form.missing_var_policy}
              onChange={(e) => setForm({ ...form, missing_var_policy: e.target.value as MissingVarPolicy })}
            >
              <option value="empty">置空</option>
              <option value="keep">保留 {'{{var}}'}</option>
              <option value="default">Schema 默认值</option>
              <option value="error">报错</option>
            </Select>
          </Field>
          <Field label="默认语言" hint="多语言回退用，如 zh-CN。可留空。">
            <Input
              value={form.default_locale}
              onChange={(e) => setForm({ ...form, default_locale: e.target.value })}
              placeholder="zh-CN"
            />
          </Field>
          <Field
            label="默认正文"
            hint={
              <>
                无分渠道内容时使用。变量用 {'{{name}}'}；与下方「分渠道内容」至少填一处。
              </>
            }
          >
            <Textarea
              value={form.body}
              onChange={(e) => setForm({ ...form, body: e.target.value })}
              placeholder="你好 {{name}}，欢迎使用 Starlink。"
            />
          </Field>

          <Field label="分渠道内容" hint="为 inbox / sms / app_push / email 分别维护标题与正文；留空则回退默认正文 + 活动标题。">
            <div className="mb-2 flex flex-wrap gap-2">
              {CONTENT_CHANNELS.map((c) => (
                <button
                  key={c}
                  type="button"
                  className={`rounded-full border px-3 py-1 text-sm ${
                    contentTab === c ? 'border-teal bg-teal/10 text-teal-deep' : 'border-line'
                  }`}
                  onClick={() => setContentTab(c)}
                >
                  {c}
                </button>
              ))}
            </div>
            <Input
              className="mb-2"
              value={tabContent.title || ''}
              onChange={(e) =>
                setForm({
                  ...form,
                  contents: {
                    ...form.contents,
                    [contentTab]: { ...tabContent, title: e.target.value },
                  },
                })
              }
              placeholder={`${contentTab} 标题（可选）`}
            />
            <Textarea
              value={tabContent.body || ''}
              onChange={(e) =>
                setForm({
                  ...form,
                  contents: {
                    ...form.contents,
                    [contentTab]: { ...tabContent, body: e.target.value },
                  },
                })
              }
              placeholder={`${contentTab} 正文（可选）`}
            />
          </Field>

          <Field label="变量 Schema (JSON)" hint="声明 name/type/required/default/example/sensitive。">
            <Textarea
              className="font-mono text-xs"
              value={form.var_schema_text}
              onChange={(e) => setForm({ ...form, var_schema_text: e.target.value })}
              rows={6}
            />
          </Field>

          <Field label="预览变量 (JSON)" hint="点击预览按钮做渲染测试。">
            <Input
              className="font-mono text-sm"
              value={previewVars}
              onChange={(e) => setPreviewVars(e.target.value)}
            />
          </Field>
          {previewText ? (
            <pre className="mb-3 overflow-auto rounded-lg border border-line bg-canvas p-3 text-xs whitespace-pre-wrap">
              {previewText}
            </pre>
          ) : null}

          <BtnRow>
            <Button variant="primary" type="submit" disabled={busy || (isEdit && !editing)}>
              {isEdit ? '保存修改' : '创建草稿'}
            </Button>
            <Button type="button" variant="ghost" disabled={busy} onClick={() => void onPreview()}>
              预览渲染
            </Button>
            <ButtonLink to="/templates" variant="ghost">
              返回列表
            </ButtonLink>
          </BtnRow>
        </form>
      </Panel>
    </div>
  )
}
