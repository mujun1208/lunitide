// Home-page wake listener. The matcher already existed; this is the
// product wiring: listen on the launch home, enter 月伴 on 「你好月汐」,
// and stop the mic as soon as we leave home. Probing is not listening;
// a transcript paints as 听到：…; energy without glyphs is 聋了.
import type { JSX } from 'react'
import { useEffect, useRef, useState } from 'react'

import type { Language } from '../../i18n/language'
import { loadCompanionSettings, type CompanionSettings } from './companionSettings'
import { useWakeWord, type WakeWordState } from './wakeWord'

export function useCompanionSettings(): CompanionSettings {
  const [settings, setSettings] = useState(loadCompanionSettings)
  useEffect(() => {
    const refresh = () => setSettings(loadCompanionSettings())
    const onStorage = (event: StorageEvent) => {
      if (event.key === 'lunitide:companion') refresh()
    }
    window.addEventListener('lunitide:companion-settings', refresh)
    window.addEventListener('storage', onStorage)
    return () => {
      window.removeEventListener('lunitide:companion-settings', refresh)
      window.removeEventListener('storage', onStorage)
    }
  }, [])
  return settings
}

export function homeWakeStatus(
  state: WakeWordState,
  zh: boolean,
  extra?: { heard?: string; deaf?: boolean },
): string | null {
  const heard = extra?.heard?.trim() ?? ''
  if (heard) {
    return zh ? `听到：${heard}` : `Heard: ${heard}`
  }
  if (state === 'probing') {
    return zh ? '正在接通麦克风…' : 'Opening the microphone…'
  }
  if (state === 'listening') {
    return zh ? '正在听：说「你好月汐」进入月伴' : 'Listening: say “你好月汐” to open companion talk'
  }
  if (state === 'error') {
    if (extra?.deaf) {
      return zh
        ? '听到声音但没有听出字。请点下方进入月伴，或检查语音模型。'
        : 'Heard sound but no words. Tap Companion talk below, or check the speech model.'
    }
    return zh ? '语音唤醒暂时不可用，请点下方进入月伴' : 'Voice wake is unavailable — tap Companion talk below'
  }
  return null
}

export function HomeWake({
  language,
  busy,
  entering,
  retry = 0,
  onWake,
}: {
  language: Language
  busy: boolean
  entering: boolean
  retry?: number
  onWake: (prompt: string) => void
}): JSX.Element | null {
  const settings = useCompanionSettings()
  const [visible, setVisible] = useState(() => document.visibilityState !== 'hidden')
  const [heard, setHeard] = useState('')
  const [deaf, setDeaf] = useState(false)
  const fired = useRef(false)
  useEffect(() => {
    const sync = () => setVisible(document.visibilityState !== 'hidden')
    document.addEventListener('visibilitychange', sync)
    return () => document.removeEventListener('visibilitychange', sync)
  }, [])
  useEffect(() => {
    fired.current = false
    setHeard('')
    setDeaf(false)
  }, [retry])
  const enabled = settings.enabled && settings.wakeWord && visible && !busy && !entering
  const state = useWakeWord({
    enabled,
    retry,
    vad: settings.wakeVad,
    onHeard: setHeard,
    onDeaf: () => setDeaf(true),
    onWake: prompt => {
      if (fired.current) return
      fired.current = true
      onWake(prompt)
    },
  })
  if (!settings.enabled) return null
  const zh = language === 'zh-CN'
  const status = enabled ? homeWakeStatus(state, zh, { heard, deaf }) : null
  if (!status) return null
  return (
    <p className="launch-wake" role="status">
      {status}
    </p>
  )
}
