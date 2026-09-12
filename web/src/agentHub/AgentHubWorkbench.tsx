import React, { useEffect, useState } from 'react'
import { useZh } from '../i18n/language'
import { agentHubApi, type AgentHubName, type AgentHubStatus, type AgentHubTask } from './agentHubApi'
import {
  agentMark,
  composeHubPrompt,
  FREE_TEMPLATES,
  HUB_SCENES,
  newIdempotencyKey,
  SCENE_KEY,
  shortWorkDir,
  stateLabel,
  workDirKey,
  type HubScene,
  type InboxFile,
} from './agentHubCopy'

const SANDBOXES = [
  { id: 'read-only', zh: '只读', en: 'Read-only' },
  { id: 'workspace-write', zh: '工作区可写', en: 'Workspace write' },
  { id: 'full-access', zh: '完全访问', en: 'Full access' },
] as const

const PLACEHOLDERS: Record<HubScene, { zh: string; en: string }> = {
  ppt: { zh: '根据本目录大纲做 12 页介绍 PPT，输出 pptx', en: 'Make a 12-page intro deck as pptx from this folder' },
  write: { zh: '在本目录按规则建文件夹并写最小可运行代码', en: 'Create folders and a minimal runnable project here' },
  fix: { zh: '说明缺陷，只改必要文件', en: 'Describe the defect; change only what is needed' },
  free: { zh: '写下要做的事。材料用上面的「添加文件」', en: 'Describe the task. Use Add files above for materials.' },
}

export function AgentHubWorkbench({
  agents,
  live,
  onOpened,
}: {
  agents: AgentHubStatus[]
  live: AgentHubTask[]
  onOpened: (taskId: string) => void
}): React.JSX.Element {
  const zh = useZh()
  const [scene, setScene] = useState<HubScene | null>(null)
  const [agent, setAgent] = useState<AgentHubName>('codex')
  const [prompt, setPrompt] = useState('')
  const [sandbox, setSandbox] = useState<(typeof SANDBOXES)[number]['id']>('workspace-write')
  const [workDir, setWorkDir] = useState('')
  const [files, setFiles] = useState<InboxFile[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const lockedAgent = scene && scene !== 'free' ? sceneAgent(scene) : agent
  const selected = agents.find(item => item.name === lockedAgent)
  const available = selected?.state === 'available' && selected.nonInteractive
  const anyAvailable = agents.some(item => item.state === 'available' && item.nonInteractive)
  const sameAgentRunning = live.some(item => item.agent === lockedAgent && item.status === 'running')
  const canRun = !!scene && !!prompt.trim() && !busy && available && (scene === 'free' || !!workDir)
  useEffect(() => {
    if (scene !== 'free') return
    const current = agents.find(item => item.name === agent)
    if (current?.state === 'available' && current.nonInteractive) return
    const first = agents.find(item => item.state === 'available' && item.nonInteractive)
    if (first) setAgent(first.name)
  }, [agents, scene, agent])
  useEffect(() => {
    if (!workDir) {
      setFiles([])
      return
    }
    let cancelled = false
    void agentHubApi.inbox({ action: 'list', workDir }).then(got => {
      if (cancelled || got.canceled) return
      setFiles(got.files ?? [])
    }).catch(err => {
      if (!cancelled) setError(userError(err, zh ? '没有读到已传入的文件。' : 'Could not list inbox files.'))
    })
    return () => { cancelled = true }
  }, [workDir, zh])
  const selectScene = (next: HubScene) => {
    if (next !== 'free') {
      const locked = agents.find(item => item.name === sceneAgent(next))
      if (agents.length > 0 && (locked?.state !== 'available' || !locked.nonInteractive)) {
        window.alert(locked?.hint ?? '')
        return
      }
      setAgent(sceneAgent(next))
    } else {
      const first = agents.find(item => item.state === 'available' && item.nonInteractive)
      if (first) setAgent(first.name)
    }
    setScene(next)
    localStorage.setItem(SCENE_KEY, next)
    setWorkDir(localStorage.getItem(workDirKey(next)) ?? '')
  }
  const ingest = async (action: 'files' | 'folder') => {
    if (!scene) return
    let dir = workDir
    if (scene !== 'free' && !dir) {
      const picked = await agentHubApi.pickDir()
      if (picked.canceled || !picked.path) return
      if (!window.confirm(zh ? '将在此目录执行 CLI，可能改文件。确定继续？' : 'The CLI may edit files in this folder. Continue?')) return
      dir = picked.path
      setWorkDir(dir)
      localStorage.setItem(workDirKey(scene), dir)
    }
    try {
      const got = await agentHubApi.inbox({ action, workDir: dir || undefined })
      if (got.canceled) return
      if (got.workDir) {
        setWorkDir(got.workDir)
        localStorage.setItem(workDirKey(scene), got.workDir)
      }
      setFiles(got.files ?? [])
      setError((got.skipped ?? []).join('；'))
    } catch (err) {
      setError(userError(err, zh ? '没有添加文件。' : 'Could not add files.'))
    }
  }
  const dropFile = async (name: string) => {
    if (!scene || !workDir) return
    try {
      const got = await agentHubApi.inbox({ action: 'drop', workDir, name })
      if (got.canceled) return
      setFiles(got.files ?? [])
      setError((got.skipped ?? []).join('；'))
    } catch (err) {
      setError(userError(err, zh ? '没有移除该文件。' : 'Could not remove the file.'))
    }
  }
  const submit = async () => {
    const text = prompt.trim()
    if (!scene || !canRun || !text) return
    if (scene === 'free' && sandbox === 'full-access' && !window.confirm(zh ? '完全访问会尽量少限制该 CLI。确定继续？' : 'Full access relaxes CLI limits. Continue?')) return
    setBusy(true)
    setError('')
    try {
      const detail = await agentHubApi.start({
        agent: lockedAgent,
        prompt: composeHubPrompt(scene, workDir, text, files),
        workDir: workDir || undefined,
        sandbox: lockedAgent === 'codex' ? (scene === 'fix' ? 'workspace-write' : sandbox) : undefined,
        timeoutMin: 60,
        idempotencyKey: newIdempotencyKey(),
      })
      onOpened(detail.task.taskId)
    } catch (err) {
      setError(userError(err, zh ? '任务没有发出，请重试。' : 'The task did not start. Please retry.'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <section>
      <div className="agent-hub-hero">
        <div className="eyebrow">Agent Hub</div>
        <h2>{zh ? <>一个入口<br />调度本机已安装的 <em>CLI</em></> : <>One place to dispatch locally installed <em>CLIs</em></>}</h2>
        <p>{zh ? '调用本机已安装的 Codex / Cursor / Kimi。不能跑的适配器保持灰色，不假装三家齐。' : 'Call Codex, Cursor or Kimi already installed on this machine. Unavailable adapters stay grey.'}</p>
      </div>
      <div className="agent-hub-scenes">
        {HUB_SCENES.map(item => {
          const locked = item.id === 'free' ? undefined : agents.find(agentItem => agentItem.name === item.agent)
          const off = item.id !== 'free' && agents.length > 0 && (locked?.state !== 'available' || !locked.nonInteractive)
          return (
            <button
              key={item.id}
              type="button"
              className={`agent-hub-scene${off ? ' is-off' : ''}`}
              aria-pressed={scene === item.id}
              onClick={() => selectScene(item.id)}
            >
              <b>{zh ? item.zh : item.en}</b>
              <small>{zh ? item.subZh : item.subEn}</small>
            </button>
          )
        })}
      </div>
      {scene === 'free' && (
        <div className="agent-hub-pills">
          {agents.map(item => (
            <button
              key={item.name}
              type="button"
              className={`agent-hub-pill${item.state === 'available' ? '' : ' is-off'}`}
              aria-pressed={agent === item.name}
              onClick={() => {
                if (item.state !== 'available' || !item.nonInteractive) {
                  window.alert(item.hint)
                  return
                }
                setAgent(item.name)
              }}
            >
              <span className="agent-hub-logo">{agentMark(item.name)}</span>
              <span>
                <b>{item.name === 'codex' ? 'Codex' : item.name === 'cursor' ? 'Cursor' : 'Kimi'}</b>
                <small><i className={`agent-hub-lamp ${item.state}`} />{stateLabel(item.state, zh)}{item.version ? ` · ${item.version}` : ' · —'}</small>
              </span>
            </button>
          ))}
        </div>
      )}
      <div className="agent-hub-console">
        <textarea
          value={prompt}
          onChange={event => setPrompt(event.target.value)}
          placeholder={scene ? (zh ? PLACEHOLDERS[scene].zh : PLACEHOLDERS[scene].en) : (zh ? '先选一张卡' : 'Pick a card first')}
          aria-label={zh ? '任务说明' : 'Task prompt'}
        />
        <div className="agent-hub-console-bar">
          {scene === 'free' && <span className="agent-hub-chip is-on">⬡ {agent === 'codex' ? 'Codex' : agent === 'cursor' ? 'Cursor' : 'Kimi'}</span>}
          <button type="button" className={`agent-hub-chip${workDir ? ' is-on' : ''}`} aria-label={zh ? '工作目录' : 'Work folder'} onClick={() => void pickWorkDir(zh, scene, setWorkDir, setError)}>
            {workDir ? shortWorkDir(workDir) : scene && scene !== 'free' ? (zh ? '选择文件夹' : 'Choose folder') : (zh ? '默认目录 agent-hub' : 'Default agent-hub folder')}
          </button>
          {scene === 'free' && workDir && (
            <button type="button" className="agent-hub-chip" onClick={() => { setWorkDir(''); localStorage.removeItem(workDirKey('free')) }}>
              {zh ? '恢复默认' : 'Use default'}
            </button>
          )}
          <button type="button" className="agent-hub-chip" disabled={!scene} onClick={() => void ingest('files')}>{zh ? '添加文件' : 'Add files'}</button>
          <button type="button" className="agent-hub-chip" disabled={!scene} onClick={() => void ingest('folder')}>{zh ? '添加资料夹' : 'Add folder'}</button>
          {files.map(item => (
            <span key={item.path} className="agent-hub-file is-on">
              {item.name}
              <small>{item.size}B</small>
              <button type="button" aria-label={zh ? `移除 ${item.name}` : `Remove ${item.name}`} onClick={() => void dropFile(item.path)}>×</button>
            </span>
          ))}
          {scene === 'free' && agent === 'codex' && SANDBOXES.map(item => (
            <button key={item.id} type="button" className={`agent-hub-chip${sandbox === item.id ? ' is-on' : ''}`} onClick={() => setSandbox(item.id)}>
              {zh ? item.zh : item.en}
            </button>
          ))}
          <button type="button" className="agent-hub-run" disabled={!canRun} onClick={() => void submit()}>
            {busy ? (zh ? '提交中…' : 'Starting…') : sameAgentRunning ? (zh ? '加入排队' : 'Queue') : (zh ? '执行' : 'Run')}
          </button>
        </div>
      </div>
      {scene === 'free' && (
        <div className="agent-hub-templates">
          {FREE_TEMPLATES.map(item => (
            <button key={item.zh} type="button" onClick={() => setPrompt(item.prompt)}>{zh ? item.zh : item.en}</button>
          ))}
        </div>
      )}
      {scene && <p className="agent-hub-hint">{zh ? '添加文件后，Agent 会在本目录的 .agenthub-inbox 里读副本。原文件不会被改。' : 'Added files are copied into .agenthub-inbox. The originals are not changed.'}</p>}
      {(scene === 'write' || scene === 'fix') && <p className="agent-hub-hint">{zh ? '项目根用「选择文件夹」。规则放在根目录（AGENTS.md、.cursor/rules、README）或写在下面。' : 'Use Choose folder for the project root. Put rules in AGENTS.md, .cursor/rules, README, or in the prompt.'}</p>}
      {scene === 'free' && !workDir && files.length === 0 && <p className="agent-hub-hint">{zh ? '不选目录就执行时，会用默认 agent-hub 目录。要用已有项目请先选文件夹。' : 'Running without a folder uses the default agent-hub directory. Pick a folder for an existing project.'}</p>}
      {scene === 'free' && agent === 'codex' && <p className="agent-hub-hint">{zh ? '本机沙箱不可用，仅约束默认目录。完全访问会尽量少限制该 CLI。' : 'Local sandbox is not a security boundary. It only steers the default folder.'}</p>}
      {scene === 'free' && agent === 'cursor' && <p className="agent-hub-hint">{zh ? 'Cursor 将自动改工作目录内文件。' : 'Cursor will edit files inside the work folder automatically.'}</p>}
      {scene === 'free' && agent === 'kimi' && <p className="agent-hub-hint">{zh ? 'Kimi 将自动改工作目录内文件。未登录时请先在该 CLI 自己的终端完成登录。' : 'Kimi will edit files inside the work folder automatically. Sign in from that CLI first if it is not logged in.'}</p>}
      {agents.length > 0 && !anyAvailable && <p className="agent-hub-hint">{zh ? '当前没有可执行的 CLI。点灰色胶囊看安装或登录说明。' : 'No runnable CLI yet. Click a grey card for install or sign-in help.'}</p>}
      {error && <p className="agent-hub-error" role="alert">{error}</p>}
      {live.map(item => (
        <button key={item.taskId} type="button" className="agent-hub-live" onClick={() => onOpened(item.taskId)}>
          <span className="agent-hub-ring" aria-hidden="true" />
          <span>
            <b>{item.status === 'queued' ? (zh ? '排队中' : 'Queued') : (zh ? '进行中' : 'Running')}</b>
            <small>{item.prompt}</small>
          </span>
        </button>
      ))}
    </section>
  )
}

function sceneAgent(scene: Exclude<HubScene, 'free'>): AgentHubName {
  return scene === 'ppt' ? 'kimi' : scene === 'write' ? 'cursor' : 'codex'
}

function userError(err: unknown, fallback: string): string {
  return err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : fallback
}

async function pickWorkDir(
  zh: boolean,
  scene: HubScene | null,
  setWorkDir: (value: string) => void,
  setError: (value: string) => void,
): Promise<void> {
  if (!scene) return
  try {
    const got = await agentHubApi.pickDir()
    if (got.canceled || !got.path) return
    if (!window.confirm(zh ? '将在此目录执行 CLI，可能改文件。确定继续？' : 'The CLI may edit files in this folder. Continue?')) return
    localStorage.setItem(workDirKey(scene), got.path)
    setWorkDir(got.path)
    setError('')
  } catch (err) {
    setError(userError(err, zh ? '没有选到工作目录。' : 'Could not choose a work folder.'))
  }
}
