import React, { useState } from 'react'
import { ConfirmDialog } from '../ui/Dialog'
import { agentHubApi, type AgentHubName, type AgentHubStatus } from './agentHubApi'
import { agentDisplayName } from './agentHubCopy'

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
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const run = async () => {
    setBusy(true)
    setError('')
    try {
      const got = await agentHubApi.install({ name, confirmed: true })
      onDone(got.agents ?? [])
      if (!got.connected) {
        setError(got.hint || (zh ? '本机安装已跑完，但仍未连上。' : 'The local install finished, but it is still not connected.'))
        return
      }
      setOpen(false)
    } catch (err) {
      setError(err instanceof Error && /[\u4e00-\u9fff]/.test(err.message) ? err.message : (zh ? '安装没有完成。' : 'Install did not finish.'))
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <button type="button" className="agent-hub-install-primary" onClick={() => { setError(''); setOpen(true) }}>
        {label ?? (zh ? '安装并连接' : 'Install and connect')}
      </button>
      <ConfirmDialog
        open={open}
        danger={false}
        busy={busy}
        error={error}
        title={zh ? `安装并连接 ${agentDisplayName(name)}` : `Install and connect ${agentDisplayName(name)}`}
        description={zh ? '先检查本机是否已有该 CLI。没有就在本机自动安装，再登录并连接。不会打开网页。' : 'Check for the local CLI first. If it is missing, install it here, then sign in and connect. No webpage will open.'}
        confirmLabel={zh ? '确定安装' : 'Install'}
        onCancel={() => { if (!busy) setOpen(false) }}
        onConfirm={() => { void run() }}
      />
    </>
  )
}
