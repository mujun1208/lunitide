import React, { useEffect, useRef, useState } from 'react'
import { useZh } from '../i18n/language'
import { SharedHubComposer, type HubAccessMode } from '../session/SharedHubComposer'
import { AgentHubAskBar } from './AgentHubAskBar'
import { AgentHubFileInspector } from './AgentHubFileInspector'
import {
  agentHubApi,
  type AgentHubPreview,
  type AgentHubThreadDetail,
  type AgentHubWorkspaceItem,
} from './agentHubApi'
import {
  agentDisplayName,
  displayUserFacingMessage,
  hubSceneToThreadScene,
  parentWorkspacePath,
  threadPptMissing,
  threadSceneToHub,
  visibleWorkspaceEntry,
  type HubScene,
  type InboxFile,
} from './agentHubCopy'

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
  const [files, setFiles] = useState<AgentHubWorkspaceItem[]>([])
  const [folder, setFolder] = useState('')
  const [preview, setPreview] = useState<AgentHubPreview>()
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')
  const [scene, setScene] = useState<HubScene>('free')
  const [accessMode, setAccessMode] = useState<HubAccessMode>('approval')
  const [inboxFiles, setInboxFiles] = useState<InboxFile[]>([])
  const [exportDir, setExportDir] = useState('')
  const threadIdRef = useRef(threadId)
  threadIdRef.current = threadId
  const wasLive = useRef(false)
  useEffect(() => { wasLive.current = false; setFolder(''); setInboxFiles([]); setExportDir('') }, [threadId])
  const refreshFiles = async (id: string, relativePath = folder) => {
    const listed = await agentHubApi.workspaceList({ threadId: id, ...(relativePath ? { relativePath } : {}) })
    if (threadIdRef.current !== id) return
    setFiles((listed.items ?? []).filter(item => visibleWorkspaceEntry(item.name)))
  }
  const applyDetail = async (next: AgentHubThreadDetail) => {
    if (threadIdRef.current !== next.thread.threadId) return
    setDetail(next)
    setScene(threadSceneToHub(next.thread.scene))
    setExportDir(next.thread.exportDir)
    if (next.thread.accessMode === 'approval' || next.thread.accessMode === 'auto-edit' || next.thread.accessMode === 'full-access') {
      setAccessMode(next.thread.accessMode)
    }
    await refreshFiles(next.thread.threadId)
  }
  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const [next, listed] = await Promise.all([
          agentHubApi.threadGet({ threadId }),
          agentHubApi.workspaceList({ threadId }),
        ])
        if (!alive) return
        setDetail(next)
        setScene(threadSceneToHub(next.thread.scene))
        setExportDir(next.thread.exportDir)
        if (next.thread.accessMode === 'approval' || next.thread.accessMode === 'auto-edit' || next.thread.accessMode === 'full-access') {
          setAccessMode(next.thread.accessMode)
        }
        setFiles((listed.items ?? []).filter(item => visibleWorkspaceEntry(item.name)))
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
      void Promise.all([
        agentHubApi.threadGet({ threadId }),
        agentHubApi.workspaceList({ threadId, ...(folder ? { relativePath: folder } : {}) }),
      ]).then(([next, listed]) => {
        if (!alive) return
        setDetail(next)
        setScene(threadSceneToHub(next.thread.scene))
        setExportDir(next.thread.exportDir)
        setFiles((listed.items ?? []).filter(item => visibleWorkspaceEntry(item.name)))
      }).catch(() => undefined)
    }, live ? 400 : 4000)
    return () => { alive = false; window.clearInterval(timer) }
  }, [threadId, detail?.thread.status, folder])
  useEffect(() => {
    const live = liveStatus(detail?.thread.status)
    if (wasLive.current && !live && detail) {
      void refreshFiles(detail.thread.threadId)
    }
    wasLive.current = live
  }, [detail])
  const live = liveStatus(detail?.thread.status)
  const send = async () => {
    const text = draft.trim()
    if (!text || live) return
    try {
      const next = await agentHubApi.threadPrompt({ threadId, text })
      await applyDetail(next)
      setDraft('')
      setError('')
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '消息没有发出。' : 'The message did not send.'))
    }
  }
  const cancel = async () => {
    try {
      const next = await agentHubApi.threadCancel({ threadId })
      await applyDetail(next)
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
  const openPreview = async (path: string) => {
    setPreview(await agentHubApi.preview({ threadId, path }))
  }
  const openItem = async (item: AgentHubWorkspaceItem) => {
    if (item.isDir) {
      setFolder(item.path)
      await refreshFiles(threadId, item.path)
      return
    }
    await openPreview(item.path)
  }
  const goUp = async () => {
    const next = parentWorkspacePath(folder)
    setFolder(next)
    await refreshFiles(threadId, next)
  }
  const noDeck = detail?.thread.status === 'success' && threadPptMissing(detail.thread.scene, detail.files)
  const visibleMessages = (detail?.messages ?? []).flatMap(item => {
    const content = displayUserFacingMessage(item.content, item.role)
    return content == null ? [] : [{ ...item, content }]
  })
  const showWorkspace = files.length > 0 || Boolean(folder) || Boolean(preview)
  return (
    <div className={`workspace-layout${showWorkspace ? ' workspace-is-open' : ''}`}>
      <section className="message-panel" aria-label={zh ? `${detail?.thread.title || '会话'} 消息` : 'messages'}>
        {noDeck ? (
          <p className="agent-hub-hint" role="status">{zh ? '没有文稿。打开目录查看本轮文件，或看时间线说明。' : 'No deck was produced. Open the folder or read the timeline.'}</p>
        ) : null}
        {visibleMessages.map(item => (
          <div key={item.id} className={`hub-msg ${item.role === 'user' ? 'is-user' : 'is-bot'}`} data-role={item.role}>
            {item.role !== 'user' ? <div className="hub-msg-meta">{agentDisplayName(detail?.thread.harnessId ?? '')}</div> : null}
            <p>{item.content}</p>
          </div>
        ))}
        {detail?.prompt ? (
          <AgentHubAskBar threadId={threadId} prompt={detail.prompt} onResponded={next => { void applyDetail(next) }} />
        ) : null}
        <SharedHubComposer
          value={draft}
          onChange={setDraft}
          onSubmit={() => void send()}
          onStop={() => void cancel()}
          live={live}
          placeholder={zh ? '继续这轮任务…' : 'Continue this thread…'}
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
      {showWorkspace ? (
        <>
          <div className="workspace-resizer" />
          <aside className="workspace-column">
            <div className="workspace">
              {folder ? (
                <button type="button" className="agent-hub-art" onClick={() => void goUp()}>{zh ? '上一级' : 'Up'}</button>
              ) : null}
              {files.map(item => (
                <button key={item.path} type="button" className="agent-hub-art" onClick={() => void openItem(item)}>
                  {item.name}
                </button>
              ))}
              <AgentHubFileInspector
                preview={preview}
                onOpen={() => { if (preview) void agentHubApi.open({ threadId, path: preview.path }) }}
              />
            </div>
          </aside>
        </>
      ) : null}
    </div>
  )
}
