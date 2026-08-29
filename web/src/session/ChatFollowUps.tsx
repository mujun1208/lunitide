import React from 'react'
import { filterChatDeliverables, type ChatArtifact } from './ChatArtifactCards'

const MAX_CHIPS = 3
const MAX_CHIP_CHARS = 72

function addUnique(out: string[], seen: Set<string>, raw: string): void {
  const text = raw.replace(/\s+/g, ' ').trim().replace(/[。．.]+$/, '')
  if (!text || text.length < 4 || text.length > MAX_CHIP_CHARS || seen.has(text)) return
  seen.add(text)
  out.push(text)
}

function kindPrompts(artifacts: readonly ChatArtifact[]): string[] {
  const kinds = new Set(filterChatDeliverables(artifacts).map(item => item.kind))
  if (kinds.has('pptx')) return ['把这一版再精简一页', '加一页联系方式', '改成深色主题']
  if (kinds.has('docx')) return ['把摘要再压缩一版', '补一节下一步建议']
  if (kinds.has('xlsx')) return ['加一列汇总', '按重点再筛一版']
  if (kinds.has('pdf') || kinds.has('html')) return ['再出一版更短的']
  return []
}

/** Turn the last assistant reply into a few clickable next-step chips. */
export function suggestChatFollowUps(text: string, artifacts: readonly ChatArtifact[] = []): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  const plain = text.replace(/```[\s\S]*?```/g, ' ').replace(/[#*_`]/g, ' ')
  const questions = [...plain.matchAll(/([^。！？\n]{6,72}[？?])/g)].map(match => match[1].trim())
  for (const question of questions.slice(-2)) addUnique(out, seen, question)
  const choice = plain.match(/还是.{2,24}还是.{2,24}/)
  if (choice) addUnique(out, seen, choice[0])
  for (const prompt of kindPrompts(artifacts)) {
    if (out.length >= MAX_CHIPS) break
    addUnique(out, seen, prompt)
  }
  if (out.length === 0 && plain.trim()) {
    addUnique(out, seen, '按这个方案继续')
    addUnique(out, seen, '换个角度再讲一遍')
  }
  return out.slice(0, MAX_CHIPS)
}

export function ChatFollowUps({
  text,
  artifacts = [],
  disabled,
  onSelect,
}: {
  text: string
  artifacts?: readonly ChatArtifact[]
  disabled?: boolean
  onSelect: (prompt: string) => void
}): React.JSX.Element | null {
  const chips = suggestChatFollowUps(text, artifacts)
  if (!chips.length) return null
  return (
    <div className="chat-follow-ups" role="group" aria-label="下一步建议">
      {chips.map(chip => (
        <button
          type="button"
          key={chip}
          className="chat-follow-up"
          disabled={disabled}
          onClick={() => onSelect(chip)}
        >
          {chip}
          <span aria-hidden="true"> →</span>
        </button>
      ))}
    </div>
  )
}
