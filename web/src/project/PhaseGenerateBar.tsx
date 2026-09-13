import React, { useState } from 'react'
import { UserAskWizard } from '../session/UserAskWizard'
import { COUNCIL_PROMPT, interviewPack, questionsForPhase } from './phaseInterview'
import { projectFactoryApi } from './projectFactoryApi'

export function PhaseGenerateBar({
  projectId,
  phase,
  expertCount = 0,
  onGenerated,
  onPrefillPrompt,
}: {
  projectId: string
  phase: number
  expertCount?: number
  onGenerated?: () => void
  onPrefillPrompt?: (text: string) => void
}): React.JSX.Element | null {
  const questions = questionsForPhase(phase)
  const [guide, setGuide] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  if (!questions.length) return null
  const pack = interviewPack(phase, questions)
  const councilReady = expertCount >= 2

  const saveGuide = async (followUp: string) => {
    const answers = parseGuideAnswers(followUp, phase)
    setBusy(true)
    setError('')
    try {
      await projectFactoryApi.interviewSave({
        projectId,
        phase,
        mode: 'guide',
        completedAt: new Date().toISOString(),
        answers,
      })
      setGuide(false)
      setNote('引导选择已保存，可生成本阶段交付物。')
    } catch (e) {
      setError(e instanceof Error ? e.message : '无法保存引导选择')
    } finally {
      setBusy(false)
    }
  }

  const generate = async () => {
    setBusy(true)
    setError('')
    try {
      const got = await projectFactoryApi.generate({ projectId, phase })
      setNote(`正在按模版或完整格式写入交付物，不会自动确认。已写入 ${got.generated} 份。`)
      onGenerated?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : '生成失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="phase-generate-bar" aria-label="生成本阶段交付物">
      <div className="checklist-actions">
        <button type="button" disabled={busy} onClick={() => setGuide(v => !v)}>引导选择</button>
        <button
          type="button"
          disabled={busy || !councilReady}
          title={councilReady ? '' : '请先在专家中心或输入框 @ 至少两名专家'}
          onClick={() => onPrefillPrompt?.(COUNCIL_PROMPT)}
        >
          专家讨论后生成
        </button>
        <button type="button" className="primary" disabled={busy} onClick={() => void generate()}>生成本阶段交付物</button>
      </div>
      <p className="gate-note">开发/技术规范已作为本项目固定规则注入后续阶段。改文档后请重新批准并物化。</p>
      {note && <p className="release-result" role="status">{note}</p>}
      {error && <p className="error" role="alert"><b>{error}</b></p>}
      {guide && (
        <UserAskWizard
          pack={pack}
          busy={busy}
          onSubmit={text => void saveGuide(text)}
        />
      )}
    </section>
  )
}

export function parseGuideAnswers(followUp: string, phase: number): Array<{ id: string; prompt: string; value: string }> {
  const lines = followUp.split('\n')
  return questionsForPhase(phase).map(q => {
    const line = lines.find(item => item.includes(q.prompt))
    const value = line?.includes('：') ? line.slice(line.indexOf('：') + 1).trim() : followUp
    return { id: q.id, prompt: q.prompt, value }
  })
}
