import React from 'react'

export type ChoiceTileOption<T extends string> = { value: T; label: string; desc: string }

export function ChoiceTiles<T extends string>({
  legend,
  name,
  value,
  options,
  onChange,
}: {
  legend: string
  name: string
  value: T
  options: readonly ChoiceTileOption<T>[]
  onChange: (value: T) => void
}): React.JSX.Element {
  return (
    <fieldset className="choice-tiles">
      <legend className="choice-tiles-legend">{legend}</legend>
      <div className="choice-tile-grid" role="radiogroup" aria-label={legend}>
        {options.map(option => {
          const on = option.value === value
          return (
            <button
              key={option.value}
              type="button"
              role="radio"
              aria-checked={on}
              className={on ? 'choice-tile on' : 'choice-tile'}
              name={name}
              onClick={() => onChange(option.value)}
            >
              <b>{option.label}</b>
              <small>{option.desc}</small>
            </button>
          )
        })}
      </div>
    </fieldset>
  )
}
