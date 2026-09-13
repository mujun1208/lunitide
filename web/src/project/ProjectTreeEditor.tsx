import React, { useEffect, useState } from 'react'
import type { ProjectDTO } from '../generated/bridge'
import { projectSpineApi, type ProjectTreeV1 } from './projectSpineApi'

export function ProjectTreeEditor({
  project,
  readOnly = false,
  onProjectUpdated,
}: {
  project: ProjectDTO
  readOnly?: boolean
  onProjectUpdated?: (project: ProjectDTO) => void
}): React.JSX.Element {
  const [dirs, setDirs] = useState('')
  const [codeRoot, setCodeRoot] = useState('src')
  const [phaseMap, setPhaseMap] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [status, setStatus] = useState(project.treeStatus ?? 'none')

  useEffect(() => {
    let cancelled = false
    void projectSpineApi.treeGet({ projectId: project.id }).then(got => {
      if (cancelled) return
      setDirs((got.tree?.dirs ?? []).join('\n'))
      setCodeRoot(got.tree?.codeRoot || 'src')
      setPhaseMap(got.tree?.phaseMap ?? {})
      setStatus(got.treeStatus ?? project.treeStatus ?? 'none')
    }).catch(e => {
      if (!cancelled) setError(e instanceof Error ? e.message : '无法读取项目目录树')
    })
    return () => { cancelled = true }
  }, [project.id, project.treeStatus])

  const treeFromForm = (): ProjectTreeV1 => ({
    version: 1,
    dirs: dirs.split(/\r?\n/).map(line => line.trim()).filter(Boolean),
    phaseMap,
    codeRoot: codeRoot.trim() || 'src',
  })

  const save = async () => {
    if (readOnly || busy) return
    setBusy(true)
    setError('')
    try {
      const saved = await projectSpineApi.treePut({ projectId: project.id, version: project.version, tree: treeFromForm() })
      onProjectUpdated?.(saved.project)
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存目录树失败')
    } finally {
      setBusy(false)
    }
  }

  const materialize = async () => {
    if (readOnly || busy) return
    setBusy(true)
    setError('')
    try {
      const saved = await projectSpineApi.treeMaterialize({ projectId: project.id, version: project.version })
      setStatus(saved.project.treeStatus ?? 'ready')
      onProjectUpdated?.(saved.project)
    } catch (e) {
      setError(e instanceof Error ? e.message : '生成目录失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="checklist-panel" aria-label="项目结构树">
      <header className="checklist-head">
        <div>
          <b>项目结构规范（可执行树）</b>
          <small>状态 {status || 'none'} · 确认需求架构后才会在磁盘上生成子目录</small>
        </div>
        {!readOnly && (
          <div className="checklist-actions">
            <button type="button" disabled={busy || !project.rootPath} onClick={() => void save()}>保存树</button>
            <button type="button" className="primary" disabled={busy || !project.rootPath} onClick={() => void materialize()}>补生成目录</button>
          </div>
        )}
      </header>
      {error && <p className="error" role="alert"><b>{error}</b></p>}
      <label>代码根<input value={codeRoot} disabled={readOnly || busy} onChange={e => setCodeRoot(e.target.value)} /></label>
      <label className="wide">目录列表<textarea rows={8} value={dirs} disabled={readOnly || busy} onChange={e => setDirs(e.target.value)} /></label>
    </section>
  )
}
