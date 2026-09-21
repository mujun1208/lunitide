import {describe, expect, it} from 'vitest'
import {readdirSync, readFileSync, statSync} from 'node:fs'
import {join} from 'node:path'

/** The desktop shell starts WebView2 with AreDefaultScriptDialogsEnabled=FALSE,
 *  so window.confirm answers false and window.prompt answers null without ever
 *  drawing anything. Any action gated on one is a button that does nothing when
 *  clicked — the exact symptom reported for skills, plugins and Agent Hub. Use
 *  useConfirmDialog / usePromptDialog from ui/useAskDialog instead. */
const FORBIDDEN = /window\.(confirm|prompt|alert)\s*\(/

function sourceFiles(dir: string): string[] {
  const out: string[] = []
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    if (statSync(full).isDirectory()) {
      out.push(...sourceFiles(full))
      continue
    }
    if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) out.push(full)
  }
  return out
}

describe('renderer never calls script dialogs', () => {
  it('has no window.confirm/prompt/alert call sites', () => {
    const offenders = sourceFiles(join(__dirname, '..'))
      .filter(file => FORBIDDEN.test(readFileSync(file, 'utf8')))
      .map(file => file.replace(join(__dirname, '..'), 'web/src').replaceAll('\\', '/'))
    expect(offenders).toEqual([])
  })
})
