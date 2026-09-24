import { Brain } from 'lucide-react'
import React, { useEffect, useId, useRef, useState } from 'react'
import { useZh } from '../i18n/language'
import {
  REASONING_LABELS,
  REASONING_LEVELS,
  reasoningLevelAt,
  reasoningLevelIndex,
  saveReasoningLevel,
  type ReasoningLevel,
} from './composerReasoning'

export function ComposerReasoningSlider({
  level,
  onChange,
}: {
  level: ReasoningLevel
  onChange: (level: ReasoningLevel) => void
}): React.JSX.Element {
  const zh = useZh()
  const label = REASONING_LABELS[level]
  const word = zh ? label.zh : label.en
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const popId = useId()
  useEffect(() => {
    if (!open) return
    const onPointer = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('pointerdown', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])
  const commit = (next: ReasoningLevel) => {
    saveReasoningLevel(next)
    onChange(next)
  }
  return (
    <div className="composer-reasoning" ref={rootRef}>
      <button
        type="button"
        className="composer-reasoning-trigger"
        aria-label={zh ? '模型使用强度' : 'Model intensity'}
        aria-expanded={open}
        aria-controls={popId}
        onClick={() => setOpen(value => !value)}
      >
        <Brain size={14} strokeWidth={1.75} aria-hidden="true" />
        <span>{word}</span>
        <i aria-hidden="true">▾</i>
      </button>
      {open ? (
        <div className="composer-reasoning-pop" id={popId} role="dialog" aria-label={zh ? '模型使用强度' : 'Model intensity'}>
          <header>
            <b>{zh ? '模型使用强度' : 'Model intensity'}</b>
            <strong>{word}</strong>
          </header>
          <div
            className="composer-reasoning-scale"
            style={{ ['--reasoning-p' as string]: String(reasoningLevelIndex(level) / (REASONING_LEVELS.length - 1)) }}
          >
            <div className="composer-reasoning-rail" aria-hidden="true">
              <i className="composer-reasoning-fill" />
              {REASONING_LEVELS.map((item, index) => (
                <span
                  key={item}
                  className={index <= reasoningLevelIndex(level) ? 'is-on' : undefined}
                  style={{ left: `${(index / (REASONING_LEVELS.length - 1)) * 100}%` }}
                />
              ))}
              <b className="composer-reasoning-thumb" />
            </div>
            <input
              type="range"
              min={0}
              max={REASONING_LEVELS.length - 1}
              step={1}
              value={reasoningLevelIndex(level)}
              aria-label={zh ? '调整模型使用强度' : 'Adjust model intensity'}
              aria-valuetext={word}
              onChange={event => commit(reasoningLevelAt(Number(event.target.value)))}
            />
          </div>
          <footer>
            <span>{zh ? '更快' : 'Faster'}</span>
            <span>{zh ? '更聪明' : 'Smarter'}</span>
          </footer>
        </div>
      ) : null}
    </div>
  )
}
