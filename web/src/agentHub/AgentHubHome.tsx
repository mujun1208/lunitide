import React, { useEffect, useState } from 'react'
import { useZh } from '../i18n/language'
import { agentHubApi, type AgentHubName, type AgentHubStatus } from './agentHubApi'
import {
  ACCESS_MODES,
  PICK_PROJECT_DIR,
  SCENE_KEY,
  THREAD_SCENES,
  hubSceneToThreadScene,
  sceneBlurb,
  shortWorkDir,
  threadTitleFromPrompt,
  workDirKey,
  type HubScene,
} from './agentHubCopy'

function defaultHarness(scene: HubScene): AgentHubName {
  if (scene === 'write') return 'cursor'
  if (scene === 'fix') return 'codex'
  if (scene === 'ppt') return 'kimi'
  return 'cursor'
}

function userError(err: unknown, fallback: string): string {
  return err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : fallback
}

export function AgentHubHome({
  onOpened,
}: {
  onOpened?: (threadId: string) => void
}): React.JSX.Element {
  const zh = useZh()
  const [scene, setScene] = useState<HubScene | null>(null)
  const [agent, setAgent] = useState<AgentHubName>('cursor')
  const [workDir, setWorkDir] = useState('')
  const [prompt, setPrompt] = useState('')
  const [exportDir, setExportDir] = useState('')
  const [accessMode, setAccessMode] = useState<'approval' | 'auto-edit' | 'full-access'>('approval')
  const [error, setError] = useState('')
  const [agents, setAgents] = useState<AgentHubStatus[]>([])
  useEffect(() => {
    void agentHubApi.detect().then(got => setAgents(got.agents ?? [])).catch(() => undefined)
  }, [])
  const blurb = scene ? sceneBlurb(hubSceneToThreadScene(scene)) : ''
  const selectScene = (next: HubScene) => {
    setScene(next)
    setAgent(defaultHarness(next))
    setWorkDir(localStorage.getItem(workDirKey(next)) ?? '')
    setError('')
    localStorage.setItem(SCENE_KEY, next)
  }
  const pickFolder = async () => {
    if (!scene) return
    try {
      const got = await agentHubApi.pickDir()
      if (got.canceled || !got.path) return
      setWorkDir(got.path)
      localStorage.setItem(workDirKey(scene), got.path)
      setError('')
    } catch (err) {
      setError(userError(err, zh ? '没有选到工作目录。' : 'Could not choose a work folder.'))
    }
  }
  const pickExport = async () => {
    try {
      const got = await agentHubApi.pickDir()
      if (got.canceled || !got.path) return
      setExportDir(got.path)
      setError('')
    } catch (err) {
      setError(userError(err, zh ? '没有选到导出目录。' : 'Could not choose an export folder.'))
    }
  }
  const addInbox = async () => {
    if (!workDir) {
      setError(PICK_PROJECT_DIR)
      return
    }
    try {
      const got = await agentHubApi.inbox({ action: 'files', workDir })
      if (got.canceled) return
      setError('')
    } catch (err) {
      setError(userError(err, zh ? '没有加入参考文件。' : 'Could not add reference files.'))
    }
  }
  const submit = async () => {
    if (!scene) return
    if ((scene === 'write' || scene === 'fix') && !workDir) {
      setError(PICK_PROJECT_DIR)
      return
    }
    if (agents.length > 0) {
      const selected = agents.find(item => item.name === agent)
      if (!selected || selected.state !== 'available') {
        setError(zh ? '当前 Agent 不可用。' : 'This Agent is not available.')
        return
      }
    }
    setError('')
    try {
      const text = prompt.trim()
      const title = threadTitleFromPrompt(text)
      const created = await agentHubApi.threadCreate({
        harnessId: agent,
        scene: hubSceneToThreadScene(scene),
        workspaceRoot: workDir,
        ...(exportDir ? { exportDir } : {}),
        ...(title ? { title } : {}),
        accessMode,
      })
      if (text) {
        await agentHubApi.threadPrompt({ threadId: created.thread.threadId, text })
      }
      onOpened?.(created.thread.threadId)
    } catch (err) {
      setError(userError(err, zh ? '会话没有创建。' : 'The thread did not start.'))
    }
  }
  return (
    <section>
      <div className="agent-hub-scenes">
        {THREAD_SCENES.map(item => (
          <button
            key={item.id}
            type="button"
            className="agent-hub-scene"
            aria-pressed={scene === item.id}
            onClick={() => selectScene(item.id)}
          >
            {zh ? item.zh : item.en}
          </button>
        ))}
      </div>
      {blurb ? <p className="agent-hub-hint">{blurb}</p> : null}
      {scene && (
        <div className="agent-hub-pills">
          {agents.map(item => (
            <button
              key={item.name}
              type="button"
              className={`agent-hub-pill${item.state === 'available' ? '' : ' is-off'}`}
              aria-pressed={agent === item.name}
              disabled={item.state !== 'available'}
              onClick={() => { if (item.state === 'available') setAgent(item.name) }}
            >
              {item.name}
            </button>
          ))}
        </div>
      )}
      {scene && (() => {
        const selected = agents.find(item => item.name === agent)
        if (!selected?.hint) return null
        if (selected.state !== 'available') return <p className="agent-hub-hint">{selected.hint}</p>
        if (selected.protocol !== 'exec' && selected.interactive !== false) return null
        return <p className="agent-hub-hint">{selected.hint}</p>
      })()}
      <div className="agent-hub-console">
        <textarea
          value={prompt}
          onChange={event => setPrompt(event.target.value)}
          aria-label={zh ? '任务说明' : 'Task prompt'}
        />
        <div className="agent-hub-console-bar">
          <button type="button" className={`agent-hub-chip${workDir ? ' is-on' : ''}`} onClick={() => void pickFolder()}>
            {workDir ? shortWorkDir(workDir) : (zh ? '选择文件夹' : 'Choose folder')}
          </button>
          <button type="button" className={`agent-hub-chip${exportDir ? ' is-on' : ''}`} onClick={() => void pickExport()}>
            {exportDir ? shortWorkDir(exportDir) : (zh ? '导出目录' : 'Export folder')}
          </button>
          <button type="button" className="agent-hub-chip" onClick={() => void addInbox()}>
            {zh ? '添加文件' : 'Add files'}
          </button>
          {ACCESS_MODES.map(item => (
            <button
              key={item.id}
              type="button"
              className={`agent-hub-chip${accessMode === item.id ? ' is-on' : ''}`}
              aria-pressed={accessMode === item.id}
              onClick={() => setAccessMode(item.id)}
            >
              {zh ? item.zh : item.en}
            </button>
          ))}
          <button type="button" className="agent-hub-run" onClick={() => void submit()}>
            {zh ? '执行' : 'Run'}
          </button>
        </div>
      </div>
      {accessMode === 'auto-edit' ? (
        <p className="agent-hub-hint">{zh ? '自动会放过改文件权限，执行和联网仍要你点。业务选项永远要人点。' : 'Auto allows file edits. Shell and network still need your click. Business choices always wait for you.'}</p>
      ) : null}
      {accessMode === 'full-access' ? (
        <p className="agent-hub-hint">{zh ? '完全访问会自动放过该 CLI 的工具权限，并可能使用你本机已配的 MCP。业务选项仍要你点。' : 'Full access auto-allows this CLI tool permissions and may use MCP you already configured. Business choices still wait for you.'}</p>
      ) : null}
      {error && <p className="agent-hub-error" role="alert">{error}</p>}
    </section>
  )
}
