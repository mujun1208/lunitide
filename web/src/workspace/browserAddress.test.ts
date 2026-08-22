import { describe, expect, it } from 'vitest'
import { isBrowserAddress, latestBrowserAddress, parseSearchCards, userWantsBrowserPanel } from './browserAddress'

describe('browserAddress', () => {
  it('syncs the URL bar from results_url even when the preview path is search.html', () => {
    const url = 'https://cn.bing.com/search?q=%E5%8F%A4%E5%A4%A9%E4%B9%90'
    expect(isBrowserAddress(url)).toBe(true)
    expect(isBrowserAddress('https://')).toBe(false)
    expect(latestBrowserAddress([
      { name: 'web.search', status: 'tool_started', summary: '搜索：古天乐' },
      { name: 'web.search', status: 'tool_completed', summary: `query: 古天乐\nresults_url: ${url}`, artifact: { kind: 'html', path: 'search.html', content: '<h1>搜索结果 · 古天乐</h1>' } },
    ])).toBe(url)
  })

  it('syncs a fetched page URL from the tool summary when the artifact is fetch.html', () => {
    expect(latestBrowserAddress([
      { name: 'web.fetch', status: 'tool_completed', summary: 'url: https://tags.sina.com.cn/star_gutianle\n\n古天乐', artifact: { kind: 'html', path: 'fetch.html', content: '<h1>古天乐</h1>' } },
    ])).toBe('https://tags.sina.com.cn/star_gutianle')
  })

  it('parses SERP cards with real result URLs', () => {
    const html = '<h1>搜索结果 · 古天乐</h1><ol class="serp"><li class="serp-hit"><a href="https://news.example/koo">1. 古天乐新闻</a><small>https://news.example/koo</small><p>电影动态</p></li></ol>'
    const parsed = parseSearchCards(html)
    expect(parsed.query).toBe('古天乐')
    expect(parsed.hits).toEqual([{ title: '古天乐新闻', url: 'https://news.example/koo', snippet: '电影动态' }])
  })

  it('uses file:// paths after writing an HTML game', () => {
    const file = 'file:///C:/Users/mu/Desktop/点球大战.html'
    expect(latestBrowserAddress([{ name: 'workspace.write', status: 'tool_completed', artifact: { kind: 'html', path: file, content: '<html></html>' } }])).toBe(file)
  })

  it('detects when the user asked to see the workspace browser', () => {
    expect(userWantsBrowserPanel('帮我打开网页看看')).toBe(true)
    expect(userWantsBrowserPanel('生成网页')).toBe(true)
    expect(userWantsBrowserPanel('现在市面上除了飞算AI，还有没有类似他的产品')).toBe(false)
    expect(userWantsBrowserPanel('搜索一下竞品')).toBe(false)
  })
})
