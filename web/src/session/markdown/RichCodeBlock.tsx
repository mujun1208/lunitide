import React, { useCallback, useState } from 'react'

const TERMINAL_LANGS = new Set(['bash', 'sh', 'shell', 'zsh', 'powershell', 'pwsh', 'cmd', 'terminal', 'console'])

const LANG_LABEL: Record<string, string> = {
  bash: 'Bash',
  sh: 'Shell',
  shell: 'Shell',
  zsh: 'Zsh',
  powershell: 'PowerShell',
  pwsh: 'PowerShell',
  cmd: 'CMD',
  terminal: 'Terminal',
  console: 'Terminal',
  ts: 'TypeScript',
  tsx: 'TSX',
  js: 'JavaScript',
  jsx: 'JSX',
  json: 'JSON',
  env: 'env',
  yaml: 'YAML',
  yml: 'YAML',
  go: 'Go',
  python: 'Python',
  py: 'Python',
  sql: 'SQL',
  dockerfile: 'Dockerfile',
  docker: 'Docker',
}

export function codeBlockLanguage(className?: string): string {
  const match = /language-([\w-]+)/i.exec(className ?? '')
  return match?.[1]?.toLowerCase() ?? ''
}

export function isTerminalLanguage(lang: string): boolean {
  return TERMINAL_LANGS.has(lang)
}

export function languageLabel(lang: string): string {
  if (!lang) return 'text'
  return LANG_LABEL[lang] ?? lang
}

export function RichCodeBlock({
  lang,
  code,
  onCopy,
}: {
  lang: string
  code: string
  onCopy?: (value: string) => void | Promise<void>
}) {
  const [copied, setCopied] = useState(false)
  const terminal = isTerminalLanguage(lang)
  const label = languageLabel(lang)
  const copy = useCallback(async () => {
    if (!onCopy) return
    await onCopy(code)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1600)
  }, [code, onCopy])

  return (
    <div className={`rich-code-block${terminal ? ' is-terminal' : ''}`}>
      <div className="rich-code-toolbar">
        <span className="rich-code-lang">{label}</span>
        {onCopy && (
          <button type="button" className="rich-code-copy" aria-label={`复制${label}代码`} onClick={() => void copy()}>
            {copied ? '已复制' : '复制'}
          </button>
        )}
      </div>
      <pre className="rich-code-pre">
        <code className={lang ? `language-${lang}` : undefined}>{code}</code>
      </pre>
    </div>
  )
}
