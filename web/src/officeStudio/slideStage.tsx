import React from 'react'
import type { OfficeSlideShape } from './officeStudioApi'

export function slidePaintColor(slideFill: string, color?: string): string {
  const given = (color || '').replace('#', '')
  if (/^[0-9A-Fa-f]{6}$/.test(given)) return `#${given.toUpperCase()}`
  return readableOn(slideFill)
}

function readableOn(fill: string): string {
  const hex = fill.replace('#', '')
  if (!/^[0-9A-Fa-f]{6}$/.test(hex)) return '#1F2937'
  const red = parseInt(hex.slice(0, 2), 16)
  const green = parseInt(hex.slice(2, 4), 16)
  const blue = parseInt(hex.slice(4, 6), 16)
  const luminance = (red * 299 + green * 587 + blue * 114) / 1000
  return luminance > 160 ? '#1F2937' : '#F8FAFC'
}

export function OfficeSlideStage({
  fill,
  shapes,
  onSelectText,
}: {
  fill: string
  shapes: OfficeSlideShape[]
  onSelectText?: (shape: OfficeSlideShape) => void
}): React.JSX.Element {
  return (
    <div className="os-slide-stage" style={fill ? { background: fill } : undefined}>
      {shapes.map((shape, index) => {
        const style: React.CSSProperties = {
          left: `${shape.x}%`,
          top: `${shape.y}%`,
          width: `${shape.w}%`,
          height: `${shape.h}%`,
          color: slidePaintColor(fill, shape.color),
          background: shape.fill ? `#${shape.fill.replace('#', '')}` : undefined,
          fontSize: shape.size ? `${shape.size}cqw` : undefined,
          fontWeight: shape.bold ? 650 : undefined,
        }
        const text = (shape.text || '').trim()
        if (!text) {
          return <span key={`fill-${index}`} className="os-slide-shape os-slide-fill" style={style} aria-hidden="true" />
        }
        if (!onSelectText) {
          return (
            <span key={`text-${index}`} className="os-slide-shape" style={style}>
              {text}
            </span>
          )
        }
        return (
          <button key={`text-${index}`} type="button" className="os-slide-shape" style={style} onClick={() => onSelectText(shape)}>
            {text}
          </button>
        )
      })}
    </div>
  )
}
