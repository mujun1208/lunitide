import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  automationBridge,
  createMutationAttempt,
  type MutationAttempt,
  type AutomationBridge,
  type ProviderBridge,
  type SessionBridge,
  providerBridge,
  sessionBridge,
} from '../bridge/client'
import type { AutomationJobListResult, AutomationRunListResult, AutomationStatusResult, SessionDTO } from '../generated/bridge'
import { AutomationCreateDialog, draftFromTemplate, type AutomationDraft } from './AutomationCreateDialog'
import {AutomationStopButton,localAutomationTimezone} from './AutomationRunControls'
import { AUTOMATION_TEMPLATES, cronToHuman, type AutomationTemplate } from './automationTemplates'
import { ensureAutomationRunner, loadDefaultModel } from './ensureAutomationRunner'
import { AutomationRunDetail, automationRunLabel, schedulerStatusLabel } from './automationRunPresentation'

type Job = AutomationJobListResult['jobs'][number]
type Run = AutomationRunListResult['runs'][number]
type Tab = 'jobs' | 'runs' | 'templates'
export type AutomationViewState = { tab: Tab; openRun?: string }

const EMPTY_DRAFT = (): AutomationDraft => ({
  name: '',
  cron: '0 9 * * *',
  timezone: localAutomationTimezone(),
  prompt: '',
  providerId: '',
  modelId: '',
  sessionId: '',
  executionMode: 'auto-edit',
  sessionMode: 'bound',
  runOnce: false,
  webhookUrl: '',
  enabled: true,
})

const MODE_LABEL: Record<AutomationDraft['executionMode'], string> = {
  approval: '手动审批',
  'auto-edit': '自动审批',
  'full-access': '完全访问',
}

const fmtTime = (iso?: string) => {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    })
  } catch {
    return iso
  }
}

export function AutomationCenterPage({
  onCreateInChat,
  onOpenSession,
  bridge = automationBridge,
  providers = providerBridge,
  sessions = sessionBridge,
  initialViewState,
  onViewStateChange,
}: {
  onCreateInChat: () => void
  onOpenSession?: (session: SessionDTO) => void | Promise<void>
  bridge?: AutomationBridge
  providers?: ProviderBridge
  sessions?: SessionBridge
  initialViewState?: AutomationViewState
  onViewStateChange?: (state: AutomationViewState) => void
}): React.JSX.Element {
  const saveAttempt = useRef<MutationAttempt<object> | undefined>(undefined)
  const generation = useRef(0)
  const [tab, setTab] = useState<Tab>(initialViewState?.tab ?? 'jobs')
  const [jobs, setJobs] = useState<Job[]>([])
  const [runs, setRuns] = useState<Run[]>([])
  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState<AutomationStatusResult>()
  const [runnerSessionId, setRunnerSessionId] = useState('')
  const [defaults, setDefaults] = useState<{ providerId: string; modelId: string }>()
  const [draft, setDraft] = useState<AutomationDraft>(EMPTY_DRAFT())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)
  const scope = useRef(0)
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => {
    scope.current++
    busyRef.current = false
    setBusy(false)
    return () => {
      scope.current++
      if (refreshTimer.current !== undefined) clearTimeout(refreshTimer.current)
    }
  }, [bridge])
  const [openRun, setOpenRun] = useState<string | undefined>(initialViewState?.openRun)
  const restoreRun = useRef(initialViewState?.openRun)
  const selectedRunRow = useRef<HTMLLIElement | null>(null)
  useEffect(() => { onViewStateChange?.({ tab, openRun }) }, [tab, openRun, onViewStateChange])
  useEffect(() => {
    if (tab === 'runs' && restoreRun.current && runs.some(run => run.id === restoreRun.current)) {
      selectedRunRow.current?.scrollIntoView?.({ block: 'nearest' })
      restoreRun.current = undefined
    }
  }, [tab, runs])

  const reload = useCallback(async () => {
    const epoch = ++generation.current
    const [j, r, s] = await Promise.all([bridge.listJobs(), bridge.listRuns({ limit: 40 }), bridge.status()])
    if (epoch !== generation.current) return
    setJobs(j.jobs)
    setRuns(r.runs)
    setStatus(s)
    setLoading(false)
    return s.runningJobs.length > 0
  }, [bridge])

  useEffect(() => {
    let alive = true
    void (async () => {
      try {
        const [{ session }, model] = await Promise.all([
          ensureAutomationRunner(undefined, sessions),
          loadDefaultModel(providers),
        ])
        if (!alive) return
        setRunnerSessionId(session.id)
        setDefaults(model)
        setDraft((d) => ({
          ...d,
          sessionId: session.id,
          providerId: model?.providerId ?? '',
          modelId: model?.modelId ?? '',
        }))
      } catch {
        /* surfaced when saving */
      }
    })()
    return () => { alive = false }
  }, [providers, sessions])

  useEffect(() => {
    let alive = true
    let timer: ReturnType<typeof setTimeout> | undefined
    const poll = async () => {
      let running = false
      try { running = (await reload()) === true }
      catch (e) { if (alive) { setLoading(false); setNotice(e instanceof Error ? e.message : '自动化刷新失败') } }
      if (alive) timer = setTimeout(() => void poll(), running ? 3_000 : 15_000)
    }
    void poll()
    return () => { alive = false; generation.current++; if (timer) clearTimeout(timer) }
  }, [reload, status?.runningJobs.length])

  const openManual = () => {
    saveAttempt.current = undefined
    setDraft((d) => ({
      ...EMPTY_DRAFT(),
      sessionId: runnerSessionId || d.sessionId,
      providerId: defaults?.providerId ?? d.providerId,
      modelId: defaults?.modelId ?? d.modelId,
    }))
    setNotice('')
    setDialogOpen(true)
  }

  const openTemplate = (template: AutomationTemplate) => {
    saveAttempt.current = undefined
    setDraft(
      draftFromTemplate(template, {
        timezone: localAutomationTimezone(),
        sessionId: runnerSessionId,
        providerId: defaults?.providerId ?? '',
        modelId: defaults?.modelId ?? '',
        executionMode: 'auto-edit',
        sessionMode: 'isolated',
        runOnce: false,
        webhookUrl: '',
        enabled: true,
      }),
    )
    setNotice('')
    setDialogOpen(true)
  }

  const save = async () => {
    if (!draft.name.trim() || !draft.prompt.trim()) {
      setNotice('请填写任务名称与提示词')
      return
    }
    if (!draft.providerId || !draft.modelId || !draft.sessionId) {
      setNotice('缺少模型或会话参数，请先在设置中配置模型')
      return
    }
    if (busyRef.current) return
    busyRef.current = true
    const operationScope = scope.current
    setBusy(true)
    setNotice('')
    try {
      const payload = {
        id: draft.id,
        expectedRevision: draft.expectedRevision,
        name: draft.name.trim(),
        cron: draft.cron.trim(),
        timezone: draft.timezone ?? 'UTC',
        prompt: draft.prompt.trim(),
        providerId: draft.providerId as never,
        modelId: draft.modelId,
        sessionId: draft.sessionId as never,
        executionMode: draft.executionMode,
        sessionMode: draft.sessionMode,
        runOnce: draft.runOnce || draft.cron.startsWith('at:'),
        webhookUrl: draft.webhookUrl.trim(),
        enabled: draft.enabled,
      }
      if (!saveAttempt.current || JSON.stringify(saveAttempt.current.payload) !== JSON.stringify(payload))
        saveAttempt.current = createMutationAttempt('automation.job.set', payload)
      await bridge.setJob(payload, { attempt: saveAttempt.current as MutationAttempt<typeof payload> })
      if (operationScope !== scope.current) return
      setDialogOpen(false)
      setTab('jobs')
      setNotice('任务已保存')
      await reload().catch(() => {
        if (operationScope === scope.current) setNotice('任务已保存，列表刷新失败，请重新刷新')
      })
    } catch (e) {
      if (operationScope !== scope.current) return
      setNotice(e instanceof Error ? e.message : '保存失败')
    } finally {
      if (operationScope === scope.current) {
        busyRef.current = false
        setBusy(false)
      }
    }
  }

  const trigger = async (job: Job) => {
    if (busyRef.current) return
    busyRef.current = true
    const operationScope = scope.current
    setBusy(true)
    setNotice('')
    try {
      await bridge.triggerJob({ id: job.id })
      if (operationScope !== scope.current) return
      setNotice(`已触发「${job.name}」`)
      setTab('runs')
      if (refreshTimer.current !== undefined) clearTimeout(refreshTimer.current)
      refreshTimer.current = setTimeout(() => {
        if (operationScope !== scope.current) return
        void reload().catch((e) => { if(operationScope === scope.current) setNotice(e instanceof Error ? e.message : '自动化刷新失败') })
      }, 800)
    } catch (e) {
      if (operationScope !== scope.current) return
      setNotice(e instanceof Error ? e.message : '触发失败')
    } finally {
      if (operationScope === scope.current) {
        busyRef.current = false
        setBusy(false)
      }
    }
  }

  const toggle = async (job: Job) => {
    if (busyRef.current) return
    busyRef.current = true
    const operationScope = scope.current
    setBusy(true)
    try {
      await bridge.setJob({
        id: job.id,
        expectedRevision: job.revision,
        name: job.name,
        cron: job.cron,
        prompt: job.prompt,
        providerId: job.providerId,
        modelId: job.modelId,
        sessionId: job.sessionId,
        executionMode: (job.executionMode as AutomationDraft['executionMode']) || 'auto-edit',
        sessionMode: job.sessionMode === 'isolated' ? 'isolated' : 'bound',
        runOnce: job.runOnce === true,
        webhookUrl: job.webhookUrl ?? '',
        enabled: !job.enabled,
      })
      if (operationScope !== scope.current) return
      await reload()
      if (operationScope !== scope.current) return
    } catch (e) {
      if (operationScope !== scope.current) return
      setNotice(e instanceof Error ? e.message : '更新失败')
    } finally {
      if (operationScope === scope.current) {
        busyRef.current = false
        setBusy(false)
      }
    }
  }

  const remove = async (job: Job) => {
    if (busyRef.current) return
    busyRef.current = true
    const operationScope = scope.current
    setBusy(true)
    try {
      await bridge.deleteJob({ id: job.id })
      if (operationScope !== scope.current) return
      await reload()
      if (operationScope !== scope.current) return
      setNotice('任务已删除')
    } catch (e) {
      if (operationScope !== scope.current) return
      setNotice(e instanceof Error ? e.message : '删除失败')
    } finally {
      if (operationScope === scope.current) {
        busyRef.current = false
        setBusy(false)
      }
    }
  }

  const nextFire = useMemo(() => (status?.nextFire ?? {}) as Record<string, string | undefined>, [status?.nextFire])

  return (
    <div className="automation-center">
      <header className="automation-center-head">
        <div>
          <h1>自动化</h1>
          <p>配置和管理自动化任务，让 Lunitide 按计划执行工作流。</p>
        </div>
        <div className="automation-center-actions">
          <button type="button" onClick={openManual}>
            手动新建
          </button>
          <button type="button" className="primary" onClick={onCreateInChat}>
            ⊕ 在对话中创建
          </button>
        </div>
      </header>
      <nav className="automation-center-tabs" aria-label="自动化视图">
        <button type="button" className={tab === 'jobs' ? 'active' : ''} onClick={() => setTab('jobs')}>
          已配置
        </button>
        <button type="button" className={tab === 'runs' ? 'active' : ''} onClick={() => setTab('runs')}>
          执行历史
        </button>
        <button type="button" className={tab === 'templates' ? 'active' : ''} onClick={() => setTab('templates')}>
          任务模板
        </button>
        <span className={status?.running ? 'automation-heartbeat is-live' : 'automation-heartbeat'} role="status">
          {schedulerStatusLabel(status)}
        </span>
      </nav>
      {tab === 'jobs' && (
        <section className="automation-center-jobs" aria-label="已配置任务">
          {jobs.length ? (
            <>
              {[
                {
                  id: 'scheduled',
                  title: '定时任务',
                  items: jobs.filter((j) => j.sessionMode !== 'isolated' && !j.runOnce && !j.cron.startsWith('at:')),
                },
                { id: 'isolated', title: '独立会话', items: jobs.filter((j) => j.sessionMode === 'isolated') },
                {
                  id: 'once',
                  title: '一次性',
                  items: jobs.filter((j) => j.sessionMode !== 'isolated' && (j.runOnce || j.cron.startsWith('at:'))),
                },
              ].map((lane) =>
                lane.items.length ? (
                  <div key={lane.id} className="automation-job-lane">
                    <h2>{lane.title}</h2>
                    <ul className="automation-jobs">
                      {lane.items.map((job) => (
                        <li key={job.id} className={`automation-job ${job.enabled ? '' : 'is-disabled'}`}>
                          <div className="automation-job-head">
                            <b>{job.name}</b>
                            <code>{cronToHuman(job.cron)}</code>
                            {!job.cron.startsWith('at:')&&<small>{job.timezone||'UTC'} 时区</small>}
                            <span className="automation-job-mode">
                              {MODE_LABEL[(job.executionMode as AutomationDraft['executionMode']) || 'auto-edit']}
                            </span>
                            {job.sessionMode === 'isolated' && <span className="automation-job-mode">独立</span>}
                          </div>
                          <div className="automation-job-meta">
                            <span>下次 {fmtTime(nextFire[job.id])}</span>
                            <span>上次 {fmtTime(job.lastRunAt)}</span>
                            {status?.runningJobs?.includes(job.id) && (
                              <span className="automation-job-running">正在执行…</span>
                            )}
                          </div>
                          <div className="automation-job-actions">
                            <AutomationStopButton run={runs.find(run=>run.jobId===job.id&&run.state==='running')} bridge={bridge} onRequested={reload}/>
                            <button type="button" disabled={busy} onClick={() => void trigger(job)}>
                              立即运行
                            </button>
                            <button type="button" disabled={busy} onClick={() => void toggle(job)}>
                              {job.enabled ? '停用' : '启用'}
                            </button>
                            <button
                              type="button"
                              className="automation-delete"
                              disabled={busy}
                              onClick={() => void remove(job)}
                            >
                              删除
                            </button>
                          </div>
                        </li>
                      ))}
                    </ul>
                  </div>
                ) : null,
              )}
            </>
          ) : (
            <p className="automation-empty">还没有定时任务。可以手动新建、从模板创建，或在对话中描述任务。</p>
          )}
        </section>
      )}
      {tab === 'runs' && (
        <section className="automation-center-runs" aria-label="执行历史">
          {runs.length ? (
            <ul className="automation-runs">
              {runs.map((run) => (
                <li key={run.id} ref={openRun === run.id ? selectedRunRow : undefined} className={`automation-run is-${run.state}`}>
                  <button
                    type="button"
                    className="automation-run-row"
                    aria-expanded={openRun === run.id}
                    onClick={() => setOpenRun(openRun === run.id ? undefined : run.id)}
                  >
                    <span className={`automation-run-state is-${run.state}`}>
                      {automationRunLabel(run)}
                    </span>
                    <b>{run.jobName}</b>
                    <small>
                      {run.trigger === 'manual' ? '手动' : '定时'} · {fmtTime(run.startedAt)}
                      {run.totalTokens ? ` · ${run.totalTokens} tok` : ''}
                    </small>
                  </button>
                  {openRun === run.id && (
                    <><AutomationStopButton run={run} bridge={bridge} onRequested={reload}/><AutomationRunDetail run={run} onOpenSession={onOpenSession ? id => {
                      void Promise.resolve().then(() => onOpenSession(id)).catch(e => setNotice(e instanceof Error ? e.message : '执行对话打开失败'))
                    } : undefined} /></>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p className="automation-empty" role="status">{loading ? '正在读取执行历史…' : notice ? '执行历史暂时未能读取，请稍后重试。' : '还没有运行记录。'}</p>
          )}
        </section>
      )}
      {tab === 'templates' && (
        <section className="automation-template-grid" aria-label="任务模板">
          {AUTOMATION_TEMPLATES.map((template) => (
            <button
              type="button"
              key={template.id}
              className="automation-template-card"
              onClick={() => openTemplate(template)}
            >
              <span className="automation-template-dots" aria-hidden="true">
                <i />
                <i />
                <i />
              </span>
              <b>{template.title}</b>
              <p>{template.description}</p>
              <small>{cronToHuman(template.cron)}</small>
            </button>
          ))}
        </section>
      )}
      {status?.lastError && <p role="alert">{status.lastError}</p>}
      {notice && (
        <p className="automation-notice" role="status">
          {notice}
        </p>
      )}
      <AutomationCreateDialog
        open={dialogOpen}
        draft={draft}
        busy={busy}
        notice={notice && dialogOpen ? notice : ''}
        templateLink={tab !== 'templates'}
        onClose={() => {
          if (!busy) setDialogOpen(false)
        }}
        onChange={setDraft}
        onSubmit={() => void save()}
        onPickTemplate={() => {
          setDialogOpen(false)
          setTab('templates')
        }}
      />
    </div>
  )
}
