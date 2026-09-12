import React from 'react'
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
  const choose = async (optionId: string) => {
    const detail = await agentHubApi.threadRespond({ threadId, callId: prompt.callId, optionId })
    onResponded?.(detail)
  }
  return (
    <div className="agent-hub-actions" role="group" aria-label={prompt.prompt}>
      <p className="agent-hub-hint">{prompt.prompt}</p>
      {prompt.options.map(option => (
        <button key={option.id} type="button" onClick={() => void choose(option.id)}>
          {option.label}
        </button>
      ))}
    </div>
  )
}
