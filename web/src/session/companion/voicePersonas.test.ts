import { describe, expect, test } from 'vitest'
import { VOICE_PATHS, VOICE_PERSONAS, omniPersonaCaption, shownVoicePath, voicePersonaGroups } from './voicePersonas'

describe('embedded 50-life catalogue', () => {
  test('ships fifty unique personas for local and MiniCPM-o', () => {
    expect(VOICE_PERSONAS).toHaveLength(50)
    expect(new Set(VOICE_PERSONAS.map(p => p.id)).size).toBe(50)
    expect(VOICE_PERSONAS.every(p => p.id.startsWith('refpack:'))).toBe(true)
  })

  test('groups the picker without mixing voice paths', () => {
    expect(VOICE_PATHS).toHaveLength(3)
    expect(VOICE_PATHS.map(p => p.value)).toEqual(['cloud', 'volc', 'local'])
    expect(VOICE_PATHS[0]).toMatchObject({ label: '云端', badge: '默认', meta: expect.stringContaining('晓晓') })
    expect(VOICE_PATHS[1]).toMatchObject({ label: '火山', meta: expect.stringContaining('晓晓') })
    expect(VOICE_PATHS[2]).toMatchObject({ label: '本地', meta: expect.stringContaining('sherpa') })
    expect(VOICE_PATHS[2].meta).toMatch(/GPT-SoVITS/)
    expect(VOICE_PATHS.some(p => /MiniCPM|omni/i.test(`${p.label}${p.badge}${p.kicker}${p.meta}${p.desc}`))).toBe(false)
    expect(shownVoicePath('omni')).toBe('cloud')
    expect(shownVoicePath('cloud')).toBe('cloud')
    expect(shownVoicePath('volc')).toBe('volc')
    expect(shownVoicePath('local')).toBe('local')
    expect(voicePersonaGroups().length).toBeGreaterThan(5)
  })

  test('captions a MiniCPM-o life without swapping identity', () => {
    expect(omniPersonaCaption('refpack:甜心少女.wav')).toBe('人生：甜心少女')
    expect(omniPersonaCaption('missing')).toBe('')
  })

  test('maps leftover MiniCPM-o onto 云端', () => {
    expect(shownVoicePath('omni')).toBe('cloud')
    expect(shownVoicePath('cloud')).toBe('cloud')
    expect(shownVoicePath('volc')).toBe('volc')
    expect(shownVoicePath('local')).toBe('local')
  })
})
