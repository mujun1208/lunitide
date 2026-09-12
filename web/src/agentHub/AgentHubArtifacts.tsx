import React, { useMemo, useState } from 'react'
import { useZh } from '../i18n/language'
import { AgentHubFileInspector } from './AgentHubFileInspector'
import { agentHubApi, type AgentHubArtifact, type AgentHubPreview } from './agentHubApi'
import { visibleHubArtifacts } from './agentHubCopy'

export function AgentHubArtifacts({
  items,
}: {
  items: AgentHubArtifact[]
}): React.JSX.Element {
  const zh = useZh()
  const [agent, setAgent] = useState('')
  const [date, setDate] = useState('')
  const [ext, setExt] = useState('')
  const [preview, setPreview] = useState<AgentHubPreview>()
  const filtered = useMemo(() => visibleHubArtifacts(items, false).filter(item => {
    if (agent && item.agent !== agent) return false
    if (date && !(item.createdAt ?? '').startsWith(date)) return false
    if (ext && !item.name.toLowerCase().endsWith(ext.toLowerCase())) return false
    return true
  }), [items, agent, date, ext])
  return (
    <section>
      <div className="agent-hub-filters">
        <select aria-label="Agent" value={agent} onChange={event => setAgent(event.target.value)}>
          <option value="">{zh ? '全部 Agent' : 'All agents'}</option>
          <option value="codex">Codex</option>
          <option value="cursor">Cursor</option>
          <option value="kimi">Kimi</option>
        </select>
        <input type="date" aria-label={zh ? '日期' : 'Date'} value={date} onChange={event => setDate(event.target.value)} />
        <input aria-label={zh ? '扩展名' : 'Extension'} placeholder=".md" value={ext} onChange={event => setExt(event.target.value)} />
      </div>
      <div className="agent-hub-grid">
        {filtered.map(item => (
          <button key={`${item.taskId}:${item.path}`} type="button" className="agent-hub-card" onClick={() => {
            if (!item.taskId) return
            void agentHubApi.preview({ taskId: item.taskId, path: item.path }).then(setPreview)
          }}>
            <b>{item.name}</b>
            <small>{item.agent} · {item.source}</small>
          </button>
        ))}
      </div>
      {filtered.length === 0 && <p className="agent-hub-hint">{zh ? '还没有匹配的产物。' : 'No matching artifacts.'}</p>}
      <AgentHubFileInspector
        preview={preview}
        onOpen={() => {
          const item = filtered.find(entry => preview && entry.path === preview.path)
          if (item?.taskId && preview) void agentHubApi.open({ taskId: item.taskId, path: preview.path })
        }}
      />
    </section>
  )
}
