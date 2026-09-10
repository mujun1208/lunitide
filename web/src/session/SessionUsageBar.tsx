import { useEffect, useState } from 'react'
import { getChatUsageBridge, type ChatUsageBridge, type ChatUsageSnapshot } from '../bridge/client'
import { TokenUsage, type TokenUsageValue } from './TokenUsage'

export function SessionUsageBar({ sessionId, usage, enabled, zh, usageApi }: { sessionId: string; usage?: TokenUsageValue; enabled?: boolean; zh: boolean; usageApi?: ChatUsageBridge }) {
  const [ledger, setLedger] = useState<ChatUsageSnapshot>()
  useEffect(() => {
    let cancelled = false
    try {
      void (usageApi ?? getChatUsageBridge()).get({ sessionId }).then(next => {
        if (!cancelled) setLedger(next)
      }).catch(() => {
        if (!cancelled) setLedger(undefined)
      })
    } catch {
      setLedger(undefined)
    }
    return () => { cancelled = true }
  }, [sessionId, usage, usageApi])
  return <TokenUsage usage={usage} enabled={enabled} zh={zh} ledger={ledger} />
}
