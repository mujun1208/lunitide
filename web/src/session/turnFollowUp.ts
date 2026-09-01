// Follow-ups while a council/tool run is live attach to the same job.
// Only 停止 (the stop button or an explicit stop command) cancels.

export type FollowUpKind = 'progress' | 'supplement' | 'task_change'

export function isStopCommand(text: string): boolean {
  const t = text.trim().replace(/[。.！!]+$/u, '')
  return t === '停止' || t === '停止生成' || t.toLowerCase() === 'stop'
}

export function inFlightLiveChat(entry: { terminal?: boolean } | undefined, chatActive: boolean): boolean {
  return Boolean((entry && !entry.terminal) || chatActive)
}

/** Same user sentence while a companion turn is live: queue/drop, never reset+restart. */
export function companionShouldDeferInFlight(incoming: string, activeGoal: string): boolean {
  const next = incoming.trim()
  return next !== '' && next === activeGoal.trim()
}

/**
 * Companion enters without loading history, so `items` stays []. Deleting on
 * exit used to wipe a turn that had already been appended.
 */
export function companionShouldDiscardOnExit(input: {
  initialCompanion: boolean
  itemsLength: number
  chatActive: boolean
  sessionEngaged: boolean
}): boolean {
  if (!input.initialCompanion) return false
  if (input.chatActive || input.sessionEngaged) return false
  return input.itemsLength === 0
}

/** Progress check while a job is live or just failed. Must attach, not wipe. */
export function isStatusFollowUp(text: string): boolean {
  const t = text.trim().replace(/[？?。.!！~…\s]+$/u, '')
  if (!t || Array.from(t).length > 40) return false
  const lower = t.toLowerCase()
  const needles = [
    '做好了没有', '做好了吗', '做完了没有', '做完了吗', '做完了没', '做好了没',
    '好了没有', '好了吗', '好了没', '完成了吗', '完成了没有', '弄好了吗',
    '还要多久', '要多久', '还要等吗', '等多久',
    '还在做吗', '在做吗', '还在跑吗', '还在吗',
    '什么进度', '到哪了', '到哪一步', '怎么样了', '如何了',
    'done yet', 'is it done', 'how long', 'progress',
  ]
  if (needles.some(p => lower === p || lower.includes(p))) return true
  return t === '进度' || lower === 'status' || lower === 'eta'
}

const supplementPrefixes = [
  '只要', '改成', '改用', '换成', '不要用', '别用', '补充', '再加上', '还有就是',
  '用这个', '继续用', '只装', '封面', '先做出', '先写', '加上', '别忘了', '记得',
]
const steerPatterns = ['改方案', '改方向', '换个方向', '调整一下', '换个结构', '不要这样']
const taskChangeNegations = [
  '别做', '不要做', '不要', '别写', '别生成', '别用', '停止这个', '不做', '算了',
  '换话题', '换个话题', '别管', '先别', '别帮我做', '不用做',
]

function looksLikeResume(text: string): boolean {
  const t = text.trim()
  return t === '继续' || t.startsWith('继续上次') || t.includes('未完成的工作')
}

/** New unrelated work that should cancel the in-flight turn. */
export function looksLikeTaskChange(text: string, activeGoal?: string): boolean {
  const t = text.trim()
  if (!t || !activeGoal?.trim() || isStatusFollowUp(t) || isStopCommand(t) || looksLikeResume(t)) return false
  for (const p of supplementPrefixes) {
    if (t.startsWith(p)) return false
  }
  for (const p of steerPatterns) {
    if (t.includes(p)) return false
  }
  for (const p of taskChangeNegations) {
    if (t.includes(p)) return true
  }
  return looksLikeIndependentTask(t)
}

function looksLikeIndependentTask(text: string): boolean {
  const t = text.trim()
  if (!t) return false
  for (const p of supplementPrefixes) {
    if (t.startsWith(p)) return false
  }
  for (const p of steerPatterns) {
    if (t.includes(p)) return false
  }
  return true
}

/** Classify an in-flight follow-up: merge (supplement), attach progress, or pivot task. */
export function classifyFollowUp(text: string, activeGoal?: string): FollowUpKind {
  if (isStatusFollowUp(text)) return 'progress'
  if (looksLikeTaskChange(text, activeGoal)) return 'task_change'
  return 'supplement'
}
