import React from 'react'
import { filterChatDeliverables, type ChatArtifact } from './ChatArtifactCards'
import { splitChatSuggestions } from './chatSuggestions'

const MAX_CHIPS = 2
const MAX_CHIP_CHARS = 72

function addUnique(out: string[], seen: Set<string>, raw: string): void {
  const text = raw.replace(/\s+/g, ' ').trim().replace(/[。．.]+$/, '')
  if (!text || text.length < 4 || text.length > MAX_CHIP_CHARS || seen.has(text)) return
  seen.add(text)
  out.push(text)
}

function kindPrompts(artifacts: readonly ChatArtifact[]): string[] {
  const latest = filterChatDeliverables(artifacts).at(-1)
  if (!latest) return []
  const name = latest.path.split(/[/\\]/).pop() ?? latest.path
  const target = `「${Array.from(name).slice(0, 32).join('')}」`
  if (latest.kind === 'xlsx') return [`检查${target}的计算、缺失值和异常数据，列出具体问题`, `依据${target}提炼关键变化，注明对应数据与分析局限`]
  if (latest.kind === 'image') return [`检查${target}是否符合本轮要求，指出可见差异`, `结合${target}中可见的信息，说明接下来可以怎么做`]
  return [`对照本轮要求复核${target}，列出遗漏和待核实的内容`, `整理${target}的关键结论、适用条件和下一步行动`]
}

/** Turn the last assistant reply into a few clickable next-step chips. */
export function suggestChatFollowUps(text: string, artifacts: readonly ChatArtifact[] = []): string[] {
  const structured = splitChatSuggestions(text).suggestions
  if (structured.length === MAX_CHIPS) return structured
  const out: string[] = []
  const seen = new Set<string>()
  for (const prompt of kindPrompts(artifacts)) {
    if (out.length >= MAX_CHIPS) break
    addUnique(out, seen, prompt)
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
