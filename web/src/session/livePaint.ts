/** One paint slot for a live chat turn. Stream tokens update the entry
 * immediately; the message panel reads them once per frame so the process
 * text and the spinner commit together. */
export type PaintSlot = { frame: number }

export function scheduleLivePaint(slot: PaintSlot, flush: () => void): void {
  if (slot.frame) return
  slot.frame = requestAnimationFrame(() => {
    slot.frame = 0
    flush()
  })
}

export function cancelLivePaint(slot: PaintSlot): void {
  if (!slot.frame) return
  cancelAnimationFrame(slot.frame)
  slot.frame = 0
}

/** A long reasoning stream is painted from the end. The full text stays in
 * the live entry; the panel only lays out the latest stretch. */
export function streamingThinkingTail(text: string, limit = 4000): string {
  if (text.length <= limit) return text
  return `…\n${text.slice(text.length - limit)}`
}
