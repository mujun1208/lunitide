import React from 'react'

export type ChoiceTileOption<T extends string> = { value: T; label: string; desc: string; disabled?: boolean; disabledReason?: string }

export function ChoiceTiles<T extends string>({
  legend,
  name,
  value,
  options,
  onChange,
  legendClassName,
}: {
  legend: string
  name: string
  value: T
  options: readonly ChoiceTileOption<T>[]
  onChange: (value: T) => void
  legendClassName?: string
}): React.JSX.Element {
  return (
    <fieldset className="choice-tiles">
      <legend className={legendClassName ? `choice-tiles-legend ${legendClassName}` : 'choice-tiles-legend'}>{legend}</legend>
      <div className="choice-tile-grid" role="radiogroup" aria-label={legend}>
        {options.map(option => {
          const on = option.value === value
          const disabled = !!option.disabled
          return (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={on}
              aria-disabled={disabled || undefined}
              disabled={disabled}
              title={disabled ? option.disabledReason : undefined}
              className={`choice-tile${on ? ' on' : ''}${disabled ? ' is-disabled' : ''}`}
              name={name}
              onClick={() => { if (!disabled) onChange(option.value) }}
            >
              <b>{option.label}</b>
              <small>{disabled && option.disabledReason ? option.disabledReason : option.desc}</small>
            </button>
          )
        })}
      </div>
    </fieldset>
  )
}
