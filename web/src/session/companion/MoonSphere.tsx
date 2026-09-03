// MoonSphere.tsx is the M9.5 moon avatar (T-9.5.2.2): a volumetric
// glowing sphere with a soft halo. It consumes only data-state and
// --moon-gain/--moon-level — no business logic. Look: pale off-white
// disc, cool-blue limb, dark radial atmosphere — no crater texture
// and no stroked ring. When WebGL2 is available idle/listening start
// as a shader jade disc and bloom into a forced-motion Orb face;
// thinking/speaking keep the circular Strands glass ball (speaking used to
// flip to a small off-center wave). Glow still follows the mic level
// (listening) or playback gain (speaking). Animations collapse
// under .reduce-motion / missing WebGL.
import { Orb } from './visual/Orb'
import { Strands } from './visual/Strands'
import { moonVisualMode, orbProps, STRANDS_THINKING, strandsSpeaking } from './visual/moonVisual'
import { canUseCompanionWebgl } from './visual/webglSupport'
import type { CompanionState } from './useCompanionMachine'

export const MOON_RING_BINS = 12

export interface MoonSphereProps {
  state: CompanionState
  /** Audio gain in [0,1] driving the speaking glow; 0 outside speaking. */
  gain: number
  /** 12 normalized ring levels while listening; visual-only. */
  levels: number[]
  /** Moon is clickable except while listening (unless unlocking audio). */
  interruptible: boolean
  onInterrupt?: () => void
  /** 0 = just arrived (jade disc only), 1 = aurora and ring fully up. */
  enter?: number
}

const idleLevels = Array.from({ length: MOON_RING_BINS }, () => 0)

const MOON_CLICK_LABELS: Record<CompanionState, string> = {
  idle: '月亮：轻点开始说话',
  listening: '月亮正在聆听',
  thinking: '月亮正在回应',
  speaking: '月亮正在说话，点击打断朗读',
}

export function MoonSphere({ state, gain, levels, interruptible, onInterrupt, enter = 1 }: MoonSphereProps): React.JSX.Element {
  const rawLevels = state === 'listening' ? levels : idleLevels
  const ringLevels = Array.from({ length: MOON_RING_BINS }, (_, i) => rawLevels[i] ?? 0)
  const ringAverage = ringLevels.reduce((sum, level) => sum + level, 0) / MOON_RING_BINS
  const visual = canUseCompanionWebgl() ? 'webgl' : 'css'
  const mode = moonVisualMode(state)
  const orb = orbProps(state, ringAverage, enter)
  const strands = state === 'speaking' ? strandsSpeaking(gain) : STRANDS_THINKING
  return (
    <div
      className={`companion-moon state-${state}`}
      data-state={state}
      data-visual={visual}
      data-mode={mode}
      style={{ '--moon-gain': gain, '--moon-level': ringAverage, '--moon-enter': enter } as React.CSSProperties}
    >
      {visual === 'webgl' && (
        <>
          <div className={`companion-moon-orb${mode === 'orb' ? ' is-on' : ''}`} aria-hidden="true">
            <Orb {...orb} active={mode === 'orb'} />
          </div>
          <div className={`companion-moon-strands${mode !== 'orb' ? ' is-on' : ''}`} aria-hidden="true">
            <Strands {...strands} active={mode !== 'orb'} />
          </div>
        </>
      )}
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
          <span className="companion-moon-face" />
          <span className="companion-moon-sheen" />
        </span>
      </button>
    </div>
  )
}
