import { describe, expect, test } from 'vitest'
import { VOICE_PATHS, VOICE_PERSONAS, omniPersonaCaption, voicePersonaGroups } from './voicePersonas'

describe('embedded 50-life catalogue', () => {
  test('ships fifty unique personas for local and MiniCPM-o', () => {
    expect(VOICE_PERSONAS).toHaveLength(50)
    expect(new Set(VOICE_PERSONAS.map(p => p.id)).size).toBe(50)
    expect(VOICE_PERSONAS.every(p => p.id.startsWith('refpack:'))).toBe(true)
  })

  test('groups the picker without mixing voice paths', () => {
    expect(VOICE_PATHS.map(p => p.value)).toEqual(['cloud', 'local', 'omni'])
    expect(voicePersonaGroups().length).toBeGreaterThan(5)
  })

  test('captions a MiniCPM-o life without swapping identity', () => {
    expect(omniPersonaCaption('refpack:甜心少女.wav')).toBe('人生：甜心少女')
    expect(omniPersonaCaption('missing')).toBe('')
  })
})
