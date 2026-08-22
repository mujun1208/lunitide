import React, { useCallback, useEffect, useState } from 'react'
import {
  BridgeClientError,
  createMutationAttempt,
  deliverableBridge,
  releaseBridge as defaultReleaseBridge,
  type ReleaseBridge,
} from '../bridge/client'
import type { ProjectDTO, ReleaseGetRevisionResult } from '../generated/bridge'
import {
  collectCrMembers,
  createProjectCrRevision,
  crIdForProject,
  readStoredCrRevision,
  writeStoredCrRevision,
} from './crRevision'

const problem = (e: unknown) =>
  e instanceof BridgeClientError
    ? e
    : new BridgeClientError(e instanceof Error ? e.message : '请求失败', 'CLIENT_ERROR', false, 'renderer')

const CHECKS = [
  '版本一致性：制品版本号与 CR 修订 digest 匹配',
  '依赖完整性：manifest members 无缺失',
  '配置隔离：dev/test/prod 环境配置分离',
  '传输对象完整性：DB/接口/开发清单均已纳入 CR',
  '语法检查：OpenAPI 已通过 parse 绑定',
  '回滚预案：保留上一 revision 可回退',
]

export function ReleasePanel({
  project,
  bridge = defaultReleaseBridge,
  readOnly = false,
}: {
  project?: ProjectDTO
  bridge?: ReleaseBridge
  readOnly?: boolean
}): React.JSX.Element {
  const crId = project ? crIdForProject(project) : ''
  const [timeline, setTimeline] = useState<ReleaseGetRevisionResult['revisions']>([])
  const [manifest, setManifest] = useState<ReleaseGetRevisionResult['manifest'] | undefined>()
  const [stored, setStored] = useState(readStoredCrRevision(project?.id ?? ''))
  const [revisionId, setRevisionId] = useState(stored?.crRevisionId ?? '')
  const [digest, setDigest] = useState(stored?.digest ?? '')
  const [packageId, setPackageId] = useState('')
  const [promotionId, setPromotionId] = useState('')
  const [targetEnv, setTargetEnv] = useState<'dev' | 'stage' | 'prod'>('dev')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')

  const loadRevision = useCallback(async () => {
    if (!crId) return
    try {
      const view = await bridge.getRevision({ crId })
      setTimeline(view.revisions)
      setManifest(view.manifest)
      const latest = view.revisions[view.revisions.length - 1]
      if (latest && !revisionId) {
        setRevisionId(stored?.crRevisionId ?? '')
        setDigest(latest.digest)
      }
    } catch (e) {
      setError(problem(e).message)
    }
  }, [bridge, crId, revisionId, stored?.crRevisionId])

  useEffect(() => { void loadRevision() }, [loadRevision])

  const [members, setMembers] = useState<{ name: string; size: number; sha256: string }[]>([])
  useEffect(() => {
    if (!project) { setMembers([]); return }
    void collectCrMembers(project, deliverableBridge).then(setMembers).catch(() => setMembers([]))
  }, [project, timeline.length])

  const createRevision = async () => {
    if (readOnly || busy || !project) return
    setBusy(true)
    setError('')
    try {
      const result = await createProjectCrRevision(project, `${project.name} 工作台发布修订`, deliverableBridge, bridge)
      setRevisionId(result.crRevisionId)
      setDigest(result.digest)
      writeStoredCrRevision(project.id, result.crRevisionId, result.digest)
      setStored({ crRevisionId: result.crRevisionId, digest: result.digest })
      setNote(`已创建 revision #${result.revisionNo}`)
      await loadRevision()
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
    }
  }

  const build = async () => {
    if (readOnly || busy || !revisionId.trim() || digest.trim().length !== 64) return
    setBusy(true)
    setError('')
    setPackageId('')
    try {
      const payload = { crRevisionId: revisionId.trim(), expectedDigest: digest.trim() }
      const result = await bridge.buildPackage(payload, { attempt: createMutationAttempt('release.buildPackage', payload) })
      setPackageId(result.packageId)
      setNote(`packageId ${result.packageId}`)
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
    }
  }

  const promote = async () => {
    if (readOnly || busy || !packageId) return
    setBusy(true)
    setError('')
    try {
      const payload = {
        packageId,
        targetEnv,
        policyContext: { requestedBy: 'workbench', projectId: project?.id },
        requestId: `promote-${project?.id ?? 'unknown'}-${Date.now()}`,
      }
      const result = await bridge.promote(payload, { attempt: createMutationAttempt('release.promote', payload) })
      setPromotionId(result.promotionId)
      setNote(`晋级 ${targetEnv} · state ${result.state}`)
    } catch (e) {
      setError(problem(e).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <aside className="pm-release-panel" aria-label="发布打包">
      <header className="pm-deliverable-head">
        <div>
          <b>CR 传输包</b>
          <small>{project ? `${project.projectCode} · ${project.name}` : '请选择项目'} · {crId || '—'}{note ? ` · ${note}` : ''}</small>
        </div>
      </header>
      <div className="release-panel-body">
        <section className="release-section">
          <h4>发布检查</h4>
          <ul className="release-checks">{CHECKS.map(c => <li key={c}>{c}</li>)}</ul>
        </section>
        <section className="release-section">
          <h4>CR 修订时间线</h4>
          {timeline.length ? (
            <ul className="release-timeline">
              {timeline.map(r => (
                <li key={r.revisionNo}>
                  <b>#{r.revisionNo}</b> {r.status} · <code>{r.digest.slice(0, 12)}…</code>
                </li>
              ))}
            </ul>
          ) : <p className="checklist-empty">尚无 CR 修订，可从交付物创建。</p>}
        </section>
        <section className="release-section">
          <h4>传输对象</h4>
          {members.length ? (
            <ul className="release-members">{members.map(m => <li key={m.name}>{m.name} · sha256 {m.sha256.slice(0, 12)}…</li>)}</ul>
          ) : <p className="checklist-empty">开发/接口/DB 绑定后将出现在 manifest。</p>}
        </section>
        {!readOnly && project && (
          <>
            <button type="button" className="primary" disabled={busy} onClick={() => void createRevision()}>从交付物创建 CR 修订</button>
            <label>CR Revision ID<input value={revisionId} onChange={e => setRevisionId(e.target.value)} placeholder="01ARZ3NDEKTSV4RRFFQ69G5FAV" disabled={readOnly || busy} /></label>
            <label>Expected Digest (sha256)<input value={digest} onChange={e => setDigest(e.target.value)} placeholder="64 位十六进制摘要" disabled={readOnly || busy} /></label>
            <button className="primary" disabled={readOnly || busy || !revisionId.trim() || digest.trim().length !== 64} onClick={() => void build()}>{busy ? '构建中…' : '构建发布包'}</button>
            {packageId && (
              <>
                <p className="release-result" role="status">packageId: <code>{packageId}</code></p>
                <label>目标环境
                  <select value={targetEnv} disabled={busy} onChange={e => setTargetEnv(e.target.value as 'dev' | 'stage' | 'prod')}>
                    <option value="dev">开发</option>
                    <option value="stage">测试</option>
                    <option value="prod">生产</option>
                  </select>
                </label>
                <button type="button" className="primary" disabled={busy} onClick={() => void promote()}>传输到 {targetEnv === 'dev' ? '开发' : targetEnv === 'stage' ? '测试' : '生产'} 环境</button>
              </>
            )}
            {promotionId && <p className="release-result">promotionId: <code>{promotionId}</code></p>}
          </>
        )}
        {manifest && Object.keys(manifest).length > 0 && (
          <details className="release-manifest"><summary>最新 manifest</summary><pre>{JSON.stringify(manifest, null, 2)}</pre></details>
        )}
        {error && <p className="error" role="alert"><b>{error}</b></p>}
      </div>
    </aside>
  )
}
