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
  const [accessOpen, setAccessOpen] = useState(false)
  const [sceneOpen, setSceneOpen] = useState(false)
  const [listening, setListening] = useState(false)
  const recognition = useRef<{ stop: () => void } | undefined>(undefined)
  useEffect(() => {
    const close = () => {
      setMenu(false)
      setAccessOpen(false)
      setSceneOpen(false)
    }
    window.addEventListener('click', close)
    return () => {
      window.removeEventListener('click', close)
      recognition.current?.stop()
    }
  }, [])
  const access = ACCESS_MODES.find(item => item.id === accessMode) ?? ACCESS_MODES[0]
  const sceneItem = THREAD_SCENES.find(item => item.id === scene) ?? THREAD_SCENES[THREAD_SCENES.length - 1]
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
          {workDir ? <span>{zh ? '项目' : 'Project'} {shortWorkDir(workDir)}</span> : null}
          {exportDir ? <span>{zh ? '产物' : 'Export'} {shortWorkDir(exportDir)}</span> : null}
          {inboxFiles.length > 0 ? <span>{zh ? '附件' : 'Files'} {inboxFiles.map(item => item.name).join(zh ? '、' : ', ')}</span> : null}
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
        <div className="composer-toolbar-start">
        {showPlus ? <div className="menu-anchor">
          <button
            type="button"
            className="composer-attach"
            aria-label={zh ? '添加上下文' : 'Add context'}
            onClick={event => {
              event.stopPropagation()
              setMenu(open => !open)
              setAccessOpen(false)
              setSceneOpen(false)
            }}
          >
            ＋
          </button>
          {menu && (
            <div className="launch-menu compact" role="menu" onClick={event => event.stopPropagation()}>
              <button type="button" onClick={() => { setMenu(false); onPickFiles?.() }}>
                <span>{zh ? '上传附件' : 'Upload files'}<small>{zh ? '加入参考文件' : 'Add reference files'}</small></span>
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
          <div className="menu-anchor hub-access-anchor">
            <button
              type="button"
              className="hub-access-chip"
              aria-label={zh ? '权限' : 'Access'}
              aria-haspopup="menu"
              aria-expanded={accessOpen}
              onClick={event => {
                event.stopPropagation()
                setAccessOpen(open => !open)
                setMenu(false)
                setSceneOpen(false)
              }}
            >
              {zh ? access.zh : access.en}
              <span aria-hidden="true">▾</span>
            </button>
            {accessOpen ? (
              <div className="launch-menu compact hub-access-menu" role="menu" onClick={event => event.stopPropagation()}>
                {ACCESS_MODES.map(item => (
                  <button
                    key={item.id}
                    type="button"
                    role="menuitemradio"
                    aria-checked={item.id === accessMode}
                    className={item.id === accessMode ? 'is-active' : undefined}
                    onClick={() => {
                      onAccessMode(item.id)
                      setAccessOpen(false)
                    }}
                  >
                    <span>
                      {zh ? item.zh : item.en}
                      <small>{zh ? item.zhDesc : item.enDesc}</small>
                    </span>
                    {item.id === accessMode ? <em aria-hidden="true">✓</em> : null}
                  </button>
                ))}
              </div>
            ) : null}
          </div>
        ) : null}
        {showScene && scene && onScene ? (
          <div className="menu-anchor hub-access-anchor">
            <button
              type="button"
              className="hub-access-chip"
              aria-label={zh ? '任务类型' : 'Task type'}
              aria-haspopup="menu"
              aria-expanded={sceneOpen}
              onClick={event => {
                event.stopPropagation()
                setSceneOpen(open => !open)
                setMenu(false)
                setAccessOpen(false)
              }}
            >
              {zh ? sceneItem.zh : sceneItem.en}
              <span aria-hidden="true">▾</span>
            </button>
            {sceneOpen ? (
              <div className="launch-menu compact hub-access-menu" role="menu" onClick={event => event.stopPropagation()}>
                {THREAD_SCENES.map(item => (
                  <button
                    key={item.id}
                    type="button"
                    role="menuitemradio"
                    aria-checked={item.id === scene}
                    className={item.id === scene ? 'is-active' : undefined}
                    onClick={() => {
                      onScene(item.id)
                      setSceneOpen(false)
                    }}
                  >
                    <span>{zh ? item.zh : item.en}</span>
                    {item.id === scene ? <em aria-hidden="true">✓</em> : null}
                  </button>
                ))}
              </div>
            ) : null}
          </div>
        ) : null}
        </div>
        <div className="composer-primary-actions composer-act">
          <button
            type="button"
            className={`composer-act-btn${listening ? ' is-on' : ''}`}
            aria-label={listening ? (zh ? '停止语音输入' : 'Stop voice input') : (zh ? '语音输入' : 'Voice input')}
            aria-pressed={listening}
            onClick={toggleVoice}
          >
            <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="9" y="3.5" width="6" height="10.5" rx="3" /><path d="M6.5 11.5a5.5 5.5 0 0 0 11 0" /><path d="M12 17v3M9.5 20.5h5" /></svg>
          </button>
          {live ? (
            <button type="button" className="composer-act-btn is-primary" aria-label={zh ? '打断' : 'Interrupt'} onClick={onStop}>
              <svg viewBox="0 0 24 24" aria-hidden="true"><rect x="7.4" y="7.4" width="9.2" height="9.2" rx="1.6" fill="currentColor" stroke="none" /></svg>
            </button>
          ) : (
            <button className="composer-act-btn is-primary" aria-label={zh ? '发送' : 'Send'} disabled={detecting || !value.trim()}>
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 17.5V6.5" /><path d="M7.5 11 12 6.5 16.5 11" /></svg>
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
