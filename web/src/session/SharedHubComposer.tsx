import React, { useEffect, useRef, useState } from 'react'
import { ACCESS_MODES, THREAD_SCENES, shortWorkDir, type HubScene, type InboxFile } from '../agentHub/agentHubCopy'

export type HubAccessMode = 'approval' | 'auto-edit' | 'full-access'

export function SharedHubComposer({
  value,
  onChange,
  onSubmit,
  onStop,
  live = false,
  detecting = false,
  placeholder,
  inputLabel,
  accessMode,
  onAccessMode,
  scene,
  onScene,
  showScene = false,
  showAccess = true,
  showPlus = true,
  workDir = '',
  exportDir = '',
  inboxFiles = [],
  onPickProject,
  onPickExport,
  onPickFiles,
  zh,
}: {
  value: string
  onChange: (next: string) => void
  onSubmit: () => void
  onStop?: () => void
  live?: boolean
  detecting?: boolean
  placeholder: string
  inputLabel?: string
  accessMode: HubAccessMode
  onAccessMode: (next: HubAccessMode) => void
  scene?: HubScene
  onScene?: (next: HubScene) => void
  showScene?: boolean
  showAccess?: boolean
  showPlus?: boolean
  workDir?: string
  exportDir?: string
  inboxFiles?: InboxFile[]
  onPickProject?: () => void
  onPickExport?: () => void
  onPickFiles?: () => void
  zh: boolean
}): React.JSX.Element {
  const [menu, setMenu] = useState(false)
  const [listening, setListening] = useState(false)
  const recognition = useRef<{ stop: () => void } | undefined>(undefined)
  useEffect(() => {
    const close = () => setMenu(false)
    window.addEventListener('click', close)
    return () => {
      window.removeEventListener('click', close)
      recognition.current?.stop()
    }
  }, [])
  const toggleVoice = () => {
    const Ctor = (window as unknown as { SpeechRecognition?: new () => SpeechRecognitionLike; webkitSpeechRecognition?: new () => SpeechRecognitionLike }).SpeechRecognition
      ?? (window as unknown as { webkitSpeechRecognition?: new () => SpeechRecognitionLike }).webkitSpeechRecognition
    if (!Ctor) return
    if (listening) {
      recognition.current?.stop()
      setListening(false)
      return
    }
    const rec = new Ctor()
    rec.lang = zh ? 'zh-CN' : 'en-US'
    rec.interimResults = false
    rec.onresult = event => {
      const text = Array.from(event.results).map(item => item[0]?.transcript ?? '').join('')
      if (text) onChange(value ? `${value} ${text}` : text)
    }
    rec.onend = () => setListening(false)
    recognition.current = rec
    rec.start()
    setListening(true)
  }
  return (
    <form
      className="launch-composer hub-composer"
      onSubmit={event => {
        event.preventDefault()
        if (live) onStop?.()
        else onSubmit()
      }}
    >
      {(workDir || exportDir || inboxFiles.length > 0) && (
        <p className="hub-composer-paths">
          {workDir ? <small>{zh ? '项目' : 'Project'} {shortWorkDir(workDir)}</small> : null}
          {exportDir ? <small>{zh ? '产物' : 'Export'} {shortWorkDir(exportDir)}</small> : null}
          {inboxFiles.length > 0 ? <small>{zh ? '附件' : 'Files'} {inboxFiles.map(item => item.name).join(zh ? '、' : ', ')}</small> : null}
        </p>
      )}
      <textarea
        value={value}
        onChange={event => onChange(event.target.value)}
        aria-label={inputLabel ?? (zh ? '任务说明' : 'Task prompt')}
        placeholder={placeholder}
        onKeyDown={event => {
          if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
            event.preventDefault()
            if (!live) onSubmit()
          }
        }}
      />
      <div className="composer-tools">
        {showPlus ? <div className="menu-anchor">
          <button
            type="button"
            className="composer-attach"
            aria-label={zh ? '添加上下文' : 'Add context'}
            onClick={event => {
              event.stopPropagation()
              setMenu(open => !open)
            }}
          >
            ＋
          </button>
          {menu && (
            <div className="launch-menu compact" role="menu" onClick={event => event.stopPropagation()}>
              <button type="button" onClick={() => { setMenu(false); onPickFiles?.() }}>
                <span>{zh ? '附件' : 'Files'}<small>{zh ? '加入参考文件' : 'Add reference files'}</small></span>
              </button>
              <button type="button" onClick={() => { setMenu(false); onPickProject?.() }}>
                <span>{zh ? '项目目录' : 'Project folder'}<small>{workDir ? shortWorkDir(workDir) : (zh ? '选择项目根' : 'Choose a root')}</small></span>
              </button>
              <button type="button" onClick={() => { setMenu(false); onPickExport?.() }}>
                <span>{zh ? '产物目录' : 'Export folder'}<small>{exportDir ? shortWorkDir(exportDir) : (zh ? '完成后复制到这里' : 'Copy results here')}</small></span>
              </button>
            </div>
          )}
        </div> : null}
        {showAccess ? (
          <>
            <label className="sr-only" htmlFor="hub-access">{zh ? '权限' : 'Access'}</label>
            <select
              id="hub-access"
              aria-label={zh ? '权限' : 'Access'}
              value={accessMode}
              onChange={event => onAccessMode(event.target.value as HubAccessMode)}
            >
              {ACCESS_MODES.map(item => (
                <option key={item.id} value={item.id}>{zh ? item.zh : item.en}</option>
              ))}
            </select>
          </>
        ) : null}
        {showScene && scene && onScene ? (
          <>
            <label className="sr-only" htmlFor="hub-scene">{zh ? '任务类型' : 'Task type'}</label>
            <select
              id="hub-scene"
              aria-label={zh ? '任务类型' : 'Task type'}
              value={scene}
              onChange={event => onScene(event.target.value as HubScene)}
            >
              {THREAD_SCENES.map(item => (
                <option key={item.id} value={item.id}>{zh ? item.zh : item.en}</option>
              ))}
            </select>
          </>
        ) : null}
        <div className="composer-primary-actions">
          <button
            type="button"
            aria-label={listening ? (zh ? '停止语音输入' : 'Stop voice input') : (zh ? '语音输入' : 'Voice input')}
            aria-pressed={listening}
            onClick={toggleVoice}
          >
            {listening ? '■' : '🎤'}
          </button>
          {live ? (
            <button type="button" className="composer-send" aria-label={zh ? '打断' : 'Interrupt'} onClick={onStop}>
              {zh ? '打断' : 'Stop'}
            </button>
          ) : (
            <button className="composer-send" aria-label={zh ? '发送' : 'Send'} disabled={detecting || !value.trim()}>
              ↑
            </button>
          )}
        </div>
      </div>
    </form>
  )
}

type SpeechRecognitionLike = {
  lang: string
  interimResults: boolean
  onresult: ((event: { results: ArrayLike<ArrayLike<{ transcript?: string }>> }) => void) | null
  onend: (() => void) | null
  start: () => void
  stop: () => void
}
