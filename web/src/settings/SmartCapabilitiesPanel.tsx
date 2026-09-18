import React, { useCallback, useEffect, useState } from 'react'
import { createMutationAttempt, getIdentityBridge, getMemoryBridge, memoryOpsBridge, newBridgeULID, type IdentityBridge, type MemoryBridge, type MemoryOpsBridge } from '../bridge/client'

type CaptureMode = 'auto' | 'manual' | 'off'

function userError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

function MemoryCaptureCard({
  ops = memoryOpsBridge,
  identity = getIdentityBridge(),
  memory = getMemoryBridge(),
  onOpenMemory,
}: {
  ops?: MemoryOpsBridge
  identity?: IdentityBridge
  memory?: MemoryBridge
  onOpenMemory?: () => void
}): React.JSX.Element {
  const [subjectId, setSubjectId] = useState('')
  const [mode, setMode] = useState<CaptureMode>('auto')
  const [version, setVersion] = useState('')
  const [autoNominate, setAutoNominate] = useState(false)
  const [growthDays, setGrowthDays] = useState(14)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [explicitText, setExplicitText] = useState('')
  const [explicitBusy, setExplicitBusy] = useState(false)

  const load = useCallback(async (id: string) => {
    if (!id) return
    try {
      const r = await ops.getSettings({ subjectId: id })
      if (!r.version) throw new Error('设置版本不可用')
      const nextMode = r.memoryEnabled === false ? 'off' : (r.captureMode ?? 'auto')
      setMode(nextMode)
      setAutoNominate(r.autoNominate)
      setGrowthDays(r.growthDays)
      setVersion(r.version)
      setError('')
    } catch (e) {
      setError(userError(e, '记忆设置载入失败'))
    }
  }, [ops])

  useEffect(() => {
    let alive = true
    void identity.get().then(value => {
      if (!alive) return
      const id = value.subjectId || 'local-user'
      setSubjectId(id)
      void load(id)
    }).catch(() => {
      if (!alive) return
      setSubjectId('local-user')
      void load('local-user')
    })
    return () => { alive = false }
  }, [identity, load])

  const save = async (next: CaptureMode) => {
    if (busy || !version || !subjectId) return
    setBusy(true)
    setNotice('')
    setError('')
    try {
      const result = await ops.updateSettings({
        subjectId,
        captureMode: next,
        memoryEnabled: next !== 'off',
        autoNominate,
        growthDays,
        expectedVersion: version,
      })
      setMode(result.memoryEnabled === false ? 'off' : (result.captureMode ?? next))
      setVersion(result.version)
      setNotice('记忆设置已保存')
    } catch (e) {
      setError(userError(e, '记忆设置保存失败'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <p className="setting-desc">自动筛选稳定偏好、身份事实和明确决定。问候、天气、一次性命令和秘密不会进入长期记忆。关闭后停止捕获、搜索、召回和注入，已有数据保留。</p>
      <div className="smart-cap-modes" role="radiogroup" aria-label="自动记忆方式">
        {([
          ['auto', '自动', '捕获并召回'],
          ['manual', '手动', '仅显式保存，仍可召回'],
          ['off', '关闭', '不捕获、不召回'],
        ] as const).map(([value, label, hint]) => (
          <button
            key={value}
            type="button"
            role="radio"
            aria-checked={mode === value}
            className={mode === value ? 'smart-cap-mode on' : 'smart-cap-mode'}
            disabled={busy || !version}
            onClick={() => void save(value)}
          >
            <strong>{label}</strong>
            <span>{hint}</span>
          </button>
        ))}
      </div>
      {error ? <p role="alert">{error}</p> : null}
      {notice ? <p role="status">{notice}</p> : null}
      {mode !== 'off' ? (
        <form
          className="smart-cap-explicit"
          onSubmit={event => {
            event.preventDefault()
            if (explicitBusy || !subjectId) return
            const text = explicitText.trim()
            if (!text) return
            setExplicitBusy(true)
            setError('')
            setNotice('')
            void (async () => {
              try {
                if (!memory.itemCreate) {
                  throw new Error('保存记忆失败')
                }
                const payload = { scopeKind: 'user' as const, text, operationId: newBridgeULID() }
                const attempt = createMutationAttempt('memory.item.create', payload)
                await memory.itemCreate(payload, { attempt })
                setExplicitText('')
                setNotice('已保存为记忆')
              } catch (e) {
                setError(userError(e, '保存记忆失败'))
              } finally {
                setExplicitBusy(false)
              }
            })()
          }}
        >
          <label>
            保存为记忆
            <textarea
              aria-label="保存为记忆"
              value={explicitText}
              disabled={explicitBusy}
              onChange={event => setExplicitText(event.target.value)}
              rows={2}
              placeholder="只写稳定偏好或身份事实，例如：我喜欢简洁的回答"
            />
          </label>
          <button type="submit" disabled={explicitBusy || !explicitText.trim()}>{explicitBusy ? '保存中…' : '保存'}</button>
        </form>
      ) : null}
      <button type="button" className="smart-cap-disclose" onClick={onOpenMemory}>
        管理已保存记忆与隐私
      </button>
    </>
  )
}

export function SmartCapabilitiesPanel({
  memoryOps,
  identity,
  memory,
  onOpenMemory,
  onOpenOCR,
}: {
  memoryOps?: MemoryOpsBridge
  identity?: IdentityBridge
  memory?: MemoryBridge
  onOpenMemory?: () => void
  onOpenOCR?: () => void
}): React.JSX.Element {
  return (
    <div className="smart-cap">
      <p className="setting-desc">首页只保留两张能力卡。色调沿用现有黑白界面，青绿仅用于焦点。</p>
      <section className="smart-cap-card" aria-labelledby="smart-mem-title">
        <h3 id="smart-mem-title">自动记忆</h3>
        <MemoryCaptureCard ops={memoryOps} identity={identity} memory={memory} onOpenMemory={onOpenMemory} />
      </section>
      <section className="smart-cap-card" aria-labelledby="smart-ocr-title">
        <h3 id="smart-ocr-title">文字识别</h3>
        <p className="setting-desc">已配置的视觉模型能用就先用，然后走本机 RapidOCR，最后用 Windows OCR 兜底。复杂文档增强当前不可用，不会在首页安装可选包。</p>
        <p>文字识别：自动</p>
        <button type="button" className="smart-cap-disclose" onClick={onOpenOCR}>打开文字识别</button>
      </section>
    </div>
  )
}
