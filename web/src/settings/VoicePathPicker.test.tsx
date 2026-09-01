import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'
import { VoicePathPicker } from './VoicePathPicker'

describe('VoicePathPicker', () => {
  afterEach(() => cleanup())

  test('offers three equal-weight cards', () => {
    render(<VoicePathPicker value="cloud" onChange={() => undefined} />)
    const cards = screen.getAllByRole('radio')
    expect(cards).toHaveLength(3)
    expect(screen.getByRole('radio', { name: /云端/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('radio', { name: /火山/ })).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByRole('radio', { name: /本地/ })).toHaveAttribute('aria-checked', 'false')
    expect(screen.getByText('默认')).toBeInTheDocument()
    expect(screen.getByText('晓晓 · 微软 Neural')).toBeInTheDocument()
    expect(screen.getByText('火山听 · 晓晓读（未配朗读）')).toBeInTheDocument()
    expect(screen.getByText('sherpa + GPT-SoVITS')).toBeInTheDocument()
    expect(screen.getByText(/豆包 App 里的温柔桃子/)).toBeInTheDocument()
    expect(screen.getAllByText(/不能插话/).length).toBeGreaterThanOrEqual(1)
    expect(document.querySelectorAll('.voice-path-card')).toHaveLength(3)
    expect(document.querySelectorAll('.voice-path-card.on')).toHaveLength(1)
  })

  test('writes 火山读 only when TTS is configured', () => {
    render(<VoicePathPicker value="volc" volcTtsReady onChange={() => undefined} />)
    expect(screen.getByText('火山听 · 火山读')).toBeInTheDocument()
    expect(screen.queryByText('晓晓读（未配朗读）')).not.toBeInTheDocument()
    expect(screen.queryByText(/已配火山朗读/)).not.toBeInTheDocument()
  })

  test('tells the user to click 火山 when TTS is ready but the path is still 云端', () => {
    render(<VoicePathPicker value="cloud" volcTtsReady onChange={() => undefined} />)
    expect(screen.getByRole('status')).toHaveTextContent(/已配火山朗读/)
    expect(screen.getByRole('status')).toHaveTextContent(/不会自动改通道/)
  })

  test('switches between cloud, volc, and local', async () => {
    const picked: string[] = []
    const user = userEvent.setup()
    render(<VoicePathPicker value="cloud" onChange={path => picked.push(path)} />)
    await user.click(screen.getByRole('radio', { name: /火山/ }))
    await user.click(screen.getByRole('radio', { name: /本地/ }))
    expect(picked).toEqual(['volc', 'local'])
  })
})

describe('voice path layout', () => {
  const css = readFileSync(resolve(process.cwd(), 'src/styles.css'), 'utf8')

  test('uses a three-column picker that stacks in a narrow settings pane', () => {
    expect(css).toMatch(/\.voice-path-picker\{[^}]*grid-template-columns:repeat\(3,minmax\(0,1fr\)\)/)
    expect(css).toMatch(/@container voice-path \(max-width:520px\)\{\.voice-path-picker\{grid-template-columns:1fr/)
    expect(css).toMatch(/\.settings-body-voice\{max-width:980px\}/)
    expect(css).toMatch(/\.voice-path-card\{[^}]*background:var\(--bg2\)/)
    expect(css).toMatch(/\.voice-path-hint\{/)
    expect(css).toMatch(/\.companion-banners\{[^}]*flex-direction:column/)
  })
})
