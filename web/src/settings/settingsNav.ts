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
  | 'channels'
  | 'subagents'
  | 'collab'
  | 'diagnostics'
  | 'about'

export type SettingsNavItem = { id: SettingsCategory; icon: string; label: string; labelEn: string; keywords: string }

export const SETTINGS_CATEGORIES: SettingsNavItem[] = [
  { id: 'general', icon: '◌', label: '常规', labelEn: 'General', keywords: '启动 语言 时区 对话 Enter 标题 工作模式 完全访问 人设 说话风格 助手 客服 老师 NPC 结构化 表单 事件 startup language' },
  { id: 'appearance', icon: '◐', label: '外观', labelEn: 'Appearance', keywords: '主题 星光 月光 密度 动效 动画 theme' },
  { id: 'profile', icon: '☺', label: '个人资料', labelEn: 'Profile', keywords: '昵称 头像 状态 部门 职位 组织 局域网 发现 配对 密码 名片 nickname' },
  { id: 'providers', icon: '◈', label: '模型与供应商', labelEn: 'Models & providers', keywords: '模型 API Key 供应商 BYOK endpoint 视觉 生图 生视频 OCR LLM models providers' },
  { id: 'voice', icon: '◉', label: '语音与麦克风', labelEn: 'Voice & microphone', keywords: '月伴 TTS ASR 朗读 麦克风 全双工 音色 云端 本地 晓晓 sherpa GPT-SoVITS 克隆 人生 唤醒 纠错 VAD 先应一声 语音插话 voice' },
  { id: 'personal', icon: '✧', label: '个人智能', labelEn: 'Personal intelligence', keywords: '记忆 偏好 专家 画像 memory' },
  { id: 'security', icon: '⛨', label: '安全与治理', labelEn: 'Security', keywords: '命令白名单 编码 技能 MCP 权限 审批 hooks 全盘' },
  { id: 'browser', icon: '⬟', label: '浏览器', labelEn: 'Browser', keywords: 'Playwright Chrome 探测 snapshot 浏览' },
  { id: 'computer', icon: '⌖', label: '电脑控制', labelEn: 'Computer control', keywords: '本机 桌面 键鼠 截图 UAC' },
  { id: 'channels', icon: '✉', label: '消息通道', labelEn: 'Message channels', keywords: '飞书 企微 钉钉 微信 QQ webhook 机器人 发消息 im' },
  { id: 'subagents', icon: '⎇', label: '子智能体', labelEn: 'Subagents', keywords: '委派 spawn 并行' },
  { id: 'collab', icon: '⌘', label: '协作门禁', labelEn: 'Collaboration', keywords: '协作 门禁 审批' },
  { id: 'diagnostics', icon: '◉', label: '诊断与更新', labelEn: 'Diagnostics', keywords: '日志 更新 健康 诊断' },
  { id: 'about', icon: 'ⓘ', label: '关于', labelEn: 'About', keywords: '版本 关于 月汐 about version' },
]

export const SETTINGS_NAV_GROUPS: { label: string; labelEn: string; ids: SettingsCategory[] }[] = [
  { label: '界面', labelEn: 'Interface', ids: ['general', 'appearance', 'profile'] },
  { label: '智能', labelEn: 'Intelligence', ids: ['providers', 'voice', 'personal'] },
  { label: '能力', labelEn: 'Capabilities', ids: ['security', 'browser', 'computer', 'channels', 'subagents', 'collab'] },
  { label: '系统', labelEn: 'System', ids: ['diagnostics', 'about'] },
]

export function filterSettingsNav(query: string): SettingsNavItem[] {
  const q = query.trim().toLowerCase()
  if (!q) return SETTINGS_CATEGORIES
  return SETTINGS_CATEGORIES.filter(item => {
    const hay = `${item.label} ${item.labelEn} ${item.keywords} ${item.id}`.toLowerCase()
    return hay.includes(q)
  })
}
