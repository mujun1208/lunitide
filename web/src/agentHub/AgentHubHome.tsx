import React, { useEffect, useState } from 'react'
import { SharedHubComposer, type HubAccessMode } from '../session/SharedHubComposer'
import { useZh } from '../i18n/language'
import { agentHubApi, type AgentHubName, type AgentHubStatus } from './agentHubApi'
import {
  FREE_TEMPLATES,
  PICK_PROJECT_DIR,
  SCENE_KEY,
  composeHubPrompt,
  hubSceneToThreadScene,
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
  const [agents, setAgents] = useState<AgentHubStatus[] | null>(null)
  useEffect(() => {
    void agentHubApi.detect().then(got => setAgents(got.agents ?? [])).catch(() => setAgents([]))
  }, [])
  useEffect(() => {
    if (selectedAgent) setAgent(selectedAgent)
  }, [selectedAgent])
  const detecting = agents === null
  const selected = agents?.find(item => item.name === agent)
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
      <p className="agent-hub-hint">{zh ? '对话页就是月汐自己的界面，不嵌官方窗口。未接入的 CLI 不会出现。' : "This page is Lunitide's own UI. Official vendor windows are not embedded. CLIs that are not wired do not appear."}</p>
      {blurb ? <p className="agent-hub-hint">{blurb}</p> : null}
      {selected?.hint && (selected.state !== 'available' || (selected.protocol === 'exec' && selected.interactive === false)) ? (
        <p className="agent-hub-hint">{selected.hint}</p>
      ) : null}
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
        workDir={workDir}
        exportDir={exportDir}
        onPickProject={() => void pickFolder()}
        onPickExport={() => void pickExport()}
        onPickFiles={() => void addInbox()}
        inboxFiles={inboxFiles}
        zh={zh}
      />
      <div className="agent-hub-templates">
        {FREE_TEMPLATES.map(item => (
          <button key={item.zh} type="button" onClick={() => setPrompt(item.prompt)}>
            {zh ? item.zh : item.en}
          </button>
        ))}
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
