import React from 'react'
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
  return (
    <label className="composer-reasoning">
      <span>{word}</span>
      <input
        type="range"
        min={0}
        max={REASONING_LEVELS.length - 1}
        step={1}
        value={reasoningLevelIndex(level)}
        aria-label={zh ? '模型使用强度' : 'Model intensity'}
        aria-valuetext={word}
        onChange={event => {
          const next = reasoningLevelAt(Number(event.target.value))
          saveReasoningLevel(next)
          onChange(next)
        }}
      />
    </label>
  )
}
