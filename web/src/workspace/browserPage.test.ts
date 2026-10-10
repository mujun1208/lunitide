import { expect, it } from 'vitest'
import { browserClickTarget, browserFollowURL, browserOpenAsk, filePageURL, filePathFromPageURL, previewShouldReload, promptWithBrowserPage } from './browserPage'

it('keeps the user words and appends the open page for the model', () => {
  expect(promptWithBrowserPage('赢单率是多少', '赢单率 75%')).toBe('赢单率是多少\n\n[浏览器页面]\n赢单率 75%')
  expect(promptWithBrowserPage('你好', '   ')).toBe('你好')
})

it('does not paste the open file into a question', () => {
  const path = 'E:\\Lunitide-Project\\poc\\it-crm\\index.html'
  const page = '商机赢单率 ' + '甲'.repeat(800)
  const prompt = promptWithBrowserPage('任务是做完了吗？', page, path)
  expect(prompt.startsWith('任务是做完了吗？')).toBe(true)
  expect(prompt).toContain(`[正在看的页面文件]\n${path}\n`)
  expect(prompt).toContain('workspace.read')
  expect(prompt).not.toContain('[浏览器页面]')
  expect(prompt).not.toContain('商机赢单率')
  expect(prompt).not.toContain('甲')
})

it('tells the model to edit the file that is on screen and then refresh', () => {
  const path = 'E:\\Lunitide-Project\\poc\\it-crm\\index.html'
  const page = '商机 赢单率 80% ' + '甲'.repeat(400)
  const prompt = promptWithBrowserPage('加上客户管理，再加一条商机', page, path)
  expect(prompt.startsWith('加上客户管理，再加一条商机')).toBe(true)
  expect(prompt).toContain(`[正在看的页面文件]\n${path}\n`)
  expect(prompt).toContain('workspace.edit')
  expect(prompt).toContain('自动刷新')
  expect(prompt).not.toContain('[浏览器页面]')
  expect(prompt).not.toContain('赢单率 80%')
  expect(filePathFromPageURL('file:///E:/Lunitide-Project/poc/it-crm/index.html')).toBe(path)
  expect(filePathFromPageURL('file:///E:/a/../Windows/notepad.exe')).toBe('')
  expect(previewShouldReload('workspace.edit', 'tool_completed', 'edited 1')).toBe(true)
  expect(previewShouldReload('workspace.write', 'tool_completed', 'ok:false')).toBe(false)
  expect(previewShouldReload('browser.act', 'tool_completed', '')).toBe(false)
})

it('turns a resolved Windows file into the side browser address', () => {
  expect(filePageURL('E:/Lunitide-Project/poc/it-crm/index.html')).toBe('file:///E:/Lunitide-Project/poc/it-crm/index.html')
  expect(filePageURL('E:\\proj\\..\\Windows\\notepad.exe')).toBe('')
  expect(filePageURL('\\\\server\\share\\index.html')).toBe('')
})

it('sends a lookup or an address into the side browser', () => {
  expect(browserFollowURL('查一下 国庆放假')).toBe('https://www.baidu.com/s?wd=' + encodeURIComponent('国庆放假'))
  expect(browserFollowURL('打开 https://www.baidu.com/')).toBe('https://www.baidu.com/')
  expect(browserFollowURL('打开百度')).toBe('https://www.baidu.com/')
  expect(browserFollowURL('打开客户')).toBe('')
  expect(browserFollowURL('你好')).toBe('')
})

it('tells an explicit open ask apart from a generic search so the panel only pops when asked', () => {
  expect(browserOpenAsk('打开 https://www.baidu.com/')).toBe(true)
  expect(browserOpenAsk('https://example.com/page')).toBe(true)
  expect(browserOpenAsk('打开百度')).toBe(true)
  expect(browserOpenAsk('查一下 国庆放假')).toBe(false)
  expect(browserOpenAsk('帮我查询中航材利顿航空科技股份有限公司，相关信息')).toBe(false)
  expect(browserOpenAsk('搜索一下竞品')).toBe(false)
  expect(browserOpenAsk('你好')).toBe(false)
})

it('follows a short open or click onto the page that is already open', () => {
  expect(browserClickTarget('打开客户')).toBe('客户')
  expect(browserClickTarget('帮我点击新建商机')).toBe('新建商机')
  expect(browserClickTarget('打开 https://example.com')).toBe('')
  expect(browserClickTarget('打开网页看看')).toBe('')
  expect(browserClickTarget('搜索竞品')).toBe('')
})
