import React, { useEffect, useState } from 'react'
import { ccBridge, type CcBridge } from '../bridge/client'
import type { CcGetAuditLogResult, CcGetConfigResult, CcUpdateConfigPayload } from '../generated/bridge'
import { Toggle } from './settingsControls'

// M10 wave-4 — 电脑控制设置：三步启用流（风险告知 → 安全级别 → 确认）、
// 安全级别 / 高危操作 / 进程黑名单（操作范围）/ 频率与确认超时、
// 紧急停止闩锁与操作审计台账（CSV 导出）。
type CcAuditRow = CcGetAuditLogResult['items'][number]
const CC_RISK_LABELS: Record<string, string> = { low: '低', medium: '中', high: '高', critical: '极高' }
const CC_STATUS_LABELS: Record<string, string> = { executed: '已执行', blocked: '已拦截', denied: '已拒绝', failed: '失败', stopped: '已停止' }
const CC_LAYER_LABELS: Record<string, string> = { intent: '意图识别', 'input-filter': '输入过滤', 'process-monitor': '进程监控' }
const CC_TOOL_LABELS: Record<string, string> = {
  'cc.mouse_move': '移动鼠标', 'cc.mouse_click': '点击鼠标', 'cc.mouse_drag': '拖拽',
  'cc.keyboard_type': '键盘输入',
  'cc.keyboard_shortcut': '组合键', 'cc.screen_capture': '全桌面截图', 'cc.get_active_window': '活动窗口',
  'cc.window_list': '窗口列表', 'cc.window_focus': '聚焦窗口',
  'cc.observe_dialog': '观察对话框', 'cc.confirm_dialog': '确认对话框',
  'cc.observe_ui': '界面节点', 'cc.wait': '等待界面', 'cc.clipboard': '剪贴板',
  'cc.window_action': '窗口操作', 'cc.app_list': '应用列表', 'cc.app_quit': '退出应用',
  'cc.paste': '粘贴', 'cc.press': '按键', 'cc.menu_click': '菜单', 'cc.set_value': '填写控件',
  'computer.act': '电脑操作',
}
const CC_LEVEL_META: Record<'standard' | 'strict', { label: string; desc: string }> = {
  standard: { label: '标准', desc: '高危操作（组合键等）需人工确认；极高风险默认拦截' },
  strict: { label: '严格', desc: '仅允许低/中风险操作；高危一律拦截，适合首次试用' },
}

export function ComputerPanel({ bridge = ccBridge }: { bridge?: CcBridge }): React.JSX.Element {
  const [settings, setSettings] = useState<CcGetConfigResult | null>(null)
  const [entries, setEntries] = useState<CcAuditRow[]>([])
  const [statusFilter, setStatusFilter] = useState('')
  const [status, setStatus] = useState('')
  const [busy, setBusy] = useState(false)
  // 三步启用向导：1 风险告知 → 2 安全级别 → 3 确认启用。
  const [wizard, setWizard] = useState<{ step: 1 | 2; agreed: boolean; level: 'standard' | 'strict'; allowCritical: boolean; timedArm: boolean } | null>(null)
  const [blockDraft, setBlockDraft] = useState('')
  const [rateDraft, setRateDraft] = useState('')
  const [timeoutDraft, setTimeoutDraft] = useState('')

  const refresh = async (filter = statusFilter) => {
    setBusy(true)
    try {
      const [cfg, log] = await Promise.all([
        bridge.getConfig(),
        bridge.getAuditLog({ limit: 50, ...(filter ? { status: filter as CcAuditRow['status'] } : {}) }),
      ])
      setSettings(cfg)
      setRateDraft(String(cfg.maxActionsPerMinute))
      setTimeoutDraft(String(cfg.confirmTimeoutSeconds))
      setEntries(log.items)
      setStatus('')
    } catch (e) {
      setStatus(e instanceof Error ? e.message : '电脑控制配置加载失败')
    } finally { setBusy(false) }
  }
  useEffect(() => { void refresh() }, [])

  const patch = async (p: CcUpdateConfigPayload, okMsg: string) => {
    setBusy(true); setStatus('')
    try {
      const cfg = await bridge.updateConfig(p)
      setSettings(cfg)
      setStatus(okMsg)
    } catch (e) { setStatus(e instanceof Error ? e.message : '设置更新失败') } finally { setBusy(false) }
  }

  const confirmEnable = async () => {
    if (!wizard) return
    setBusy(true); setStatus('')
    try {
      const cfg = await bridge.updateConfig({ enabled: true, securityLevel: wizard.level, allowCritical: wizard.allowCritical, armMinutes: wizard.timedArm ? 30 : 0 })
      setSettings(cfg)
      setWizard(null)
      setStatus('电脑控制已启用')
    } catch (e) { setStatus(e instanceof Error ? e.message : '启用失败') } finally { setBusy(false) }
  }

  const emergencyStop = async () => {
    if (!window.confirm('确认紧急停止？所有电脑控制操作将立即失效，直到重新走启用流程。')) return
    setBusy(true); setStatus('')
    try {
      const cfg = await bridge.emergencyStop({ actor: 'renderer', reason: '设置页手动急停' })
      setSettings(cfg)
      setStatus('紧急停止已激活')
    } catch (e) { setStatus(e instanceof Error ? e.message : '紧急停止失败') } finally { setBusy(false) }
  }

  const addBlock = async () => {
    if (!settings) return
    const entry = blockDraft.trim().toLowerCase()
    if (!entry) return
    if (entry.includes('/') || entry.includes('\\')) { setStatus('黑名单条目为进程名（如 cmd.exe），不能含路径分隔符'); return }
    if (settings.processBlocklist.includes(entry)) { setStatus('条目已存在'); return }
    await patch({ processBlocklist: [...settings.processBlocklist, entry] }, `已添加 ${entry}`)
    setBlockDraft('')
  }
  const removeBlock = async (entry: string) => {
    if (!settings) return
    await patch({ processBlocklist: settings.processBlocklist.filter(x => x !== entry) }, `已移除 ${entry}`)
  }

  const exportCsv = () => {
    const header = '时间,会话,工具,动作,风险,状态,拦截层,详情'
    const esc = (v: string) => `"${v.replace(/"/g, '""')}"`
    const lines = entries.map(e => [
      e.createdAt, e.sessionId, CC_TOOL_LABELS[e.tool] ?? e.tool, e.action,
      CC_RISK_LABELS[e.riskLevel] ?? e.riskLevel, CC_STATUS_LABELS[e.status] ?? e.status,
      e.layer ? CC_LAYER_LABELS[e.layer] ?? e.layer : '', e.detail,
    ].map(esc).join(','))
    const blob = new Blob(['\ufeff' + [header, ...lines].join('\r\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `cc-audit-${new Date().toISOString().slice(0, 10)}.csv`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="setting-group">
      <div className="setting-group-title">电脑控制</div>
      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
          <div className="setting-desc">允许模型操作本机：截图 / 界面节点 / 对话框 / 窗口列表与聚焦 / 鼠标（点击、拖拽、滚轮）/ 键盘（对指定应用先聚焦再输入）/ 剪贴板（纯文本）。点击坐标与截图像素一致；点按钮优先用界面节点或对话框观察，不要盲点。所有操作经过意图识别、输入过滤、进程监控三层拦截，并写入不可篡改的审计台账。紧急停止后月伴不会自动重新打开。</div>
        {settings && (
          <div className="setting-label" style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
            <span>当前状态：</span>
            <span style={{ color: settings.emergencyStopped ? 'var(--red)' : settings.enabled ? 'var(--ok)' : 'var(--muted)', fontWeight: 600 }}>
              {settings.emergencyStopped ? '紧急停止' : settings.enabled ? '已启用' : '已停用'}
            </span>
            {settings.enabled && settings.armedUntil && (
              <span className="setting-desc">将于 {new Date(settings.armedUntil).toLocaleString()} 自动关闭</span>
            )}
          </div>
        )}
        {settings?.emergencyStopped && (
          <p role="alert" className="notice" style={{ color: 'var(--red)' }}>紧急停止已激活（{settings.emergencyStoppedAt ? new Date(settings.emergencyStoppedAt).toLocaleString() : ''}）：所有电脑控制操作一律拒绝，需重新走启用流程。</p>
        )}
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          {settings && !settings.enabled && !wizard && <button disabled={busy} onClick={() => setWizard({ step: 1, agreed: false, level: 'standard', allowCritical: false, timedArm: true })}>三步启用…</button>}
          {settings?.enabled && <button disabled={busy} onClick={() => void patch({ enabled: false }, '电脑控制已停用')}>停用</button>}
          {settings?.enabled && !settings.emergencyStopped && <button disabled={busy} onClick={() => void emergencyStop()} style={{ color: 'var(--red)' }}>紧急停止</button>}
        </div>
      </div>

      {wizard && (
        <div className="setting-row" style={{ gridTemplateColumns: '1fr', border: '1px solid var(--rule)', borderRadius: 8, padding: 12 }}>
          <div className="setting-group-title">启用电脑控制（步骤 {wizard.step}/2）</div>
          {wizard.step === 1 && (
            <>
              <div className="setting-desc">电脑控制会授予模型操作本机输入的能力。请确认：</div>
              <ul className="setting-desc" style={{ margin: '0 0 0 18px', display: 'grid', gap: 4 }}>
                <li>高危操作（组合键、关闭窗口等）默认需要人工逐次确认；</li>
                <li>cmd / powershell / regedit 等系统进程默认在黑名单中，不会被操作；</li>
                <li>随时可在会话中或本页执行「紧急停止」，立即熔断全部操作。</li>
              </ul>
              <label style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 13 }}>
                <input type="checkbox" checked={wizard.agreed} onChange={ev => setWizard({ ...wizard, agreed: ev.target.checked })} aria-label="已了解风险" />
                我已了解上述风险与防护机制
              </label>
              <div style={{ display: 'flex', gap: 8 }}>
                <button disabled={!wizard.agreed} onClick={() => setWizard({ ...wizard, step: 2 })}>下一步：选择安全级别</button>
                <button onClick={() => setWizard(null)}>取消</button>
              </div>
            </>
          )}
          {wizard.step === 2 && (
            <>
              <div className="br-mode-grid" role="radiogroup" aria-label="电脑控制安全级别">
                {(['standard', 'strict'] as const).map(level => (
                  <button key={level} type="button" role="radio" aria-checked={wizard.level === level}
                    className={`br-mode-card ${wizard.level === level ? 'on' : ''}`}
                    onClick={() => setWizard({ ...wizard, level })}>
                    <b>{CC_LEVEL_META[level].label}</b>
                    <small>{CC_LEVEL_META[level].desc}</small>
                  </button>
                ))}
              </div>
              <label style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 13 }}>
                <input type="checkbox" checked={wizard.allowCritical} onChange={ev => setWizard({ ...wizard, allowCritical: ev.target.checked })} aria-label="允许极高风险操作" />
                允许极高风险操作（Alt+F4 / Win+R 等，仍需逐次人工确认；不建议开启）
              </label>
              <label style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 13 }}>
                <input type="checkbox" checked={wizard.timedArm} onChange={ev => setWizard({ ...wizard, timedArm: ev.target.checked })} aria-label="30 分钟后自动关闭" />
                30 分钟后自动关闭（可取消，改为一直开到手动停用）
              </label>
              <div style={{ display: 'flex', gap: 8 }}>
                <button disabled={busy} onClick={() => void confirmEnable()}>确认启用</button>
                <button disabled={busy} onClick={() => setWizard({ ...wizard, step: 1 })}>上一步</button>
                <button disabled={busy} onClick={() => setWizard(null)}>取消</button>
              </div>
            </>
          )}
        </div>
      )}

      {settings?.enabled && (
        <>
          <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
            <div className="setting-group-title" style={{ marginTop: 8 }}>安全级别</div>
            <div className="br-mode-grid" role="radiogroup" aria-label="电脑控制安全级别">
              {(['standard', 'strict'] as const).map(level => (
                <button key={level} type="button" role="radio" aria-checked={settings.securityLevel === level}
                  className={`br-mode-card ${settings.securityLevel === level ? 'on' : ''}`}
                  onClick={() => void patch({ securityLevel: level }, `安全级别已切换为 ${CC_LEVEL_META[level].label}`)}>
                  <b>{CC_LEVEL_META[level].label}</b>
                  <small>{CC_LEVEL_META[level].desc}</small>
                </button>
              ))}
            </div>
            <Toggle on={settings.allowCritical} onChange={v => void patch({ allowCritical: v }, v ? '已允许极高风险操作（仍需逐次确认）' : '已禁止极高风险操作')} label="允许极高风险操作" desc="Alt+F4 / Win+R / Win+L 等系统级组合键；开启后每次执行仍需人工确认" />
          </div>

          <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
            <div className="setting-group-title" style={{ marginTop: 8 }}>操作范围{settings.processBlocklist.length > 0 ? `（黑名单 ${settings.processBlocklist.length}/128）` : ''}</div>
            <div className="setting-desc">前台进程命中黑名单时，鼠标 / 键盘 / 截图操作一律拦截（M10-CC-009）。条目为进程名，不区分大小写。</div>
            <div style={{ display: 'flex', gap: 8, maxWidth: 560 }}>
              <input className="setting-input" style={{ flex: 1, fontFamily: 'var(--mono)', fontSize: 12 }} placeholder="例如 taskmgr.exe" value={blockDraft} onChange={ev => setBlockDraft(ev.target.value)} aria-label="黑名单新条目" onKeyDown={ev => { if (ev.key === 'Enter') { ev.preventDefault(); void addBlock() } }} />
              <button disabled={busy || !blockDraft.trim()} onClick={() => void addBlock()}>添加</button>
            </div>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', maxWidth: 640 }}>
              {settings.processBlocklist.map(entry => (
                <code key={entry} style={{ display: 'inline-flex', gap: 6, alignItems: 'center', border: '1px solid var(--rule)', borderRadius: 6, padding: '2px 8px', fontFamily: 'var(--mono)', fontSize: 12 }}>
                  {entry}
                  <button disabled={busy} style={{ border: 'none', background: 'none', cursor: 'pointer', padding: 0, color: 'var(--muted)' }} aria-label={`移除 ${entry}`} onClick={() => void removeBlock(entry)}>✕</button>
                </code>
              ))}
            </div>
          </div>

          <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
            <div className="setting-group-title" style={{ marginTop: 8 }}>频率与确认</div>
            <label style={{ display: 'grid', gap: 4, fontSize: 12, color: 'var(--muted)', maxWidth: 560 }}>每分钟最大操作数（1–120，默认 60）
              <div style={{ display: 'flex', gap: 8 }}>
                <input className="setting-input" style={{ width: 120, fontFamily: 'var(--mono)' }} inputMode="numeric" value={rateDraft} onChange={ev => setRateDraft(ev.target.value.replace(/\D/g, ''))} aria-label="每分钟最大操作数" />
                <button disabled={busy || rateDraft === '' || Number(rateDraft) === settings.maxActionsPerMinute} onClick={() => void patch({ maxActionsPerMinute: Number(rateDraft) }, '频率上限已保存')}>保存</button>
              </div>
            </label>
            <label style={{ display: 'grid', gap: 4, fontSize: 12, color: 'var(--muted)', maxWidth: 560 }}>高危确认超时（秒，10–600，默认 60）
              <div style={{ display: 'flex', gap: 8 }}>
                <input className="setting-input" style={{ width: 120, fontFamily: 'var(--mono)' }} inputMode="numeric" value={timeoutDraft} onChange={ev => setTimeoutDraft(ev.target.value.replace(/\D/g, ''))} aria-label="高危确认超时秒数" />
                <button disabled={busy || timeoutDraft === '' || Number(timeoutDraft) === settings.confirmTimeoutSeconds} onClick={() => void patch({ confirmTimeoutSeconds: Number(timeoutDraft) }, '确认超时已保存')}>保存</button>
              </div>
            </label>
          </div>
        </>
      )}

      <div className="setting-row" style={{ gridTemplateColumns: '1fr' }}>
        <div className="setting-group-title" style={{ marginTop: 8 }}>操作审计（最近 {entries.length} 条）</div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
          <select className="setting-input" style={{ width: 160 }} value={statusFilter} onChange={ev => { setStatusFilter(ev.target.value); void refresh(ev.target.value) }} aria-label="状态筛选">
            <option value="">全部状态</option>
            <option value="executed">已执行</option>
            <option value="blocked">已拦截</option>
            <option value="denied">已拒绝</option>
            <option value="failed">失败</option>
            <option value="stopped">已停止</option>
          </select>
          <button disabled={busy} onClick={() => void refresh()}>刷新</button>
          <button disabled={busy || entries.length === 0} onClick={exportCsv}>导出 CSV</button>
        </div>
        {entries.length === 0 && <div className="setting-desc">暂无操作记录。</div>}
        {entries.length > 0 && (
          <div style={{ overflowX: 'auto' }}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
              <thead>
                <tr style={{ textAlign: 'left', color: 'var(--muted)' }}>
                  <th style={{ padding: '6px 10px 6px 0' }}>时间</th>
                  <th style={{ padding: '6px 10px 6px 0' }}>工具</th>
                  <th style={{ padding: '6px 10px 6px 0' }}>风险</th>
                  <th style={{ padding: '6px 10px 6px 0' }}>状态</th>
                  <th style={{ padding: '6px 10px 6px 0' }}>拦截层</th>
                  <th style={{ padding: '6px 10px 6px 0' }}>详情</th>
                </tr>
              </thead>
              <tbody>
                {entries.map(e => (
                  <tr key={e.entryId} style={{ borderTop: '1px solid var(--rule)' }}>
                    <td style={{ padding: '6px 10px 6px 0', whiteSpace: 'nowrap', fontFamily: 'var(--mono)' }}>{new Date(e.createdAt).toLocaleString()}</td>
                    <td style={{ padding: '6px 10px 6px 0' }}>{CC_TOOL_LABELS[e.tool] ?? e.tool}</td>
                    <td style={{ padding: '6px 10px 6px 0' }}>{CC_RISK_LABELS[e.riskLevel] ?? e.riskLevel}</td>
                    <td style={{ padding: '6px 10px 6px 0', color: e.status === 'executed' ? 'var(--ok)' : e.status === 'blocked' || e.status === 'stopped' ? 'var(--red)' : 'var(--muted)' }}>{CC_STATUS_LABELS[e.status] ?? e.status}</td>
                    <td style={{ padding: '6px 10px 6px 0' }}>{e.layer ? CC_LAYER_LABELS[e.layer] ?? e.layer : '—'}</td>
                    <td style={{ padding: '6px 0', overflowWrap: 'anywhere', fontFamily: 'var(--mono)', fontSize: 11, color: 'var(--muted)' }}>{e.detail}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
      {status && <p role="status" className="notice">{status}</p>}
    </div>
  )
}
