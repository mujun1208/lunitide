import React, { useEffect, useMemo, useRef, useState } from 'react'
import { BridgeClientError, createMutationAttempt, templateBridge, type TemplateBridge } from '../bridge/client'
import type { TemplateCreatePayload, TemplateListResult } from '../generated/bridge'
import { ConfirmDialog, Dialog } from '../ui/Dialog'

type TemplateDTO = TemplateListResult['items'][number]

const DOCUMENT_TYPES = [
  '业务需求分析报告', '系统实现评估报告', '需求任务清单', '系统架构设计文档', '系统硬件配置文档',
  '系统业务规范', '系统开发规范', '系统技术规范', '项目结构规范', '业务流程图', '业务流程清单',
  '业务蓝图文档', '接口清单', '功能开发清单', '功能详细设计文档', '接口详细设计文档', '数据库详细设计文档',
  'UI界面详细设计', '单元测试报告', '系统运维清单', '集成测试场景清单', '集成测试报告',
  '上线策略和风险评估报告', '应急预案报告', '上线问题清单',
] as const

const TYPE_LABEL: Record<TemplateDTO['templateType'], string> = { document: '文档模版', scaffold: '脚手架模版' }
const STATUS_LABEL: Record<TemplateDTO['status'], string> = { draft: '创建', enabled: '可用', disabled: '停用', void: '作废' }

const problem = (e: unknown) => e instanceof BridgeClientError ? e : new BridgeClientError(e instanceof Error ? e.message : '请求失败', 'CLIENT_ERROR', false, 'renderer')
const ordered = (items: TemplateDTO[]) => [...items].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt) || b.id.localeCompare(a.id))

const readFileBase64 = (file: File): Promise<string> => new Promise((resolve, reject) => {
  const reader = new FileReader()
  reader.onload = () => {
    const raw = String(reader.result ?? '')
    const comma = raw.indexOf(',')
    resolve(comma >= 0 ? raw.slice(comma + 1) : raw)
  }
  reader.onerror = () => reject(reader.error ?? new Error('文件读取失败'))
  reader.readAsDataURL(file)
})

const detectMime = (fileName: string): string => {
  const lower = fileName.toLowerCase()
  if (lower.endsWith('.tar.gz')) return 'application/gzip'
  const ext = lower.slice(lower.lastIndexOf('.'))
  const map: Record<string, string> = {
    '.md': 'text/markdown', '.txt': 'text/plain', '.html': 'text/html', '.htm': 'text/html',
    '.json': 'application/json', '.yaml': 'application/yaml', '.yml': 'application/yaml',
    '.csv': 'text/csv', '.pdf': 'application/pdf', '.doc': 'application/msword',
    '.docx': 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    '.xls': 'application/vnd.ms-excel', '.xlsx': 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    '.zip': 'application/zip',
  }
  return map[ext] ?? 'application/octet-stream'
}

interface UploadForm {
  name: string
  templateType: TemplateDTO['templateType']
  documentType: string
  description: string
  client: string
  file: File | null
}

const emptyForm = (): UploadForm => ({ name: '', templateType: 'document', documentType: '', description: '', client: '', file: null })

export function AssetManagerPage({ templates = templateBridge }: { templates?: TemplateBridge }): React.JSX.Element {
  const [items, setItems] = useState<TemplateDTO[]>([])
  const [query, setQuery] = useState('')
  const [typeFilter, setTypeFilter] = useState<'all' | TemplateDTO['templateType']>('all')
  const [statusFilter, setStatusFilter] = useState<'all' | TemplateDTO['status']>('all')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [loadError, setLoadError] = useState<BridgeClientError>()
  const [actionError, setActionError] = useState<BridgeClientError>()
  const [notice, setNotice] = useState('')
  const [uploadOpen, setUploadOpen] = useState(false)
  const [form, setForm] = useState<UploadForm>(emptyForm)
  const [formError, setFormError] = useState('')
  const [confirm, setConfirm] = useState<{ kind: 'delete' | 'void' | 'restore'; item: TemplateDTO }>()
  const mounted = useRef(true)
  const loadToken = useRef(0)

  const load = async () => {
    const token = ++loadToken.current
    setLoading(true)
    try {
      const result = await templates.list()
      if (mounted.current && token === loadToken.current) {
        setItems(ordered(result.items))
        setLoadError(undefined)
      }
    } catch (e) {
      if (mounted.current && token === loadToken.current) setLoadError(problem(e))
    } finally {
      if (mounted.current && token === loadToken.current) setLoading(false)
    }
  }

  useEffect(() => {
    mounted.current = true
    void load()
    return () => { mounted.current = false; loadToken.current++ }
  }, [])

  useEffect(() => {
    if (notice) {
      const t = window.setTimeout(() => setNotice(''), 2500)
      return () => window.clearTimeout(t)
    }
  }, [notice])

  const filtered = useMemo(() => items.filter(item =>
    (typeFilter === 'all' || item.templateType === typeFilter) &&
    (statusFilter === 'all' || item.status === statusFilter) &&
    (!query.trim() || `${item.name} ${item.templateCode} ${item.documentType ?? ''} ${item.client ?? ''}`.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()))
  ), [items, typeFilter, statusFilter, query])

  const patch = <K extends keyof UploadForm>(key: K, value: UploadForm[K]) => setForm(current => ({ ...current, [key]: value }))

  const submitUpload = async (e: React.FormEvent) => {
    e.preventDefault()
    if (busy) return
    if (!form.name.trim()) { setFormError('请输入模版名称'); return }
    if (!form.description.trim()) { setFormError('请输入模版描述'); return }
    if (form.templateType === 'document' && !form.documentType) { setFormError('请选择文件类型'); return }
    if (!form.file) { setFormError('请上传附件'); return }
    setBusy(true)
    setFormError('')
    setActionError(undefined)
    try {
      const contentBase64 = await readFileBase64(form.file)
      const payload: TemplateCreatePayload = {
        name: form.name.trim(),
        templateType: form.templateType,
        description: form.description.trim(),
        fileName: form.file.name,
        contentBase64,
        ...(form.templateType === 'document' ? { documentType: form.documentType } : {}),
        ...(form.client.trim() ? { client: form.client.trim() } : {}),
      }
      const attempt = createMutationAttempt('template.create', payload)
      const saved = await templates.create(attempt.payload, { attempt })
      if (mounted.current) {
        setItems(current => ordered([saved, ...current.filter(item => item.id !== saved.id)]))
        setUploadOpen(false)
        setForm(emptyForm())
        setNotice(`模版已上传，编号 ${saved.templateCode}，状态为创建`)
      }
    } catch (err) {
      if (mounted.current) setFormError(problem(err).message)
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  const runAction = async (kind: 'enable' | 'void' | 'delete' | 'restore', item: TemplateDTO) => {
    if (busy) return
    setBusy(true)
    setActionError(undefined)
    try {
      if (kind === 'enable') {
        const attempt = createMutationAttempt('template.enable', { id: item.id, expectedVersion: item.version })
        const saved = await templates.enable(attempt.payload, { attempt })
        if (mounted.current) setItems(current => ordered(current.map(row => row.id === saved.id ? { ...row, ...saved, status: 'enabled' } : row)))
        setNotice(`模版 ${saved.templateCode} 已启用`)
      } else if (kind === 'void') {
        const attempt = createMutationAttempt('template.void', { id: item.id, expectedVersion: item.version })
        const saved = await templates.void(attempt.payload, { attempt })
        if (mounted.current) setItems(current => ordered(current.map(row => row.id === saved.id ? { ...row, ...saved, status: 'void' } : row)))
        setNotice(`模版 ${saved.templateCode} 已作废`)
      } else if (kind === 'restore') {
        const attempt = createMutationAttempt('template.restore', { id: item.id, expectedVersion: item.version })
        const saved = await templates.restore(attempt.payload, { attempt })
        if (mounted.current) setItems(current => ordered(current.map(row => row.id === saved.id ? { ...row, ...saved, status: 'draft' } : row)))
        setNotice(`模版 ${saved.templateCode} 已恢复为创建`)
      } else {
        const attempt = createMutationAttempt('template.delete', { id: item.id, expectedVersion: item.version })
        await templates.delete(attempt.payload, { attempt })
        if (mounted.current) {
          setItems(current => current.filter(row => row.id !== item.id))
          setNotice(`模版 ${item.templateCode} 已彻底删除`)
        }
      }
      setConfirm(undefined)
    } catch (err) {
      if (mounted.current) setActionError(problem(err))
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  const acceptUpload = form.templateType === 'scaffold'
    ? '.zip,.tar.gz,application/zip,application/gzip,application/x-gzip'
    : '.md,.txt,.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.html,.htm,.json,.yaml,.yml,.csv,text/*,application/*'

  return (
    <div className="shell project-page pm-shell">
      <header className="project-hero">
        <div>
          <p className="eyebrow">ASSET MANAGEMENT</p>
          <h1>资产管理</h1>
          <p>维护各阶段交付物模版：上传、启用、作废、恢复与彻底删除。</p>
        </div>
      </header>
      <section className="pm-body">
        <div className="pm-toolbar">
          <h2>模版清单</h2>
          <label className="pm-search"><span aria-hidden="true">⌕</span><input aria-label="搜索模版" placeholder="搜索名称、编号、文件类型或客户…" value={query} onChange={e => setQuery(e.target.value)} /></label>
          <label className="pm-filter">类型<select aria-label="筛选模版类型" value={typeFilter} onChange={e => setTypeFilter(e.target.value as typeof typeFilter)}><option value="all">全部</option><option value="document">文档模版</option><option value="scaffold">脚手架模版</option></select></label>
          <label className="pm-filter">状态<select aria-label="筛选模版状态" value={statusFilter} onChange={e => setStatusFilter(e.target.value as typeof statusFilter)}><option value="all">全部</option><option value="draft">创建</option><option value="enabled">可用</option><option value="void">作废</option></select></label>
          <button aria-label="刷新模版" disabled={busy || loading} onClick={() => void load()}>↻</button>
          <button className="primary" onClick={() => { setForm(emptyForm()); setFormError(''); setUploadOpen(true) }}>＋ 上传资产</button>
        </div>
        <p className="gate-note">上传后状态为「创建」；点击「启用」变为「可用」。作废后不可被新引用；仅「创建」态可彻底删除。</p>
        {notice && <p className="notice project-notice" role="status">{notice}</p>}
        {actionError && <div className="error" role="alert"><b>{actionError.message}</b><button onClick={() => setActionError(undefined)}>关闭</button></div>}
        {loading && !items.length ? <p role="status">正在载入模版…</p> : loadError && !items.length ? <p className="error" role="alert">{loadError.message}</p> : !filtered.length ? <div className="empty"><b>{items.length ? '没有符合筛选条件的模版' : '还没有模版'}</b><span>点击「上传资产」添加各阶段交付物模版。</span></div> : (
          <ul className="pm-list">
            {filtered.map(item => (
              <li key={item.id} className={`pm-item status-${item.status}`}>
                <div className="pm-item-main">
                  <span className="pm-item-title"><b>{item.name}</b><small>{item.templateCode}{item.client ? ` · ${item.client}` : ''}{item.fileName ? ` · ${item.fileName}` : ''}</small></span>
                  <span className="pm-item-meta">
                    <i className="pm-chip">{TYPE_LABEL[item.templateType]}</i>
                    {item.documentType && <i className="pm-chip">{item.documentType}</i>}
                    <i className="pm-chip">{item.mimeType || detectMime(item.fileName ?? '')}</i>
                    <i className={`pm-chip status-${item.status}`}>{STATUS_LABEL[item.status]}</i>
                    <i className="pm-chip mono">v{item.version}</i>
                  </span>
                  <span className="pm-item-actions">
                    {item.status === 'draft' && <button className="primary" disabled={busy} onClick={() => void runAction('enable', item)}>启用</button>}
                    {item.status === 'draft' && <button className="danger" disabled={busy} onClick={() => setConfirm({ kind: 'delete', item })}>删除</button>}
                    {item.status === 'enabled' && <button disabled={busy} onClick={() => setConfirm({ kind: 'void', item })}>作废</button>}
                    {item.status === 'void' && <button disabled={busy} onClick={() => setConfirm({ kind: 'restore', item })}>恢复</button>}
                  </span>
                </div>
                {item.description && <p className="gate-note">{item.description}</p>}
              </li>
            ))}
          </ul>
        )}
      </section>

      <Dialog open={uploadOpen} wide title="上传资产模版" onClose={() => { if (!busy) setUploadOpen(false) }}>
        <form className="project-editor-dialog" onSubmit={e => void submitUpload(e)}>
          <fieldset disabled={busy}>
            <div className="form-grid">
              <label>1 模版编号<input value="保存时自动生成 TPLxxxxx" disabled /></label>
              <label>2 模版名称 *<input autoFocus value={form.name} maxLength={200} onChange={e => patch('name', e.target.value)} placeholder="例如：业务需求分析报告模板" /></label>
              <label>3 模版类型 *<select value={form.templateType} onChange={e => patch('templateType', e.target.value as UploadForm['templateType'])}><option value="document">文档模版</option><option value="scaffold">脚手架模版</option></select></label>
              {form.templateType === 'document' && (
                <label>文件类型 *<select value={form.documentType} onChange={e => patch('documentType', e.target.value)}><option value="">请选择</option>{DOCUMENT_TYPES.map(type => <option key={type} value={type}>{type}</option>)}</select></label>
              )}
              <label className="wide">4 模版描述 *<textarea rows={2} maxLength={2000} value={form.description} onChange={e => patch('description', e.target.value)} placeholder="描述模版用途和适用范围" /></label>
              <label>5 客户<input value={form.client} maxLength={200} onChange={e => patch('client', e.target.value)} placeholder="可选" /></label>
              <label>6 状态<input value="创建" disabled /></label>
              <label className="wide">附件 *<input type="file" accept={acceptUpload} onChange={e => { const file = e.target.files?.[0] ?? null; patch('file', file); if (file && !form.name.trim()) patch('name', file.name.replace(/\.[^.]+$/, '')) }} />{form.file && <small>{form.file.name} · MIME {detectMime(form.file.name)}（自动识别）</small>}</label>
            </div>
            {formError && <p className="error" role="alert"><b>{formError}</b></p>}
          </fieldset>
          <div className="dialog-actions"><button type="button" disabled={busy} onClick={() => setUploadOpen(false)}>取消</button><button className="primary" disabled={busy}>{busy ? '上传中…' : '上传并保存'}</button></div>
        </form>
      </Dialog>

      <ConfirmDialog open={confirm?.kind === 'delete'} title={`彻底删除模版「${confirm?.item.name ?? ''}」？`} description="删除后不再恢复，其他地方也无法引用该资产。确认是否删除？" busy={busy} onCancel={() => setConfirm(undefined)} onConfirm={() => confirm && void runAction('delete', confirm.item)} />
      <ConfirmDialog open={confirm?.kind === 'void'} title={`作废模版「${confirm?.item.name ?? ''}」？`} description="作废后，其他地方无法引用该资产。确认是否作废？" busy={busy} onCancel={() => setConfirm(undefined)} onConfirm={() => confirm && void runAction('void', confirm.item)} />
      <ConfirmDialog open={confirm?.kind === 'restore'} title={`恢复模版「${confirm?.item.name ?? ''}」？`} description="确认是否恢复作废的资产？恢复后状态回到「创建」。" busy={busy} onCancel={() => setConfirm(undefined)} onConfirm={() => confirm && void runAction('restore', confirm.item)} />
    </div>
  )
}
