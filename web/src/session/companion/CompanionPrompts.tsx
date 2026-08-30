import { useEffect, useState } from 'react'
import type { CompanionState } from './useCompanionMachine'
import { COMPANION_PROMPTS_EN, COMPANION_PROMPTS_ZH } from './visual/moonVisual'
import { prefersCompanionStillVisual } from './visual/webglSupport'

const APPEAR_MS = 1200
const ROTATE_MS = 4000

export function shouldShowCompanionPrompts(_input: {
  state: CompanionState
  hasUserRound: boolean
  hasInterim: boolean
  voiceHeard: boolean
}): boolean {
  // Live stage used to rotate sample chips («今天天气怎么样？») after 1.2s
  // of quiet listening. That looked like the user had already spoken.
  return false
}

export interface CompanionPromptsProps {
  visible: boolean
  language: 'zh' | 'en'
  onPick: (text: string) => void
}

export function CompanionPrompts({ visible, language, onPick }: CompanionPromptsProps): React.JSX.Element | null {
  const prompts = language === 'zh' ? COMPANION_PROMPTS_ZH : COMPANION_PROMPTS_EN
  const still = prefersCompanionStillVisual()
  const [armed, setArmed] = useState(false)
  const [index, setIndex] = useState(0)

  useEffect(() => {
    if (!visible) {
      setArmed(false)
      setIndex(0)
      return
    }
    const timer = window.setTimeout(() => setArmed(true), APPEAR_MS)
    return () => window.clearTimeout(timer)
  }, [visible])

  useEffect(() => {
    if (!visible || !armed || still) return
    const timer = window.setInterval(() => setIndex(current => (current + 1) % prompts.length), ROTATE_MS)
    return () => window.clearInterval(timer)
  }, [visible, armed, still, prompts.length])

  if (!visible || !armed) return null

  const labelFor = (text: string) => (language === 'zh' ? `试试问：${text}` : `Try asking: ${text}`)

  if (still) {
    return (
      <div className="companion-prompts" role="group" aria-label={language === 'zh' ? '试试这样问' : 'Try asking'}>
        {prompts.map(text => (
          <button
            key={text}
            type="button"
            className="companion-prompt"
            aria-label={labelFor(text)}
            onClick={() => onPick(text)}
          >
            {text}
          </button>
        ))}
      </div>
    )
  }

  const text = prompts[index] ?? prompts[0]
  return (
    <div className="companion-prompts">
      <button
        key={text}
        type="button"
        className="companion-prompt companion-prompt-fade"
        aria-label={labelFor(text)}
        onClick={() => onPick(text)}
      >
        {text}
      </button>
    </div>
  )
}
