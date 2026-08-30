import { describe, expect, test } from 'vitest'
import { companionAsrPathLabel } from './asrPath'

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
