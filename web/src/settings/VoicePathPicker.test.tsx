import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'
import { VoicePathPicker } from './VoicePathPicker'

describe('VoicePathPicker', () => {
  afterEach(() => cleanup())

  test('offers exactly two equal-weight cards', () => {
    render(<VoicePathPicker value="cloud" onChange={() => undefined} />)
    const cards = screen.getAllByRole('radio')
    expect(cards).toHaveLength(2)
    expect(screen.getByRole('radio', { name: /云端/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('radio', { name: /本地/ })).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByText('默认')).toBeInTheDocument()
    expect(screen.getByText('晓晓 · 微软 Neural')).toBeInTheDocument()
    expect(screen.getByText('sherpa + GPT-SoVITS')).toBeInTheDocument()
    expect(document.querySelectorAll('.voice-path-card')).toHaveLength(2)
    expect(document.querySelectorAll('.voice-path-card.on')).toHaveLength(1)
    expect(document.querySelector('.voice-path-picker')?.className).not.toMatch(/3/)
  })

  test('switches between cloud and local', async () => {
    const picked: string[] = []
    const user = userEvent.setup()
    render(<VoicePathPicker value="cloud" onChange={path => picked.push(path)} />)
    await user.click(screen.getByRole('radio', { name: /本地/ }))
    expect(picked).toEqual(['local'])
  })
})

describe('voice path layout', () => {
  const css = readFileSync(resolve(process.cwd(), 'src/styles.css'), 'utf8')

  test('uses a two-column picker that stacks in a narrow settings pane', () => {
    expect(css).toMatch(/\.voice-path-picker\{[^}]*grid-template-columns:repeat\(2,minmax\(0,1fr\)\)/)
    expect(css).not.toMatch(/\.voice-path-picker\{[^}]*grid-template-columns:repeat\(3/)
    expect(css).toMatch(/@container voice-path \(max-width:520px\)\{\.voice-path-picker\{grid-template-columns:1fr/)
    expect(css).toMatch(/\.settings-body-voice\{max-width:720px\}/)
    expect(css).toMatch(/\.voice-path-card\{[^}]*background:var\(--bg2\)/)
  })
})
