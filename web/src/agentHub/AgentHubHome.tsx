import React, { useEffect, useState } from 'react'
import { SharedHubComposer, type HubAccessMode } from '../session/SharedHubComposer'
import { useZh } from '../i18n/language'
import { AgentHubInstallActions } from './AgentHubInstallActions'
import { agentHubApi, type AgentHubName, type AgentHubStatus } from './agentHubApi'
import {
  agentDisplayName,
  agentInstall,
  composeHubPrompt,
  hubReadyState,
  hubSceneToThreadScene,
  PICK_PROJECT_DIR,
  SCENE_KEY,
  sceneBlurb,
  threadTitleFromPrompt,
  workDirKey,
  type HubScene,
  type InboxFile,
} from './agentHubCopy'

function readScene(): HubScene {
  const saved = localStorage.getItem(SCENE_KEY)
  if (saved === 'write' || saved === 'fix' || saved === 'ppt' || saved === 'docs' || saved === 'free') return saved
  return 'free'
}

function defaultHarness(scene: HubScene): AgentHubName {
  if (scene === 'write' || scene === 'docs') return 'cursor'
  if (scene === 'fix') return 'codex'
  if (scene === 'ppt') return 'kimi'
  return 'cursor'
}

function userError(err: unknown, fallback: string): string {
  return err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : fallback
}

export function AgentHubHome({
  onOpened,
  selectedAgent,
  onSelectAgent,
}: {
  onOpened?: (threadId: string) => void
  selectedAgent?: AgentHubName
  onSelectAgent?: (name: AgentHubName) => void
}): React.JSX.Element {
  const zh = useZh()
  const [scene, setScene] = useState<HubScene>(readScene)
  const [agent, setAgent] = useState<AgentHubName>(selectedAgent ?? 'cursor')
  const [workDir, setWorkDir] = useState(() => localStorage.getItem(workDirKey(readScene())) ?? '')
  const [prompt, setPrompt] = useState('')
  const [exportDir, setExportDir] = useState('')
  const [accessMode, setAccessMode] = useState<HubAccessMode>('approval')
  const [inboxFiles, setInboxFiles] = useState<InboxFile[]>([])
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [agents, setAgents] = useState<AgentHubStatus[] | null>(null)
  useEffect(() => {
    void agentHubApi.detect().then(got => setAgents(got.agents ?? [])).catch(() => setAgents(current => current ?? []))
  }, [])
  useEffect(() => {
    if (selectedAgent) {
      setAgent(selectedAgent)
      return
    }
    onSelectAgent?.(agent)
  }, [selectedAgent, onSelectAgent, agent])
  const detecting = agents === null
  const selected = agents?.find(item => item.name === agent)
  const mode = hubReadyState(selected?.state)
  const ready = mode === 'ready'
  const install = agentInstall(agent)
  const refreshDetect = async () => {
    try {
      setAgents((await agentHubApi.detect()).agents ?? [])
    } catch {
      setAgents(current => current ?? [])
    }
  }
  const blurb = scene === 'docs'
    ? (zh ? '根据本目录已有材料写文档，只在本目录保存。' : 'Write documents from this folder.')
    : sceneBlurb(hubSceneToThreadScene(scene))
  const selectScene = (next: HubScene) => {
    setScene(next)
    const nextAgent = selectedAgent ?? defaultHarness(next)
    setAgent(nextAgent)
    onSelectAgent?.(nextAgent)
    setWorkDir(localStorage.getItem(workDirKey(next)) ?? '')
    setError('')
    localStorage.setItem(SCENE_KEY, next)
  }
  const pickFolder = async () => {
    try {
      const got = await agentHubApi.pickDir()
      if (got.canceled || !got.path) return
      setWorkDir(got.path)
      setInboxFiles([])
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
      setInboxFiles(got.files ?? [])
      setError('')
    } catch (err) {
      setError(userError(err, zh ? '没有加入参考文件。' : 'Could not add reference files.'))
    }
  }
  const submit = async () => {
    if ((scene === 'write' || scene === 'fix' || scene === 'docs') && !workDir) {
      setError(PICK_PROJECT_DIR)
      return
    }
    if (agents === null) {
      setError(zh ? '正在检测本机 Agent…' : 'Still looking for local Agents.')
      return
    }
    if (!selected || selected.state !== 'available') {
      setError(zh ? '当前 Agent 不可用。' : 'This Agent is not available.')
      return
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
        await agentHubApi.threadPrompt({
          threadId: created.thread.threadId,
          text: composeHubPrompt(scene, workDir, text, inboxFiles),
        })
      }
      onOpened?.(created.thread.threadId)
    } catch (err) {
      setError(userError(err, zh ? '会话没有创建。' : 'The thread did not start.'))
    }
  }
  return (
    <section className="hub-home">
      {agents && mode === 'missing' ? (
        <article className="agent-hub-install">
          <h2>{zh ? `${agentDisplayName(agent)} 还不能对话` : `${agentDisplayName(agent)} is not ready`}</h2>
          <p>{selected?.hint || (zh ? '先检查本机 CLI，确认后再自动安装并连接。' : 'Check the local CLI, then install and connect here.')}</p>
          <div className="agent-hub-install-row">
            <AgentHubInstallActions name={agent} zh={zh} onDone={next => { setAgents(next); setNotice('') }} />
          </div>
          <p className="usage">{zh ? `本机安装：${install.command}` : `Local install: ${install.command}`}</p>
        </article>
      ) : agents && mode === 'unsigned' ? (
        <article className="agent-hub-install">
          <h2>{zh ? `${agentDisplayName(agent)} 已安装，还没连上` : `${agentDisplayName(agent)} is installed, but not connected`}</h2>
          <p>{selected?.hint || (zh ? '会自动登录该 CLI，再连上。不会打开网页。' : 'It will sign in to that CLI, then connect. No webpage will open.')}</p>
          <div className="agent-hub-install-row">
            <AgentHubInstallActions name={agent} zh={zh} onDone={next => { setAgents(next); setNotice('') }} />
          </div>
        </article>
      ) : agents && mode === 'unknown' ? (
        <article className="agent-hub-install">
          <h2>{zh ? `还没确认 ${agentDisplayName(agent)} 的状态` : `Still checking ${agentDisplayName(agent)}`}</h2>
          <p>{zh ? '重新探测本机 CLI，或直接安装并连接。' : 'Probe the local CLI again, or install and connect.'}</p>
          <div className="agent-hub-install-row">
            <button type="button" className="agent-hub-install-ghost" onClick={() => void refreshDetect()}>
              {zh ? '重新检测' : 'Retry'}
            </button>
            <AgentHubInstallActions name={agent} zh={zh} onDone={next => { setAgents(next); setNotice('') }} />
          </div>
        </article>
      ) : (
        <p className="agent-hub-hint">{zh ? `这是 ${agentDisplayName(agent)} 自己的对话窗。换到别的 Agent 不会带走这里的记忆。` : `This is ${agentDisplayName(agent)}'s own thread. Other Agents do not share this memory.`}</p>
      )}
      {blurb ? <p className="agent-hub-hint">{blurb}</p> : null}
      {selected?.hint && ready && selected.protocol === 'exec' && selected.interactive === false ? (
        <p className="agent-hub-hint">{selected.hint}</p>
      ) : null}
      {notice ? <p className="agent-hub-hint" role="status">{notice}</p> : null}
      <SharedHubComposer
        value={prompt}
        onChange={setPrompt}
        onSubmit={() => void submit()}
        detecting={detecting}
        placeholder={zh ? '向 Agent 描述任务…' : 'Describe the task for this Agent…'}
        accessMode={accessMode}
        onAccessMode={setAccessMode}
        scene={scene}
        onScene={selectScene}
        showScene
        showAccess
        showPlus
        workDir={workDir}
        exportDir={exportDir}
        onPickProject={() => void pickFolder()}
        onPickExport={() => void pickExport()}
        onPickFiles={() => void addInbox()}
        inboxFiles={inboxFiles}
        zh={zh}
      />
      {ready || detecting ? (
        <>
          {accessMode === 'auto-edit' ? (
            <p className="agent-hub-hint">{zh ? '自动会放过改文件权限，执行和联网仍要你点。业务选项永远要人点。' : 'Auto allows file edits. Shell and network still need your click. Business choices always wait for you.'}</p>
          ) : null}
          {accessMode === 'full-access' ? (
            <p className="agent-hub-hint">{zh ? '完全访问会自动放过该 CLI 的工具权限，并可能使用你本机已配的 MCP。业务选项仍要你点。' : 'Full access auto-allows this CLI tool permissions and may use MCP you already configured. Business choices still wait for you.'}</p>
          ) : null}
        </>
      ) : null}
      {error && <p className="agent-hub-error" role="alert">{error}</p>}
    </section>
  )
}
