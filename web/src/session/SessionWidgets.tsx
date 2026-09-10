import {useEffect, useState} from 'react'
import {getWidgetBridge, type WidgetBridge} from '../bridge/client'
import {WidgetBoard} from '../widgets/WidgetBoard'
import {isRegisteredWidget, type WidgetSpec, type WidgetState} from '../widgets/registry'

type BoardWidget = WidgetSpec & {boardId: string; revision: number; boardTitle: string}

export function SessionWidgetsBar({sessionId, zh, widgetApi}: {sessionId: string; zh: boolean; widgetApi?: WidgetBridge}) {
  const [widgets, setWidgets] = useState<BoardWidget[]>()
  const api = widgetApi ?? getWidgetBridge()
  useEffect(() => {
    let cancelled = false
    try {
      void api.query({owner: sessionId}).then(next => {
        if (cancelled) return
        const specs: BoardWidget[] = []
        for (const item of next.items ?? []) {
          for (const widget of item.widgets ?? []) {
            if (isRegisteredWidget(widget.kind)) {
              specs.push({
                id: widget.id,
                kind: widget.kind,
                title: item.title,
                state: widget.state,
                boardId: item.id,
                revision: item.revision,
                boardTitle: item.title,
              })
            }
          }
        }
        setWidgets(specs)
      }).catch(() => {
        if (!cancelled) setWidgets([])
      })
    } catch {
      setWidgets([])
    }
    return () => { cancelled = true }
  }, [sessionId, widgetApi])
  const persist = (id: string, state: WidgetState) => {
    const current = widgets ?? []
    const target = current.find(item => item.id === id)
    if (!target || !api.update) return
    const siblings = current.filter(item => item.boardId === target.boardId).map(item => item.id === id ? {...item, state} : item)
    setWidgets(current.map(item => item.id === id ? {...item, state} : item))
    void api.update({
      owner: sessionId,
      id: target.boardId,
      revision: target.revision,
      title: target.boardTitle,
      widgets: siblings.map(item => ({id: item.id, kind: item.kind, ...(item.state ? {state: item.state} : {})})),
    }).then(got => {
      setWidgets(cur => (cur ?? []).map(item => item.boardId === target.boardId ? {...item, revision: got.revision} : item))
    }).catch(() => undefined)
  }
  if (!widgets || widgets.length === 0) return null
  return (
    <div className="chat-usage token-usage" role="status" aria-label={zh ? '本会话组件' : 'Session widgets'}>
      <WidgetBoard widgets={widgets} onState={persist} />
    </div>
  )
}
