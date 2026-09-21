import { describe, expect, it } from 'vitest'
import { previewNeedsScripts } from './previewInteractivity'

/** The preview renders HTML faithfully but runs no JavaScript, so a generated app
 *  looks finished and then ignores every click. Saying nothing about that is what
 *  made a working preview read as broken, so these are the documents that must
 *  carry the explanation — and the ones that must not, or the notice becomes noise
 *  on every static report. */
describe('previewNeedsScripts', () => {
  it('flags documents whose behaviour depends on scripts', () => {
    expect(previewNeedsScripts('<script src="app.js"></script><nav>…</nav>')).toBe(true)
    expect(previewNeedsScripts('<script>\nlocalStorage.getItem("crm")\n</script>')).toBe(true)
    expect(previewNeedsScripts('<button onclick="show(\'customers\')">客户管理</button>')).toBe(true)
    expect(previewNeedsScripts('<form><input name="q"></form>')).toBe(true)
  })

  it('stays quiet on documents that preview faithfully', () => {
    expect(previewNeedsScripts('<h1>周报</h1><table><tr><td>销售额</td></tr></table>')).toBe(false)
    expect(previewNeedsScripts('<style>p{color:red}</style><p>hi</p>')).toBe(false)
    expect(previewNeedsScripts('')).toBe(false)
  })

  it('is not fooled by prose that merely mentions a script', () => {
    // "<script" has to open a tag; the word alone in body text must not trip it.
    expect(previewNeedsScripts('<p>把 script 标签放在底部</p>')).toBe(false)
  })
})
