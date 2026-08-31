import { afterEach, describe, expect, test, vi } from 'vitest'
import { companionAsrPathLabel, companionListenFailover, companionListenKind, companionListenLightLabel, companionVolcDeafGiveUp, VOLC_DEAF_RESTART_LIMIT, withDeadline } from './asrPath'

describe('companionAsrPathLabel', () => {
  test('says local when the sidecar is actually decoding', () => {
    expect(companionAsrPathLabel('local', 'auto')).toMatch(/本机识别/)
    expect(companionAsrPathLabel('local', 'local')).not.toMatch(/离开本机/)
  })

  test('does not pretend auto is local when the system recognizer took over', () => {
    expect(companionAsrPathLabel('cloud', 'auto')).toMatch(/系统识别/)
    expect(companionAsrPathLabel('cloud', 'auto')).toMatch(/离开本机/)
  })

  test('an explicit cloud choice is just 系统识别', () => {
    expect(companionAsrPathLabel('cloud', 'cloud')).toBe('系统识别')
  })

  test('volc is seed-asr, not 系统识别', () => {
    expect(companionAsrPathLabel('volc', 'auto')).toBe('火山听写 · seed-asr')
    expect(companionAsrPathLabel('volc', 'cloud')).not.toMatch(/离开本机/)
  })
})

describe('companionListenLightLabel', () => {
  test('follows the live route, not the saved card', () => {
    expect(companionListenLightLabel('local')).toBe('本机 sherpa')
    expect(companionListenLightLabel('volc')).toBe('火山 seed-asr')
    expect(companionListenLightLabel('cloud')).toBe('系统识别')
  })
})

describe('companionListenKind', () => {
  test('product cards pick their own listen engine', () => {
    expect(companionListenKind('volc', 'auto')).toBe('volc')
    expect(companionListenKind('local', 'auto')).toBe('local')
    expect(companionListenKind('local', 'cloud')).toBe('local')
    expect(companionListenKind('cloud', 'cloud')).toBe('cloud')
    expect(companionListenKind('cloud', 'local')).toBe('local')
    expect(companionListenKind('cloud', 'auto')).toBe('auto')
    expect(companionListenKind('omni', 'auto')).toBe('auto')
  })
})

describe('companionListenFailover', () => {
  test('explicit 火山 stays on seed-asr even when sherpa is ready', () => {
    expect(companionListenFailover('volc', 'volc', true)).toBe('volc')
    expect(companionListenFailover('volc', 'volc', false)).toBe('volc')
  })

  test('a live volc route never failovers to sherpa, even if the card preference drifted', () => {
    expect(companionListenFailover('volc', 'auto', true)).toBe('volc')
    expect(companionListenFailover('volc', 'cloud', true)).toBe('volc')
  })

  test('deaf system recognition uses sherpa when it is installed', () => {
    expect(companionListenFailover('cloud', 'cloud', true)).toBe('local')
    expect(companionListenFailover('cloud', 'auto', false)).toBe('cloud')
  })

  test('explicit 本地 never ships audio off the machine', () => {
    expect(companionListenFailover('local', 'local', false)).toBe('local')
  })
})

describe('companionVolcDeafGiveUp', () => {
  test('stops an explicit 火山 deaf loop after the restart budget', () => {
    expect(companionVolcDeafGiveUp('volc', 'volc', 1)).toBe(false)
    expect(companionVolcDeafGiveUp('volc', 'volc', VOLC_DEAF_RESTART_LIMIT)).toBe(true)
    expect(companionVolcDeafGiveUp('local', 'local', VOLC_DEAF_RESTART_LIMIT)).toBe(false)
    expect(companionVolcDeafGiveUp('cloud', 'local', VOLC_DEAF_RESTART_LIMIT)).toBe(false)
  })
})

describe('withDeadline', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  test('resolves when work finishes in time', async () => {
    await expect(withDeadline(Promise.resolve('ok'), 50)).resolves.toBe('ok')
  })

  test('rejects when work hangs past the budget', async () => {
    vi.useFakeTimers()
    const pending = withDeadline(new Promise<string>(() => {}), 40)
    const settled = pending.then(
      () => 'ok',
      error => (error instanceof Error ? error.message : 'other'),
    )
    await vi.advanceTimersByTimeAsync(40)
    expect(await settled).toBe('LISTEN_DEADLINE')
  })
})
