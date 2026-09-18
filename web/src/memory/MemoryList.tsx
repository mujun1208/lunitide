import React from 'react'
import type { MemoryItemDTO } from '../generated/bridge'
import { MemoryItemMenu } from './MemoryItemMenu'

const KIND_LABELS: Record<string, string> = {
  profile: '身份',
  preference: '偏好',
  goal: '目标',
  constraint: '约束',
  decision: '决定',
  procedure: '流程',
  episode: '经历',
  observation: '观察',
  working: '进行中',
}

const SCOPE_LABELS: Record<MemoryItemDTO['scopeKind'], string> = {
  user: '个人',
  project: '项目',
}

export function memoryUpdatedOn(updatedAt: string): string {
  return updatedAt.slice(0, 10)
}

export function MemoryList({
  items,
  query,
  onOpen,
  onForget,
}: {
  items: MemoryItemDTO[]
  query: string
  onOpen: (factId: string) => void
  onForget: (factId: string) => void
}): React.JSX.Element {
  const needle = query.trim()
  const visible = needle
    ? items.filter(item => (item.text ?? '').includes(needle) || KIND_LABELS[item.kind]?.includes(needle))
    : items
  if (visible.length === 0) {
    return <div className="empty"><b>暂无记忆</b><span>用「保存为记忆」写下稳定偏好，或在高级管理里手动新增。</span></div>
  }
  return (
    <ul className="memory-list">
      {visible.map(item => (
        <li key={item.factId} className="memory-list-row">
          <button type="button" className="memory-list-item" onClick={() => onOpen(item.factId)}>
            <span className="memory-list-kind">{KIND_LABELS[item.kind] ?? item.kind}</span>
            <span className="memory-list-text">{item.text || '（已忘记）'}</span>
            <span className="memory-list-meta">
              <span>{SCOPE_LABELS[item.scopeKind]}</span>
              <span>{memoryUpdatedOn(item.updatedAt)}</span>
            </span>
          </button>
          <MemoryItemMenu
            label={item.text || item.factId}
            onView={() => onOpen(item.factId)}
            onCorrect={() => onOpen(item.factId)}
            onForget={() => onForget(item.factId)}
          />
        </li>
      ))}
    </ul>
  )
}
