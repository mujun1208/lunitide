export type SettingsCategory =
  | 'general'
  | 'appearance'
  | 'profile'
  | 'providers'
  | 'voice'
  | 'personal'
  | 'security'
  | 'browser'
  | 'computer'
  | 'subagents'
  | 'collab'
  | 'diagnostics'
  | 'about'

export type SettingsNavItem = { id: SettingsCategory; icon: string; label: string; keywords: string }

export const SETTINGS_CATEGORIES: SettingsNavItem[] = [
  { id: 'general', icon: '◌', label: '常规', keywords: '启动 语言 时区 对话 Enter 标题 工作模式 完全访问 人设 说话风格 助手 客服 老师 NPC 结构化 表单 事件' },
  { id: 'appearance', icon: '◐', label: '外观', keywords: '主题 星光 月光 密度 动效 动画' },
  { id: 'profile', icon: '☺', label: '个人资料', keywords: '昵称 头像 状态 部门 职位 组织 局域网 发现 配对 密码 名片' },
  { id: 'providers', icon: '◈', label: '模型与供应商', keywords: '模型 API Key 供应商 BYOK endpoint 视觉 生图 生视频 OCR LLM' },
  { id: 'voice', icon: '◉', label: '语音与麦克风', keywords: '月伴 TTS ASR 朗读 麦克风 MiniCPM 全双工 音色 云端 本地模型 人生' },
  { id: 'personal', icon: '✧', label: '个人智能', keywords: '记忆 偏好 专家 画像' },
  { id: 'security', icon: '⛨', label: '安全与治理', keywords: '命令白名单 编码 技能 MCP 权限 审批 hooks 全盘' },
  { id: 'browser', icon: '⬟', label: '浏览器', keywords: 'Playwright Chrome 探测 snapshot 浏览' },
  { id: 'computer', icon: '⌖', label: '电脑控制', keywords: '本机 桌面 键鼠 截图 UAC' },
  { id: 'subagents', icon: '⎇', label: '子智能体', keywords: '委派 spawn 并行' },
  { id: 'collab', icon: '⌘', label: '协作门禁', keywords: '协作 门禁 审批' },
  { id: 'diagnostics', icon: '◉', label: '诊断与更新', keywords: '日志 更新 健康 诊断' },
  { id: 'about', icon: 'ⓘ', label: '关于', keywords: '版本 关于 月汐' },
]

export const SETTINGS_NAV_GROUPS: { label: string; ids: SettingsCategory[] }[] = [
  { label: '界面', ids: ['general', 'appearance', 'profile'] },
  { label: '智能', ids: ['providers', 'voice', 'personal'] },
  { label: '能力', ids: ['security', 'browser', 'computer', 'subagents', 'collab'] },
  { label: '系统', ids: ['diagnostics', 'about'] },
]

export function filterSettingsNav(query: string): SettingsNavItem[] {
  const q = query.trim().toLowerCase()
  if (!q) return SETTINGS_CATEGORIES
  return SETTINGS_CATEGORIES.filter(item => {
    const hay = `${item.label} ${item.keywords} ${item.id}`.toLowerCase()
    return hay.includes(q)
  })
}
