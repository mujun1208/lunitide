import React, { useEffect, useState } from 'react'
import { useZh } from '../i18n/language'
import { agentHubApi, type AgentHubName, type AgentHubStatus } from './agentHubApi'
import {
  PICK_PROJECT_DIR,
  SCENE_KEY,
  THREAD_SCENES,
  hubSceneToThreadScene,
  sceneBlurb,
  shortWorkDir,
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
  const submit = async () => {
    if (!scene) return
    if ((scene === 'write' || scene === 'fix') && !workDir) {
      setError(PICK_PROJECT_DIR)
      return
    }
    setError('')
    try {
      const created = await agentHubApi.threadCreate({
        harnessId: scene === 'free' ? agent : defaultHarness(scene),
        scene: hubSceneToThreadScene(scene),
        workspaceRoot: workDir,
      })
      const text = prompt.trim()
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
      {scene === 'free' && (
        <div className="agent-hub-pills">
          {agents.map(item => (
            <button
              key={item.name}
              type="button"
              className="agent-hub-pill"
              aria-pressed={agent === item.name}
              onClick={() => setAgent(item.name)}
            >
              {item.name}
            </button>
          ))}
        </div>
      )}
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
          <button type="button" className="agent-hub-run" onClick={() => void submit()}>
            {zh ? '执行' : 'Run'}
          </button>
        </div>
      </div>
      {error && <p className="agent-hub-error" role="alert">{error}</p>}
    </section>
  )
}
