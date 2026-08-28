import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const css = readFileSync(resolve(process.cwd(), 'src/styles.css'), 'utf8')
const app = readFileSync(resolve(process.cwd(), 'src/App.tsx'), 'utf8')

describe('project workbench native-frame stability', () => {
  it('does not wrap the workbench in the 3-column app-shell grid with Atmosphere as a sibling row', () => {
    expect(app).toMatch(/className="session-shell workbench-route"/)
    expect(app).not.toMatch(/className="app-shell session-shell"/)
  })

  it('locks the workbench route to the host client instead of stacking 100vh rows', () => {
    expect(css).toMatch(/\.workbench-route\.session-shell\{[^}]*display:block;[^}]*height:100%;[^}]*overflow:hidden/)
    expect(css).toMatch(/\.pm-workbench\.launch-shell\{height:100%;min-height:0;overflow:hidden\}/)
    expect(css).toMatch(/\.pm-workbench \.launch-content,\.pm-workbench \.pm-workbench-center\{height:100%;overflow:hidden/)
    expect(css).not.toMatch(/\.pm-workbench\.launch-shell\{height:100vh/)
    expect(css).toMatch(/html,#root\{[^}]*height:100%/)
    expect(css).toMatch(/body\{background:#03060c;overflow:hidden;height:100%/)
  })

  it('keeps expert-chip glow from invalidating the window chrome compositor', () => {
    expect(css).toMatch(/\.skill-chip\{[^}]*contain:paint/)
    expect(css).toMatch(/\.atmosphere\{[^}]*contain:paint/)
  })
})
