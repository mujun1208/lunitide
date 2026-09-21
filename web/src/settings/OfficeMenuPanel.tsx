import React, { useState } from 'react'
import { useZh } from '../i18n/language'
import { Toggle } from './settingsControls'
import { saveOfficeMenu, useOfficeMenu, type OfficeMenuSettings } from './officeMenuSettings'
import './officeMenu.css'

const ITEMS: { key: keyof OfficeMenuSettings; zh: string; en: string; description: string; descriptionEn: string }[] = [
  { key: 'office', zh: '办公工作台', en: 'Office Studio', description: '制作文档、表格和演示文稿，查看版本与产物。', descriptionEn: 'Create documents, spreadsheets and presentations; review versions and outputs.' },
  { key: 'automation', zh: '自动化', en: 'Automation', description: '定时任务与执行历史。关闭只隐藏导航。', descriptionEn: 'Scheduled jobs and execution history. Hiding this only removes the navigation item.' },
  { key: 'media', zh: '媒体中心', en: 'Media Center', description: '播放本机已授权的音频和视频。关闭只隐藏导航。', descriptionEn: 'Play local audio and video you authorize. Hiding this only removes the navigation item.' },
  { key: 'people', zh: '同事聊天', en: 'Colleague chat', description: '与同事和已启用的专家交流。', descriptionEn: 'Talk with colleagues and enabled experts.' },
  { key: 'mro', zh: '机务工作台', en: 'MRO workbench', description: '启用相关机务专家后，在办公菜单显示工作台。', descriptionEn: 'Show the workbench when its operations expert is enabled.' },
  { key: 'meetings', zh: '会议记录', en: 'Meeting notes', description: '录制麦克风与电脑声音，整理会议纪要。', descriptionEn: 'Record microphone and system audio and organize meeting notes.' },
  { key: 'productHub', zh: '产品总览', en: 'Product Hub', description: '产品知识中枢。关闭只隐藏导航；打开后先输入管理员用户名和密码。', descriptionEn: 'Product knowledge hub. Hiding this only removes the navigation item. Opening it asks for the admin username and password.' },
]

export function OfficeMenuPanel({ onSaved }: { onSaved?: () => void }): React.JSX.Element {
  const zh = useZh(), settings = useOfficeMenu()
  const [error, setError] = useState('')
  return <section className="setting-group office-menu-settings">
    <p className="setting-desc">{zh ? '选择办公菜单显示的功能。本机全局生效，切换模型和对话后仍保留。关闭只隐藏导航，已有任务、对话和文件都保留。' : 'Choose which features appear in Office. These preferences apply throughout this app and persist across chats and models. Hidden features retain their tasks, conversations and files.'}</p>
    {ITEMS.map(item => <Toggle key={item.key} label={zh ? item.zh : item.en} desc={zh ? item.description : item.descriptionEn} on={settings[item.key]} onChange={enabled => {
      try { saveOfficeMenu(item.key, enabled); setError(''); onSaved?.() }
      catch { setError(zh ? '设置未保存，请重试。' : 'Could not save this preference. Please retry.') }
    }} />)}
    {error && <p role="alert">{error}</p>}
  </section>
}
