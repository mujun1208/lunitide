import { useState } from 'react'

export interface CompanionAskProps {
  language: 'zh' | 'en'
  onSend: (text: string) => void
}

export function CompanionAsk({ language, onSend }: CompanionAskProps): React.JSX.Element {
  const [value, setValue] = useState('')
  const trimmed = value.trim()
  const zh = language === 'zh'
  const submit = () => {
    if (!trimmed) return
    setValue('')
    onSend(trimmed)
  }
  return (
    <form
      className="companion-ask"
      onSubmit={event => {
        event.preventDefault()
        submit()
      }}
    >
      <input
        type="text"
        value={value}
        onChange={event => setValue(event.target.value)}
        placeholder={zh ? '问问月汐…' : 'Ask 月汐…'}
        aria-label={zh ? '输入想对月汐说的话' : 'Type a message for the companion'}
        autoComplete="off"
        enterKeyHint="send"
      />
      <button type="submit" disabled={!trimmed} aria-label={zh ? '发送给月汐' : 'Send to companion'}>
        {zh ? '发送' : 'Send'}
      </button>
    </form>
  )
}
