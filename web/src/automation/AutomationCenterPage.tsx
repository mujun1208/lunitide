import React, { useCallback, useEffect, useMemo, useState } from 'react'
import {
  automationBridge,
  type AutomationBridge,
  type ProviderBridge,
  type SessionBridge,
  providerBridge,
  sessionBridge,
} from '../bridge/client'
import type { AutomationJobListResult, AutomationRunListResult, AutomationStatusResult } from '../generated/bridge'
import { AutomationCreateDialog, draftFromTemplate, type AutomationDraft } from './AutomationCreateDialog'
import { AUTOMATION_TEMPLATES, cronToHuman, type AutomationTemplate } from './automationTemplates'
import { ensureAutomationRunner, loadDefaultModel } from './ensureAutomationRunner'

type Job = AutomationJobListResult['jobs'][number]
type Run = AutomationRunListResult['runs'][number]
type Tab = 'jobs' | 'runs' | 'templates'

const EMPTY_DRAFT = (): AutomationDraft => ({
  name: '',
  cron: '0 9 * * *',
  prompt: '',
  providerId: '',
  modelId: '',
  sessionId: '',
  executionMode: 'auto-edit',
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
    return new Date(iso).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  } catch {
    return iso
  }
}

const STATE_LABEL: Record<string, string> = { running: '执行中', succeeded: '成功', failed: '失败' }

export function AutomationCenterPage({
  onCreateInChat,
  bridge = automationBridge,
  providers = providerBridge,
  sessions = sessionBridge,
}: {
  onCreateInChat: () => void
  bridge?: AutomationBridge
  providers?: ProviderBridge
  sessions?: SessionBridge
}): React.JSX.Element {
  const [tab, setTab] = useState<Tab>('jobs')
  const [jobs, setJobs] = useState<Job[]>([])
  const [runs, setRuns] = useState<Run[]>([])
  const [status, setStatus] = useState<AutomationStatusResult>()
  const [runnerSessionId, setRunnerSessionId] = useState('')
  const [defaults, setDefaults] = useState<{ providerId: string; modelId: string }>()
  const [draft, setDraft] = useState<AutomationDraft>(EMPTY_DRAFT())
  const [dialogOpen, setDialogOpen] = useState(false)
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [openRun, setOpenRun] = useState<string>()

  const reload = useCallback(async () => {
    const [j, r, s] = await Promise.all([bridge.listJobs(), bridge.listRuns({ limit: 40 }), bridge.status()])
    setJobs(j.jobs)
    setRuns(r.runs)
    setStatus(s)
  }, [bridge])

  useEffect(() => {
    let alive = true
    void (async () => {
      try {
        const [{ session }, model] = await Promise.all([ensureAutomationRunner(undefined, sessions), loadDefaultModel(providers)])
        if (!alive) return
        setRunnerSessionId(session.id)
        setDefaults(model)
        setDraft(d => ({ ...d, sessionId: session.id, providerId: model?.providerId ?? '', modelId: model?.modelId ?? '' }))
      } catch {
        /* surfaced when saving */
      }
      await reload()
    })()
    const timer = window.setInterval(() => void reload(), 30_000)
    return () => {
      alive = false
      window.clearInterval(timer)
    }
  }, [providers, reload, sessions])

  const openManual = () => {
    setDraft(d => ({ ...EMPTY_DRAFT(), sessionId: runnerSessionId || d.sessionId, providerId: defaults?.providerId ?? d.providerId, modelId: defaults?.modelId ?? d.modelId }))
    setNotice('')
    setDialogOpen(true)
  }

  const openTemplate = (template: AutomationTemplate) => {
    setDraft(
      draftFromTemplate(template, {
        sessionId: runnerSessionId,
        providerId: defaults?.providerId ?? '',
        modelId: defaults?.modelId ?? '',
        executionMode: 'auto-edit',
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
    if (busy) return
    setBusy(true)
    setNotice('')
    try {
      await bridge.setJob({
        id: draft.id,
        name: draft.name.trim(),
        cron: draft.cron.trim(),
        prompt: draft.prompt.trim(),
        providerId: draft.providerId as never,
        modelId: draft.modelId,
        sessionId: draft.sessionId as never,
        executionMode: draft.executionMode,
        webhookUrl: draft.webhookUrl.trim(),
        enabled: draft.enabled,
      })
      await reload()
      setDialogOpen(false)
      setTab('jobs')
      setNotice('任务已保存')
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  const trigger = async (job: Job) => {
    if (busy) return
    setBusy(true)
    setNotice('')
    try {
      await bridge.triggerJob({ id: job.id })
      setNotice(`已触发「${job.name}」`)
      setTimeout(() => void reload(), 800)
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '触发失败')
    } finally {
      setBusy(false)
    }
  }

  const toggle = async (job: Job) => {
    if (busy) return
    setBusy(true)
    try {
      await bridge.setJob({
        id: job.id,
        name: job.name,
        cron: job.cron,
        prompt: job.prompt,
        providerId: job.providerId,
        modelId: job.modelId,
        sessionId: job.sessionId,
        executionMode: (job.executionMode as AutomationDraft['executionMode']) || 'auto-edit',
        webhookUrl: job.webhookUrl ?? '',
        enabled: !job.enabled,
      })
      await reload()
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '更新失败')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (job: Job) => {
    if (busy) return
    setBusy(true)
    try {
      await bridge.deleteJob({ id: job.id })
      await reload()
      setNotice('任务已删除')
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '删除失败')
    } finally {
      setBusy(false)
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
          {status?.running ? '调度器运行中' : '调度器未启动'}
        </span>
      </nav>
      {tab === 'jobs' && (
        <section className="automation-center-jobs" aria-label="已配置任务">
          {jobs.length ? (
            <ul className="automation-jobs">
              {jobs.map(job => (
                <li key={job.id} className={`automation-job ${job.enabled ? '' : 'is-disabled'}`}>
                  <div className="automation-job-head">
                    <b>{job.name}</b>
                    <code>{cronToHuman(job.cron)}</code>
                    <span className="automation-job-mode">{MODE_LABEL[(job.executionMode as AutomationDraft['executionMode']) || 'auto-edit']}</span>
                  </div>
                  <div className="automation-job-meta">
                    <span>下次 {fmtTime(nextFire[job.id])}</span>
                    <span>上次 {fmtTime(job.lastRunAt)}</span>
                    {status?.runningJobs?.includes(job.id) && <span className="automation-job-running">正在执行…</span>}
                  </div>
                  <div className="automation-job-actions">
                    <button type="button" disabled={busy} onClick={() => void trigger(job)}>
                      立即运行
                    </button>
                    <button type="button" disabled={busy} onClick={() => void toggle(job)}>
                      {job.enabled ? '停用' : '启用'}
                    </button>
                    <button type="button" className="automation-delete" disabled={busy} onClick={() => void remove(job)}>
                      删除
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <p className="automation-empty">还没有定时任务。可以手动新建、从模板创建，或在对话中描述任务。</p>
          )}
        </section>
      )}
      {tab === 'runs' && (
        <section className="automation-center-runs" aria-label="执行历史">
          {runs.length ? (
            <ul className="automation-runs">
              {runs.map(run => (
                <li key={run.id} className={`automation-run is-${run.state}`}>
                  <button
                    type="button"
                    className="automation-run-row"
                    aria-expanded={openRun === run.id}
                    onClick={() => setOpenRun(openRun === run.id ? undefined : run.id)}
                  >
                    <span className={`automation-run-state is-${run.state}`}>{STATE_LABEL[run.state] ?? run.state}</span>
                    <b>{run.jobName}</b>
                    <small>
                      {run.trigger === 'manual' ? '手动' : '定时'} · {fmtTime(run.startedAt)}
                      {run.totalTokens ? ` · ${run.totalTokens} tok` : ''}
                    </small>
                  </button>
                  {openRun === run.id && (
                    <div className="automation-run-detail">
                      {run.state === 'failed' ? <p role="alert">{run.error}</p> : run.summary ? <pre>{run.summary}</pre> : <p>无摘要</p>}
                    </div>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p className="automation-empty">还没有运行记录。</p>
          )}
        </section>
      )}
      {tab === 'templates' && (
        <section className="automation-template-grid" aria-label="任务模板">
          {AUTOMATION_TEMPLATES.map(template => (
            <button type="button" key={template.id} className="automation-template-card" onClick={() => openTemplate(template)}>
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
      {notice && <p className="automation-notice" role="status">{notice}</p>}
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
