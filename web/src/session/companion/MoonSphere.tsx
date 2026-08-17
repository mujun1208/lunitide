// MoonSphere.tsx is the M9.5 moon avatar (T-9.5.2.2): a pure SVG moon
// with CSS-layered visuals for the four companion states. It consumes
// only data-state and --moon-gain/--moon-level — no business logic.
// The look follows the frozen reference art: a cold blue-grey moon with
// a bright lower face, dark crater blotches pinned to the upper half, a
// near-white rim line and a soft blue glow that flickers with the mic
// level (listening) or playback gain (speaking) — no ripple rings.
// Colors reference the frozen theme variables and every animation
// collapses under the global .reduce-motion downgrade.
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
  return (
    <div
      className={`companion-moon state-${state}`}
      data-state={state}
      style={{ '--moon-gain': gain, '--moon-level': ringAverage } as React.CSSProperties}
    >
      <div className="companion-moon-halo" aria-hidden="true" />
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
            {/* Reference-art moon: bright white lower face fading into a
                cold blue-grey upper hemisphere with a dark limb. */}
            <radialGradient id="companion-moon-gradient" cx="50%" cy="74%" r="84%">
              <stop offset="0%" stopColor="#ffffff" />
              <stop offset="20%" stopColor="#f4fafa" />
              <stop offset="46%" stopColor="#c9d2d8" />
              <stop offset="70%" stopColor="#a8b3bb" />
              <stop offset="88%" stopColor="#8c949a" />
              <stop offset="100%" stopColor="#6d8195" />
            </radialGradient>
          </defs>
          <circle cx="50" cy="50" r="48" fill="url(#companion-moon-gradient)" />
          {/* Crater blotches ride the slowly spinning face group and stay
              in the upper hemisphere (lower face stays the bright zone). */}
          <g className="companion-moon-face">
            <ellipse cx="34" cy="34" rx="10" ry="7.5" fill="#4e647a" opacity=".5" />
            <ellipse cx="62" cy="27" rx="6" ry="5" fill="#79838c" opacity=".55" />
            <ellipse cx="68" cy="48" rx="8" ry="6" fill="#4e647a" opacity=".4" transform="rotate(-14 68 48)" />
            <ellipse cx="43" cy="24" rx="4.5" ry="3.5" fill="#79838c" opacity=".5" />
            <ellipse cx="29" cy="52" rx="5" ry="4" fill="#4e647a" opacity=".34" />
          </g>
          {/* Near-white rim line — the reference art's signature stroke. */}
          <circle cx="50" cy="50" r="47.1" fill="none" stroke="#fdfeff" strokeWidth="1.3" opacity=".92" />
        </svg>
      </button>
      <span className="companion-ring-value" role="status">{`音量 ${Math.round(ringAverage * 100)}%`}</span>
    </div>
  )
}
