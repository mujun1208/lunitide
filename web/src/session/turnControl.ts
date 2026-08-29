// Market-standard composer + per-turn stop behavior (ChatGPT / Claude patterns).

export type ComposerPrimaryAction = 'send' | 'stop' | 'follow-up-send'

/** Primary composer button while idle, streaming, or sending a follow-up. */
export function composerPrimaryAction(opts: {
  streaming: boolean
  activeTurnCount: number
  composerHasText: boolean
}): ComposerPrimaryAction {
  if (!opts.streaming) return 'send'
  if (opts.composerHasText) return 'follow-up-send'
  if (opts.activeTurnCount <= 1) return 'stop'
  return 'follow-up-send'
}

export function composerPrimaryLabel(action: ComposerPrimaryAction): string {
  switch (action) {
    case 'send':
      return '↑ 发送并对话'
    case 'stop':
      return '停止'
    case 'follow-up-send':
      return '↑ 发送'
  }
}

/** Separate stop chip in composer toolbar when multiple turns are in flight. */
export function showComposerStopButton(streaming: boolean, activeTurnCount: number): boolean {
  return streaming && activeTurnCount > 1
}

/** Stop control on the live agent segment or active user round. */
export function showSegmentStopControl(activeTurnCount: number): boolean {
  return activeTurnCount >= 1
}

export const FOLLOW_UP_QUEUE_NOTICE = '已排队，将在当前步继续…'
