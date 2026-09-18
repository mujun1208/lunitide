import React from 'react'
import type { MediaAssetDTO, MediaSnapshotDTO } from '../generated/bridge'
import { mediaText } from './mediaCopy'
import { useZh } from '../i18n/language'

export function MediaQueueDrawer({
  open,
  snapshot,
  assets,
  onClose,
  onJump,
  onRemove,
  onClear,
}: {
  open: boolean
  snapshot: MediaSnapshotDTO | null
  assets: MediaAssetDTO[]
  onClose: () => void
  onJump: (assetId: string) => void
  onRemove: (assetId: string) => void
  onClear: () => void
}): React.JSX.Element | null {
  const zh = useZh()
  const copy = mediaText(zh)
  if (!open) return null
  const external = snapshot?.origin === 'external'
  return (
    <div className="media-queue-drawer" role="dialog" aria-label={copy.queueLabel}>
      <header>
        <h2>{copy.queue}</h2>
        <button type="button" onClick={onClose}>{copy.close}</button>
      </header>
      {external ? <p>{copy.externalQueue}</p> : null}
      {!external && assets.length === 0 ? <p>{copy.emptyQueue}</p> : null}
      {!external ? (
        <ul>
          {assets.map(asset => (
            <li key={asset.assetId}>
              <button type="button" onClick={() => onJump(asset.assetId)}>{asset.title}</button>
              <button type="button" onClick={() => onRemove(asset.assetId)}>{copy.remove}</button>
            </li>
          ))}
        </ul>
      ) : null}
      {!external && assets.length > 0 ? <button type="button" onClick={onClear}>{copy.clear}</button> : null}
    </div>
  )
}
