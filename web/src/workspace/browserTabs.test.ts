import { expect, it } from 'vitest'
import {
  browserTabLabel,
  closeBrowserTab,
  createBrowserTab,
  moveBrowserTab,
  navigateBrowserTab,
  placeBrowserAddress,
  retitleBrowserTab,
} from './browserTabs'

it('names a tab from the page title, then the site', () => {
  const tab = createBrowserTab('a')
  expect(browserTabLabel(tab)).toBe('新标签页')
  expect(browserTabLabel(navigateBrowserTab(tab, 'https://www.baidu.com/'))).toBe('baidu.com')
  expect(browserTabLabel(retitleBrowserTab(navigateBrowserTab(tab, 'https://www.baidu.com/'), '百度一下，你就知道'))).toBe('百度一下，你就知道')
})

it('closes one tab and keeps a neighbor, and never leaves the strip empty', () => {
  const first = navigateBrowserTab(createBrowserTab('a'), 'https://www.baidu.com/')
  const second = navigateBrowserTab(createBrowserTab('b'), 'https://www.bing.com/')
  const third = navigateBrowserTab(createBrowserTab('c'), 'https://example.com/')
  const closed = closeBrowserTab([first, second, third], 'b', 'b', 'fresh')
  expect(closed.tabs.map(tab => tab.id)).toEqual(['a', 'c'])
  expect(closed.activeId).toBe('c')
  const gone = closeBrowserTab(closed.tabs, 'c', 'a', 'fresh')
  expect(gone.activeId).toBe('c')
  const last = closeBrowserTab(gone.tabs, 'c', 'c', 'fresh')
  expect(last.tabs).toHaveLength(1)
  expect(last.activeId).toBe('fresh')
  expect(browserTabLabel(last.tabs[0])).toBe('新标签页')
})

it('opens another page beside the one already showing', () => {
  const current = navigateBrowserTab(createBrowserTab('a'), 'https://www.baidu.com/')
  const opened = placeBrowserAddress([current], 'a', 'https://cn.bing.com/search?q=news', 'b')
  expect(opened.tabs.map(tab => tab.url)).toEqual(['https://www.baidu.com/', 'https://cn.bing.com/search?q=news'])
  expect(opened.activeId).toBe('b')
  const filled = placeBrowserAddress([createBrowserTab('a')], 'a', 'https://www.baidu.com/', 'b')
  expect(filled.tabs).toHaveLength(1)
  expect(filled.tabs[0].url).toBe('https://www.baidu.com/')
})

it('walks back along the tab that is open', () => {
  const visited = navigateBrowserTab(navigateBrowserTab(createBrowserTab('a'), 'https://example.com/a'), 'https://example.com/b')
  const back = moveBrowserTab(visited, -1)
  expect(back.url).toBe('https://example.com/a')
  expect(moveBrowserTab(back, 1).url).toBe('https://example.com/b')
})
