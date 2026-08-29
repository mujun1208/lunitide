// Follow-ups while a council/tool run is live attach to the same job.
// Only 停止 (the stop button or an explicit stop command) cancels.

export function isStopCommand(text: string): boolean {
  const t = text.trim().replace(/[。.！!]+$/u, '')
  return t === '停止' || t === '停止生成' || t.toLowerCase() === 'stop'
}

export function inFlightLiveChat(entry: { terminal?: boolean } | undefined, chatActive: boolean): boolean {
  return Boolean((entry && !entry.terminal) || chatActive)
}
