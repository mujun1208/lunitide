import React, { useState } from 'react'
import { useZh } from '../i18n/language'

export type ExpertDetailTab = 'overview' | 'knowledge' | 'growth'

export function ExpertDetailTabs({
  overview, knowledge, growth, initial = 'overview',
}: {
  overview: React.ReactNode
  knowledge: React.ReactNode
  growth: React.ReactNode
  initial?: ExpertDetailTab
}): React.JSX.Element {
  const zh = useZh()
  const [tab, setTab] = useState<ExpertDetailTab>(initial)
  const labels: Record<ExpertDetailTab, string> = {
    overview: zh ? '概览' : 'Overview',
    knowledge: zh ? '知识' : 'Knowledge',
    growth: zh ? '路径' : 'Path',
  }
  return (
    <div className="expert-detail-tabset">
      <div className="skill-status-tabs expert-detail-tabs" role="tablist" aria-label={zh ? '专家详情' : 'Expert detail'}>
        {(Object.keys(labels) as ExpertDetailTab[]).map(id => (
          <button type="button" role="tab" key={id} aria-selected={tab === id} onClick={() => setTab(id)}>
            {labels[id]}
          </button>
        ))}
      </div>
      <div role="tabpanel">
        {tab === 'overview' ? overview : tab === 'knowledge' ? knowledge : growth}
      </div>
    </div>
  )
}
