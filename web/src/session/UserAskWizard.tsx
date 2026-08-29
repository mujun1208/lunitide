import React, {useMemo, useState} from 'react'
import {
  formatUserAskFollowUp,
  USER_ASK_OTHER_ID,
  userAskChoiceReady,
  type UserAskAnswers,
  type UserAskChoice,
  type UserAskPack,
} from './userAsk'

export function UserAskWizard({
  pack,
  busy = false,
  onSubmit,
}: {
  pack: UserAskPack
  busy?: boolean
  onSubmit: (followUp: string) => void
}): React.JSX.Element {
  const [step, setStep] = useState(0)
  const [answers, setAnswers] = useState<UserAskAnswers>({})
  const question = pack.questions[Math.min(step, pack.questions.length - 1)]
  const choice = answers[question.id]
  const last = step === pack.questions.length - 1
  const ready = userAskChoiceReady(question, choice)
  const optionIndex = (id: string) => question.options.findIndex(option => option.id === id)

  const select = (next: UserAskChoice) => {
    setAnswers(current => ({...current, [question.id]: next}))
  }

  const submit = () => {
    if (!last || !ready || busy) return
    onSubmit(formatUserAskFollowUp(pack, answers))
  }

  const progress = useMemo(() => `问题 ${step + 1} / ${pack.questions.length}`, [step, pack.questions.length])

  return (
    <form
      className="user-ask-wizard"
      aria-label={pack.title || '需要你决策'}
      onSubmit={e => {
        e.preventDefault()
        if (last) submit()
        else if (ready) setStep(value => Math.min(pack.questions.length - 1, value + 1))
      }}
    >
      <header>
        <b>{pack.title || '需要你决策'}</b>
        <small>{progress}</small>
      </header>
      <p>{question.prompt}</p>
      <div className="user-ask-options" role="radiogroup" aria-label={question.prompt}>
        {question.options.map((option, index) => (
          <button
            type="button"
            role="radio"
            aria-checked={choice?.optionId === option.id}
            className={choice?.optionId === option.id ? 'on' : undefined}
            key={option.id}
            disabled={busy}
            onClick={() => select({optionId: option.id})}
          >
            <i>{index + 1}</i>
            <span>{option.label}</span>
          </button>
        ))}
        <button
          type="button"
          role="radio"
          aria-checked={choice?.optionId === USER_ASK_OTHER_ID}
          className={choice?.optionId === USER_ASK_OTHER_ID ? 'on' : undefined}
          disabled={busy}
          onClick={() => select({optionId: USER_ASK_OTHER_ID, otherText: choice?.optionId === USER_ASK_OTHER_ID ? choice.otherText : ''})}
        >
          <i>{question.options.length + 1}</i>
          <span>其他（自己填）</span>
        </button>
      </div>
      {choice?.optionId === USER_ASK_OTHER_ID && (
        <label>
          补充说明
          <input
            value={choice.otherText ?? ''}
            maxLength={500}
            disabled={busy}
            placeholder="请填写你的选择…"
            aria-label="其他选项说明"
            onChange={e => select({optionId: USER_ASK_OTHER_ID, otherText: e.target.value})}
          />
        </label>
      )}
      <div className="user-ask-actions">
        <button type="button" disabled={busy || step === 0} onClick={() => setStep(value => Math.max(0, value - 1))}>
          上一步
        </button>
        {last ? (
          <button type="submit" className="primary" disabled={busy || !ready}>
            {busy ? '提交中…' : '提交决策'}
          </button>
        ) : (
          <button type="submit" className="primary" disabled={busy || !ready}>
            下一题
          </button>
        )}
      </div>
      {choice?.optionId && choice.optionId !== USER_ASK_OTHER_ID && (
        <span className="sr-only">{`已选 ${optionIndex(choice.optionId) + 1}`}</span>
      )}
    </form>
  )
}
