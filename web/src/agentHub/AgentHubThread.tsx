import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useZh } from '../i18n/language'
import { SharedHubComposer, type HubAccessMode } from '../session/SharedHubComposer'
import { usePanelResize } from '../ui/usePanelResize'
import { Workspace } from '../workspace/Workspace'
import { AgentHubAskBar } from './AgentHubAskBar'
import {
  agentHubApi,
  type AgentHubThreadDetail,
} from './agentHubApi'
import {
  agentDisplayName,
  displayUserFacingMessage,
  hubSceneToThreadScene,
  threadPptMissing,
  threadSceneToHub,
  type HubScene,
  type InboxFile,
} from './agentHubCopy'
import { createAgentHubLocalWorkspace, HUB_ATTACHMENTS } from './agentHubWorkspace'

function liveStatus(status: string | undefined): boolean {
  return status === 'running' || status === 'waiting_user'
}

export function AgentHubThread({
  threadId,
}: {
  threadId: string
}): React.JSX.Element {
  const zh = useZh()
  const [detail, setDetail] = useState<AgentHubThreadDetail>()
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')
  const [pending, setPending] = useState(false)
  const [scene, setScene] = useState<HubScene>('free')
  const [accessMode, setAccessMode] = useState<HubAccessMode>('approval')
  const [inboxFiles, setInboxFiles] = useState<InboxFile[]>([])
  const [exportDir, setExportDir] = useState('')
  const [workspaceOpen, setWorkspaceOpen] = useState(true)
  const [workspaceExpanded, setWorkspaceExpanded] = useState(false)
  const [workspaceRev, setWorkspaceRev] = useState(0)
  const [workspaceWidth, startWorkspaceResize] = usePanelResize({
    storageKey: 'lunitide:workspace-width',
    initial: 520,
    min: 300,
    max: () => Math.min(800, window.innerWidth - 80),
    reverse: true,
  })
  const threadIdRef = useRef(threadId)
  const rootRef = useRef('')
  threadIdRef.current = threadId
  rootRef.current = detail?.thread.workspaceRoot ?? ''
  const localWorkspace = useMemo(
    () => createAgentHubLocalWorkspace({ threadId: () => threadIdRef.current, root: () => rootRef.current }),
    [],
  )
  const wasLive = useRef(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const pinBottom = useRef(true)
  useEffect(() => { wasLive.current = false; setInboxFiles([]); setExportDir(''); setPending(false); setWorkspaceRev(0); pinBottom.current = true }, [threadId])
  const applyDetail = async (next: AgentHubThreadDetail) => {
    if (threadIdRef.current !== next.thread.threadId) return
    setDetail(next)
    setScene(threadSceneToHub(next.thread.scene))
    setExportDir(next.thread.exportDir)
    if (next.thread.accessMode === 'approval' || next.thread.accessMode === 'auto-edit' || next.thread.accessMode === 'full-access') {
      setAccessMode(next.thread.accessMode)
    }
    setWorkspaceRev(value => value + 1)
  }
  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const next = await agentHubApi.threadGet({ threadId })
        if (!alive) return
        setDetail(next)
        setScene(threadSceneToHub(next.thread.scene))
        setExportDir(next.thread.exportDir)
        if (next.thread.accessMode === 'approval' || next.thread.accessMode === 'auto-edit' || next.thread.accessMode === 'full-access') {
          setAccessMode(next.thread.accessMode)
        }
        setError('')
      } catch (err) {
        if (alive) setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '会话读取失败。' : 'Could not load this thread.'))
      }
    }
    void load()
    return () => { alive = false }
  }, [threadId, zh])
  useEffect(() => {
    const live = liveStatus(detail?.thread.status)
    let alive = true
    const timer = window.setInterval(() => {
      void agentHubApi.threadGet({ threadId }).then(next => {
        if (!alive) return
        setDetail(next)
        setScene(threadSceneToHub(next.thread.scene))
        setExportDir(next.thread.exportDir)
        if (liveStatus(next.thread.status) || pending) setWorkspaceRev(value => value + 1)
        if (!liveStatus(next.thread.status)) setPending(false)
      }).catch(() => undefined)
    }, live || pending ? 400 : 4000)
    return () => { alive = false; window.clearInterval(timer) }
  }, [threadId, detail?.thread.status, pending])
  useEffect(() => {
    const live = liveStatus(detail?.thread.status)
    if (wasLive.current && !live && detail) {
      setWorkspaceRev(value => value + 1)
      setPending(false)
    }
    wasLive.current = live
  }, [detail])
  const visibleMessages = (detail?.messages ?? []).flatMap(item => {
    const content = displayUserFacingMessage(item.content, item.role)
    return content == null ? [] : [{ ...item, content }]
  })
  useEffect(() => {
    const node = scrollRef.current
    if (!node || !pinBottom.current) return
    node.scrollTop = node.scrollHeight
  }, [visibleMessages.length, detail?.prompt, pending, detail?.thread.status])
  const live = liveStatus(detail?.thread.status)
  const send = async () => {
    const text = draft.trim()
    if (!text || live || pending) return
    setPending(true)
    setError('')
    pinBottom.current = true
    try {
      const next = await agentHubApi.threadPrompt({ threadId, text })
      await applyDetail(next)
      setDraft('')
    } catch (err) {
      setPending(false)
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '消息没有发出。' : 'The message did not send.'))
    }
  }
  const cancel = async () => {
    try {
      const next = await agentHubApi.threadCancel({ threadId })
      await applyDetail(next)
      setPending(false)
      setError('')
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '没有取消。' : 'Could not cancel.'))
    }
  }
  const pickFolder = async () => {
    try {
      const got = await agentHubApi.pickDir()
      if (got.canceled || !got.path) return
      const next = await agentHubApi.threadUpdate({ threadId, workspaceRoot: got.path })
      await applyDetail(next)
      setError('')
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '没有选到工作目录。' : 'Could not choose a work folder.'))
    }
  }
  const pickExport = async () => {
    try {
      const got = await agentHubApi.pickDir()
      if (got.canceled || !got.path) return
      await applyDetail(await agentHubApi.threadUpdate({ threadId, exportDir: got.path }))
      setError('')
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '没有选到导出目录。' : 'Could not choose an export folder.'))
    }
  }
  const changeScene = async (next: HubScene) => {
    setScene(next)
    try {
      await applyDetail(await agentHubApi.threadUpdate({ threadId, scene: hubSceneToThreadScene(next) }))
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '任务类型没有改成。' : 'Could not change the task type.'))
    }
  }
  const changeAccess = async (next: HubAccessMode) => {
    setAccessMode(next)
    try {
      await applyDetail(await agentHubApi.threadUpdate({ threadId, accessMode: next }))
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '权限没有改成。' : 'Could not change access.'))
    }
  }
  const addInbox = async () => {
    const workDir = detail?.thread.workspaceRoot
    if (!workDir) {
      setError(zh ? '请先选择项目目录' : 'Pick a project folder first')
      return
    }
    try {
      const got = await agentHubApi.inbox({ action: 'files', workDir })
      if (got.canceled) return
      setInboxFiles(got.files ?? [])
      setError('')
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '没有加入参考文件。' : 'Could not add reference files.'))
    }
  }
  const noDeck = detail?.thread.status === 'success' && threadPptMissing(detail.thread.scene, detail.files)
  return (
    <div
      className={`workspace-layout${workspaceOpen ? ' workspace-is-open' : ''}${workspaceExpanded ? ' workspace-is-expanded' : ''}`}
      style={{ '--workspace-width': `${workspaceWidth}px` } as React.CSSProperties}
    >
      <section
        className="message-panel personal-message-panel"
        aria-hidden={workspaceExpanded || undefined}
        inert={workspaceExpanded || undefined}
        aria-label={zh ? `${detail?.thread.title || '会话'} 消息` : 'messages'}
      >
        <div
          className="conversation-scroll hub-conversation-scroll"
          ref={scrollRef}
          onScroll={event => {
            const node = event.currentTarget
            pinBottom.current = node.scrollHeight - node.scrollTop - node.clientHeight < 80
          }}
        >
          <div className="message-list hub-message-list">
            {noDeck ? (
              <p className="agent-hub-hint" role="status">{zh ? '没有文稿。打开目录查看本轮文件，或看时间线说明。' : 'No deck was produced. Open the folder or read the timeline.'}</p>
            ) : null}
            {visibleMessages.map(item => (
              <div key={item.id} className={`hub-msg ${item.role === 'user' ? 'is-user' : 'is-bot'}`} data-role={item.role}>
                {item.role !== 'user' ? <div className="hub-msg-meta">{agentDisplayName(detail?.thread.harnessId ?? '')}</div> : null}
                <p>{item.content}</p>
              </div>
            ))}
            {(live || pending) ? (
              <p className="agent-hub-hint hub-waiting" role="status">{zh ? 'Agent 处理中…' : 'Agent is working…'}</p>
            ) : null}
            {detail?.prompt ? (
              <AgentHubAskBar threadId={threadId} prompt={detail.prompt} onResponded={next => { void applyDetail(next) }} />
            ) : null}
          </div>
        </div>
        <SharedHubComposer
          value={draft}
          onChange={setDraft}
          onSubmit={() => void send()}
          onStop={() => void cancel()}
          live={live || pending}
          placeholder={zh ? '向 Agent 描述任务…' : 'Describe the task for the Agent…'}
          inputLabel={zh ? '消息' : 'Message'}
          accessMode={accessMode}
          onAccessMode={next => { void changeAccess(next) }}
          scene={scene}
          onScene={next => { void changeScene(next) }}
          showScene
          showAccess
          showPlus
          workDir={detail?.thread.workspaceRoot}
          exportDir={exportDir || detail?.thread.exportDir}
          inboxFiles={inboxFiles}
          onPickProject={() => void pickFolder()}
          onPickExport={() => void pickExport()}
          onPickFiles={() => void addInbox()}
          zh={zh}
        />
        {error && <p className="agent-hub-error" role="alert">{error}</p>}
      </section>
      {workspaceOpen ? (
        <div
          className="workspace-resizer"
          role="separator"
          aria-orientation="vertical"
          aria-label={zh ? '调整工作区宽度' : 'Resize workspace'}
          onPointerDown={startWorkspaceResize}
        />
      ) : null}
      <aside className="workspace-column" hidden={!workspaceOpen}>
        {detail ? (
          <Workspace
            attachments={HUB_ATTACHMENTS}
            projectId={threadId}
            sessionId={threadId}
            onClose={() => setWorkspaceOpen(false)}
            expanded={workspaceExpanded}
            onToggleExpand={() => setWorkspaceExpanded(open => !open)}
            localWorkspace={localWorkspace}
            filesFocus="local"
            projectRoot={detail.thread.workspaceRoot}
            executionMode={accessMode}
            refreshRevision={workspaceRev}
          />
        ) : null}
      </aside>
      {!workspaceOpen ? (
        <button type="button" className="workspace-toggle hub-workspace-toggle" onClick={() => setWorkspaceOpen(true)}>
          {zh ? '工作区' : 'Workspace'}
        </button>
      ) : null}
    </div>
  )
}
