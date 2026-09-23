import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { ComposerReasoningSlider } from './ComposerReasoningSlider'
import { reasoningLevelAt, reasoningLevelIndex } from './composerReasoning'

describe('composer reasoning intensity', () => {
  test('maps the slider stops onto the four efforts the model request sends', () => {
    expect(reasoningLevelAt(0)).toBe('low')
    expect(reasoningLevelAt(1)).toBe('high')
    expect(reasoningLevelAt(2)).toBe('max')
    expect(reasoningLevelIndex('max')).toBe(2)
    expect(reasoningLevelIndex('high')).toBe(1)
  })

  test('the top stop is max, which the chat request sends as reasoning effort', () => {
    const onChange = vi.fn()
    render(
      <LanguageProvider value="zh-CN">
        <ComposerReasoningSlider level="high" onChange={onChange} />
      </LanguageProvider>,
    )
    fireEvent.change(screen.getByLabelText('模型使用强度'), { target: { value: '2' } })
    expect(onChange).toHaveBeenCalledWith('max')
  })
})
