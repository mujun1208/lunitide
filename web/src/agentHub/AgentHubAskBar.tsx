import React, { useState } from 'react'
import { agentHubApi, type AgentHubOpenPrompt, type AgentHubThreadDetail } from './agentHubApi'

export function AgentHubAskBar({
  threadId,
  prompt,
  onResponded,
}: {
  threadId: string
  prompt: AgentHubOpenPrompt
  onResponded?: (detail: AgentHubThreadDetail) => void
}): React.JSX.Element {
  const [text, setText] = useState('')
  const choose = async (optionId: string) => {
    const extra = text.trim()
    const detail = await agentHubApi.threadRespond({
      threadId,
      callId: prompt.callId,
      optionId,
      ...(extra ? { text: extra } : {}),
    })
    onResponded?.(detail)
  }
  return (
    <div className="agent-hub-actions" role="group" aria-label={prompt.prompt}>
      <p className="agent-hub-hint">{prompt.prompt}</p>
      <textarea
        aria-label="补充说明"
        value={text}
        onChange={event => setText(event.target.value)}
      />
      {prompt.options.map(option => (
        <button key={option.id} type="button" onClick={() => void choose(option.id)}>
          {option.label}
        </button>
      ))}
    </div>
  )
}
