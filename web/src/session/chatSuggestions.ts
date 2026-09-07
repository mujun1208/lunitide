/** The optional final section is generated with the answer, without a second model request. */
export function splitChatSuggestions(text: string): { body: string; suggestions: string[] } {
  const match = /(?:^|\n)### (?:下一步建议|Suggested next steps)\s*\n([\s\S]*)$/i.exec(text)
  if (!match) return { body: text, suggestions: [] }
  // A heading inside a code block is source content, never UI metadata.
  const prefix = text.slice(0, match.index)
  if ((prefix.match(/^\s*```/gm)?.length ?? 0) % 2 !== 0) return { body: text, suggestions: [] }
  const lines = match[1].trim().split(/\r?\n/).filter(line => line.trim())
  if (lines.length !== 2 || lines.some(line => !/^\s*(?:[-*]|[12][.)])\s+/.test(line))) return { body: text, suggestions: [] }
  const suggestions = lines.map(line => line.replace(/^\s*(?:[-*]|[12][.)])\s+/, '').trim())
  if (suggestions.some(line => Array.from(line).length < 6 || Array.from(line).length > 96 || /[<>\r\n]/.test(line)) || new Set(suggestions).size !== 2) return { body: text, suggestions: [] }
  return { body: prefix.trimEnd(), suggestions }
}
