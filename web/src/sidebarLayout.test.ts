import{readFileSync}from'node:fs'
import{resolve}from'node:path'
import{describe,expect,it}from'vitest'

const css=readFileSync(resolve(process.cwd(),'src/styles.css'),'utf8')

describe('sidebar scrolling layout',()=>{
 it('scrolls only the conversation list while keeping its scrollbar hidden',()=>{
  expect(css).toMatch(/\.launch-sidebar\{[^}]*overflow:hidden/)
  expect(css).toMatch(/\.launch-nav\{[^}]*min-height:0;overflow:hidden/)
  expect(css).toMatch(/\.conversation-group\{[^}]*min-height:0;[^}]*flex:1 1 0/)
  expect(css).toMatch(/\.conversation-list\{[^}]*min-height:0;[^}]*overflow-y:auto;[^}]*flex:1 1 0/)
  const group=css.match(/\.conversation-group\{([^}]*)\}/)?.[1]??'',list=css.match(/\.conversation-list\{([^}]*)\}/)?.[1]??''
  expect(group).not.toMatch(/(?:^|;)height:0(?:;|$)/)
  expect(list).not.toMatch(/(?:^|;)height:0(?:;|$)/)
  expect(css).toMatch(/\.conversation-list::\-webkit-scrollbar\{[^}]*display:none/)
  expect(css).toMatch(/\.conversation-list\{[^}]*scrollbar-width:none;[^}]*-ms-overflow-style:none/)
  expect(css).toMatch(/\.conversation-list\{[^}]*touch-action:pan-y/)
  expect(css).toMatch(/\.conversation-list\{[^}]*height:100%/)
  expect(css).toMatch(/\.conversation-group\.is-closed\{[^}]*flex:none/)
  expect(css).toMatch(/\.conversation-group\.is-empty\{[^}]*flex:none/)
  expect(css).toMatch(/\.conversation-group\.is-empty \.conversation-list\{[^}]*flex:none/)
  expect(css).toMatch(/\.conversation-row:nth-last-child\(-n\+4\) \.conversation-menu\{[^}]*bottom:calc\(100% \+ 4px\)/)
  expect(readFileSync(resolve(process.cwd(),'src/App.tsx'),'utf8')).not.toContain('<details className="conversation-directory"')
  expect(css).toMatch(/\.launch-bottom\{[^}]*flex:none/)
 })
})
