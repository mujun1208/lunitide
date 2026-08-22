import React, { useEffect, useState } from 'react'
import { Dialog } from '../ui/Dialog'
import { scheduleToCron, type AutomationTemplate } from './automationTemplates'

export type AutomationDraft = {
  id?: string
  name: string
  cron: string
  prompt: string
  providerId: string
  modelId: string
  sessionId: string
  executionMode: 'approval' | 'auto-edit' | 'full-access'
  webhookUrl: string
  enabled: boolean
}

type Props = {
  open: boolean
  draft: AutomationDraft
  busy?: boolean
  notice?: string
  templateLink?: boolean
  onClose: () => void
  onChange: (draft: AutomationDraft) => void
  onSubmit: () => void
  onPickTemplate?: () => void
}

const MODE_LABEL: Record<AutomationDraft['executionMode'], string> = {
  approval: '手动审批',
  'auto-edit': '自动审批',
  'full-access': '完全访问',
}

function cronParts(cron: string): { freq: 'daily' | 'weekdays' | 'weekly'; time: string } {
  const parts = cron.trim().split(/\s+/)
  if (parts.length < 5) return { freq: 'daily', time: '09:00' }
  const [min, hour, , , dow] = parts
  const time = `${String(Number(hour) || 9).padStart(2, '0')}:${String(Number(min) || 0).padStart(2, '0')}`
  if (dow === '1-5') return { freq: 'weekdays', time }
  if (dow === '1') return { freq: 'weekly', time }
  return { freq: 'daily', time }
}

export function AutomationCreateDialog({
  open,
  draft,
  busy,
  notice,
  templateLink,
  onClose,
  onChange,
  onSubmit,
  onPickTemplate,
}: Props): React.JSX.Element {
  const [freq, setFreq] = useState<'daily' | 'weekdays' | 'weekly'>('daily')
  const [time, setTime] = useState('09:00')

  useEffect(() => {
    if (!open) return
    const parsed = cronParts(draft.cron)
    setFreq(parsed.freq)
    setTime(parsed.time)
  }, [open, draft.cron])

  const updateSchedule = (nextFreq: typeof freq, nextTime: string) => {
    setFreq(nextFreq)
    setTime(nextTime)
    onChange({ ...draft, cron: scheduleToCron(nextFreq, nextTime) })
  }

  return (
    <Dialog
      open={open}
      title="新建自动化任务"
      description={templateLink ? '填写任务详情后保存到自动化清单' : undefined}
      onClose={() => {
        if (!busy) onClose()
      }}
    >
      {templateLink && onPickTemplate && (
        <button type="button" className="automation-template-link" onClick={onPickTemplate}>
          从模板创建
        </button>
      )}
      <form
        className="automation-create-form"
        onSubmit={e => {
          e.preventDefault()
          onSubmit()
        }}
      >
        <label>
          任务名称
          <input
            autoFocus
            aria-label="任务名称"
            value={draft.name}
            placeholder="请输入任务名称"
            onChange={e => onChange({ ...draft, name: e.target.value })}
          />
        </label>
        <div className="automation-create-schedule">
          <label>
            触发频率
            <select
              aria-label="触发频率"
              value={freq}
              onChange={e => updateSchedule(e.target.value as typeof freq, time)}
            >
              <option value="daily">每天</option>
              <option value="weekdays">工作日</option>
              <option value="weekly">每周一</option>
            </select>
          </label>
          <label>
            触发时间
            <input aria-label="触发时间" type="time" value={time} onChange={e => updateSchedule(freq, e.target.value)} />
          </label>
        </div>
        <label className="automation-create-prompt">
          你希望 Lunitide 做什么？
          <textarea
            aria-label="执行提示词"
            value={draft.prompt}
            placeholder="帮你整理理论综述、编写 PPT、分析 Excel 等日常工作，输出专业级工作成果。"
            onChange={e => onChange({ ...draft, prompt: e.target.value })}
          />
        </label>
        <label>
          执行模式
          <select
            aria-label="执行模式"
            value={draft.executionMode}
            onChange={e => onChange({ ...draft, executionMode: e.target.value as AutomationDraft['executionMode'] })}
          >
            {(Object.keys(MODE_LABEL) as AutomationDraft['executionMode'][]).map(mode => (
              <option key={mode} value={mode}>
                {MODE_LABEL[mode]}
              </option>
            ))}
          </select>
        </label>
        <label className="automation-enabled">
          <input
            type="checkbox"
            aria-label="启用任务"
            checked={draft.enabled}
            onChange={e => onChange({ ...draft, enabled: e.target.checked })}
          />
          创建后立即启用
        </label>
        {notice && <p className="automation-notice" role="status">{notice}</p>}
        <div className="dialog-actions">
          <button type="button" disabled={busy} onClick={onClose}>
            取消
          </button>
          <button className="primary" type="submit" disabled={busy || !draft.name.trim() || !draft.prompt.trim()}>
            {busy ? '创建中…' : '创建'}
          </button>
        </div>
      </form>
    </Dialog>
  )
}

export function draftFromTemplate(
  template: AutomationTemplate,
  base: Omit<AutomationDraft, 'name' | 'cron' | 'prompt'>,
): AutomationDraft {
  return {
    ...base,
    name: template.title,
    cron: template.cron,
    prompt: template.prompt,
  }
}
