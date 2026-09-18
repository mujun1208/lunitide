import React, { useEffect, useRef } from 'react'

export type MemoryDrawerState =
  | { kind: 'closed' }
  | { kind: 'settings' }
  | { kind: 'item'; factId: string }
  | { kind: 'advanced'; section: 'recent' | 'review' | 'privacy' | 'import-export' | 'purge' }

const TITLES: Record<Exclude<MemoryDrawerState['kind'], 'closed'>, string> = {
  settings: '记忆设置',
  item: '记忆详情',
  advanced: '高级管理',
}

export function MemoryDrawer({
  state,
  onClose,
  children,
}: {
  state: MemoryDrawerState
  onClose: () => void
  children: React.ReactNode
}): React.JSX.Element | null {
  const closeRef = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (state.kind === 'closed') return
    closeRef.current?.focus()
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [state.kind, onClose])
  if (state.kind === 'closed') return null
  return (
    <aside className="memory-drawer" role="complementary" aria-label={TITLES[state.kind]}>
      <header className="memory-drawer-head">
        <h2>{TITLES[state.kind]}</h2>
        <button ref={closeRef} type="button" className="ui-btn" onClick={onClose} aria-label="关闭">关闭</button>
      </header>
      {children}
    </aside>
  )
}
