// Whole-word ASR repairs, after the homophone regexes in companionText.
//
// Streaming models skip OOV English as a unit (OpenClaw → "open cloud").
// A small substitution table is cheaper than swapping the recognizer.
// Built-ins cover this product's names; users add "误识别 : 正确" lines
// in settings. Missing storage fails open — the transcript is unchanged.

export interface AsrCorrection {
  from: string
  to: string
}

const STORAGE_KEY = 'lunitide:asr-corrections'

export const BUILTIN_ASR_CORRECTIONS: AsrCorrection[] = [
  { from: 'gpt so vits', to: 'GPT-SoVITS' },
  { from: 'gpt sovits', to: 'GPT-SoVITS' },
  { from: 'gpt-sovits', to: 'GPT-SoVITS' },
  { from: 'web view 2', to: 'WebView2' },
  { from: 'web view2', to: 'WebView2' },
  { from: 'webview 2', to: 'WebView2' },
  { from: 'luni tide', to: 'Lunitide' },
  { from: 'luna tide', to: 'Lunitide' },
  { from: 'by ok', to: 'BYOK' },
]

export function parseAsrCorrectionTable(raw: string): AsrCorrection[] {
  const pairs: AsrCorrection[] = []
  const seen = new Set<string>()
  for (const line of raw.split(/\r?\n/)) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    const split = trimmed.split(':')
    if (split.length < 2) continue
    const from = split[0]!.trim()
    const to = split.slice(1).join(':').trim()
    if (from.length < 2 || !to || from === to) continue
    const key = from.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    pairs.push({ from, to })
  }
  return pairs
}

export function formatAsrCorrectionTable(pairs: AsrCorrection[]): string {
  return pairs.map(pair => `${pair.from} : ${pair.to}`).join('\n')
}

export function loadUserAsrCorrectionText(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) ?? ''
  } catch {
    return ''
  }
}

export function saveUserAsrCorrectionText(text: string): void {
  try {
    if (text.trim()) localStorage.setItem(STORAGE_KEY, text)
    else localStorage.removeItem(STORAGE_KEY)
  } catch {
    /* private mode */
  }
}

export function loadUserAsrCorrections(): AsrCorrection[] {
  return parseAsrCorrectionTable(loadUserAsrCorrectionText())
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** Longer keys first so "gpt so vits" wins over a shorter overlapping pair. */
export function applyAsrCorrections(text: string, extra: AsrCorrection[] = []): string {
  if (!text) return text
  const pairs = [...BUILTIN_ASR_CORRECTIONS, ...extra]
    .filter(pair => pair.from && pair.to && pair.from !== pair.to)
    .sort((a, b) => b.from.length - a.from.length)
  let out = text
  for (const { from, to } of pairs) {
    if (/[A-Za-z]/.test(from)) {
      const parts = from.trim().split(/\s+/)
      const pattern = new RegExp(`\\b${parts.map(escapeRegExp).join('\\s+')}\\b`, 'gi')
      out = out.replace(pattern, to)
    } else if (out.includes(from)) {
      out = out.split(from).join(to)
    }
  }
  return out
}

export function correctAsrText(text: string): string {
  return applyAsrCorrections(text, loadUserAsrCorrections())
}
