import React, { useState } from 'react'
import { useZh } from '../i18n/language'
import { Toggle } from './settingsControls'
import { saveOfficeMenu, useOfficeMenu, type OfficeMenuSettings } from './officeMenuSettings'
import './officeMenu.css'

const ITEMS: { key: keyof OfficeMenuSettings; zh: string; en: string; description: string; descriptionEn: string }[] = [
  { key: 'people', zh: '同事聊天', en: 'Colleague chat', description: '与同事和已启用的专家交流。', descriptionEn: 'Talk with colleagues and enabled experts.' },
  { key: 'mro', zh: '机务工作台', en: 'MRO workbench', description: '启用相关机务专家后，在办公菜单显示工作台。', descriptionEn: 'Show the workbench when its operations expert is enabled.' },
  { key: 'office', zh: '办公工作台', en: 'Office Studio', description: '制作文档、表格和演示文稿，查看版本与产物。', descriptionEn: 'Create documents, spreadsheets and presentations; review versions and outputs.' },
  { key: 'agentHub', zh: 'Agent 调度台', en: 'Agent Hub', description: '在左侧显示月汐 / 外接 Agent 切换，用本机 Cursor、Codex、Kimi 对话。产物在本页打开，不进入月汐对话列表。', descriptionEn: 'Show the Lunitide / Agents switch in the sidebar and talk with local Cursor, Codex, or Kimi. Artifacts open on this page and do not enter the Lunitide chat list.' },
  { key: 'meetings', zh: '会议记录', en: 'Meeting notes', description: '录制麦克风与电脑声音，整理会议纪要。', descriptionEn: 'Record microphone and system audio and organize meeting notes.' },
]

export function OfficeMenuPanel({ onSaved }: { onSaved?: () => void }): React.JSX.Element {
  const zh = useZh(), settings = useOfficeMenu()
  const [error, setError] = useState('')
  return <section className="setting-group office-menu-settings">
    <p className="setting-desc">{zh ? '选择办公菜单显示的功能。本机全局生效，切换模型和对话后仍保留。关闭只隐藏导航，已有任务、对话和文件都保留。自动化始终显示。' : 'Choose which features appear in Office. These preferences apply throughout this app and persist across chats and models. Hidden features retain their tasks, conversations and files. Automation stays visible.'}</p>
    {ITEMS.map(item => <Toggle key={item.key} label={zh ? item.zh : item.en} desc={zh ? item.description : item.descriptionEn} on={settings[item.key]} onChange={enabled => {
      try { saveOfficeMenu(item.key, enabled); setError(''); onSaved?.() }
      catch { setError(zh ? '设置未保存，请重试。' : 'Could not save this preference. Please retry.') }
    }} />)}
    {error && <p role="alert">{error}</p>}
  </section>
}
