import React, { useEffect, useState } from 'react'
import {
  REPLY_STYLE_OPTIONS,
  STRUCTURED_TEMPLATE_OPTIONS,
  loadReplySettings,
  replyStyleLabel,
  saveReplySettings,
  structuredTemplateLabel,
  subscribeReplySettings,
  type ReplyStyle,
  type StructuredTemplate,
} from '../settings/replySettings'

export function ComposerReplyChips(): React.JSX.Element {
  const [settings, setSettings] = useState(loadReplySettings)
  const [open, setOpen] = useState<'style' | 'template' | null>(null)

  useEffect(() => subscribeReplySettings(() => setSettings(loadReplySettings())), [])

  const chooseStyle = (replyStyle: ReplyStyle) => {
    saveReplySettings({ replyStyle })
    setSettings(loadReplySettings())
    setOpen(null)
  }
  const chooseTemplate = (structuredTemplate: StructuredTemplate) => {
    saveReplySettings({ structuredTemplate })
    setSettings(loadReplySettings())
    setOpen(null)
  }

  return (
    <div className="composer-reply-chips">
      <div className="composer-popover-anchor">
        <button
          type="button"
          aria-label="说话风格"
          aria-expanded={open === 'style'}
          className={settings.replyStyle !== 'default' ? 'is-active' : ''}
          onClick={e => {
            e.stopPropagation()
            setOpen(open === 'style' ? null : 'style')
          }}
        >
          {replyStyleLabel(settings.replyStyle)}
        </button>
        {open === 'style' && (
          <div className="composer-popover" role="menu" onClick={e => e.stopPropagation()}>
            {REPLY_STYLE_OPTIONS.map(option => (
              <button
                type="button"
                role="menuitem"
                key={option.value}
                className={option.value === settings.replyStyle ? 'selected' : ''}
                onClick={() => chooseStyle(option.value)}
              >
                <b>{option.label}</b>
                <small>{option.desc}</small>
              </button>
            ))}
          </div>
        )}
      </div>
      <div className="composer-popover-anchor">
        <button
          type="button"
          aria-label="结构化输出"
          aria-expanded={open === 'template'}
          className={settings.structuredTemplate !== 'off' ? 'is-active' : ''}
          onClick={e => {
            e.stopPropagation()
            setOpen(open === 'template' ? null : 'template')
          }}
        >
          {structuredTemplateLabel(settings.structuredTemplate)}
        </button>
        {open === 'template' && (
          <div className="composer-popover" role="menu" onClick={e => e.stopPropagation()}>
            {STRUCTURED_TEMPLATE_OPTIONS.map(option => (
              <button
                type="button"
                role="menuitem"
                key={option.value}
                className={option.value === settings.structuredTemplate ? 'selected' : ''}
                onClick={() => chooseTemplate(option.value)}
              >
                <b>{option.label}</b>
                <small>{option.desc}</small>
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
