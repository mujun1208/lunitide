import type { AgentHubState } from './agentHubApi'

export type HubScene = 'ppt' | 'write' | 'fix' | 'free'
export type AgentHubThreadScene = 'write_project' | 'fix' | 'ppt' | 'free'
export type InboxFile = { name: string; path: string; size: number }

export const PICK_PROJECT_DIR = '请先选择项目目录'

export const SCENE_BLURBS: Record<AgentHubThreadScene, string> = {
  write_project: '在你选的文件夹里按你的规则创建子目录并写文件。不要把已有文件挪到别处。',
  fix: '在此仓库根内检索和修改。已有文件保持原路径。新文件按已有结构和你的规则放置。',
  ppt: '用 Kimi 自己的技能做文稿。pptx 写在工作区；指定了导出目录则完成时复制过去。',
  free: '',
}

export const ACCESS_MODES = [
  { id: 'approval' as const, zh: '手动', en: 'Manual' },
  { id: 'auto-edit' as const, zh: '自动', en: 'Auto' },
  { id: 'full-access' as const, zh: '完全访问', en: 'Full access' },
] as const

export const THREAD_SCENES = [
  { id: 'write' as const, zh: '写项目', en: 'Write project' },
  { id: 'fix' as const, zh: '改代码', en: 'Fix code' },
  { id: 'ppt' as const, zh: '做 PPT', en: 'Make a PPT' },
  { id: 'free' as const, zh: '自由', en: 'Free' },
] as const

export function sceneBlurb(scene: AgentHubThreadScene): string {
  return SCENE_BLURBS[scene]
}

export function threadTitleFromPrompt(text: string): string {
  const first = text.trim().split(/\r?\n/, 1)[0]?.trim() ?? ''
  if (!first) return ''
  const runes = Array.from(first)
  return runes.length <= 200 ? first : runes.slice(0, 200).join('')
}

export function hubSceneToThreadScene(scene: HubScene): AgentHubThreadScene {
  return scene === 'write' ? 'write_project' : scene
}

export const SCENE_KEY = 'lunitide:agent-hub-scene'
export const workDirKey = (scene: HubScene) => `lunitide:agent-hub-workdir:${scene}`

export function scenePrefix(scene: HubScene, workDir: string, userText: string): string {
  if (scene === 'free') return userText
  if (scene === 'ppt') {
    return `【场景：做 PPT】\n工作目录：${workDir}\n只在本目录写文件。优先使用 kimi-slides。产出 pptx。参考文件在本目录或 .agenthub-inbox 时先读再做。\n\n用户任务：\n${userText}`
  }
  if (scene === 'write') {
    return `【场景：写新项目】\n项目根：${workDir}\n把该路径当作唯一项目根。只在该根下创建文件夹和文件。\n先读 AGENTS.md、.cursor/rules、README（没有则跳过）。\n用户传入的需求/设计在 .agenthub-inbox（没有则跳过）。\n用户写的规则优先。做完即结束。\n\n用户任务：\n${userText}`
  }
  return `【场景：改现有代码】\n项目根：${workDir}\n只阅读和修改这个根内的文件。先看 README、AGENTS.md 和相关源码。\n用户传入的说明/截图在 .agenthub-inbox（没有则跳过）。\n改动保持最小。不要 git 提交或推远程。做完即结束。\n\n用户任务：\n${userText}`
}

export function inboxPrefix(files: InboxFile[]): string {
  if (files.length === 0) return ''
  const lines = files.map(item => `- ${item.path}`)
  return `\n\n用户传入的文件已复制到 .agenthub-inbox/（原路径未改）。请先阅读。可以修改这些副本，或在本工作目录写出新文件。不要去改用户原路径上的文件。完成后把结果留在本目录。\n${lines.join('\n')}`
}

export function composeHubPrompt(scene: HubScene, workDir: string, userText: string, files: InboxFile[]): string {
  return `${scenePrefix(scene, workDir, userText)}${inboxPrefix(files)}`
}

export const FREE_TEMPLATES = [
  { zh: '写文档', en: 'Write docs', prompt: '根据本工作目录已有材料写一份 Markdown 说明，只在本目录保存。' },
  { zh: '总结本目录', en: 'Summarize this folder', prompt: '阅读本工作目录里的资料（含 .agenthub-inbox 与 pdf/ppt/md/txt），写一份结构化总结 Markdown，只在本目录保存。' },
  { zh: '做小游戏', en: 'Make a small game', prompt: '在本工作目录做一个可运行的小游戏（说明怎么运行），只在本目录写文件。' },
  { zh: '写周报 Markdown', en: 'Write weekly-report Markdown', prompt: '根据本工作目录材料写一份周报 Markdown，不要生成 Office 文档。' },
] as const

export const HUB_TEMPLATES = FREE_TEMPLATES

export const HUB_SCENES = [
  { id: 'ppt' as const, agent: 'kimi' as const, zh: '做 PPT', en: 'Make a PPT', subZh: '固定交给 Kimi，用它自己的技能做演示文稿', subEn: 'Always Kimi, using its own slides skill' },
  { id: 'write' as const, agent: 'cursor' as const, zh: '写新项目', en: 'New project', subZh: '先选项目根，固定交给 Cursor 建目录并写代码', subEn: 'Pick a root, then Cursor writes the project' },
  { id: 'fix' as const, agent: 'codex' as const, zh: '改现有代码', en: 'Fix code', subZh: '先选仓库根，固定交给 Codex 阅读并修改', subEn: 'Pick a repo root, then Codex edits' },
  { id: 'free' as const, agent: '' as const, zh: '其它任务', en: 'Other task', subZh: '自己选 Codex / Cursor / Kimi：写文档、总结资料、做小游戏…', subEn: 'Pick Codex, Cursor or Kimi yourself' },
] as const

export function parentWorkspacePath(path: string): string {
  const parts = path.replace(/\\/g, '/').split('/').filter(Boolean)
  return parts.slice(0, -1).join('/')
}

function producedPptx(item: { name: string; path: string; source?: string }): boolean {
  const path = item.path.replace(/\\/g, '/')
  if (path.includes('.agenthub-inbox/')) return false
  if (item.source === 'inbox') return false
  return /\.pptx$/i.test(item.name) || /\.pptx$/i.test(path)
}

export function threadPptMissing(scene: string, files: { name: string; path: string; source?: string }[]): boolean {
  if (scene !== 'ppt') return false
  return !files.some(producedPptx)
}

export function pptDeckMissing(agent: string, prompt: string, artifacts: { name: string; path: string; source?: string }[]): boolean {
  if (agent !== 'kimi' || !prompt.includes('【场景：做 PPT】')) return false
  return !artifacts.some(item => {
    if (item.source && item.source !== 'changed' && item.source !== 'event') return false
    return /\.pptx$/i.test(item.name) || /\.pptx$/i.test(item.path)
  })
}

export function visibleHubArtifacts<T extends { source: string }>(items: T[], showScan: boolean): T[] {
  return items.filter(item => {
    if (item.source === 'outside') return false
    if (item.source === 'scan') return showScan
    return item.source === 'event' || item.source === 'changed' || item.source === 'inbox'
  })
}

export function stateLabel(state: AgentHubState, zh: boolean): string {
  if (state === 'available') return zh ? '可用' : 'Available'
  if (state === 'not_installed') return zh ? '未安装' : 'Not installed'
  if (state === 'not_logged_in') return zh ? '未登录' : 'Not signed in'
  return zh ? '未知' : 'Unknown'
}

export function statusLabel(status: string, zh: boolean): string {
  const map: Record<string, [string, string]> = {
    queued: ['排队中', 'Queued'],
    running: ['进行中', 'Running'],
    success: ['已完成', 'Done'],
    failed: ['失败', 'Failed'],
    timeout: ['超时', 'Timed out'],
    cancelled: ['已取消', 'Cancelled'],
    idle: ['空闲', 'Idle'],
    waiting_user: ['等你回答', 'Waiting for you'],
    faulted: ['失败', 'Failed'],
  }
  const pair = map[status]
  return pair ? (zh ? pair[0] : pair[1]) : status
}

export function agentMark(name: string): string {
  if (name === 'cursor') return 'Cu'
  if (name === 'kimi') return 'K'
  return 'C'
}

export function newIdempotencyKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, value => value.toString(16).padStart(2, '0')).join('')
}

export function shortWorkDir(path: string): string {
  const parts = path.replace(/\\/g, '/').split('/').filter(Boolean)
  if (parts.length <= 2) return parts.join('/') || path
  return parts.slice(-2).join('/')
}

export function taskElapsed(task: { startedAt?: string; finishedAt?: string }, nowMs: number): string {
  if (!task.startedAt) return '—'
  const start = Date.parse(task.startedAt)
  const end = task.finishedAt ? Date.parse(task.finishedAt) : nowMs
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return '—'
  const sec = Math.floor((end - start) / 1000)
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return m > 0 ? `${m}m ${s}s` : `${s}s`
}

export function mergeEvents<T extends { seq: number }>(current: T[], incoming: T[]): T[] {
  const seen = new Set(current.map(item => item.seq))
  const next = [...current]
  for (const item of incoming) {
    if (seen.has(item.seq)) continue
    seen.add(item.seq)
    next.push(item)
  }
  return next.sort((a, b) => a.seq - b.seq)
}
