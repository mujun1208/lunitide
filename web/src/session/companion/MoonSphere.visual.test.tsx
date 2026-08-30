import { cleanup, fireEvent, render } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'
import { MoonSphere } from './MoonSphere'

afterEach(cleanup)

const levels = Array.from({ length: 12 }, () => 0)

describe('MoonSphere visual contract', () => {
  test('jsdom has no WebGL2, so the CSS moon stays clickable', () => {
    const onInterrupt = vi.fn()
    const { container } = render(
      <MoonSphere state="idle" gain={0} levels={levels} interruptible onInterrupt={onInterrupt} />,
    )
    const moon = container.querySelector('.companion-moon') as HTMLElement
    expect(moon.getAttribute('data-visual')).toBe('css')
    expect(moon.getAttribute('data-state')).toBe('idle')
    expect(moon.getAttribute('data-mode')).toBe('orb')
    expect(container.querySelector('.companion-moon-orb')).toBeNull()
    expect(container.querySelector('.companion-moon-strands')).toBeNull()
    const button = container.querySelector('.companion-moon-body') as HTMLButtonElement
    expect(button.getAttribute('aria-label')).toBe('月亮：轻点开始说话')
    fireEvent.click(button)
    expect(onInterrupt).toHaveBeenCalledTimes(1)
  })

  test('thinking and speaking keep the interrupt button and Chinese labels', () => {
    const { container, rerender } = render(
      <MoonSphere state="thinking" gain={0} levels={levels} interruptible onInterrupt={vi.fn()} />,
    )
    expect(container.querySelector('.companion-moon')?.getAttribute('data-state')).toBe('thinking')
    expect(container.querySelector('.companion-moon')?.getAttribute('data-mode')).toBe('glass')
    const button = container.querySelector('.companion-moon-body') as HTMLButtonElement
    expect(button.disabled).toBe(false)
    expect(button.getAttribute('aria-label')).toBe('月亮正在回应')
    rerender(<MoonSphere state="speaking" gain={0.6} levels={levels} interruptible onInterrupt={vi.fn()} />)
    expect(container.querySelector('.companion-moon')?.getAttribute('data-state')).toBe('speaking')
    expect(container.querySelector('.companion-moon')?.getAttribute('data-mode')).toBe('wave')
    expect((container.querySelector('.companion-moon-body') as HTMLButtonElement).getAttribute('aria-label')).toBe(
      '月亮正在说话，点击打断朗读',
    )
  })
})
