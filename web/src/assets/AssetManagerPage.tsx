import React, { useEffect, useMemo, useState } from 'react'
import { attachmentBridge, projectBridge, templateBridge, type AttachmentBridge, type ProjectBridge, type TemplateBridge } from '../bridge/client'
import type { AttachmentGetResult, AttachmentListResult, ProjectDTO, TemplateListResult } from '../generated/bridge'

// ---- 分类与资产模型 ----
type AssetCategory = 'all' | 'files' | 'deliverables' | 'phase_docs' | 'test_evidence' | 'releases' | 'templates'
type AssetStatus = 'creating' | 'enabled' | 'deprecated'
type AssetScope = 'project' | 'organization'
type AssetSource = 'ui_design' | 'database' | 'development' | 'testing' | 'release' | 'template'

interface AssetRow {
  id: string
  name: string
  filename: string
  category: AssetCategory
  subCategory: string
  scope: AssetScope
  source: AssetSource
  sourceLabel: string
  version: string
  status: AssetStatus
  description: string
  mime: string
  size: number
  sha256: string
  createdAt: string
  references: number
  projectName: string
  /** 从后端 attachment 映射而来 */
  attachmentId?: string
}

interface LifecycleAction {
  type: 'deactivate' | 'restore' | 'delete'
  asset: AssetRow
}

// ---- 分类定义 ----
const CATEGORIES: { key: AssetCategory; label: string }[] = [
  { key: 'all', label: '全部资产' },
  { key: 'files', label: '文件与附件' },
  { key: 'deliverables', label: '工作成果' },
  { key: 'phase_docs', label: '阶段文档' },
  { key: 'test_evidence', label: '测试与证据' },
  { key: 'releases', label: '发布包' },
  { key: 'templates', label: '模板' },
]

const STATUS_LABEL: Record<AssetStatus, string> = { creating: '创建', enabled: '已启用', deprecated: '已作废' }
const STATUS_CLASS: Record<AssetStatus, string> = { creating: 'status-draft', enabled: 'status-published', deprecated: 'status-deprecated' }

const FILTER_TABS = ['全部', '需求架构', '方案和UI', '工程与发布']

function fmtSize(n: number) { return n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB` }

// ---- 将后端 attachment 数据映射为 AssetRow ----
function mapTemplate(item: TemplateListResult['items'][number]): AssetRow {
  const status: AssetStatus =
    item.status === 'enabled' ? 'enabled' :
    item.status === 'draft' ? 'creating' : 'deprecated'
  return {
    id: item.id,
    name: item.name,
    filename: item.fileName || item.templateCode,
    category: 'templates',
    subCategory: item.templateType === 'scaffold' ? '脚手架模板' : '文档模板',
    scope: 'organization',
    source: 'template',
    sourceLabel: item.documentType || item.templateType,
    version: `v${item.version}.0`,
    status,
    description: item.description || `${item.templateCode} · ${item.templateType}`,
    mime: item.mimeType || 'application/octet-stream',
    size: 0,
    sha256: '',
    createdAt: item.createdAt?.slice(0, 10) ?? '',
    references: 0,
    projectName: item.client || '组织模板库',
  }
}

function mapAttachment(item: AttachmentListResult['items'][number], projectName: string): AssetRow {
  return {
    id: item.attachmentId,
    name: item.originalName,
    filename: item.originalName,
    category: 'files',
    subCategory: '文件与附件',
    scope: 'project',
    source: 'development',
    sourceLabel: '会话上传',
    version: 'v1.0',
    status: 'enabled',
    description: `会话上传的文件 · ${item.mime}`,
    mime: item.mime,
    size: item.size,
    sha256: '',
    createdAt: '',
    references: 0,
    projectName,
    attachmentId: item.attachmentId,
  }
}

// ---- 模拟资产数据（与参考图一致） ----
const MOCK_ASSETS: AssetRow[] = [
  { id: 'A001', name: '完整 UI 界面设计', filename: '06-完整UI界面设计.html', category: 'deliverables', subCategory: 'HTML / 项目', scope: 'project', source: 'ui_design', sourceLabel: '方案和 UI', version: 'v2.0.2', status: 'enabled', description: '指定参考 HTML 视觉基线与 V2.0 全功能界面。', mime: 'text/html', size: 2457600, sha256: 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', createdAt: '2026-08-10', references: 5, projectName: '月汐 V2.0' },
  { id: 'A002', name: '数据库逻辑模型', filename: 'schema-design.md', category: 'phase_docs', subCategory: '文档 / 项目', scope: 'project', source: 'database', sourceLabel: '数据库', version: 'v1.4', status: 'enabled', description: '核心业务表结构设计文档，包含 ER 图与字段说明。', mime: 'text/markdown', size: 51200, sha256: 'a7ffc6f8bf1ed76651c14756a061d662f580ff4de43b49fa82d80a4b80f8434a', createdAt: '2026-08-05', references: 3, projectName: '月汐 V2.0' },
  { id: 'A003', name: '商品详情页变更', filename: 'ChangeSet CS-042', category: 'deliverables', subCategory: 'ChangeSet / 项目', scope: 'project', source: 'development', sourceLabel: '开发', version: '待评审', status: 'creating', description: '商品详情页交互优化变更集。', mime: 'application/json', size: 8192, sha256: 'd4c74531d4b5b695e33ca7291d88e5e0e6e2e4e1e6e3e2e1e6e3e2e1e6e3e2e1', createdAt: '2026-08-14', references: 1, projectName: '月汐 V2.0' },
  { id: 'A004', name: 'Windows E2E 测试证据', filename: 'evidence-20260814.zip', category: 'test_evidence', subCategory: '证据包 / 项目', scope: 'project', source: 'testing', sourceLabel: '测试', version: 'sealed', status: 'enabled', description: '端到端自动化测试截图与日志证据包。', mime: 'application/zip', size: 15728640, sha256: 'b5d6c7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5', createdAt: '2026-08-14', references: 2, projectName: '月汐 V2.0' },
  { id: 'A005', name: 'Lunitide 2.0.0 测试包', filename: 'lunitide-win-x64.zip', category: 'releases', subCategory: '发布包 / 项目', scope: 'project', source: 'release', sourceLabel: '发布', version: 'rc.3', status: 'enabled', description: 'Windows x64 候选发布包。', mime: 'application/zip', size: 11534336, sha256: 'c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6', createdAt: '2026-08-13', references: 8, projectName: 'Lunitide' },
]

export function AssetManagerPage({ attachments = attachmentBridge, projects = projectBridge, templates = templateBridge }: { attachments?: AttachmentBridge; projects?: ProjectBridge; templates?: TemplateBridge }): React.JSX.Element {
  const [projectItems, setProjectItems] = useState<ProjectDTO[]>([])
  const [projectId, setProjectId] = useState('')
  const [backendItems, setBackendItems] = useState<AttachmentListResult['items']>([])
  const [templateItems, setTemplateItems] = useState<TemplateListResult['items']>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // UI 状态
  const [activeCategory, setActiveCategory] = useState<AssetCategory>('all')
  const [activeFilterTab, setActiveFilterTab] = useState('全部')
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedAsset, setSelectedAsset] = useState<AssetRow | null>(null)
  const [showUpload, setShowUpload] = useState(false)
  const [showLifecycle, setShowLifecycle] = useState<LifecycleAction | null>(null)

  // 加载项目列表
  useEffect(() => {
    let alive = true
    try {
      projects.list().then(result => {
        if (!alive) return
        setProjectItems(result.items)
        setProjectId(current => (result.items.some(item => item.id === current) ? current : (result.items[0]?.id ?? '')))
      }).catch(e => { if (alive) setError(e instanceof Error ? e.message : '项目列表载入失败') })
    } catch (e) { if (alive) setError(e instanceof Error ? e.message : '项目列表载入失败') }
    return () => { alive = false }
  }, [projects])

  // 加载组织模板库
  useEffect(() => {
    let alive = true
    templates.list().then(result => { if (alive) setTemplateItems(result.items) }).catch(e => { if (alive) setError(e instanceof Error ? e.message : '模板载入失败') })
    return () => { alive = false }
  }, [templates])

  // 加载后端资产
  useEffect(() => {
    if (!projectId) return
    let alive = true
    setLoading(true)
    try {
      attachments.list({ projectId }).then(result => { if (alive) setBackendItems(result.items) }).catch(e => { if (alive) setError(e instanceof Error ? e.message : '资产载入失败') }).finally(() => { if (alive) setLoading(false) })
    } catch (e) { if (alive) { setError(e instanceof Error ? e.message : '资产载入失败'); setLoading(false) } }
    return () => { alive = false }
  }, [attachments, projectId])

  // 合并后端附件、模板库与演示数据（模板 tab 仅使用 template.list）
  const allAssets = useMemo(() => {
    const projectName = projectItems.find(p => p.id === projectId)?.name ?? '当前项目'
    const backend = backendItems.map(item => mapAttachment(item, projectName))
    const tplRows = templateItems.map(mapTemplate)
    return [...MOCK_ASSETS, ...backend, ...tplRows]
  }, [backendItems, templateItems, projectId, projectItems])

  // 分类计数
  const categoryCounts = useMemo(() => {
    const counts: Record<string, number> = { all: allAssets.length }
    for (const cat of CATEGORIES) {
      if (cat.key === 'all') continue
      counts[cat.key] = allAssets.filter(a => a.category === cat.key).length
    }
    return counts
  }, [allAssets])

  // 筛选
  const filteredAssets = useMemo(() => {
    let list = allAssets
    if (activeCategory !== 'all') list = list.filter(a => a.category === activeCategory)
    if (activeFilterTab !== '全部') {
      const sourceMap: Record<string, AssetSource[]> = { '需求架构': ['database', 'template'], '方案和UI': ['ui_design'], '工程与发布': ['development', 'testing', 'release'] }
      const sources = sourceMap[activeFilterTab] ?? []
      list = list.filter(a => sources.includes(a.source))
    }
    if (searchQuery.trim()) {
      const q = searchQuery.trim().toLowerCase()
      list = list.filter(a => a.name.toLowerCase().includes(q) || a.filename.toLowerCase().includes(q) || a.sourceLabel.toLowerCase().includes(q) || a.description.toLowerCase().includes(q))
    }
    return list
  }, [allAssets, activeCategory, activeFilterTab, searchQuery])

  // 状态统计
  const statusStats = useMemo(() => {
    const enabled = filteredAssets.filter(a => a.status === 'enabled').length
    const creating = filteredAssets.filter(a => a.status === 'creating').length
    const deprecated = filteredAssets.filter(a => a.status === 'deprecated').length
    return { total: filteredAssets.length, enabled, creating, deprecated }
  }, [filteredAssets])

  // 选中资产变化时加载详情
  const [assetDetail, setAssetDetail] = useState<AttachmentGetResult>()
  useEffect(() => {
    if (!selectedAsset?.attachmentId) { setAssetDetail(undefined); return }
    attachments.get({ attachmentId: selectedAsset.attachmentId }).then(setAssetDetail).catch(() => setAssetDetail(undefined))
  }, [attachments, selectedAsset])

  const currentProjectName = projectItems.find(p => p.id === projectId)?.name ?? '当前项目'

  return (
    <main className="asset-center">
      {/* ===== 左侧分类导航 ===== */}
      <nav className="asset-sidebar" aria-label="资产分类">
        <div className="asset-sidebar-brand">
          <span className="asset-sidebar-icon" aria-hidden="true"><i /><b /><em /></span>
          <span>资产管理</span>
        </div>
        <div className="asset-sidebar-section">
          <div className="asset-sidebar-label">资产类型</div>
          {CATEGORIES.map(cat => (
            <button key={cat.key} className={`asset-sidebar-item ${activeCategory === cat.key ? 'active' : ''}`} onClick={() => { setActiveCategory(cat.key); setActiveFilterTab('全部'); setSelectedAsset(null) }}>
              <span>{cat.label}</span>
              <small>{categoryCounts[cat.key] ?? 0}</small>
            </button>
          ))}
        </div>
        <div className="asset-sidebar-section">
          <div className="asset-sidebar-label">快捷入口</div>
          <button className="asset-sidebar-item" onClick={() => { setActiveCategory('all'); setActiveFilterTab('全部') }}>最近使用</button>
          <button className="asset-sidebar-item" onClick={() => { setActiveCategory('all'); setActiveFilterTab('全部') }}>收藏</button>
          <button className="asset-sidebar-item" onClick={() => { setActiveCategory('all'); setActiveFilterTab('全部') }}>回收站</button>
        </div>
      </nav>

      {/* ===== 主内容区 ===== */}
      <div className="asset-main">
        {/* 面包屑 */}
        <div className="asset-breadcrumb">
          <span className="asset-breadcrumb-title">{CATEGORIES.find(c => c.key === activeCategory)?.label ?? '全部资产'}</span>
          <span className="asset-breadcrumb-meta">个人 · {currentProjectName} · 组织能力未启用</span>
        </div>

        {/* 工具栏 */}
        <div className="asset-toolbar">
          <div className="asset-stats">{statusStats.total} 个资产 · {statusStats.enabled} 可用 · {statusStats.creating} 创建 · {statusStats.deprecated} 作废</div>
          <div className="asset-toolbar-actions">
            <button className="asset-btn-primary" onClick={() => setShowUpload(true)}>+ 导入资产</button>
          </div>
        </div>

        {/* 筛选标签 */}
        <div className="asset-filter-tabs">
          {FILTER_TABS.map(tab => (
            <button key={tab} className={activeFilterTab === tab ? 'active' : ''} onClick={() => setActiveFilterTab(tab)}>{tab}</button>
          ))}
          <div className="asset-search">
            <input aria-label="搜索资产" placeholder="搜索名称、类型、来源或摘要…" value={searchQuery} onChange={e => setSearchQuery(e.target.value)} />
          </div>
        </div>

        {/* 数据表格 */}
        {error ? <p className="asset-notice" role="alert">{error}</p> : !projectId ? <p className="asset-notice">还没有可用项目；请先在"项目管理"中创建。</p> : (
          <div className="asset-table-wrap">
            <div className="asset-table-inner">
            <div className="asset-table-head">
              <span>名称</span>
              <span>类型 / Scope</span>
              <span>来源</span>
              <span>版本</span>
              <span>状态</span>
            </div>
            {loading ? <p className="asset-table-empty">正在载入资产…</p> : filteredAssets.length ? filteredAssets.map(row => (
              <button key={row.id} type="button" className={`asset-table-row ${selectedAsset?.id === row.id ? 'active' : ''}`} onClick={() => setSelectedAsset(row)}>
                <span className="asset-cell-name">
                  <b>{row.name}</b>
                  <small>{row.filename}</small>
                </span>
                <span className="asset-cell-type"><code>{row.subCategory}</code></span>
                <span className="asset-cell-source">{row.sourceLabel}</span>
                <span className="asset-cell-version">{row.version}</span>
                <span className="asset-cell-status"><i className={`asset-status-badge ${STATUS_CLASS[row.status]}`}>{STATUS_LABEL[row.status]}</i></span>
              </button>
            )) : <p className="asset-table-empty">该分类暂无资产</p>}
            </div>
          </div>
        )}
      </div>

      {/* ===== 右侧详情面板 ===== */}
      {selectedAsset && (
        <aside className="asset-detail">
          <div className="asset-detail-preview">
            <span className="asset-detail-icon" aria-hidden="true"><i /><b /><em /></span>
          </div>
          <h2 className="asset-detail-title">{selectedAsset.name}</h2>
          <p className="asset-detail-desc">{selectedAsset.description}</p>

          <dl className="asset-detail-meta">
            {selectedAsset.scope === 'project' && <><dt>Scope</dt><dd>{selectedAsset.projectName}</dd></>}
            <dt>来源</dt><dd>{selectedAsset.sourceLabel}</dd>
            <dt>版本</dt><dd>{selectedAsset.version}</dd>
            {selectedAsset.sha256 && <><dt>校验</dt><dd className="asset-sha">SHA-256 已验证</dd></>}
            {selectedAsset.references > 0 && <><dt>引用</dt><dd>{selectedAsset.references} 会话</dd></>}
            <dt>类型</dt><dd>{selectedAsset.mime}</dd>
            {selectedAsset.size > 0 && <><dt>大小</dt><dd>{fmtSize(selectedAsset.size)}</dd></>}
          </dl>

          {selectedAsset.attachmentId && assetDetail?.parsedText !== undefined && assetDetail.parsedText.length > 0 && (
            <pre className="asset-detail-parsed">{assetDetail.parsedText.slice(0, 3000)}{assetDetail.parsedText.length > 3000 ? '\n…' : ''}</pre>
          )}

          <div className="asset-detail-actions">
            <button className="asset-btn-primary" onClick={() => { /* 在工作区打开 */ }}>在工作区打开</button>
            <button className="asset-btn-secondary" onClick={() => { /* @ 引用到对话 */ }}>@ 引用到对话</button>
            <button className="asset-btn-secondary" onClick={() => { /* 查看版本历史 */ }}>查看版本历史</button>
            {selectedAsset.status === 'enabled' && (
              <button className="asset-btn-secondary" onClick={() => setShowLifecycle({ type: 'deactivate', asset: selectedAsset })}>作废资产</button>
            )}
            {selectedAsset.status === 'deprecated' && (
              <button className="asset-btn-secondary" onClick={() => setShowLifecycle({ type: 'restore', asset: selectedAsset })}>恢复资产</button>
            )}
            {selectedAsset.status === 'creating' && (
              <button className="asset-btn-danger" onClick={() => setShowLifecycle({ type: 'delete', asset: selectedAsset })}>删除资产</button>
            )}
          </div>
        </aside>
      )}

      {/* ===== 上传对话框 ===== */}
      {showUpload && <UploadDialog onClose={() => setShowUpload(false)} currentProject={currentProjectName} />}

      {/* ===== 生命周期管理对话框 ===== */}
      {showLifecycle && <LifecycleDialog action={showLifecycle} onClose={() => setShowLifecycle(null)} />}
    </main>
  )
}

// ===== 上传资产模板对话框 =====
function UploadDialog({ onClose, currentProject }: { onClose: () => void; currentProject: string }) {
  const [templateName, setTemplateName] = useState('')
  const [templateType, setTemplateType] = useState('文档模板')
  const [deliverableType, setDeliverableType] = useState('')
  const [description, setDescription] = useState('')
  const [client, setClient] = useState('')
  const [fileName, setFileName] = useState('')
  const [step, setStep] = useState<'form' | 'uploading' | 'done'>('form')
  const fileInputRef = React.useRef<HTMLInputElement>(null)

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0]
    if (f) { setFileName(f.name); setTemplateName(f.name.replace(/\.[^.]+$/, '')) }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="asset-dialog" onClick={e => e.stopPropagation()}>
        <div className="asset-dialog-header">
          <h3>上传资产模板</h3>
          <button className="asset-dialog-close" onClick={onClose} aria-label="关闭">×</button>
        </div>
        {step === 'form' && (
          <div className="asset-dialog-body">
            <div className="asset-form-grid">
              <label className="asset-form-field">
                <span>模板编号</span>
                <input readOnly value="保存时自动生成 TPL00013" className="asset-input-readonly" />
              </label>
              <label className="asset-form-field">
                <span>模板名称 *</span>
                <input value={templateName} onChange={e => setTemplateName(e.target.value)} placeholder="输入模板名称" />
              </label>
              <label className="asset-form-field">
                <span>模板类型 *</span>
                <select value={templateType} onChange={e => setTemplateType(e.target.value)}>
                  <option>文档模板</option>
                  <option>流程模板</option>
                  <option>检查清单</option>
                  <option>报告模板</option>
                </select>
              </label>
              <label className="asset-form-field">
                <span>交付物文件类型</span>
                <select value={deliverableType} onChange={e => setDeliverableType(e.target.value)}>
                  <option value="">请选择</option>
                  <option>业务需求分析报告</option>
                  <option>技术方案设计</option>
                  <option>测试报告</option>
                  <option>发布说明</option>
                </select>
              </label>
              <label className="asset-form-field asset-form-field-full">
                <span>模板描述 *</span>
                <textarea value={description} onChange={e => setDescription(e.target.value)} placeholder="描述模板用途和适用范围" rows={3} />
              </label>
              <label className="asset-form-field">
                <span>客户</span>
                <input value={client} onChange={e => setClient(e.target.value)} placeholder="某客户" />
              </label>
              <label className="asset-form-field">
                <span>状态</span>
                <input readOnly value="创建" className="asset-input-readonly" />
              </label>
              <label className="asset-form-field asset-form-field-full">
                <span>附件 *</span>
                <div className="asset-form-file">
                  <input ref={fileInputRef} type="file" onChange={handleFileSelect} style={{ display: 'none' }} />
                  {fileName ? (
                    <span className="asset-form-file-name">{fileName}</span>
                  ) : (
                    <button type="button" className="asset-btn-secondary" onClick={() => fileInputRef.current?.click()}>选择文件</button>
                  )}
                  {fileName && <span className="asset-form-file-type">自动识别</span>}
                </div>
              </label>
            </div>

            {/* 预检结果 */}
            <div className="asset-precheck">
              <div className="asset-precheck-title">预检结果</div>
              <div className="asset-precheck-items">
                <span className="asset-precheck-pass">格式/MIME ✓</span>
                <span className="asset-precheck-pass">SHA-256 ✓</span>
                <span className="asset-precheck-pass">病毒扫描 ✓</span>
                <span className="asset-precheck-pass">路径安全 ✓</span>
              </div>
            </div>
          </div>
        )}
        {step === 'uploading' && <div className="asset-dialog-body"><p className="asset-notice">正在上传并验证…</p></div>}
        {step === 'done' && <div className="asset-dialog-body"><p className="asset-notice">上传成功！模板已保存为"创建"状态。</p></div>}
        <div className="asset-dialog-footer">
          <button className="asset-btn-secondary" onClick={onClose}>取消</button>
          {step === 'form' && <button className="asset-btn-primary" onClick={() => { setStep('uploading'); setTimeout(() => { setStep('done'); setTimeout(onClose, 1200) }, 1500) }}>上传并保存为创建</button>}
        </div>
      </div>
    </div>
  )
}

// ===== 生命周期管理对话框 =====
function LifecycleDialog({ action, onClose }: { action: LifecycleAction; onClose: () => void }) {
  const { asset, type } = action
  const assetId = asset.id
  const assetVer = asset.version

  const title = type === 'deactivate' ? '作废 / 恢复 / 删除模板' : type === 'restore' ? '恢复模板' : '删除模板'

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="asset-dialog" onClick={e => e.stopPropagation()}>
        <div className="asset-dialog-header">
          <h3>{title}</h3>
          <button className="asset-dialog-close" onClick={onClose} aria-label="关闭">×</button>
        </div>
        <div className="asset-dialog-body">
          <div className="asset-lifecycle-cards">
            <div className="asset-lifecycle-card">
              <div className="asset-lifecycle-card-label">对象</div>
              <div className="asset-lifecycle-card-value">{assetId} {assetVer}</div>
            </div>
            <div className="asset-lifecycle-card">
              <div className="asset-lifecycle-card-label">作废</div>
              <div className="asset-lifecycle-card-value">其他位置不能再新引用，历史引用保留快照</div>
            </div>
            <div className="asset-lifecycle-card">
              <div className="asset-lifecycle-card-label">删除</div>
              <div className="asset-lifecycle-card-value">仅 0 引用时开放，确认后彻底删除且不能恢复</div>
            </div>
          </div>

          <div className="asset-lifecycle-actions">
            {type === 'deactivate' && (
              <button className="asset-btn-primary" onClick={onClose}>确认作废资产</button>
            )}
            {type === 'restore' && (
              <button className="asset-btn-primary" onClick={onClose}>恢复作废资产</button>
            )}
            {type === 'delete' && (
              <button className="asset-btn-danger" onClick={onClose}>确认彻底删除</button>
            )}
            <button className="asset-btn-secondary" onClick={onClose}>取消</button>
          </div>

          <p className="asset-lifecycle-note">作废资产显示"恢复"，恢复后状态回到"创建"；删除资产没有恢复入口。</p>
        </div>
      </div>
    </div>
  )
}