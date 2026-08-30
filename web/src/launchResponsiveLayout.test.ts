import{readFileSync}from'node:fs'
import{resolve}from'node:path'
import{describe,expect,it}from'vitest'

const css=readFileSync(resolve(process.cwd(),'src/styles.css'),'utf8')

describe('launch responsive layout',()=>{
 it('lets the home content follow the available window width',()=>{
  expect(css).toMatch(/\.launch-main\{[^}]*padding:[^;]*clamp\(24px,5%,72px\)/)
  expect(css).toMatch(/\.launch-center\{[^}]*width:100%;[^}]*min-width:0/)
  expect(css).not.toMatch(/\.launch-center\{[^}]*width:min\(1040px,100%\)/)
 })
 it('lets the home aurora show through the launch panel in both themes',()=>{
  expect(css).toMatch(/\.atmosphere-aurora\{position:absolute;inset:0/)
  expect(css).toMatch(/\.launch-content:has\(\.launch-main\),\.launch-shell:has\(\.launch-main\)\{background:transparent\}/)
  expect(css).toMatch(/html\[data-theme="light"\] \.launch-content:has\(\.launch-main\)\{background:transparent\}/)
  expect(css).toMatch(/html\[data-theme="light"\] \.atmosphere\[data-aurora="webgl"\] \.sky\{background:#f7f8fc\}/)
 })
 it('gives collapsed sidebar space back to the chat content',()=>{
  expect(css).toMatch(/\.launch-shell\{--sidebar-width:var\(--sidebar-expanded-width,288px\)/)
  expect(css).toMatch(/\.launch-shell\.sidebar-collapsed\{--sidebar-width:0px\}/)
  expect(css).toMatch(/\.sidebar-collapsed>\.launch-content\{width:100%;margin-left:0\}/)
 })
})
