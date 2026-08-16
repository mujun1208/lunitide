// MoonSphere.tsx is the M9.5 moon avatar (T-9.5.2.2): a pure SVG moon
// with CSS-layered visuals for the four companion states. It consumes
// only data-state and --moon-gain plus the 12-bin listening ring
// levels — no business logic. Colors reference the frozen theme
// variables (--moon/--glow/--tide1~3) and every animation collapses
// under the global .reduce-motion downgrade.
import type { CompanionState } from './useCompanionMachine'

export const MOON_RING_BINS = 12

export interface MoonSphereProps {
  state: CompanionState
  /** Audio gain in [0,1] driving the speaking glow; 0 outside speaking. */
  gain: number
  /** 12 normalized ring levels while listening; visual-only. */
  levels: number[]
  /** True while speaking — a moon click interrupts playback. */
  interruptible: boolean
  onInterrupt?: () => void
}

const idleLevels = Array.from({ length: MOON_RING_BINS }, () => 0)

export function MoonSphere({ state, gain, levels, interruptible, onInterrupt }: MoonSphereProps): React.JSX.Element {
  const ringLevels = state === 'listening' ? levels : idleLevels
  const ringAverage = ringLevels.reduce((sum, level) => sum + level, 0) / MOON_RING_BINS
  return (
    <div className={`companion-moon state-${state}`} data-state={state} style={{ '--moon-gain': gain } as React.CSSProperties}>
      <div className="companion-moon-halo" aria-hidden="true" />
      <div className="companion-moon-ring" aria-hidden="true">
        {ringLevels.map((level, index) => (
          <i key={index} style={{ '--ring-level': Math.max(0.06, level), '--ring-angle': `${(360 / MOON_RING_BINS) * index}deg` } as React.CSSProperties} />
        ))}
      </div>
      <button
        type="button"
        className="companion-moon-body"
        onClick={interruptible ? onInterrupt : undefined}
        disabled={!interruptible}
        aria-label={interruptible ? '月亮正在说话，点击打断朗读' : '月亮'}
        tabIndex={-1}
      >
        <svg viewBox="0 0 100 100" aria-hidden="true" focusable="false">
          <defs>
            <radialGradient id="companion-moon-gradient" cx="36%" cy="30%" r="82%">
              <stop offset="0%" stopColor="#ffffff" />
              <stop offset="26%" stopColor="#f1f5ff" />
              <stop offset="62%" stopColor="#d6e1f6" />
              <stop offset="84%" stopColor="#aebfdf" />
              <stop offset="100%" stopColor="#8399c1" />
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
