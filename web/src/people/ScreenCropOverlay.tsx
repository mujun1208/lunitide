import React, { useEffect, useRef, useState } from 'react'
import { cropImageFile, mapContainPoint, normalizeCropRect, type CropRect } from './peopleCapture'

export function ScreenCropOverlay({
  file,
  onConfirm,
  onCancel,
}: {
  file: File
  onConfirm: (file: File) => void
  onCancel: () => void
}): React.JSX.Element {
  const stage = useRef<HTMLDivElement>(null)
  const img = useRef<HTMLImageElement>(null)
  const [url, setUrl] = useState('')
  const [drag, setDrag] = useState<{ x0: number; y0: number; x1: number; y1: number }>()
  const [sel, setSel] = useState<CropRect>()
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (typeof URL.createObjectURL !== 'function') return undefined
    const next = URL.createObjectURL(file)
    setUrl(next)
    return () => {
      if (typeof URL.revokeObjectURL === 'function') URL.revokeObjectURL(next)
    }
  }, [file])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onCancel()
      }
      if (e.key === 'Enter' && sel && !busy) {
        e.preventDefault()
        void confirm(sel)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [sel, busy, onCancel])

  const pointOnImage = (clientX: number, clientY: number) => {
    const el = img.current
    const box = stage.current?.getBoundingClientRect()
    if (!el || !box) return undefined
    return mapContainPoint(clientX, clientY, box, el.naturalWidth || 0, el.naturalHeight || 0)
  }

  const confirm = async (rect: CropRect) => {
    if (busy) return
    setBusy(true)
    try {
      onConfirm(await cropImageFile(file, rect, 512 * 1024))
    } catch {
      onCancel()
    }
  }

  const displaySel = (): { left: number; top: number; width: number; height: number } | undefined => {
    const rect = sel || (drag ? normalizeCropRect(drag.x0, drag.y0, drag.x1, drag.y1, img.current?.naturalWidth || 0, img.current?.naturalHeight || 0) : undefined)
    const el = img.current
    const box = stage.current?.getBoundingClientRect()
    if (!rect || !el || !box || !el.naturalWidth) return undefined
    const scale = Math.min(box.width / el.naturalWidth, box.height / el.naturalHeight)
    const dispW = el.naturalWidth * scale
    const dispH = el.naturalHeight * scale
    const offX = (box.width - dispW) / 2
    const offY = (box.height - dispH) / 2
    return {
      left: offX + rect.x * scale,
      top: offY + rect.y * scale,
      width: rect.w * scale,
      height: rect.h * scale,
    }
  }

  const box = displaySel()

  return (
    <div
      ref={stage}
      className="people-snip-overlay"
      role="dialog"
      aria-label="框选截图"
      onContextMenu={e => { e.preventDefault(); onCancel() }}
      onPointerDown={e => {
        if ((e.target as HTMLElement).closest('button')) return
        const pt = pointOnImage(e.clientX, e.clientY)
        if (!pt) return
        ;(e.currentTarget as HTMLDivElement).setPointerCapture(e.pointerId)
        setSel(undefined)
        setDrag({ x0: pt.x, y0: pt.y, x1: pt.x, y1: pt.y })
      }}
      onPointerMove={e => {
        if (!drag) return
        const pt = pointOnImage(e.clientX, e.clientY)
        if (!pt) return
        setDrag(d => d ? { ...d, x1: pt.x, y1: pt.y } : d)
      }}
      onPointerUp={() => {
        if (!drag) return
        const next = normalizeCropRect(drag.x0, drag.y0, drag.x1, drag.y1, img.current?.naturalWidth || 0, img.current?.naturalHeight || 0)
        setDrag(undefined)
        setSel(next)
      }}
    >
      {url ? <img ref={img} src={url} alt="" draggable={false} /> : null}
      {box ? <div className="people-snip-sel" style={box} /> : null}
      <p className="people-snip-hint">拖动选择区域 · 点完成或按 Enter 发送 · 右键 / Esc 取消</p>
      <div className="people-snip-toolbar">
        {sel ? <button type="button" className="primary" disabled={busy} onClick={() => void confirm(sel)}>完成</button> : null}
        <button type="button" disabled={busy} onClick={onCancel}>取消</button>
      </div>
    </div>
  )
}
