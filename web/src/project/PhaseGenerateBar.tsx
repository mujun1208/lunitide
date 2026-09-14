import React, { useEffect, useState } from 'react'
import { UserAskWizard } from '../session/UserAskWizard'
import { pickPhaseAssetBindings, type PhaseAssetBinding } from './phaseAssets'
import {
  COUNCIL_PROMPT,
  INCOMPLETE_INTERVIEW_BANNER,
  answersFromInterview,
  interviewPack,
  parseGuideAnswers,
  phaseAnswersComplete,
  questionsForPhase,
} from './phaseInterview'
import { projectFactoryApi } from './projectFactoryApi'

export type GenerateBarDoc = { key: string; title: string }
export type GenerateBarItem = { documentType: string; templateId?: string }
export type GenerateBarTemplate = { id: string; name: string; updatedAt?: string }

export function PhaseGenerateBar({
  projectId,
  phase,
  expertCount = 0,
  docs = [],
  items = [],
  templatesByDoc,
  onGenerated,
  onPrefillPrompt,
  onBindTemplates,
}: {
  projectId: string
  phase: number
  expertCount?: number
  docs?: GenerateBarDoc[]
  items?: GenerateBarItem[]
  templatesByDoc?: Map<string, GenerateBarTemplate[]>
  onGenerated?: () => void
  onPrefillPrompt?: (text: string) => void
  onBindTemplates?: (bindings: PhaseAssetBinding[]) => Promise<void>
}): React.JSX.Element | null {
  const questions = questionsForPhase(phase)
  const [guide, setGuide] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')
  const [overwriteDrafts, setOverwriteDrafts] = useState(false)
  const [interviewIncomplete, setInterviewIncomplete] = useState(true)
  const [pendingAssets, setPendingAssets] = useState<PhaseAssetBinding[]>([])
  const pack = interviewPack(phase, questions)
  const councilReady = expertCount >= 2

  useEffect(() => {
    let cancelled = false
    void projectFactoryApi.interviewGet({ projectId }).then(got => {
      if (cancelled) return
      setInterviewIncomplete(!phaseAnswersComplete(phase, answersFromInterview(got.interview, phase)))
    }).catch(() => {
      if (!cancelled) setInterviewIncomplete(true)
    })
    return () => { cancelled = true }
  }, [projectId, phase])

  if (!questions.length) return null

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
      setInterviewIncomplete(!phaseAnswersComplete(phase, answers))
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
      const got = await projectFactoryApi.generate({ projectId, phase, overwriteDrafts })
      setNote(`正在按模版或完整格式写入交付物，不会自动确认。已写入 ${got.generated} 份。`)
      onGenerated?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : '生成失败')
    } finally {
      setBusy(false)
    }
  }

  const previewAssets = () => {
    const bindings = pickPhaseAssetBindings(docs, items, templatesByDoc ?? new Map())
    if (!bindings.length) {
      setPendingAssets([])
      setNote('没有可套用的资产模块。')
      return
    }
    setPendingAssets(bindings)
    setNote(`将套用：${bindings.map(item => item.name).join('、')}。确认后只写入模版引用，不会确认交付物。`)
  }

  const confirmAssets = async () => {
    if (!pendingAssets.length || !onBindTemplates) return
    setBusy(true)
    setError('')
    try {
      await onBindTemplates(pendingAssets)
      setPendingAssets([])
      setNote('已套用本阶段资产模块，尚未确认交付物。')
    } catch (e) {
      setError(e instanceof Error ? e.message : '套用资产模块失败')
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
        <button type="button" disabled={busy} onClick={previewAssets}>套用资产模块</button>
        <button type="button" className="primary" disabled={busy} onClick={() => void generate()}>生成本阶段交付物</button>
      </div>
      <label className="gate-note">
        <input type="checkbox" checked={overwriteDrafts} disabled={busy} onChange={e => setOverwriteDrafts(e.target.checked)} />
        覆盖草稿
      </label>
      <p className="gate-note">开发/技术规范已作为本项目固定规则注入后续阶段。改文档后请重新批准并物化。</p>
      {interviewIncomplete && <p className="gate-note" role="status">{INCOMPLETE_INTERVIEW_BANNER}</p>}
      {pendingAssets.length > 0 && (
        <div className="gate-note">
          <ul>{pendingAssets.map(item => <li key={item.key}>{item.title} → {item.name}</li>)}</ul>
          <button type="button" disabled={busy} onClick={() => void confirmAssets()}>确认套用</button>
        </div>
      )}
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

export { parseGuideAnswers }
