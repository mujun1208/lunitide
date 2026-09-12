import React, { useMemo, useState } from 'react'
import { useZh } from '../i18n/language'
import { agentHubApi, type AgentHubCounts, type AgentHubTask } from './agentHubApi'
import { agentMark, newIdempotencyKey, shortWorkDir, statusLabel } from './agentHubCopy'

export function AgentHubTasks({
  items,
  counts,
  onOpened,
  onChanged,
}: {
  items: AgentHubTask[]
  counts: AgentHubCounts
  onOpened: (taskId: string) => void
  onChanged: () => void
}): React.JSX.Element {
  const zh = useZh()
  const [status, setStatus] = useState('')
  const [agent, setAgent] = useState('')
  const [date, setDate] = useState('')
  const [error, setError] = useState('')
  const filtered = useMemo(() => items.filter(item => {
    if (status && item.status !== status) return false
    if (agent && item.agent !== agent) return false
    if (date && !item.createdAt.startsWith(date)) return false
    return true
  }), [items, status, agent, date])
  const rerun = async (item: AgentHubTask) => {
    try {
      const detail = await agentHubApi.start({
        agent: item.agent as 'codex' | 'cursor' | 'kimi',
        prompt: item.prompt,
        workDir: item.workDir || undefined,
        sandbox: item.sandbox || undefined,
        idempotencyKey: newIdempotencyKey(),
      })
      setError('')
      onOpened(detail.task.taskId)
      onChanged()
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '没有重新发出任务。' : 'Could not rerun this task.'))
    }
  }
  return (
    <section>
      <div className="agent-hub-stats">
        {(['running', 'queued', 'success', 'failed'] as const).map(key => (
          <div key={key} className="agent-hub-stat"><small>{statusLabel(key, zh)}</small><b>{counts[key] ?? 0}</b></div>
        ))}
      </div>
      <div className="agent-hub-filters">
        <select aria-label={zh ? '状态' : 'Status'} value={status} onChange={event => setStatus(event.target.value)}>
          <option value="">{zh ? '全部状态' : 'All statuses'}</option>
          {['queued', 'running', 'success', 'failed', 'timeout', 'cancelled'].map(value => <option key={value} value={value}>{statusLabel(value, zh)}</option>)}
        </select>
        <select aria-label="Agent" value={agent} onChange={event => setAgent(event.target.value)}>
          <option value="">{zh ? '全部 Agent' : 'All agents'}</option>
          <option value="codex">Codex</option>
          <option value="cursor">Cursor</option>
          <option value="kimi">Kimi</option>
        </select>
        <input type="date" aria-label={zh ? '日期' : 'Date'} value={date} onChange={event => setDate(event.target.value)} />
      </div>
      {filtered.map(item => (
        <div key={item.taskId} className="agent-hub-row">
          <button type="button" onClick={() => onOpened(item.taskId)} style={{ all: 'unset', cursor: 'pointer', flex: 1, display: 'flex', gap: 14, alignItems: 'center' }}>
            <span className="agent-hub-logo">{agentMark(item.agent)}</span>
            <span><b>{item.prompt}</b><small>{item.agent} · {shortWorkDir(item.workDir)}</small></span>
            <em className={`agent-hub-status ${item.status}`}>{statusLabel(item.status, zh)}</em>
          </button>
          {(item.status === 'running' || item.status === 'queued') && (
            <button type="button" onClick={() => void agentHubApi.cancel({ taskId: item.taskId }).then(() => { setError(''); onChanged() }).catch(err => setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '没有取消成功。' : 'Could not cancel this task.')))}>{zh ? '取消' : 'Cancel'}</button>
          )}
          {item.status !== 'running' && item.status !== 'queued' && (
            <button type="button" onClick={() => void rerun(item)}>{zh ? '重跑' : 'Rerun'}</button>
          )}
        </div>
      ))}
      {error && <p className="agent-hub-error" role="alert">{error}</p>}
      {filtered.length === 0 && <p className="agent-hub-hint">{zh ? '还没有匹配的任务。' : 'No matching tasks.'}</p>}
    </section>
  )
}
