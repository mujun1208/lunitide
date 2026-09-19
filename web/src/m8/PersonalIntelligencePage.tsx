import React, { useEffect, useState } from 'react'
import { getMemoryBridge, type OCRPackBridge, type OCRRoutingBridge, type OCRRunBridge, type ProviderBridge } from '../bridge/client'
import { MemoryNominationQueue } from '../memory/MemoryNominationQueue'
import { MemoryPage } from '../memory/MemoryPage'
import { PrivacyConsole } from './PrivacyConsole'
import { OCRSettingsPanel } from '../settings/OCRSettingsPanel'
import { SmartCapabilitiesPanel } from '../settings/SmartCapabilitiesPanel'
import type { SettingsIntelligenceView } from '../settings/settingsNav'

export type IntelligenceView = SettingsIntelligenceView

export function PersonalIntelligencePage({
  view = 'overview',
  onViewChange,
  providers,
  ocr,
  packApi,
  runs,
}: {
  view?: SettingsIntelligenceView
  onViewChange?: (view: SettingsIntelligenceView) => void
  providers?: ProviderBridge
  ocr?: OCRRoutingBridge
  packApi?: OCRPackBridge
  runs?: OCRRunBridge
}): React.JSX.Element {
  const [current, setCurrent] = useState<SettingsIntelligenceView>(view)
  const [privacyOpen, setPrivacyOpen] = useState(false)

  useEffect(() => {
    setCurrent(view)
  }, [view])

  const open = (next: SettingsIntelligenceView) => {
    setCurrent(next)
    onViewChange?.(next)
  }

  if (current === 'memory') {
    return (
      <main className="pi-page">
        <header className="pi-head">
          <button type="button" className="pi-back" onClick={() => open('overview')}>返回智能能力</button>
          <div>
            <h1>记忆</h1>
            <p>待确认提名先列在上面；确认后才进长期记忆。已保存的记忆可在下面纠正或忘记。</p>
          </div>
          <button type="button" className="ocr-disclose" aria-expanded={privacyOpen} onClick={() => setPrivacyOpen(value => !value)}>隐私</button>
        </header>
        <section className="pi-panel" aria-label="待确认提名"><MemoryNominationQueue /></section>
        <section className="pi-panel" aria-label="记忆"><MemoryPage /></section>
        {privacyOpen ? <section className="pi-panel" aria-label="隐私"><PrivacyConsole items={getMemoryBridge()} /></section> : null}
      </main>
    )
  }

  if (current === 'ocr') {
    return (
      <main className="pi-page">
        <header className="pi-head">
          <button type="button" className="pi-back" onClick={() => open('overview')}>返回智能能力</button>
        </header>
        <OCRSettingsPanel providers={providers} ocr={ocr} packApi={packApi} runs={runs} />
      </main>
    )
  }

  return (
    <main className="pi-page">
      <header className="pi-head">
        <div>
          <h1>智能能力</h1>
          <p>首页只保留自动记忆和文字识别。色调沿用现有黑白界面。</p>
        </div>
      </header>
      <SmartCapabilitiesPanel onOpenMemory={() => open('memory')} onOpenOCR={() => open('ocr')} />
    </main>
  )
}
