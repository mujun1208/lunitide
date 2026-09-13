import React, { useCallback, useEffect, useState } from 'react'
import { asUserBridgeError } from '../bridge/bridgeUserError'
import { BridgeClientError } from '../bridge/client'
import type { ProjectDTO } from '../generated/bridge'
import { formatBoardStats, summarizeBoardItems, type BoardStats } from './boardStats'
import { projectFactoryApi } from './projectFactoryApi'
import { projectSpineApi } from './projectSpineApi'

function boardUserError(err: unknown, fallback: string): string {
  if (err instanceof BridgeClientError) return asUserBridgeError(err, fallback).message
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

type BoardItem = {
  id: string
  title: string
  status: string
  needsReprocess?: boolean
  changeKind?: string
  sourceKind?: string
  selfTestPass?: boolean
  lastResultSummary?: string
  method?: string
  path?: string
  memberIds?: string[]
}

type BoardDoc = { version: number; items: BoardItem[] }

export function WorkBoardPanel({
  project,
  boardKind,
  title,
  readOnly = false,
  onOpenItem,
  onBrief,
  currentItemId,
}: {
  project: ProjectDTO
  boardKind: 'interface' | 'dev' | 'test' | 'integration'
  title: string
  readOnly?: boolean
  onOpenItem?: (itemId: string) => void
  onBrief?: (text: string) => void
  currentItemId?: string
}): React.JSX.Element {
  const [board, setBoard] = useState<BoardDoc>({ version: 1, items: [] })
  const [stats, setStats] = useState<BoardStats>()
  const [statsText, setStatsText] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [selfTest, setSelfTest] = useState<Record<string, boolean>>({})
  const [summary, setSummary] = useState<Record<string, string>>({})
  const [evidence, setEvidence] = useState<Record<string, string>>({})
  const [reason, setReason] = useState<Record<string, string>>({})

  const load = useCallback(async () => {
    setError('')
    try {
      const got = await projectFactoryApi.boardGet({ projectId: project.id, boardKind })
      const next = (got.board ?? { version: 1, items: [] }) as BoardDoc
      next.items = next.items ?? []
      setBoard(next)
      setStatsText(got.statsText || formatBoardStats(summarizeBoardItems(next.items)))
      setStats(got.stats as BoardStats)
    } catch (e) {
      setError(boardUserError(e, '无法读取工作台'))
    }
  }, [project.id, boardKind])

  useEffect(() => { void load() }, [load])

  const sync = async () => {
    if (readOnly || busy) return
    setBusy(true)
    setError('')
    try {
      const got = await projectFactoryApi.boardSync({ projectId: project.id, boardKind })
      setStatsText(got.statsText)
      setStats(got.stats as BoardStats)
      await load()
    } catch (e) {
      setError(boardUserError(e, '同步失败'))
    } finally {
      setBusy(false)
    }
  }

  const open = async (id: string) => {
    if (readOnly || busy) return
    setBusy(true)
    try {
      const opened = await projectFactoryApi.boardItemOpen({ projectId: project.id, boardKind, itemId: id })
      onBrief?.(opened.brief?.text ?? '')
      onOpenItem?.(id)
      await load()
    } catch (e) {
      setError(boardUserError(e, '无法打开条目'))
    } finally {
      setBusy(false)
    }
  }

  const complete = async (id: string) => {
    if (readOnly || busy) return
    if (!selfTest[id] || !summary[id]?.trim()) {
      setError('请先勾选自测并填写结果摘要')
      return
    }
    setBusy(true)
    setError('')
    try {
      const items = board.items.map(item => item.id === id
        ? { ...item, status: 'dev_done', selfTestPass: true, lastResultSummary: summary[id].trim() }
        : item)
      await projectFactoryApi.boardPut({ projectId: project.id, boardKind, board: { ...board, items } })
      await load()
    } catch (e) {
      setError(boardUserError(e, '无法完成条目'))
    } finally {
      setBusy(false)
    }
  }

  const runUnit = async (id: string, failed = false) => {
    if (readOnly || busy) return
    const note = (evidence[id] ?? '').trim()
    if (!note) {
      setError('请先填写测试记录')
      return
    }
    setBusy(true)
    setError('')
    try {
      await projectFactoryApi.testRun({
        projectId: project.id,
        itemId: id,
        kind: 'unit',
        evidence: failed ? `fail: ${note}` : note,
      })
      await load()
    } catch (e) {
      setError(boardUserError(e, '无法记录测试'))
    } finally {
      setBusy(false)
    }
  }

  const sendBack = async (id: string) => {
    if (readOnly || busy) return
    const why = (reason[id] ?? '').trim()
    if (!why) {
      setError('退回需要填写原因')
      return
    }
    setBusy(true)
    setError('')
    try {
      await projectSpineApi.returnFromTest({ projectId: project.id, testItemId: id, reason: why })
      await load()
    } catch (e) {
      setError(boardUserError(e, '无法退回'))
    } finally {
      setBusy(false)
    }
  }

  const dirty = stats?.needsReprocess ?? board.items.filter(i => i.needsReprocess).length
  const platform = boardKind === 'interface' ? '接口' : boardKind === 'dev' ? '开发' : boardKind === 'test' ? '测试' : '集成'
  const processable = boardKind === 'interface' || boardKind === 'dev'
  const testable = boardKind === 'test' || boardKind === 'integration'

  return (
    <section className="checklist-panel" aria-label={title}>
      <header className="checklist-head">
        <div>
          <b>{title}</b>
          <small>{statsText || '尚未同步'}</small>
        </div>
        {!readOnly && <button type="button" disabled={busy} onClick={() => void sync()}>从清单同步</button>}
      </header>
      {dirty > 0 && <p className="error" role="status">{platform}清单已更新，{dirty} 条需要再次处理。</p>}
      {error && <p className="error" role="alert"><b>{error}</b></p>}
      {!board.items.length && <p className="checklist-empty">工作台为空，可从源清单同步。</p>}
      <ul className="checklist-list">
        {board.items.map(item => (
          <li key={item.id} className={currentItemId === item.id ? 'is-current' : undefined}>
            <button type="button" disabled={readOnly || busy} onClick={() => void open(item.id)}>
              {item.id} {item.title} · {item.status}{item.method ? ` · ${item.method} ${item.path ?? ''}` : ''}
            </button>
            {processable && !readOnly && item.status !== 'dev_done' && item.changeKind !== 'removed' && (
              <div className="board-item-actions">
                <label>
                  <input type="checkbox" checked={!!selfTest[item.id]} onChange={e => setSelfTest(prev => ({ ...prev, [item.id]: e.target.checked }))} />
                  已对照详细设计自测通过
                </label>
                <input aria-label={`${item.id} 结果摘要`} value={summary[item.id] ?? ''} onChange={e => setSummary(prev => ({ ...prev, [item.id]: e.target.value }))} placeholder="结果摘要" />
                <button type="button" disabled={busy} onClick={() => void complete(item.id)}>完成</button>
              </div>
            )}
            {testable && !readOnly && item.changeKind !== 'removed' && (
              <div className="board-item-actions">
                <input aria-label={`${item.id} 测试记录`} value={evidence[item.id] ?? ''} onChange={e => setEvidence(prev => ({ ...prev, [item.id]: e.target.value }))} placeholder="测试记录 / 证据" />
                <button type="button" disabled={busy} onClick={() => void runUnit(item.id)}>记录通过</button>
                <button type="button" disabled={busy} onClick={() => void runUnit(item.id, true)}>记录失败</button>
                <input aria-label={`${item.id} 退回原因`} value={reason[item.id] ?? ''} onChange={e => setReason(prev => ({ ...prev, [item.id]: e.target.value }))} placeholder="退回原因" />
                <button type="button" disabled={busy} onClick={() => void sendBack(item.id)}>退回</button>
              </div>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}
