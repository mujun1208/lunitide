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
  const [error, setError] = useState('')
  const choose = async (optionId: string) => {
    const extra = text.trim()
    try {
      const detail = await agentHubApi.threadRespond({
        threadId,
        callId: prompt.callId,
        optionId,
        ...(extra ? { text: extra } : {}),
      })
      setError('')
      onResponded?.(detail)
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : '没有提交选择。')
    }
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
      {error ? <p className="agent-hub-error" role="alert">{error}</p> : null}
    </div>
  )
}
