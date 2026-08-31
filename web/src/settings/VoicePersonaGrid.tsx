import React, { useMemo, useState } from 'react'
import { VOICE_PERSONAS, voicePersonaGroups, type VoicePersona } from '../session/companion/voicePersonas'

export function VoicePersonaGrid({
  value,
  onChange,
  caption,
  personas = VOICE_PERSONAS,
  filterPlaceholder = '在 50 种人生里查找',
  filterLabel = '查找人生',
  empty = '没有匹配的人生。',
}: {
  value: string
  onChange: (id: string) => void
  caption?: string
  personas?: VoicePersona[]
  filterPlaceholder?: string
  filterLabel?: string
  empty?: string
}): React.JSX.Element {
  const [query, setQuery] = useState('')
  const groups = useMemo(() => {
    const q = query.trim().toLowerCase()
    const all = voicePersonaGroups(personas)
    if (!q) return all
    return all
      .map(([group, items]): [string, VoicePersona[]] => [
        group,
        items.filter(persona => `${persona.name} ${persona.group} ${persona.gender}`.toLowerCase().includes(q)),
      ])
      .filter(([, items]) => items.length > 0)
  }, [personas, query])

  return (
    <div className="persona-gallery">
      {caption && <p className="persona-gallery-caption">{caption}</p>}
      <input
        className="persona-gallery-filter"
        type="search"
        value={query}
        onChange={e => setQuery(e.target.value)}
        placeholder={filterPlaceholder}
        aria-label={filterLabel}
      />
      {groups.map(([group, items]) => (
        <section key={group} className="persona-gallery-group">
          <h4>{group}</h4>
          <div className="persona-gallery-grid">
            {items.map((persona: VoicePersona) => {
              const on = persona.id === value
              return (
                <button
                  key={persona.id}
                  type="button"
                  className={on ? 'persona-card on' : 'persona-card'}
                  aria-pressed={on}
                  onClick={() => onChange(persona.id)}
                >
                  <b>{persona.name}</b>
                  <small>{persona.gender === 'female' ? '女声' : '男声'}</small>
                </button>
              )
            })}
          </div>
        </section>
      ))}
      {groups.length === 0 && <p className="persona-gallery-caption">{empty}</p>}
    </div>
  )
}
