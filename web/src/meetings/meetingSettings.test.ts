import { afterEach, describe, expect, test } from 'vitest'
import { defaultMeetingSettings, loadMeetingSettings, saveMeetingSettings } from './meetingSettings'

afterEach(() => {
  localStorage.clear()
})

describe('meetingSettings', () => {
  test('defaults to system listen and empty notes model', () => {
    expect(defaultMeetingSettings()).toEqual({ listen: 'cloud', modelId: '' })
    expect(loadMeetingSettings()).toEqual({ listen: 'cloud', modelId: '' })
  })

  test('persists listen and notes model without defaulting to local', () => {
    expect(saveMeetingSettings({ listen: 'volc', modelId: 'qwen-plus' })).toEqual({ listen: 'volc', modelId: 'qwen-plus' })
    expect(loadMeetingSettings()).toEqual({ listen: 'volc', modelId: 'qwen-plus' })
  })

  test('notifies the meeting workspace when settings change', () => {
    const seen: string[] = []
    const onChange = () => seen.push('ok')
    window.addEventListener('lunitide:meeting', onChange)
    saveMeetingSettings({ listen: 'local', modelId: 'glm-5.3' })
    window.removeEventListener('lunitide:meeting', onChange)
    expect(seen).toEqual(['ok'])
  })
})
