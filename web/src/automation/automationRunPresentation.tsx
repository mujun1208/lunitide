import React from 'react'
import type { AutomationRunListResult, AutomationStatusResult, SessionDTO } from '../generated/bridge'

type Run = AutomationRunListResult['runs'][number]

export function schedulerStatusLabel(status?: AutomationStatusResult): string {
  if (!status) return '正在读取调度状态'
  const count = status.runningJobs?.length ?? 0
  if (count > 0) return `正在执行 ${count} 个任务${status.running ? '' : ' · 定时调度未启动'}`
  return status.running ? '定时调度已开启 · 当前无任务执行' : '定时调度未启动'
}

export function automationRunLabel(run: Run): string {
  if (run.cancelled) return '已停止本次执行'
  if (run.state === 'running') return '执行中'
  if (run.state === 'failed') return run.outcomeUnknown ? '执行已停止 · 结果待核对' : '执行失败'
  return run.state === 'succeeded' ? '成功' : run.state
}

function failureMessage(error?: string): string {
  if (error?.includes('context deadline exceeded')) return '执行超时，任务已停止。可查看已生成内容，核对后再决定是否重新运行。'
  if (error?.includes('context canceled')) return '执行已中断，任务已停止。可查看已生成内容。'
  return error || '任务未完成，请查看执行对话了解详情。'
}

export function AutomationRunDetail({ run, onOpenSession }: { run: Run; onOpenSession?: (session: SessionDTO) => void }): React.JSX.Element {
  return <div className="automation-run-detail">
    {run.state === 'failed' && <p role="alert" title={run.error}>{failureMessage(run.error)}</p>}
    {run.state === 'running' && <p>任务正在执行，完成后会更新结果。</p>}
    {run.summary ? <>
      {run.state === 'failed' && <p>已生成内容（任务未完成）</p>}
      <pre>{run.summary}</pre>
    </> : run.state !== 'running' && <p>本次没有可显示的摘要。</p>}
    {run.finishedAt && <small>结束于 {new Date(run.finishedAt).toLocaleString('zh-CN')}</small>}
    {run.session && onOpenSession && <p><button type="button" onClick={() => onOpenSession(run.session!)}>查看完整对话与产物</button></p>}
  </div>
}
