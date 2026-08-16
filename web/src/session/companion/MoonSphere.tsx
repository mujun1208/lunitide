// MoonSphere.tsx is the M9.5 moon avatar (T-9.5.2.2): a pure SVG moon
// with CSS-layered visuals for the four companion states. It consumes
// only data-state and --moon-gain/--moon-level/--moon-wave plus the
// 12-bin listening levels — no business logic. Listening and speaking
// both render as breathing light ripples (no radial equalizer bars):
// --moon-wave is the mic average while listening and the playback gain
// while speaking. Colors reference the frozen theme variables
// (--moon/--glow/--tide1~3) and every animation collapses under the
// global .reduce-motion downgrade.
import type { CompanionState } from './useCompanionMachine'

export const MOON_RING_BINS = 12

export interface MoonSphereProps {
  state: CompanionState
  /** Audio gain in [0,1] driving the speaking glow; 0 outside speaking. */
  gain: number
  /** 12 normalized ring levels while listening; visual-only. */
  levels: number[]
  /** False only while thinking — every other state is clickable. */
  interruptible: boolean
  onInterrupt?: () => void
}

const idleLevels = Array.from({ length: MOON_RING_BINS }, () => 0)

const MOON_CLICK_LABELS: Record<CompanionState, string> = {
  idle: '月亮：轻点开始说话',
  listening: '月亮正在聆听，轻点暂停',
  thinking: '月亮思考中',
  speaking: '月亮正在说话，点击打断朗读',
}

export function MoonSphere({ state, gain, levels, interruptible, onInterrupt }: MoonSphereProps): React.JSX.Element {
  // Defensive pad/truncate: the average assumes the MOON_RING_BINS divisor,
  // so an off-size levels array (caller bug) never skews the visuals.
  const rawLevels = state === 'listening' ? levels : idleLevels
  const ringLevels = Array.from({ length: MOON_RING_BINS }, (_, i) => rawLevels[i] ?? 0)
  const ringAverage = ringLevels.reduce((sum, level) => sum + level, 0) / MOON_RING_BINS
  // One wave variable drives the ripple rings in both live states: the mic
  // level while listening, the playback gain while speaking.
  const wave = state === 'listening' ? Math.max(ringAverage, 0.08) : gain
  return (
    <div
      className={`companion-moon state-${state}`}
      data-state={state}
      style={{ '--moon-gain': gain, '--moon-level': ringAverage, '--moon-wave': wave } as React.CSSProperties}
    >
      <div className="companion-moon-halo" aria-hidden="true" />
      <div className="companion-moon-ripples" aria-hidden="true">
        <i />
        <i />
        <i />
      </div>
      <button
        type="button"
        className="companion-moon-body"
        onClick={interruptible ? onInterrupt : undefined}
        disabled={!interruptible}
        aria-label={MOON_CLICK_LABELS[state]}
        tabIndex={-1}
      >
        <svg viewBox="0 0 100 100" aria-hidden="true" focusable="false">
          <defs>
            <radialGradient id="companion-moon-gradient" cx="36%" cy="30%" r="86%">
              <stop offset="0%" stopColor="#ffffff" />
              <stop offset="30%" stopColor="#f6faff" />
              <stop offset="60%" stopColor="#dfeefc" />
              <stop offset="84%" stopColor="#c0d6f6" />
              <stop offset="100%" stopColor="#a2bfe6" />
            </radialGradient>
          </defs>
          <circle cx="50" cy="50" r="48" fill="url(#companion-moon-gradient)" />
          <g className="companion-moon-face">
            <ellipse cx="36" cy="52" rx="11" ry="8" fill="rgba(108,128,168,.16)" />
            <ellipse cx="66" cy="34" rx="6.5" ry="6" fill="rgba(105,126,166,.15)" />
            <ellipse cx="60" cy="70" rx="8" ry="6" fill="rgba(113,133,171,.14)" transform="rotate(-18 60 70)" />
            <ellipse cx="44" cy="26" rx="4.5" ry="3.6" fill="rgba(108,128,168,.12)" />
          </g>
        </svg>
      </button>
      <span className="companion-ring-value" role="status">{`音量 ${Math.round(ringAverage * 100)}%`}</span>
    </div>
  )
}
