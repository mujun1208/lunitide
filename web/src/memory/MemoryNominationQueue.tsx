import React, { useCallback, useEffect, useState } from 'react'
import { getMemoryBridge, getNominationBridge, newBridgeULID, type MemoryBridge, type NominationBridge } from '../bridge/client'
import type { MemoryNominationListResult } from '../generated/bridge'

type NominationItem = MemoryNominationListResult['items'][number]

function preview(content: string, max = 160): string {
  const text = content.trim()
  if (text.length <= max) return text
  return `${text.slice(0, max)}…`
}

export function MemoryNominationQueue({
  nominations = getNominationBridge(),
  memory = getMemoryBridge(),
}: {
  nominations?: NominationBridge
  memory?: Pick<MemoryBridge, 'confirmCandidate'>
}): React.JSX.Element {
  const [items, setItems] = useState<NominationItem[]>([])
  const [error, setError] = useState('')
  const [busyId, setBusyId] = useState('')

  const load = useCallback(async () => {
    try {
      const listed = await nominations.list({ state: 'nominated', limit: 50 })
      setItems(listed.items.filter(item => item.state === 'nominated'))
      setError('')
    } catch (err) {
      const detail = err instanceof Error ? err.message.trim() : ''
      setError(/[\u4e00-\u9fff]/.test(detail) ? detail : '待确认提名载入失败')
    }
  }, [nominations])

  useEffect(() => {
    void load()
  }, [load])

  const act = async (item: NominationItem, action: 'confirm' | 'withdraw') => {
    if (busyId) return
    setBusyId(item.nominationId)
    try {
      if (action === 'withdraw') {
        await nominations.withdraw({ nominationId: item.nominationId, actor: 'settings' })
      } else {
        const confirm = memory.confirmCandidate
        if (!confirm) {
          setError('确认失败')
          return
        }
        await confirm({
          candidateId: item.candidateId,
          confirmationToken: item.confirmationToken,
          action: 'confirm',
          requestId: newBridgeULID(),
        })
      }
      await load()
    } catch (err) {
      const detail = err instanceof Error ? err.message.trim() : ''
      setError(/[\u4e00-\u9fff]/.test(detail) ? detail : action === 'withdraw' ? '撤回失败' : '确认失败')
    } finally {
      setBusyId('')
    }
  }

  return (
    <section className="memory-nomination-queue" aria-label="待确认提名">
      <h2>待确认提名</h2>
      <p>整理和压缩只会提名。点确认后才进入长期记忆。</p>
      {error ? <p role="alert">{error}</p> : null}
      {items.length === 0 && !error ? <p className="memory-nomination-empty">当前没有待确认提名。</p> : null}
      {items.length > 0 ? (
        <ul className="memory-list">
          {items.map(item => (
            <li key={item.nominationId} className="memory-list-row">
              <div className="memory-list-item">
                <span className="memory-list-kind">{item.nominator || '引擎'}</span>
                <span className="memory-list-text">{preview(item.content)}</span>
                {item.reason ? <span className="memory-list-meta">{item.reason}</span> : null}
              </div>
              <div className="memory-nomination-actions">
                <button type="button" className="ui-btn primary" disabled={busyId === item.nominationId} onClick={() => void act(item, 'confirm')}>
                  确认
                </button>
                <button type="button" className="ui-btn" disabled={busyId === item.nominationId} onClick={() => void act(item, 'withdraw')}>
                  撤回
                </button>
              </div>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  )
}
