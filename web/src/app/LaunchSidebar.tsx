import React, { useEffect, useRef, useState } from 'react'
import { createMutationAttempt, getIdentityBridge, getPeopleBridge, type MessageBridge, type ProjectBridge, type SessionBridge } from '../bridge/client'
import type { IdentityDTO, MessageSearchResult, SessionDTO } from '../generated/bridge'
import { isProtectedSidebarChat, localizedSessionTitle } from '../session/sessionTitle'
import type { Language } from '../i18n/language'
import { listActiveSessionIds, subscribeLiveChatRegistry } from '../session/liveChat'
import { initials, unreadTotal } from '../people/peopleRoster'
import { readChatsOpen, readOfficeOpen, SIDEBAR_CHATS_OPEN_KEY, SIDEBAR_OFFICE_OPEN_KEY, writeSidebarFlag } from '../sidebarSplit'
import { ConfirmDialog, Dialog } from '../ui/Dialog'
import { usePanelResize } from '../ui/usePanelResize'
import { findPersonalProject, isOrdinarySidebarChat, PERSONAL_CHAT_PROJECT_ID_KEY } from './appHelpers'
import type { ChatTarget, Page, Theme } from './appTypes'
import { useOfficeMenu } from '../settings/officeMenuSettings'
import { openOfficeHome } from '../officeStudio/officeNavigation'
import { NavIcon } from './navIcons'
import { WorkspaceToolsNav } from './WorkspaceToolsNav'

function sidebarUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

function officePage(page: Page): boolean {
  return page === 'office' || page === 'automation' || page === 'media' || page === 'people' || page === 'meetings' || page === 'mro' || page === 'productHub'
}

export function LaunchSidebar({
  open, page, setPage: navigate, projects, sessions, messages, onSelect, onNew, theme, language, refreshKey, localChats, deletedChatIds, draftSessionIds, visibleDraftId, onToggleTheme, onToggleLanguage, collapsed, onUpdated, onDeleted, onOpenPeople, mroEnabled, topSlot, replaceMainNav,
}: {
  open: boolean
  page: Page
  setPage: (page: Page) => void
  projects: ProjectBridge
  sessions: SessionBridge
  messages: MessageBridge
  onSelect: (p: ChatTarget) => void
  onNew: () => void
  theme: Theme
  language: Language
  refreshKey: number
  localChats: Array<ChatTarget & { pending?: boolean }>
  deletedChatIds: Set<string>
  draftSessionIds: Set<string>
  visibleDraftId?: string
  onToggleTheme: () => void
  onToggleLanguage: () => void
  collapsed: boolean
  onUpdated: (session: SessionDTO) => void
  onDeleted: (id: string) => void
  onOpenPeople: (rail: 'chats' | 'contacts' | 'me') => void
  mroEnabled?: boolean
  topSlot?: React.ReactNode
  replaceMainNav?: React.ReactNode
}): React.JSX.Element {
  const setPage = (next: Page) => { if (next === 'office') openOfficeHome(); navigate(next) }
  const [recent, setRecent] = useState<Array<ChatTarget & { pending?: boolean }>>([])
  const [menuId, setMenuId] = useState('')
  const [renameTarget, setRenameTarget] = useState<ChatTarget>()
  const [deleteTarget, setDeleteTarget] = useState<ChatTarget>()
  const [renameTitle, setRenameTitle] = useState('')
  const [busy, setBusy] = useState(false)
  const [actionError, setActionError] = useState('')
  const [searchOpen, setSearchOpen] = useState(false)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchHits, setSearchHits] = useState<MessageSearchResult['items']>([])
  const [searchBusy, setSearchBusy] = useState(false)
  const [searchError, setSearchError] = useState('')
  const [identity, setIdentity] = useState<IdentityDTO>()
  const [peopleUnread, setPeopleUnread] = useState(0)
  const officeMenu = useOfficeMenu()
  const searchInput = useRef<HTMLInputElement>(null)
  const [chatsOpen, setChatsOpen] = useState(readChatsOpen)
  const [officeOpen, setOfficeOpen] = useState(() => officePage(page) || readOfficeOpen())
  useEffect(() => { if (officePage(page)) setOfficeOpen(true) }, [page])
  const [chatsHeight, startChatsResize] = usePanelResize({ storageKey: 'lunitide:sidebar-chats-height', initial: 220, min: 88, max: () => Math.max(88, typeof window === 'undefined' ? 220 : window.innerHeight - 360), axis: 'y' })
  const [hubAgentsHeight, startHubAgentsResize] = usePanelResize({ storageKey: 'lunitide:sidebar-hub-agents-height', initial: 280, min: 88, max: () => Math.max(88, typeof window === 'undefined' ? 280 : window.innerHeight - 360), axis: 'y' })
  const showDraft = (id: string) => !draftSessionIds.has(id) || id === visibleDraftId
  const sortRecent = (values: Array<ChatTarget & { pending?: boolean }>) => [...values].sort((a, b) => Number(b.session.pinned) - Number(a.session.pinned) || b.session.updatedAt.localeCompare(a.session.updatedAt) || b.session.id.localeCompare(a.session.id))
  useEffect(() => {
    let alive = true
    projects.list().then(async r => {
      const personal = findPersonalProject(r.items)
      if (!personal) { if (alive) setRecent([]); return }
      localStorage.setItem(PERSONAL_CHAT_PROJECT_ID_KEY, personal.id)
      const items = (await sessions.list({ projectId: personal.id })).items.filter(session => !deletedChatIds.has(session.id) && showDraft(session.id) && isOrdinarySidebarChat(session.title))
      if (alive) setRecent(current => {
        const remote = new Map(items.map(session => [session.id, session]))
        return sortRecent([
          ...current.filter(value => !deletedChatIds.has(value.session.id) && showDraft(value.session.id) && !remote.has(value.session.id) && isOrdinarySidebarChat(value.session.title)),
          ...items.map(session => {
            const local = current.find(value => value.session.id === session.id)
            return { project: personal, session: (local?.session.version ?? 0) > session.version ? local!.session : session, personal: true as const, pending: local?.pending ?? false }
          }),
        ])
      })
    }).catch(e => { if (alive) setActionError(sidebarUserError(e, '对话列表加载失败，请重试。')) })
    return () => { alive = false }
  }, [projects, sessions, page, refreshKey, deletedChatIds, draftSessionIds, visibleDraftId])
  useEffect(() => {
    setRecent(values => sortRecent([
      ...localChats.filter(value => !deletedChatIds.has(value.session.id) && showDraft(value.session.id) && isOrdinarySidebarChat(value.session.title)),
      ...values.filter(value => !deletedChatIds.has(value.session.id) && showDraft(value.session.id) && !localChats.some(local => local.session.id === value.session.id) && isOrdinarySidebarChat(value.session.title)),
    ]))
  }, [localChats, deletedChatIds, draftSessionIds, visibleDraftId])
  const [liveSessionIds, setLiveSessionIds] = useState<string[]>(() => listActiveSessionIds())
  useEffect(() => {
    const sync = () => {
      const next = listActiveSessionIds()
      setLiveSessionIds(current => current.length === next.length && current.every((id, i) => id === next[i]) ? current : next)
    }
    sync()
    return subscribeLiveChatRegistry(sync)
  }, [])
  useEffect(() => {
    let alive = true
    const load = () => {
      void getIdentityBridge().get().then(value => { if (alive) setIdentity(value) }).catch(() => {})
      void getPeopleBridge().threadList().then(result => { if (alive) setPeopleUnread(unreadTotal(result.items)) }).catch(() => {})
    }
    load()
    const timer = window.setInterval(load, 2000)
    return () => { alive = false; window.clearInterval(timer) }
  }, [page, refreshKey])
  useEffect(() => {
    const close = () => setMenuId('')
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [])
  useEffect(() => {
    if (replaceMainNav) return
    const key = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setSearchOpen(true)
      }
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [replaceMainNav])
  useEffect(() => {
    if (!searchOpen) return
    setSearchQuery('')
    setSearchHits([])
    setSearchError('')
    setSearchBusy(false)
  }, [searchOpen])
  useEffect(() => {
    if (replaceMainNav || !searchOpen) return
    const q = searchQuery.trim()
    if (!q) { setSearchHits([]); setSearchBusy(false); setSearchError(''); return }
    if (!messages.search) { setSearchHits([]); setSearchBusy(false); return }
    let alive = true
    setSearchBusy(true)
    setSearchError('')
    const timer = window.setTimeout(() => {
      const projectId = recent[0]?.project.id ?? localStorage.getItem(PERSONAL_CHAT_PROJECT_ID_KEY) ?? ''
      void messages.search!({ query: q, limit: 32, ...(projectId ? { projectId } : {}) }).then(result => {
        if (!alive) return
        setSearchHits(result.items ?? [])
      }).catch(e => {
        if (!alive) return
        setSearchHits([])
        setSearchError(sidebarUserError(e, '搜索失败，请稍后重试。'))
      }).finally(() => { if (alive) setSearchBusy(false) })
    }, 200)
    return () => { alive = false; window.clearTimeout(timer) }
  }, [replaceMainNav, searchOpen, searchQuery, messages, recent])
  const update = async (item: ChatTarget, title: string, pinned: boolean) => {
    setBusy(true)
    setActionError('')
    try {
      const payload = { id: item.session.id, title, pinned, version: item.session.version }
      const attempt = createMutationAttempt('session.update', payload)
      const updated = await sessions.update(attempt.payload, { attempt })
      setRecent(values => sortRecent(values.map(value => value.session.id === updated.id ? { ...value, session: updated } : value)))
      onUpdated(updated)
      setRenameTarget(undefined)
      setMenuId('')
    } catch (e) {
      setActionError(sidebarUserError(e, '会话更新失败，请重试。'))
    } finally {
      setBusy(false)
    }
  }
  const remove = async () => {
    if (!deleteTarget || isProtectedSidebarChat(deleteTarget.session.title)) return
    setBusy(true)
    setActionError('')
    try {
      const id = deleteTarget.session.id
      const attempt = createMutationAttempt('session.delete', { id })
      await sessions.delete(attempt.payload, { attempt })
      setRecent(values => values.filter(value => value.session.id !== id))
      onDeleted(id)
      setDeleteTarget(undefined)
      setMenuId('')
    } catch (e) {
      setActionError(sidebarUserError(e, '会话删除失败，请重试。'))
    } finally {
      setBusy(false)
    }
  }
  const zh = language === 'zh-CN'
  const query = searchQuery.trim().toLocaleLowerCase()
  const terms = query.split(/\s+/).filter(Boolean)
  const titleMatch = (title: string) => title.toLocaleLowerCase().includes(query) || terms.every(term => title.toLocaleLowerCase().includes(term))
  const ftsBySession = new Map(searchHits.map(hit => [hit.sessionId, hit]))
  const results = query
    ? recent.flatMap(item => {
      const hit = ftsBySession.get(item.session.id)
      if (hit) return [{ item, snippet: hit.snippet as string | undefined }]
      if (titleMatch(localizedSessionTitle(item.session.title, zh))) return [{ item, snippet: undefined }]
      return []
    })
    : recent.map(item => ({ item, snippet: undefined as string | undefined }))
  const officeList = (
    <section className={`office-group project-group ${officeOpen ? 'is-open' : 'is-closed'}`}>
      <button
        type="button"
        className={`conversation-heading office-heading${officePage(page) ? ' is-current' : ''}`}
        aria-expanded={officeOpen}
        aria-controls="office-list"
        onClick={() => setOfficeOpen(value => {
          const next = !value
          writeSidebarFlag(SIDEBAR_OFFICE_OPEN_KEY, next)
          return next
        })}
      >
        <span aria-hidden="true">›</span>{zh ? '办公' : 'Office'}
      </button>
      {officeOpen ? (
        <div id="office-list" className="office-nav-list">
          {officeMenu.office ? <button className={page === 'office' ? 'active' : ''} onClick={() => setPage('office')} aria-label={zh ? '办公工作台' : 'Office Studio'}><NavIcon name="office" /><span>{zh ? '办公工作台' : 'Office Studio'}</span></button> : null}
          {officeMenu.automation ? <button className={page === 'automation' ? 'active' : ''} onClick={() => setPage('automation')} aria-label={zh ? '自动化' : 'Automation'}><NavIcon name="automation" /><span>{zh ? '自动化' : 'Automation'}</span></button> : null}
          {officeMenu.media ? <button className={page === 'media' ? 'active' : ''} onClick={() => setPage('media')} aria-label={zh ? '媒体中心' : 'Media Center'}><NavIcon name="media" /><span>{zh ? '媒体中心' : 'Media Center'}</span></button> : null}
          {officeMenu.people ? <button className={page === 'people' ? 'active' : ''} onClick={() => onOpenPeople('chats')} aria-label={zh ? '同事聊天' : 'Colleague chat'}><NavIcon name="people" /><span>{zh ? '同事聊天' : 'Colleague chat'}</span>{peopleUnread > 0 ? <em className="people-unread" aria-label={zh ? `${peopleUnread} 条未读` : `${peopleUnread} unread`}>{peopleUnread > 99 ? '99+' : peopleUnread}</em> : null}</button> : null}
          {officeMenu.mro && mroEnabled ? <button className={page === 'mro' ? 'active' : ''} onClick={() => setPage('mro')} aria-label={zh ? '机务工作台' : 'MRO workbench'}><NavIcon name="mro" /><span>{zh ? '机务工作台' : 'MRO workbench'}</span></button> : null}
          {officeMenu.meetings ? <button className={page === 'meetings' ? 'active' : ''} onClick={() => setPage('meetings')} aria-label={zh ? '会议记录' : 'Meeting notes'}><NavIcon name="meetings" /><span>{zh ? '会议记录' : 'Meeting notes'}</span></button> : null}
          {officeMenu.productHub ? <button className={page === 'productHub' ? 'active' : ''} onClick={() => setPage('productHub')} aria-label={zh ? '产品总览' : 'Product Hub'}><NavIcon name="productHub" /><span>{zh ? '产品总览' : 'Product Hub'}</span></button> : null}
        </div>
      ) : null}
    </section>
  )
  return (
    <aside id="launch-sidebar" className={`launch-sidebar ${open ? 'drawer-open' : ''} ${topSlot ? 'has-shell-head' : ''}`} aria-label={zh ? '主导航' : 'Main navigation'} aria-hidden={collapsed && !open ? true : undefined}>
      <div className="primary-actions">
        {topSlot}
        {replaceMainNav ? null : <button className="new-chat" onClick={onNew}><NavIcon name="new" /><span>{zh ? '新对话' : 'New chat'}</span><kbd>Ctrl N</kbd></button>}
        {replaceMainNav ? null : <button onClick={() => setSearchOpen(true)}><NavIcon name="search" /><span>{zh ? '搜索' : 'Search'}</span><kbd>Ctrl K</kbd></button>}
      </div>
      {replaceMainNav ? (
        <div className="hub-sidebar-stack" style={{ '--hub-agents-height': `${hubAgentsHeight}px` } as React.CSSProperties}>
          {replaceMainNav}
          <div className="panel-resizer sidebar-row-resizer" role="separator" aria-orientation="horizontal" aria-label={zh ? '调整 Agent 与项目' : 'Resize agents and projects'} onPointerDown={startHubAgentsResize} />
          <WorkspaceToolsNav page={page} setPage={setPage} zh={zh} />
        </div>
      ) : (
        <nav className="launch-nav" aria-label={zh ? '工作区' : 'Workspace'} style={{ '--sidebar-chats-height': `${chatsHeight}px` } as React.CSSProperties}>
          <section className={`conversation-group ${chatsOpen ? 'is-open' : 'is-closed'}${recent.length === 0 ? ' is-empty' : ''}`}>
            <div className="conversation-directory">
              <button className="conversation-heading" aria-expanded={chatsOpen} aria-controls="conversation-list" onClick={() => setChatsOpen(value => { const next = !value; writeSidebarFlag(SIDEBAR_CHATS_OPEN_KEY, next); return next })}>
                <span aria-hidden="true">›</span>{zh ? '对话' : 'Chats'}
              </button>
              {chatsOpen ? (
                <>
                  {actionError ? <p className="sidebar-action-error" role="alert">{actionError}</p> : null}
                  <div id="conversation-list" className="conversation-list">
                    {recent.length ? recent.map(item => {
                      const current = item.session.id === visibleDraftId
                      return (
                      <div className={`conversation-row ${item.session.pinned ? 'is-pinned' : ''} ${current ? 'is-current' : ''}`} key={item.session.id}>
                        <button className="conversation-open" onClick={() => onSelect(item)} title={localizedSessionTitle(item.session.title, zh)} aria-current={current ? 'true' : undefined} aria-busy={item.pending || liveSessionIds.includes(item.session.id) || undefined}>
                          {(item.pending || liveSessionIds.includes(item.session.id)) ? <span className="session-pending" aria-hidden="true" /> : null}
                          {item.session.pinned ? <span aria-hidden="true">⌃</span> : null}
                          {localizedSessionTitle(item.session.title, zh)}
                        </button>
                        <div className="conversation-actions">
                          <button className="conversation-more" aria-label={`${zh ? '更多操作' : 'More actions'} ${localizedSessionTitle(item.session.title, zh)}`} aria-haspopup="menu" aria-expanded={menuId === item.session.id} onClick={e => { e.stopPropagation(); setMenuId(id => id === item.session.id ? '' : item.session.id); setActionError('') }}>⋯</button>
                          {menuId === item.session.id ? (
                            <div className="conversation-menu" role="menu" onClick={e => e.stopPropagation()}>
                              <button role="menuitem" disabled={busy} onClick={() => void update(item, item.session.title, !item.session.pinned)}>{item.session.pinned ? (zh ? '取消置顶' : 'Unpin') : (zh ? '置顶' : 'Pin')}</button>
                              {!isProtectedSidebarChat(item.session.title) ? <button role="menuitem" disabled={busy} onClick={() => { setRenameTarget(item); setRenameTitle(item.session.title); setMenuId('') }}>{zh ? '重命名' : 'Rename'}</button> : null}
                              {!isProtectedSidebarChat(item.session.title) ? <button role="menuitem" className="danger-item" disabled={busy} onClick={() => { setDeleteTarget(item); setMenuId('') }}>{zh ? '删除' : 'Delete'}</button> : null}
                            </div>
                          ) : null}
                        </div>
                      </div>
                      )
                    }) : <p>{zh ? '还没有对话' : 'No chats yet'}</p>}
                  </div>
                </>
              ) : null}
            </div>
          </section>
          {recent.length > 0 ? <div className="panel-resizer sidebar-row-resizer" role="separator" aria-orientation="horizontal" aria-label={zh ? '调整对话与办公' : 'Resize chats and office'} onPointerDown={startChatsResize} /> : null}
          {officeList}
        </nav>
      )}
      <div className="launch-bottom">
        <button className={page === 'settings' || page === 'providers' ? 'active' : ''} onClick={() => setPage('settings')} aria-label={zh ? '设置' : 'Settings'}><NavIcon name="settings" /><span>{zh ? '设置' : 'Settings'}</span></button>
        <div className="account-controls">
          <button className="account-placeholder" onClick={() => onOpenPeople('me')} aria-label={zh ? '打开我的资料' : 'Open my profile'}><span className="account-avatar">{identity?.avatar ? <img src={identity.avatar} alt="" /> : initials(identity?.nickname || '月')}</span><b>{identity?.nickname || (zh ? '我' : 'Me')}</b></button>
          <button onClick={onToggleTheme} aria-label={theme === 'dark' ? (zh ? '切换到白天模式' : 'Switch to light mode') : (zh ? '切换到黑夜模式' : 'Switch to dark mode')} title={theme === 'dark' ? (zh ? '白天模式' : 'Light mode') : (zh ? '黑夜模式' : 'Dark mode')}>{theme === 'dark' ? '☀' : '☾'}</button>
          <button onClick={onToggleLanguage} aria-label={zh ? '切换到英文' : 'Switch to Chinese'}>{zh ? '中/EN' : 'EN/中'}</button>
        </div>
      </div>
      <Dialog open={searchOpen} title={zh ? '搜索历史对话' : 'Search chat history'} description={zh ? '按标题或消息内容查找普通对话' : 'Find ordinary chats by title or message content'} onClose={() => setSearchOpen(false)} initialFocus={searchInput}>
        <div className="conversation-search">
          <label>
            <span aria-hidden="true">⌕</span>
            <input ref={searchInput} value={searchQuery} onChange={e => setSearchQuery(e.target.value)} placeholder={zh ? '输入关键词、关键字或一段话' : 'Enter keywords or a phrase'} aria-label={zh ? '搜索对话标题和内容' : 'Search chat titles and content'} />
          </label>
          <div className="conversation-search-results">
            {searchError ? <p role="alert">{searchError}</p> : null}
            {searchBusy ? <p role="status">{zh ? '正在搜索…' : 'Searching…'}</p> : results.length ? results.map(({ item, snippet }) => (
              <button key={item.session.id} onClick={() => { setSearchOpen(false); onSelect(item) }}>
                <span aria-hidden="true">▱</span>
                <b>{localizedSessionTitle(item.session.title, zh)}</b>
                <small>{snippet ?? (zh ? '标题匹配' : 'Title match')}</small>
              </button>
            )) : <p>{zh ? '没有匹配的历史对话' : 'No matching chats'}</p>}
          </div>
        </div>
      </Dialog>
      <Dialog open={!!renameTarget} title={zh ? '重命名会话' : 'Rename chat'} onClose={() => { if (!busy) setRenameTarget(undefined) }}>
        <form onSubmit={e => { e.preventDefault(); const title = renameTitle.trim().replace(/\s+/g, ' '); if (renameTarget && title) void update(renameTarget, title, renameTarget.session.pinned) }}>
          <label>{zh ? '会话名称' : 'Chat name'}<input autoFocus value={renameTitle} maxLength={200} onChange={e => setRenameTitle(e.target.value)} /></label>
          <div className="dialog-actions">
            <button type="button" disabled={busy} onClick={() => setRenameTarget(undefined)}>{zh ? '取消' : 'Cancel'}</button>
            <button className="primary" disabled={busy || !renameTitle.trim()}>{busy ? (zh ? '保存中…' : 'Saving…') : (zh ? '保存' : 'Save')}</button>
          </div>
        </form>
      </Dialog>
      <ConfirmDialog open={!!deleteTarget} title={`${zh ? '删除会话' : 'Delete chat'}「${deleteTarget?.session.title ?? ''}」？`} description={zh ? '该会话及其消息将被永久删除，此操作不可撤销。' : 'This chat and its messages will be permanently deleted.'} busy={busy} onCancel={() => setDeleteTarget(undefined)} onConfirm={() => void remove()} />
    </aside>
  )
}
