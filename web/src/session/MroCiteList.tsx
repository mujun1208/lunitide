import React, { useState } from 'react'
import { useZh } from '../i18n/language'
import type { MroCiteView } from './mroCite'
import { downloadCitedChecklist } from './mroChecklist'

export function MroCiteList({cites, discarded, gate, restored}: MroCiteView): React.JSX.Element | null {
  const zh = useZh()
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')
  if (!cites.length && !discarded && !gate && !restored) return null
  const hasCitedSteps = cites.some(cite => cite.quote.trim().length > 0)
  const onDownload = async () => {
    setBusy(true)
    setNote('')
    try {
      const ok = await downloadCitedChecklist(cites)
      if (!ok) setNote(zh ? '没有带引用的步骤可下载' : 'No cited steps to download')
    } catch {
      setNote(zh ? '检查单生成失败' : 'Could not build checklist')
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="mro-cite-list" aria-label={zh ? '引用' : 'Citations'}>
      {cites.map((cite, index) => (
        <article className="mro-cite" key={`${cite.locator}:${index}`}>
          <b>{cite.docType ? `${cite.docType} ` : ''}{zh ? `修订 ${cite.revision}` : `Rev ${cite.revision}`}</b>
          {cite.expertName ? <small>{cite.expertName}</small> : null}
          {cite.quote ? <p>{cite.quote}</p> : null}
        </article>
      ))}
      {gate === 'ungrounded' ? (
        <article className="mro-cite is-ungrounded" role="alert">{zh ? '未找到受控依据，勿据此操作' : 'No controlled source. Do not act on this.'}</article>
      ) : null}
      {typeof discarded === 'number' && discarded > 0 ? (
        <small>{zh ? `${discarded} 块因机尾不适用已丢弃` : `${discarded} chunks dropped for tail effectivity`}</small>
      ) : null}
      {restored ? <small>{zh ? '已补回机务引用' : 'MRO citations restored'}</small> : null}
      {hasCitedSteps ? (
        <div className="mro-cite-actions">
          <button type="button" className="mro-checklist-download" onClick={() => void onDownload()} disabled={busy}>
            {zh ? '下载检查单 JSON' : 'Download checklist JSON'}
          </button>
          {note ? <small role="status">{note}</small> : null}
        </div>
      ) : null}
    </div>
  )
}
