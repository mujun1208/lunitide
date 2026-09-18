import React, { useEffect, useState } from 'react'
import { ConfirmDialog } from '../ui/Dialog'
import { agentHubApi, type AgentHubName, type AgentHubStatus } from './agentHubApi'
import { agentDisplayName } from './agentHubCopy'
import { clearInstallJob, getInstallJob, startAgentInstall, subscribeInstallJobs } from './agentHubInstallStore'

export function AgentHubInstallActions({
  name,
  zh,
  label,
  onDone,
}: {
  name: AgentHubName
  zh: boolean
  label?: string
  onDone: (agents: AgentHubStatus[]) => void
}): React.JSX.Element {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(() => getInstallJob(name)?.status === 'running')
  const [error, setError] = useState('')
  useEffect(() => subscribeInstallJobs(() => {
    const job = getInstallJob(name)
    setBusy(job?.status === 'running')
    if (job?.status === 'done') {
      onDone(job.agents ?? [])
      setOpen(false)
      clearInstallJob(name)
    } else if (job?.status === 'error') {
      setError(job.hint || job.error || (zh ? '安装没有完成。' : 'Install did not finish.'))
      if (job.agents) onDone(job.agents)
    }
  }), [name, onDone, zh])
  const run = async () => {
    setError('')
    setBusy(true)
    const job = await startAgentInstall(name)
    if (job.agents) onDone(job.agents)
    if (job.status === 'done') {
      setOpen(false)
      clearInstallJob(name)
      return
    }
    setError(job.hint || job.error || (zh ? '安装没有完成。' : 'Install did not finish.'))
    setBusy(false)
  }
  return (
    <>
      <button type="button" className="agent-hub-install-primary" onClick={() => { setError(''); setOpen(true) }}>
        {busy ? (zh ? '安装中…' : 'Installing…') : (label ?? (zh ? '安装并连接' : 'Install and connect'))}
      </button>
      <ConfirmDialog
        open={open || busy}
        danger={false}
        busy={busy}
        error={error}
        title={zh ? `安装并连接 ${agentDisplayName(name)}` : `Install and connect ${agentDisplayName(name)}`}
        description={zh
          ? '先检查本机是否已有该 CLI。没有就在本机自动安装，再登录并连接。切换页面不会中断安装。'
          : 'Check for the local CLI first. If missing, install here, then sign in. Leaving this page will not stop the install.'}
        confirmLabel={busy ? (zh ? '处理中…' : 'Working…') : (zh ? '确定安装' : 'Install')}
        onCancel={() => { if (!busy) setOpen(false) }}
        onConfirm={() => { if (!busy) void run() }}
      />
    </>
  )
}
