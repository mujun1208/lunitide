/** Matching an expert to its equipment by keyword table means a skill whose
 *  description says exactly what the job needs is missed unless someone
 *  remembered to add the word to the table. This module reads the descriptions
 *  instead: the expert's six-section brief on one side, each installed skill's
 *  and MCP preset's own text on the other.
 *
 *  Precision is the requirement, not coverage. A candidate is selected only when
 *  its own distinctive vocabulary shows up in the brief — not when the brief
 *  happens to share filler with it — because a wrongly equipped expert is worse
 *  than an under-equipped one the user can top up by hand. */

export interface MatchCandidate {
  /** The bind key to store when this candidate wins. */
  key: string
  /** Everything the candidate says about itself: name, display name, description. */
  text: string
}

export interface MatchResult {
  key: string
  score: number
  /** The candidate's own terms that appeared in the brief, for the notice line. */
  hits: string[]
}

/** Latin words shorter than this carry no meaning on their own ("to", "of"). */
const MIN_LATIN_TERM = 3
/** Terms that appear in nearly every brief and every description. Matching on
 *  these is how "乱匹配" happens, so they never count. */
const STOPWORDS = new Set([
  'the', 'and', 'for', 'with', 'you', 'your', 'this', 'that', 'from', 'into', 'are', 'not',
  'use', 'used', 'using', 'can', 'will', 'when', 'what', 'how', 'all', 'any', 'out', 'per',
  'skill', 'skills', 'tool', 'tools', 'agent', 'expert', 'lunitide', 'claude', 'mcp',
  '技能', '工具', '专家', '使用', '可以', '需要', '进行', '如果', '不要', '必须', '然后',
  '完成', '输出', '内容', '结果', '问题', '用户', '这个', '一个', '并且', '以及', '或者',
  '交付', '模板', '身份', '使命', '规则', '流程', '成功', '度量', '说明', '要求', '支持',
])

/** Words a description uses to describe itself that are strong enough on their
 *  own: seeing one in the brief is decisive, no second hit required. */
const STRONG_MIN_LATIN = 5

/** terms pulls the vocabulary out of mixed Chinese/English text. CJK has no
 *  spaces, so adjacent-character pairs stand in for words: "周报" survives as a
 *  unit, and a description that says 周报 matches a brief that says 写周报. */
export function terms(text: string): Set<string> {
  const out = new Set<string>()
  if (!text) return out
  const lower = text.toLowerCase()
  for (const word of lower.match(/[a-z][a-z0-9_-]*/g) ?? []) {
    const clean = word.replace(/^-+|-+$/g, '')
    if (clean.length < MIN_LATIN_TERM || STOPWORDS.has(clean)) continue
    out.add(clean)
    // slide-builder should also match a brief that only says "slide".
    for (const part of clean.split(/[-_]/)) {
      if (part.length >= MIN_LATIN_TERM && !STOPWORDS.has(part)) out.add(part)
    }
  }
  const cjk = lower.match(/[\u4e00-\u9fff]+/g) ?? []
  for (const run of cjk) {
    if (run.length === 1) continue
    for (let i = 0; i + 1 < run.length; i++) {
      const pair = run.slice(i, i + 2)
      if (!STOPWORDS.has(pair)) out.add(pair)
    }
  }
  return out
}

/** A term is distinctive when it is not shared by most of the field. A skill
 *  whose only overlap with the brief is a word every other skill also uses has
 *  told us nothing. */
function distinctive(candidates: readonly MatchCandidate[]): (term: string) => boolean {
  const seen = new Map<string, number>()
  for (const candidate of candidates) {
    for (const term of terms(candidate.text)) seen.set(term, (seen.get(term) ?? 0) + 1)
  }
  const ceiling = Math.max(2, Math.floor(candidates.length / 2))
  return term => (seen.get(term) ?? 0) <= ceiling
}

/** matchByDescription scores every candidate against the brief and returns the
 *  ones that earned their place, best first.
 *
 *  A candidate needs either two distinctive shared terms, or one long enough to
 *  be unmistakable (an installed skill literally named in the brief). `limit`
 *  caps the result so a verbose brief cannot select the entire library. */
export function matchByDescription(
  brief: string,
  candidates: readonly MatchCandidate[],
  limit = 8,
): MatchResult[] {
  const briefTerms = terms(brief)
  if (!briefTerms.size) return []
  const isDistinctive = distinctive(candidates)
  const scored: MatchResult[] = []
  for (const candidate of candidates) {
    const hits: string[] = []
    let score = 0
    let decisive = false
    for (const term of terms(candidate.text)) {
      if (!briefTerms.has(term) || !isDistinctive(term)) continue
      hits.push(term)
      const weight = term.length >= STRONG_MIN_LATIN || /[\u4e00-\u9fff]/.test(term) ? 2 : 1
      score += weight
      if (term.length >= STRONG_MIN_LATIN && keyTerms(candidate.key).has(term)) decisive = true
    }
    if (!hits.length) continue
    if (!decisive && hits.length < 2) continue
    scored.push({ key: candidate.key, score, hits: hits.slice(0, 4) })
  }
  scored.sort((a, b) => b.score - a.score || a.key.localeCompare(b.key))
  return scored.slice(0, limit)
}

function keyTerms(key: string): Set<string> {
  return terms(key.replace(/^(?:mcp|brain):/, '').replace(/^tpl-/, ''))
}
