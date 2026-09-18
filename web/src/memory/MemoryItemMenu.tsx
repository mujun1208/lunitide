import React, { useEffect, useRef, useState } from 'react'

export function MemoryItemMenu({
  label,
  onView,
  onCorrect,
  onForget,
}: {
  label: string
  onView: () => void
  onCorrect: () => void
  onForget: () => void
}): React.JSX.Element {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onPointer = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    window.addEventListener('mousedown', onPointer)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', onPointer)
      window.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div ref={rootRef} className="memory-item-menu">
      <button
        type="button"
        className="memory-item-more"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`更多 ${label}`}
        onClick={event => {
          event.stopPropagation()
          setOpen(value => !value)
        }}
      >
        更多
      </button>
      {open ? (
        <div className="memory-item-menu-list" role="menu">
          <button type="button" role="menuitem" onClick={() => { setOpen(false); onView() }}>查看</button>
          <button type="button" role="menuitem" onClick={() => { setOpen(false); onCorrect() }}>更正</button>
          <button type="button" role="menuitem" className="danger-item" onClick={() => { setOpen(false); onForget() }}>忘记</button>
        </div>
      ) : null}
    </div>
  )
}
