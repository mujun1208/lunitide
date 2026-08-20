// MoonSphere.tsx is the M9.5 moon avatar (T-9.5.2.2): a photoreal moon
// disc with CSS-layered visuals for the four companion states. It consumes
// only data-state and --moon-gain/--moon-level — no business logic.
// Look: reference full-moon texture, limb bloom (solid-to-soft aura), no
// watermark and no stroked halo ring. Glow still flickers with the mic
// level (listening) or playback gain (speaking). Animations collapse
// under .reduce-motion.
import type { CompanionState } from './useCompanionMachine'

export const MOON_RING_BINS = 12
export const MOON_TEXTURE = '/brand/moon-companion.png'

export interface MoonSphereProps {
  state: CompanionState
  /** Audio gain in [0,1] driving the speaking glow; 0 outside speaking. */
  gain: number
  /** 12 normalized ring levels while listening; visual-only. */
  levels: number[]
  /** Moon is clickable in every state except pure listening (mic is live). */
  interruptible: boolean
  onInterrupt?: () => void
}

const idleLevels = Array.from({ length: MOON_RING_BINS }, () => 0)

const MOON_CLICK_LABELS: Record<CompanionState, string> = {
  idle: '月亮：轻点开始说话',
  listening: '月亮正在聆听，轻点暂停',
  thinking: '月亮正在回应',
  speaking: '月亮正在说话，点击打断朗读',
}

export function MoonSphere({ state, gain, levels, interruptible, onInterrupt }: MoonSphereProps): React.JSX.Element {
  const rawLevels = state === 'listening' ? levels : idleLevels
  const ringLevels = Array.from({ length: MOON_RING_BINS }, (_, i) => rawLevels[i] ?? 0)
  const ringAverage = ringLevels.reduce((sum, level) => sum + level, 0) / MOON_RING_BINS
  return (
    <div
      className={`companion-moon state-${state}`}
      data-state={state}
      style={{ '--moon-gain': gain, '--moon-level': ringAverage } as React.CSSProperties}
    >
      <div className="companion-moon-halo" aria-hidden="true" />
      <div className="companion-moon-halo-wave" aria-hidden="true" />
      <button
        type="button"
        className="companion-moon-body"
        onClick={interruptible ? onInterrupt : undefined}
        disabled={!interruptible}
        aria-label={MOON_CLICK_LABELS[state]}
        tabIndex={-1}
      >
        <span className="companion-moon-disc" aria-hidden="true">
          <span className="companion-moon-face">
            <img src={MOON_TEXTURE} alt="" draggable={false} />
          </span>
        </span>
      </button>
    </div>
  )
}
