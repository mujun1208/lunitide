import { useEffect, useState } from 'react'
import { Atmosphere } from './App'
import { canUseCompanionWebgl } from './session/companion/visual/webglSupport'

export function LaunchAtmospherePreview(): React.JSX.Element {
  const [theme, setTheme] = useState<'dark' | 'light'>('dark')
  const webgl = canUseCompanionWebgl()

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.documentElement.style.colorScheme = theme
    document.documentElement.lang = 'zh-CN'
  }, [theme])

  return (
    <div className="launch-shell">
      <Atmosphere theme={theme} aurora />
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
              <button type="button"><span>☻&nbsp; 同事聊天</span></button>
              <button type="button"><span>◎&nbsp; 会议记录</span></button>
            </div>
          </section>
          <button type="button"><span>⌕&nbsp; 搜索</span></button>
        </div>
      </aside>
      <main className="launch-content">
        <div className="launch-main">
          <div className="launch-center">
            <span className="real-moon" aria-hidden="true"><i /><b /><em /></span>
            <h1>今天想聊什么？</h1>
            <p className="launch-lead">直接开始对话；需要持续工程上下文时，再进入项目。</p>
            <form className="launch-composer" onSubmit={event => event.preventDefault()}>
              <textarea readOnly value="" placeholder="向月汐提问，或描述你想完成的事情…" />
              <div className="composer-tools">
                <button type="button">＋</button>
                <button type="button" className="composer-send" disabled>↑</button>
              </div>
            </form>
            <div className="launch-actions">
              <button type="button"><b>月伴对话</b><span>纯语音交流：你说话，月汐回答，像聊天一样</span></button>
              <button type="button"><b>项目管理</b><span>创建、查看或继续已有项目</span></button>
              <button type="button"><b>恢复对话</b><span>回到最近一次普通聊天</span></button>
            </div>
          </div>
        </div>
      </main>
      <div className="companion-preview-dock" role="toolbar" aria-label="首页极光预览">
        <strong>首页极光预览</strong>
        <button type="button" className={theme === 'dark' ? 'is-on' : ''} onClick={() => setTheme('dark')}>黑夜</button>
        <button type="button" className={theme === 'light' ? 'is-on' : ''} onClick={() => setTheme('light')}>白天</button>
        <span className="companion-preview-hint">
          {webgl ? 'WebGL 极光已开' : '当前无 WebGL，走原来的天空降级'}
          {' · '}
          Cursor 里截图常是白板，请用系统浏览器看
        </span>
      </div>
    </div>
  )
}
