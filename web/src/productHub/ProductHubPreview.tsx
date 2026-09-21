import { useEffect, useState } from 'react'
import { installProductHubBridge } from '../bridge/client'
import { ProductHubPage } from './ProductHubPage'
import { createProductHubPreviewBridge, setProductHubPreviewUnlocked, unlockProductHubPreview } from './productHubPreviewFixture'
import { writeHubToken } from './productHubTypes'
import './productHubPreview.css'

installProductHubBridge(createProductHubPreviewBridge())
setProductHubPreviewUnlocked(true)
writeHubToken(unlockProductHubPreview())

export function ProductHubPreview(): React.JSX.Element {
  const [theme, setTheme] = useState<'dark' | 'light'>('dark')
  const [screen, setScreen] = useState<'unlock' | 'hub'>('hub')
  const [pageKey, setPageKey] = useState(0)

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.documentElement.style.colorScheme = theme
    document.documentElement.lang = 'zh-CN'
  }, [theme])

  const show = (next: 'unlock' | 'hub') => {
    const entered = next === 'hub'
    setProductHubPreviewUnlocked(entered)
    writeHubToken(entered ? unlockProductHubPreview() : '')
    setScreen(next)
    setPageKey(value => value + 1)
  }

  return (
    <div className="launch-shell product-hub-preview-shell">
      <aside className="launch-sidebar" aria-label="主导航">
        <button className="launch-brand" type="button">
          <span className="real-moon small" aria-hidden="true"><i /><b /><em /></span>
          <span><b>月汐</b></span>
        </button>
        <div className="primary-actions">
          <button type="button"><span>＋&nbsp; 新对话</span></button>
          <section className="office-group is-open">
            <button type="button" className="conversation-heading office-heading" aria-expanded="true"><span aria-hidden="true">›</span>办公</button>
            <div id="office-list" className="office-nav-list">
              <button type="button"><span>⏱&nbsp; 自动化</span></button>
              <button type="button"><span>🖼&nbsp; 媒体</span></button>
              <button type="button" className="active" aria-current="page"><span>◈&nbsp; 产品总览</span></button>
            </div>
          </section>
        </div>
      </aside>
      <main className="launch-content">
        <ProductHubPage key={`${screen}-${pageKey}`} language="zh-CN" />
      </main>
      <div className="companion-preview-dock" role="toolbar" aria-label="产品总览样式预览">
        <strong>产品总览样式预览</strong>
        <div className="companion-preview-dock-row">
          <button type="button" className={theme === 'dark' ? 'is-on' : ''} onClick={() => setTheme('dark')}>黑夜</button>
          <button type="button" className={theme === 'light' ? 'is-on' : ''} onClick={() => setTheme('light')}>白天</button>
          <button type="button" className={screen === 'unlock' ? 'is-on' : ''} onClick={() => show('unlock')}>登录框</button>
          <button type="button" className={screen === 'hub' ? 'is-on' : ''} onClick={() => show('hub')}>总览</button>
        </div>
        <span className="companion-preview-hint">假数据 · mujun / 1234567890 · 全景点功能进卡片 · 变更在抽屉</span>
      </div>
    </div>
  )
}
