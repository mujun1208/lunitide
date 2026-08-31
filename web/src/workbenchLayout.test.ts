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

  it('locks ordinary chat and the workbench to the host client instead of stacking 100vh rows', () => {
    expect(css).toMatch(/\.workbench-route\.session-shell\{[^}]*display:block;[^}]*height:100%;[^}]*overflow:hidden/)
    expect(css).toMatch(/\.pm-workbench\.launch-shell\{height:100%;min-height:0;overflow:hidden/)
    expect(css).toMatch(/\.pm-workbench \.launch-content,\.pm-workbench \.pm-workbench-center\{height:100%;overflow:hidden/)
    expect(css).not.toMatch(/\.pm-workbench\.launch-shell\{height:100vh/)
    expect(css).not.toMatch(/\.launch-content\{[^}]*height:100vh/)
    expect(css).not.toMatch(/\.launch-shell\{[^}]*min-height:100vh/)
    expect(css).not.toMatch(/\.main\.project-workspace-shell\{[^}]*min-height:100vh/)
    expect(css).toMatch(/\.chat-shell\.launch-shell\{height:100%;min-height:0;overflow:hidden/)
    expect(css).toMatch(/\.chat-content\{[^}]*min-height:0/)
    expect(css).not.toMatch(/\.chat-content\{[^}]*min-height:100vh/)
    expect(css).not.toMatch(/min-height:calc\(100vh - 92px\)/)
    expect(css).toMatch(/html,#root\{[^}]*height:100%/)
    expect(css).toMatch(/html,#root\{[^}]*overflow:hidden/)
    expect(css).toMatch(/html,#root\{[^}]*overflow-anchor:none/)
    expect(css).toMatch(/body\{background:#03060c;overflow:hidden;[^}]*height:100%/)
    expect(css).toMatch(/body\{[^}]*overflow-anchor:none/)
  })

  it('pins stream layout so thinking tokens cannot shake the native frame', () => {
    expect(css).toMatch(/\.conversation-scroll\{[^}]*overflow-anchor:none/)
    expect(css).toMatch(/\.workspace-layout\{[^}]*overflow-anchor:none/)
    expect(css).toMatch(/\.message-body\{[^}]*overflow-anchor:none/)
    expect(css).toMatch(/\.thinking-panel\{[^}]*overflow-anchor:none/)
    expect(css).toMatch(/\.thinking-panel\{[^}]*contain:content/)
    expect(css).toMatch(/\.thinking-live-text\{[^}]*max-height:18em/)
    expect(css).toMatch(/\.thinking-live-text\{[^}]*min-height:0/)
    expect(css).toMatch(/\.waiting-response\{[^}]*min-height:1\.7em/)
    expect(css).toMatch(/\.mermaid-host\{[^}]*max-height:min\(40vh,360px\)/)
    expect(css).toMatch(/\.workspace-layout\.workspace-is-open\{[^}]*grid-template-rows:minmax\(0,1fr\)/)
    expect(css).toMatch(/\.workspace-layout>\.message-panel\{[^}]*min-height:0;[^}]*height:100%/)
    expect(css).toMatch(/\.project-chat-panel\.workspace-layout>\.message-panel\{min-height:0;height:100%/)
  })

  it('keeps expert-chip glow from invalidating the window chrome compositor', () => {
    expect(css).toMatch(/\.skill-chip\{[^}]*contain:paint/)
    expect(css).toMatch(/\.atmosphere\{[^}]*contain:paint/)
  })
})
