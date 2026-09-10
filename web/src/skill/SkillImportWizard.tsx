import React, { useEffect, useRef, useState } from 'react'
import { asUserBridgeError } from '../bridge/bridgeUserError'
import { BridgeClientError, createMutationAttempt, skillImportBridge as defaultSkillImportBridge, type SkillImportBridge, type MutationAttempt, type MutationMethod } from '../bridge/client'
import type { SkillImportDiscoverResult } from '../generated/bridge'
import { Dialog } from '../ui/Dialog'

function skillImportUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}
const problem = (e: unknown) => e instanceof BridgeClientError ? asUserBridgeError(e, '请求失败') : new BridgeClientError(skillImportUserError(e, '请求失败'), 'CLIENT_ERROR', false, 'renderer')
type Props = { open: boolean; onClose: () => void; onApproved?: (skillId?: string) => void; bridge?: SkillImportBridge; initialUrl?: string }

export function SkillImportWizard({ open, onClose, onApproved, bridge = defaultSkillImportBridge, initialUrl = '' }: Props): React.JSX.Element {
  const [step, setStep] = useState<1 | 2 | 3 | 4 | 5>(1)
  const [sourceUrl, setSourceUrl] = useState(initialUrl)
  const [commit, setCommit] = useState('')
  const [candidate, setCandidate] = useState<SkillImportDiscoverResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const attempts = useRef(new Map<MutationMethod, MutationAttempt<object>>())
  const attemptFor = <T extends object,>(method: MutationMethod, payload: T): MutationAttempt<T> => {
    const previous = attempts.current.get(method)
    if (previous && JSON.stringify(previous.payload) === JSON.stringify(payload)) return previous as MutationAttempt<T>
    const attempt = createMutationAttempt(method, payload)
    attempts.current.set(method, attempt)
    return attempt
  }

  useEffect(() => { if (open) setSourceUrl(initialUrl) }, [open, initialUrl])
  const close = () => {
    if (busy) return
    attempts.current.clear()
    setStep(1); setSourceUrl(initialUrl); setCommit(''); setCandidate(null); setError(''); onClose()
  }
  const perform = async (action: () => Promise<void>) => {
    if (busy) return
    setBusy(true); setError('')
    try { await action() } catch (e) { setError(problem(e).message) } finally { setBusy(false) }
  }
  const discover = () => perform(async () => {
    const payload = { assetType: 'skill' as const, sourceUrl: sourceUrl.trim(), immutableCommit: commit.trim() }
    const result = await bridge.discover(payload, { attempt: attemptFor('skill.import.discover', payload) })
    setCandidate(result)
    // Reopening an unfinished import resumes the committed step.
    setStep(result.state === 'approved' ? 5 : result.state === 'awaiting_approval' ? 4 : result.state === 'inspected' ? 3 : 2)
    if (result.state === 'approved') onApproved?.(result.skillId)
  })
  const inspect = () => perform(async () => {
    if (!candidate) return
    const payload = { candidateId: candidate.candidateId, expectedVersion: candidate.version }
    const result = await bridge.inspect(payload, { attempt: attemptFor('skill.import.inspect', payload) })
    setCandidate({ ...candidate, ...result }); setStep(3)
  })
  const submit = () => perform(async () => {
    if (!candidate) return
    const payload = { candidateId: candidate.candidateId, expectedVersion: candidate.version }
    const result = await bridge.submit(payload, { attempt: attemptFor('skill.import.submit', payload) })
    setCandidate({ ...candidate, ...result }); setStep(4)
  })
  const approve = () => perform(async () => {
    if (!candidate) return
    const payload = { candidateId: candidate.candidateId, expectedVersion: candidate.version, approval: { source: 'github-import', scope: 'instructions-only-draft' } }
    const result = await bridge.approve(payload, { attempt: attemptFor('skill.import.approve', payload) })
    setCandidate({ ...candidate, ...result }); setStep(5); onApproved?.(result.skillId)
  })

  return <Dialog open={open} title="从 GitHub 导入技能" description="读取固定提交 → 校验文件 → 静态检查 → 导入草稿" onClose={close} wide>
    <div className="skill-import-wizard">
      <p className="gate-note">支持公开仓库中的标准 SKILL.md（含 name、description 和正文）。只导入说明正文，不安装或执行仓库脚本；导入后先保存为只读权限的草稿。</p>
      {step === 1 && <>
        <label>GitHub 仓库或技能目录 URL<input value={sourceUrl} onChange={e => setSourceUrl(e.target.value)} placeholder="https://github.com/org/repo" /></label>
        <label>固定提交 SHA<input value={commit} onChange={e => setCommit(e.target.value)} placeholder="40 位小写提交 SHA" /></label>
        <p className="gate-note">子目录使用 /tree/同一提交SHA/目录。归档最多 8 MiB，解压内容最多 32 MiB，SKILL.md 最多 48 KiB。未完成的导入可用相同地址和提交继续。</p>
        <div className="dialog-actions"><button disabled={busy} onClick={close}>取消</button><button className="primary" disabled={busy || !sourceUrl.trim() || !/^[0-9a-f]{40}$/.test(commit.trim())} onClick={() => void discover()}>{busy ? '读取中…' : '读取技能'}</button></div>
      </>}
      {candidate?.summary && <div className="gate-note">
        <b>{candidate.summary.name}</b><p>{candidate.summary.description}</p>
        <p>许可证：{candidate.summary.license === 'unknown' ? '未知（仓库未提供可识别的许可证文件）' : candidate.summary.license}</p>
        <p>未导入的其他文件：{candidate.summary.skippedFiles} 个</p>
        <details><summary>源文件校验值</summary><code>{candidate.summary.archiveHash}</code></details>
      </div>}
      {step === 2 && <div className="dialog-actions"><button disabled={busy} onClick={close}>稍后继续</button><button className="primary" disabled={busy} onClick={() => void inspect()}>{busy ? '校验中…' : '校验固定文件'}</button></div>}
      {step === 3 && <><p className="gate-note">固定文件校验通过。下一步检查正文中的已知指令覆盖和权限绕过标记。</p><div className="dialog-actions"><button disabled={busy} onClick={close}>稍后继续</button><button className="primary" disabled={busy} onClick={() => void submit()}>{busy ? '检查中…' : '运行静态检查'}</button></div></>}
      {step === 4 && <><p className="gate-note">静态规则检查未发现已知标记。未执行脚本，也未验证技能效果；批准后请在技能中心审阅草稿，再决定是否启用。</p><div className="dialog-actions"><button disabled={busy} onClick={close}>稍后继续</button><button className="primary" disabled={busy} onClick={() => void approve()}>{busy ? '导入中…' : '批准导入草稿'}</button></div></>}
      {step === 5 && <><p className="gate-note">技能已导入为草稿。返回技能库即可查看目录与文件、在对话中试用，再决定是否安装并正式发布。</p><div className="dialog-actions"><button className="primary" onClick={close}>完成</button></div></>}
      {error && <p className="error" role="alert"><b>{error}</b></p>}
    </div>
  </Dialog>
}
