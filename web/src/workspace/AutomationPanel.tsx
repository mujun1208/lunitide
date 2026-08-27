import React, { useCallback, useEffect, useState } from 'react'
import { automationBridge, type AutomationBridge } from '../bridge/client'
import type { AutomationJobListResult, AutomationRunListResult, AutomationStatusResult } from '../generated/bridge'
import { cronToHuman, delayAtCron } from '../automation/automationTemplates'

type Job = AutomationJobListResult['jobs'][number]
type Run = AutomationRunListResult['runs'][number]
type Draft = {
  id?: string
  name: string
  cron: string
  prompt: string
  providerId: string
  modelId: string
  sessionId: string
  executionMode: 'approval' | 'auto-edit' | 'full-access'
  sessionMode: 'bound' | 'isolated'
  runOnce: boolean
  webhookUrl: string
  enabled: boolean
}

const EMPTY_DRAFT: Draft = {
  name: '',
  cron: '30 8 * * *',
  prompt: '',
  providerId: '',
  modelId: '',
  sessionId: '',
  executionMode: 'auto-edit',
  sessionMode: 'bound',
  runOnce: false,
  webhookUrl: '',
  enabled: true,
}

const MODE_LABEL: Record<Draft['executionMode'], string> = {
  approval: '手动审批',
  'auto-edit': '自动审批',
  'full-access': '完全访问',
}

const fmtTime = (iso?: string) => {
  if (!iso) return '—'
  try {
    const d = new Date(iso)
    return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  } catch {
    return iso
  }
}

const STATE_LABEL: Record<string, string> = { running: '执行中', succeeded: '成功', failed: '失败' }

function jobPayload(draft: Draft, enabled: boolean) {
  return {
    id: draft.id,
    name: draft.name.trim(),
    cron: draft.cron.trim(),
    prompt: draft.prompt.trim(),
    providerId: draft.providerId as never,
    modelId: draft.modelId,
    sessionId: draft.sessionId as never,
    executionMode: draft.executionMode,
    sessionMode: draft.sessionMode,
    runOnce: draft.runOnce || draft.cron.startsWith('at:'),
    webhookUrl: draft.webhookUrl.trim(),
    enabled,
  }
}

export function AutomationPanel({
  sessionId = '',
  providerId = '',
  modelId = '',
  bridge = automationBridge,
  executionMode = 'auto-edit',
  mode = 'full',
}: {
  sessionId?: string
  providerId?: string
  modelId?: string
  bridge?: AutomationBridge
  executionMode?: Draft['executionMode']
  mode?: 'full' | 'runs'
}): React.JSX.Element {
  const [jobs, setJobs] = useState<Job[]>([])
  const [runs, setRuns] = useState<Run[]>([])
  const [status, setStatus] = useState<AutomationStatusResult | undefined>()
  const [draft, setDraft] = useState<Draft>(() => ({ ...EMPTY_DRAFT }))
  const [editing, setEditing] = useState(false)
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [openRun, setOpenRun] = useState<string>()

  const reload = useCallback(async () => {
    try {
      const [j, r, s] = await Promise.all([bridge.listJobs(), bridge.listRuns({ limit: 30 }), bridge.status()])
      setJobs(j.jobs)
      setRuns(r.runs)
      setStatus(s)
    } catch {
      /* keep last snapshot */
    }
  }, [bridge])

  useEffect(() => {
    let active = true
    void reload().then(() => {
      if (active) void 0
    })
    const timer = window.setInterval(() => {
      void reload()
    }, 30_000)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [reload])

  useEffect(() => {
    setDraft(d => ({
      ...d,
      sessionId,
      providerId: d.providerId || providerId,
      modelId: d.modelId || modelId,
      executionMode,
    }))
  }, [sessionId, providerId, modelId, executionMode])

  const save = async () => {
    if (!draft.name.trim()) {
      setNotice('请填写任务名称')
      return
    }
    if (!draft.prompt.trim()) {
      setNotice('请填写执行提示词')
      return
    }
    if (!draft.providerId || !draft.modelId || !draft.sessionId) {
      setNotice('缺少模型或会话参数，请先在会话中发起一次对话')
      return
    }
    const hook = draft.webhookUrl.trim()
    if (hook && !/^https:\/\//i.test(hook)) {
      setNotice('IM 通知地址需为 https 链接（飞书/企业微信/钉钉自定义机器人）')
      return
    }
    if (busy) return
    setBusy(true)
    setNotice('')
    try {
      await bridge.setJob(jobPayload({ ...draft, webhookUrl: hook }, draft.enabled))
      await reload()
      setDraft({ ...EMPTY_DRAFT, sessionId, providerId, modelId, executionMode })
      setEditing(false)
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
      setNotice(`已触发「${job.name}」，完成后将弹出系统通知`)
      setTimeout(() => {
        void reload()
      }, 800)
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
      await bridge.setJob(
        jobPayload(
          {
            id: job.id,
            name: job.name,
            cron: job.cron,
            prompt: job.prompt,
            providerId: job.providerId,
            modelId: job.modelId,
            sessionId: job.sessionId,
            executionMode: (job.executionMode as Draft['executionMode']) || 'auto-edit',
            sessionMode: job.sessionMode === 'isolated' ? 'isolated' : 'bound',
            runOnce: !!job.runOnce || job.cron.startsWith('at:'),
            webhookUrl: job.webhookUrl ?? '',
            enabled: job.enabled,
          },
          !job.enabled,
        ),
      )
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

  const startEdit = (job: Job) => {
    setDraft({
      id: job.id,
      name: job.name,
      cron: job.cron,
      prompt: job.prompt,
      providerId: job.providerId,
      modelId: job.modelId,
      sessionId: job.sessionId,
      executionMode: (job.executionMode as Draft['executionMode']) || 'auto-edit',
      sessionMode: job.sessionMode === 'isolated' ? 'isolated' : 'bound',
      runOnce: !!job.runOnce || job.cron.startsWith('at:'),
      webhookUrl: job.webhookUrl ?? '',
      enabled: job.enabled,
    })
    setEditing(true)
  }

  const runsOnly = mode === 'runs'
  return (
    <section className="automation-panel" aria-label={runsOnly ? '运行中心' : '自动化任务'}>
      <div className="automation-head">
        <h3>{runsOnly ? '运行中心' : '自动化任务'}</h3>
        <span className={status?.running ? 'automation-heartbeat is-live' : 'automation-heartbeat'} role="status">
          {status?.running ? '调度器运行中' : '调度器未启动'}
        </span>
        {!runsOnly && (
          <button
            type="button"
            className="automation-new"
            onClick={() => {
              setDraft({ ...EMPTY_DRAFT, sessionId, providerId, modelId, executionMode })
              setEditing(true)
            }}
          >
            新建任务
          </button>
        )}
      </div>
      {!runsOnly && editing && (
        <div className="automation-editor" role="form" aria-label="任务编辑">
          <div className="automation-editor-row">
            <label>
              名称
              <input aria-label="任务名称" value={draft.name} onChange={e => setDraft({ ...draft, name: e.target.value })} placeholder="每日站会摘要" />
            </label>
            <label>
              cron（分 时 日 月 周，或 at:时刻）
              <input aria-label="cron 表达式" value={draft.cron} onChange={e => setDraft({ ...draft, cron: e.target.value })} placeholder="30 8 * * 1-5" />
            </label>
            <label>
              执行模式
              <select aria-label="执行模式" value={draft.executionMode} onChange={e => setDraft({ ...draft, executionMode: e.target.value as Draft['executionMode'] })}>
                {(Object.keys(MODE_LABEL) as Draft['executionMode'][]).map(m => (
                  <option key={m} value={m}>
                    {MODE_LABEL[m]}
                  </option>
                ))}
              </select>
            </label>
            <label>
              会话
              <select aria-label="会话模式" value={draft.sessionMode} onChange={e => setDraft({ ...draft, sessionMode: e.target.value as Draft['sessionMode'] })}>
                <option value="bound">绑定当前会话</option>
                <option value="isolated">独立会话</option>
              </select>
            </label>
            <label className="automation-enabled">
              <input type="checkbox" aria-label="仅运行一次" checked={draft.runOnce || draft.cron.startsWith('at:')} onChange={e => setDraft({ ...draft, runOnce: e.target.checked })} />
              仅运行一次
            </label>
            <label className="automation-enabled">
              <input type="checkbox" aria-label="启用任务" checked={draft.enabled} onChange={e => setDraft({ ...draft, enabled: e.target.checked })} />
              启用
            </label>
          </div>
          <div className="automation-editor-actions">
            <button
              type="button"
              onClick={() => setDraft({ ...draft, cron: delayAtCron(20), runOnce: true })}
            >
              20 分钟后
            </button>
            <span className="automation-notice">{cronToHuman(draft.cron)}</span>
          </div>
          <label className="automation-editor-prompt">
            提示词（无头执行，发送到会话）
            <textarea aria-label="执行提示词" value={draft.prompt} onChange={e => setDraft({ ...draft, prompt: e.target.value })} placeholder="汇总昨天会话里的待办并生成今日站会摘要" />
          </label>
          <label className="automation-editor-webhook">
            IM 通知（可选，飞书/企业微信/钉钉自定义机器人的 https 地址，完成后推送结果）
            <input aria-label="IM 通知 webhook 地址" value={draft.webhookUrl} onChange={e => setDraft({ ...draft, webhookUrl: e.target.value })} placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/…" />
          </label>
          <div className="automation-editor-actions">
            <button type="button" disabled={busy} onClick={() => void save()}>
              保存
            </button>
            <button
              type="button"
              onClick={() => {
                setEditing(false)
                setNotice('')
              }}
            >
              取消
            </button>
          </div>
        </div>
      )}
      {!runsOnly &&
        (jobs.length ? (
          <ul className="automation-jobs">
            {jobs.map(job => {
              const nextMap = (status?.nextFire ?? {}) as Record<string, string | undefined>
              const next = nextMap[job.id]
              return (
                <li key={job.id} className={`automation-job ${job.enabled ? '' : 'is-disabled'}`}>
                  <div className="automation-job-head">
                    <b>{job.name}</b>
                    <code>{job.cron}</code>
                    <span className="automation-job-mode">{MODE_LABEL[(job.executionMode as Draft['executionMode']) || 'auto-edit']}</span>
                    {job.sessionMode === 'isolated' && <span className="automation-job-mode">独立</span>}
                    {(job.runOnce || job.cron.startsWith('at:')) && <span className="automation-job-mode">一次</span>}
                    {job.webhookUrl && (
                      <span className="automation-job-webhook" title={job.webhookUrl}>
                        IM 通知
                      </span>
                    )}
                  </div>
                  <div className="automation-job-meta">
                    <span>下次 {fmtTime(next)}</span>
                    <span>上次 {fmtTime(job.lastRunAt)}</span>
                    {status?.runningJobs?.includes(job.id) && (
                      <span className="automation-job-running" role="status">
                        正在执行…
                      </span>
                    )}
                  </div>
                  <div className="automation-job-actions">
                    <button type="button" disabled={busy} onClick={() => void trigger(job)}>
                      立即运行
                    </button>
                    <button type="button" disabled={busy} onClick={() => void toggle(job)}>
                      {job.enabled ? '停用' : '启用'}
                    </button>
                    <button type="button" disabled={busy} onClick={() => startEdit(job)}>
                      编辑
                    </button>
                    <button type="button" className="automation-delete" disabled={busy} onClick={() => void remove(job)}>
                      删除
                    </button>
                  </div>
                </li>
              )
            })}
          </ul>
        ) : (
          !editing && <p className="automation-empty">还没有定时任务。新建后，Lunitide 会在后台按时无头执行并发系统通知。</p>
        ))}
      {runs.length > 0 ? (
        <div className="automation-runs">
          <h4>运行历史</h4>
          <ul>
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
        </div>
      ) : (
        runsOnly && <p className="automation-empty">还没有运行记录。任务触发后会出现在这里。</p>
      )}
      {notice && (
        <p className="automation-notice" role="status">
          {notice}
        </p>
      )}
    </section>
  )
}
