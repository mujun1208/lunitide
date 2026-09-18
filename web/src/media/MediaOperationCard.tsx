import React from 'react'
import type { MediaOperationDTO } from '../generated/bridge'
import { mediaText } from './mediaCopy'
import { useZh } from '../i18n/language'

export function MediaOperationCard({ operation }: { operation: MediaOperationDTO }): React.JSX.Element {
  const zh = useZh()
  const copy = mediaText(zh)
  const pending = operation.phase === 'requested' || operation.phase === 'awaiting_approval'
  const dispatched = operation.phase === 'dispatching' || operation.phase === 'verifying'
  const failed = operation.phase === 'failed' || operation.phase === 'uncertain'
  const label = dispatched ? copy.dispatched : pending ? copy.pending : operation.phase === 'succeeded' ? copy.confirmed : failed ? copy.unconfirmed : operation.phase
  return (
    <article className={`media-op-card${failed ? ' is-error' : ''}`} aria-label={copy.operation}>
      <b>{operation.action}</b>
      <span>{label}</span>
      {operation.errorCode ? <small role="status">{operation.errorCode}</small> : null}
    </article>
  )
}
