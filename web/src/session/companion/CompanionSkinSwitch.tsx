import type { CompanionSkin } from './particle/particleMoon'

export function CompanionSkinSwitch({
  value,
  onChange,
  compact = false,
  zh = true,
}: {
  value: CompanionSkin
  onChange: (next: CompanionSkin) => void
  compact?: boolean
  zh?: boolean
}): React.JSX.Element {
  const classic = zh ? '玉盘' : 'Jade'
  const particle = zh ? '星尘' : 'Stardust'
  return (
    <div
      className={`companion-skin-switch${compact ? ' is-compact' : ''}`}
      role="radiogroup"
      aria-label={zh ? '月伴外观' : 'Companion look'}
    >
      <button type="button" role="radio" aria-checked={value === 'classic'} aria-pressed={value === 'classic'} onClick={() => onChange('classic')}>
        {classic}
      </button>
      <button type="button" role="radio" aria-checked={value === 'particle'} aria-pressed={value === 'particle'} onClick={() => onChange('particle')}>
        {particle}
      </button>
    </div>
  )
}
