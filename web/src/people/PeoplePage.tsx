import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { getFeedbackBridge, getIdentityBridge, getMemoryBridge, getPeopleBridge, type FeedbackBridge, type IdentityBridge, type MemoryBridge, type PeopleBridge } from '../bridge/client'
import type { IdentityDTO, PeopleContactDTO, PeopleMessageDTO, PeopleThreadDTO } from '../generated/bridge'
import { clipboardImages, normalizePastedImages } from '../session/attachments'
import { PendingMemoryBanner } from '../session/PendingMemoryBanner'
import { pickLatestPending, type PendingMemoryItem } from '../session/pendingMemory'
import { ProfilePanel } from '../settings/ProfilePanel'
import { Dialog } from '../ui/Dialog'
import { usePanelResize } from '../ui/usePanelResize'
import { captureThisPcFrame } from './peopleCapture'
import { ScreenCropOverlay } from './ScreenCropOverlay'
import { stageBrowserFile } from './peopleStage'
import { filterMentionMembers, insertMention, mentionQuery, parseClaimedTasks, pendingClaimTask } from './peopleMentions'
import { PEOPLE_EMOJI, contactAvatarIsImage, displayName, filterContacts, filterMessages, filterThreads, formatBytes, groupContactsByOrg, initials, isAgentContact, lastPreview, orgGroupCollapsed, peopleShowsOpenThread, relativeTime, resolveColleaguePeerId, shouldPinPeopleLog, shouldReloadOpenThread, statusLabel, threadHeading, threadPeer, threadTitle, trustLabel, visiblePeopleThreads } from './peopleRoster'

const STICK_PX = 48

const MAX_FILE = 32 * 1024 * 1024
const INLINE_MAX = 80 * 1024
type Rail = 'chats' | 'contacts' | 'me'

export function PeoplePage({
  identity = getIdentityBridge(),
  people = getPeopleBridge(),
  feedback = getFeedbackBridge(),
  memory = getMemoryBridge(),
  initialRail = 'chats',
  initialPeerSubjectId,
  initialPeerName,
  onOpenExpertCenter,
}: {
  identity?: IdentityBridge
  people?: PeopleBridge
  feedback?: FeedbackBridge
  memory?: MemoryBridge
  initialRail?: Rail
  initialPeerSubjectId?: string
  initialPeerName?: string
  onOpenExpertCenter?: (expertId: string) => void
}): React.JSX.Element {
  const [rail, setRail] = useState<Rail>(initialRail)
  const [me, setMe] = useState<IdentityDTO>()
  const [contacts, setContacts] = useState<PeopleContactDTO[]>([])
  const [threads, setThreads] = useState<PeopleThreadDTO[]>([])
  const [thread, setThread] = useState<PeopleThreadDTO>()
  const [card, setCard] = useState<PeopleContactDTO>()
  const [messages, setMessages] = useState<PeopleMessageDTO[]>([])
  const [draft, setDraft] = useState('')
  const [query, setQuery] = useState('')
  const [rosterQuery, setRosterQuery] = useState('')
  const [pairCode, setPairCode] = useState('')
  const [peerAddr, setPeerAddr] = useState('')
  const [groupOpen, setGroupOpen] = useState(false)
  const [groupTitle, setGroupTitle] = useState('')
  const [groupOwner, setGroupOwner] = useState('')
  const [groupMembers, setGroupMembers] = useState<string[]>([])
  const [emojiOpen, setEmojiOpen] = useState(false)
  const [cropFile, setCropFile] = useState<File>()
  const [pendingSave, setPendingSave] = useState<PeopleMessageDTO>()
  const [notice, setNotice] = useState('')
  const [noticeError, setNoticeError] = useState(false)
  const [busy, setBusy] = useState(false)
  const [membersOpen, setMembersOpen] = useState(false)
  const [memberQuery, setMemberQuery] = useState('')
  const [moreOpen, setMoreOpen] = useState(false)
  const [mentionHi, setMentionHi] = useState(0)
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({})
  const [midWidth, startMidResize] = usePanelResize({
    storageKey: 'lunitide:people-mid-width',
    initial: 300,
    min: 240,
    max: () => Math.min(480, Math.max(280, window.innerWidth - 420)),
  })
  const [pendingPref, setPendingPref] = useState<PendingMemoryItem>()
  const [prefBusy, setPrefBusy] = useState(false)
  const prefDismissedRef = useRef('')
  const [composerHeight, startComposerResize] = usePanelResize({
    storageKey: 'lunitide:people-composer-height',
    initial: 96,
    min: 56,
    max: () => Math.min(420, Math.max(160, window.innerHeight - 280)),
    axis: 'y',
    reverse: true,
  })
  const imageRef = useRef<HTMLInputElement>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const threadIdRef = useRef<string | undefined>(undefined)
  const sending = useRef(false)
  const cropOpenRef = useRef(false)
  const grabScreenRef = useRef<() => Promise<void>>(async () => {})
  const stickToBottomRef = useRef(true)
  const lastStickIdRef = useRef('')
  const messagesRef = useRef<PeopleMessageDTO[]>([])
  const openedPeerRef = useRef('')
  threadIdRef.current = thread?.threadId
  cropOpenRef.current = Boolean(cropFile)
  messagesRef.current = messages

  const showNotice = (message: string, error = false) => {
    setNotice(message)
    setNoticeError(Boolean(message) && error)
  }

  const refresh = async () => {
    const [profile, list, threadList] = await Promise.all([identity.get(), people.list(), people.threadList()])
    setMe(profile)
    setContacts(list.items)
    setThreads(threadList.items)
  }

  useEffect(() => { setRail(initialRail) }, [initialRail])
  useEffect(() => { void refresh().catch(e => showNotice(e instanceof Error ? e.message : '通讯录加载失败', true)) }, [identity, people])
  useEffect(() => {
    const raw = initialPeerSubjectId?.trim()
    if (!raw || openedPeerRef.current === raw) return
    let alive = true
    void (async () => {
      try {
        const list = await people.list()
        if (!alive) return
        setContacts(list.items)
        const resolved = resolveColleaguePeerId(list.items, raw, initialPeerName)
        const peerId = resolved || raw
        if (list.items.some(item => item.self && item.subjectId === peerId)) {
          openedPeerRef.current = raw
          showNotice('不能和自己开会话', true)
          return
        }
        setRail('chats')
        setBusy(true)
        showNotice('')
        const opened = await people.threadOpen({ peerSubjectId: peerId })
        if (!alive) return
        openedPeerRef.current = raw
        stickToBottomRef.current = true
        setThread(opened.thread)
        setMessages(opened.messages)
        setCard(threadPeer(opened.thread.members, me?.subjectId) ?? opened.thread.members.find(m => !m.self))
        await refresh()
      } catch (e) {
        if (alive) showNotice(e instanceof Error ? e.message : '无法打开专家会话', true)
      } finally {
        if (alive) setBusy(false)
      }
    })()
    return () => { alive = false }
  }, [initialPeerSubjectId, initialPeerName, people])
  useEffect(() => {
    const el = scroller.current
    if (!el) return
    const nextLastId = messages.at(-1)?.messageId ?? ''
    if (!shouldPinPeopleLog({ stickToBottom: stickToBottomRef.current, previousLastId: lastStickIdRef.current, nextLastId })) return
    lastStickIdRef.current = nextLastId
    el.scrollTop = el.scrollHeight
  }, [messages])
  useEffect(() => {
    const tick = async () => {
      if (typeof document !== 'undefined' && document.hidden) return
      const listed = await people.threadList()
      setThreads(listed.items)
      const id = threadIdRef.current
      if (id && rail !== 'me') {
        const item = listed.items.find(row => row.threadId === id)
        if (item) setThread(current => current && current.threadId === id ? { ...current, ...item } : current)
        if (shouldReloadOpenThread({
          stickToBottom: stickToBottomRef.current,
          listedLastId: item?.lastMessage?.messageId,
          localLastId: messagesRef.current.at(-1)?.messageId,
        })) {
          const opened = await people.threadOpen({ threadId: id })
          setThread(opened.thread)
          setMessages(opened.messages)
        }
      }
      const list = await people.list()
      setContacts(list.items)
    }
    const timer = window.setInterval(() => { void tick().catch(() => {}) }, 1500)
    const onVis = () => { if (typeof document !== 'undefined' && !document.hidden) void tick().catch(() => {}) }
    document.addEventListener('visibilitychange', onVis)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [people, rail])
  useEffect(() => {
    if (!thread || !draft.trim()) return
    const timer = window.setTimeout(() => { void people.threadTyping({ threadId: thread.threadId }).catch(() => {}) }, 600)
    return () => window.clearTimeout(timer)
  }, [draft, thread, people])

  const loadPendingPref = useCallback(async () => {
    try {
      const r = await feedback.candidates({ limit: 3 })
      setPendingPref(pickLatestPending(r.items, prefDismissedRef.current))
    } catch {
      setPendingPref(undefined)
    }
  }, [feedback])
  useEffect(() => {
    void loadPendingPref()
    const timer = window.setTimeout(() => { void loadPendingPref() }, 500)
    return () => window.clearTimeout(timer)
  }, [loadPendingPref, messages.length])
  const decidePref = async (action: 'confirm' | 'later') => {
    if (!pendingPref) return
    if (action === 'later') {
      prefDismissedRef.current = pendingPref.candidateId
      setPendingPref(undefined)
      return
    }
    if (!memory.confirmCandidate || prefBusy) return
    setPrefBusy(true)
    try {
      await memory.confirmCandidate({ candidateId: pendingPref.candidateId, confirmationToken: pendingPref.confirmationToken, action: 'confirm', requestId: `people-${Date.now()}` })
      prefDismissedRef.current = pendingPref.candidateId
      setPendingPref(undefined)
      showNotice('已确认，后续对话会遵守这条偏好')
    } catch (e) {
      showNotice(e instanceof Error ? e.message : '确认偏好失败', true)
    } finally {
      setPrefBusy(false)
    }
  }

  const visibleContacts = useMemo(() => filterContacts(contacts, rosterQuery), [contacts, rosterQuery])
  const groups = useMemo(() => groupContactsByOrg(visibleContacts), [visibleContacts])
  const listedThreads = useMemo(() => visiblePeopleThreads(threads, me?.subjectId), [threads, me?.subjectId])
  const visibleThreads = useMemo(() => filterThreads(listedThreads, rosterQuery), [listedThreads, rosterQuery])
  const visible = useMemo(() => filterMessages(messages, query), [messages, query])
  const trusted = contacts.filter(c => (c.trustState === 'trusted' || c.self) && !c.blocked)
  const threadAgents = (thread?.members ?? []).filter(member => isAgentContact(member) && !member.self && member.subjectId !== me?.subjectId && !member.blocked)
  const mentionable = (thread?.members ?? []).filter(member => !member.self && member.subjectId !== me?.subjectId && !member.blocked)
  const mentionNeedle = mentionQuery(draft)
  const mentionHits = mentionNeedle === null ? [] : filterMentionMembers(mentionable.map(item => ({ subjectId: item.subjectId, nickname: displayName(item), avatar: item.avatar })), mentionNeedle)
  const pendingTask = pendingClaimTask([...messages].reverse().find(item => item.senderSubjectId === me?.subjectId && item.kind === 'text')?.body ?? '')
  const claimedTasks = parseClaimedTasks(messages.map(item => item.body || ''))
  const typingNames = (thread?.typingSubjectIds ?? []).map(id => displayName(thread?.members.find(m => m.subjectId === id) ?? { nickname: '同事' }))
  const readHint = readReceipt(thread, me?.subjectId, messages)
  const peerMember = threadPeer(thread?.members, me?.subjectId)
  const searchingRoster = rosterQuery.trim().length > 0
  const drawerMembers = mentionable.filter(member => {
    const q = memberQuery.trim().toLowerCase()
    return !q || displayName(member).toLowerCase().includes(q) || member.nickname.toLowerCase().includes(q)
  })

  const openPeer = async (peer: PeopleContactDTO) => {
    setCard(peer)
    if (peer.self) return
    setBusy(true)
    showNotice('')
    try {
      const opened = await people.threadOpen({ peerSubjectId: peer.subjectId })
      stickToBottomRef.current = true
      setThread(opened.thread)
      setMessages(opened.messages)
      await refresh()
    } catch (e) {
      showNotice(e instanceof Error ? e.message : '无法打开会话', true)
    } finally {
      setBusy(false)
    }
  }

  const openThread = async (item: PeopleThreadDTO) => {
    setBusy(true)
    try {
      const opened = await people.threadOpen({ threadId: item.threadId })
      stickToBottomRef.current = true
      setThread(opened.thread)
      setMessages(opened.messages)
      setCard(threadPeer(opened.thread.members, me?.subjectId))
      setMembersOpen(false)
      await refresh()
    } catch (e) {
      showNotice(e instanceof Error ? e.message : '无法打开会话', true)
    } finally {
      setBusy(false)
    }
  }

  const send = async (kind: 'text' | 'emoji' | 'image' | 'file', body = draft, file?: File, localPath?: string, fileName?: string, fileMime?: string) => {
    const threadId = threadIdRef.current
    if (!threadId) {
      showNotice('请先打开会话', true)
      return
    }
    if (sending.current) {
      showNotice('正在发送上一条消息，请稍候', true)
      return
    }
    sending.current = true
    setBusy(true)
    showNotice('')
    try {
      let payload: Parameters<PeopleBridge['threadSend']>[0] = { threadId, kind, body }
      if (localPath) {
        payload = { threadId, kind, fileName, fileMime, localPath }
      } else if (file) {
        const nativePath = (file as File & { path?: string }).path
        const fileKind = kind === 'image' || file.type.startsWith('image/') ? 'image' : 'file'
        if (nativePath) {
          payload = { threadId, kind: fileKind, fileName: file.name || fileName, fileMime: file.type || fileMime, localPath: nativePath }
        } else {
          if (file.size > INLINE_MAX) showNotice(`正在读取 ${file.name}…`)
          const buf = new Uint8Array(await readBrowserFile(file))
          if (buf.length > MAX_FILE) throw new Error('文件需小于 32 MiB')
          if (buf.length <= INLINE_MAX) {
            payload = { threadId, kind: fileKind, fileName: file.name, fileMime: file.type || fileMime, contentBase64: bytesToB64(buf) }
          } else {
            const staged = await stageBrowserFile(people, file, buf, (percent, chunk, total) => {
              showNotice(`正在分片上传 ${file.name}… ${percent}%（${chunk}/${total}）`)
            })
            payload = { threadId, kind: fileKind, fileName: file.name, fileMime: file.type || fileMime, localPath: staged }
          }
        }
      }
      const result = await people.threadSend(payload)
      stickToBottomRef.current = true
      setMessages(items => [...items, result.message])
      setDraft('')
      setEmojiOpen(false)
      await refresh()
      if (result.offer?.status === 'pending') showNotice(`已发出文件「${result.offer.fileName}」，对方必须确认后才会保存。`)
    } catch (e) {
      showNotice(e instanceof Error ? e.message : '发送失败', true)
    } finally {
      sending.current = false
      setBusy(false)
    }
  }

  const pickNative = async (folder: boolean) => {
    if (!thread) {
      showNotice('请先打开会话', true)
      return
    }
    if (sending.current) {
      showNotice('正在发送上一条消息，请稍候', true)
      return
    }
    try {
      const picked = await people.filePick({ folder })
      const kind = !folder && picked.fileName.match(/\.(png|jpe?g|gif|webp|bmp)$/i) ? 'image' : 'file'
      await send(kind, '', undefined, picked.path, folder ? `${picked.fileName}.zip` : picked.fileName)
    } catch (e) {
      const msg = e instanceof Error ? e.message : '无法选择文件'
      if (/取消/.test(msg)) return
      showNotice(msg, true)
    }
  }

  const decide = async (offerId: string, accept: boolean) => {
    try {
      const offer = await people.fileDecide({ offerId, accept })
      setMessages(items => items.map(item => item.offerId === offerId ? { ...item, offerStatus: offer.status, destPath: offer.destPath } : item))
      setPendingSave(undefined)
      setNotice(accept ? `已接收到本机收件夹：${offer.destPath || offer.fileName}` : `已拒绝 ${offer.fileName}`)
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '无法确认文件')
    }
  }

  const grabScreen = async () => {
    if (!threadIdRef.current) {
      showNotice('请先打开会话', true)
      return
    }
    if (sending.current) {
      showNotice('正在发送上一条消息，请稍候', true)
      return
    }
    if (cropOpenRef.current) return
    showNotice('拖动鼠标框选要发送的区域…')
    try {
      const shot = await captureThisPcFrame({
        maxBytes: 512 * 1024,
        nativeCapture: () => people.screenCapture({ region: true }),
      })
      if (shot.source === 'display') {
        setCropFile(shot.file)
        showNotice('拖动框选要发送的区域，Enter 发送，Esc 取消')
        return
      }
      await send('image', '', shot.file)
    } catch (e) {
      const name = e instanceof DOMException ? e.name : ''
      if (name === 'AbortError' || name === 'NotAllowedError') {
        showNotice('')
        return
      }
      const code = e && typeof e === 'object' && 'code' in e ? String((e as { code?: string }).code ?? '') : ''
      if (code === 'PEOPLE_CANCELED' || (e instanceof Error && /取消/.test(e.message))) {
        showNotice('')
        return
      }
      showNotice(e instanceof Error ? e.message : '无法截取本机画面', true)
    }
  }
  grabScreenRef.current = grabScreen

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.repeat || event.ctrlKey || event.metaKey || event.shiftKey || !event.altKey) return
      if (event.code !== 'KeyA' && event.key.toLowerCase() !== 'a') return
      if (rail === 'me' || !threadIdRef.current) return
      event.preventDefault()
      void grabScreenRef.current()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [rail])

  const pairPerson = async (peer: PeopleContactDTO) => {
    try {
      await people.pair({ pairingCode: pairCode, subjectId: peer.subjectId, nickname: peer.nickname })
      setPairCode('')
      await refresh()
      setNotice(`已与 ${displayName(peer)} 配对`)
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '配对失败')
    }
  }

  const addPeer = async () => {
    const hostAddr = peerAddr.trim()
    if (!hostAddr) return
    setBusy(true)
    try {
      const added = await people.peerAdd({ hostAddr })
      setPeerAddr('')
      setCard(added)
      await refresh()
      setNotice(`已添加 ${displayName(added)}${added.hostAddr ? ` · ${added.hostAddr}` : ''}，配对后才能发消息。`)
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '无法添加该地址')
    } finally {
      setBusy(false)
    }
  }

  const title = threadHeading(thread ?? {}, '同事对话', me?.subjectId)
  const showThread = peopleShowsOpenThread(rail, thread, card)

  return (
    <div className="people-shell" data-rail={rail} style={{ '--people-mid-width': `${midWidth}px` } as React.CSSProperties}>
      <nav className="people-rail" aria-label="同事工作区">
        <button type="button" className={rail === 'chats' ? 'on' : ''} aria-current={rail === 'chats'} onClick={() => setRail('chats')}>
          <span aria-hidden="true">💬</span>聊天
        </button>
        <button type="button" className={rail === 'contacts' ? 'on' : ''} aria-current={rail === 'contacts'} onClick={() => setRail('contacts')}>
          <span aria-hidden="true">📒</span>通讯录
        </button>
        <button type="button" className={rail === 'me' ? 'on' : ''} aria-current={rail === 'me'} onClick={() => setRail('me')}>
          <span aria-hidden="true">☺</span>我
        </button>
      </nav>

      <aside className="people-mid" aria-label={rail === 'chats' ? '会话列表' : rail === 'contacts' ? '组织通讯录' : '资料摘要'}>
        {rail === 'chats' && (
          <>
            <header>
              <h2>聊天</h2>
              <button type="button" className="people-create-group" onClick={() => { setGroupOpen(true); setGroupOwner(me?.subjectId ?? ''); setGroupMembers([]) }}>＋ 创建群聊</button>
            </header>
            <label className="people-search people-mid-search">搜索会话<input value={rosterQuery} onChange={e => setRosterQuery(e.target.value)} placeholder="昵称或最近一条" aria-label="搜索会话" /></label>
            {listedThreads.length === 0 ? <p className="people-empty">还没有会话</p> : visibleThreads.length === 0 ? <p className="people-empty">没有匹配的会话</p> : visibleThreads.map(item => {
              const unread = item.unreadCount ?? 0
              return (
                <button type="button" key={item.threadId} className={`people-chat-row ${thread?.threadId === item.threadId ? 'on' : ''}`} onClick={() => { setRail('chats'); void openThread(item) }}>
                  <ThreadAvatar thread={item} selfId={me?.subjectId} />
                  <span className="people-chat-copy">
                    <b>{threadHeading(item, '同事对话', me?.subjectId)}{unread > 0 ? <em className="people-unread">{unread > 99 ? '99+' : unread}</em> : null}</b>
                    <small>{item.typingSubjectIds?.length ? '正在输入…' : lastPreview(item.lastMessage?.kind, item.lastMessage?.body, item.lastMessage?.fileName) || (item.kind === 'group' ? `${item.members.length} 人` : '一对一')}</small>
                  </span>
                  <time>{relativeTime(item.lastMessage?.createdAt || item.updatedAt)}</time>
                </button>
              )
            })}
          </>
        )}
        {rail === 'contacts' && (
          <>
            <header>
              <h2>通讯录</h2>
              <p>按每人填写的组织/部门自动成树。发现默认关闭。</p>
            </header>
            <label className="people-search people-mid-search">搜索同事<input value={rosterQuery} onChange={e => setRosterQuery(e.target.value)} placeholder="昵称、部门、组织" aria-label="搜索同事" /></label>
            <form className="people-ip-form" onSubmit={e => { e.preventDefault(); void addPeer() }}>
              <label>对方地址<input value={peerAddr} onChange={e => setPeerAddr(e.target.value)} placeholder="10.0.0.8 或 host:36422" aria-label="对方地址" /></label>
              <button type="submit" disabled={busy || !peerAddr.trim()}>添加</button>
            </form>
            <button type="button" disabled={busy} onClick={() => void people.discoverySet({ enabled: !me?.discoveryEnabled }).then(d => setMe(cur => cur ? { ...cur, discoveryEnabled: d.enabled } : cur)).catch(e => setNotice(e instanceof Error ? e.message : '无法切换发现'))}>
              {me?.discoveryEnabled ? '发现开' : '发现关'}
            </button>
            {groups.length === 0 ? <p className="people-empty">{contacts.length === 0 ? '还没有同事' : '没有匹配的同事'}</p> : groups.map(group => {
              const collapsed = orgGroupCollapsed(collapsedGroups[group.key], group.people.length, searchingRoster)
              return (
                <section key={group.key}>
                  <button type="button" className="people-group-head" aria-expanded={!collapsed} onClick={() => setCollapsedGroups(curr => ({ ...curr, [group.key]: !collapsed }))}>
                    <span aria-hidden="true">{collapsed ? '▸' : '▾'}</span>
                    {group.label}
                    <small>{group.people.length}</small>
                  </button>
                  {collapsed ? null : group.people.map(person => (
                    <ContactRow key={person.subjectId} person={person} active={card?.subjectId === person.subjectId} onOpen={() => void openPeer(person)} />
                  ))}
                </section>
              )
            })}
          </>
        )}
        {rail === 'me' && (
          <div className="people-me-summary">
            <span className="people-ava" aria-hidden="true">{me?.avatar ? <img src={me.avatar} alt="" /> : initials(me?.nickname || '月')}</span>
            <div className="people-me-copy">
              <h2>{me?.nickname || 'Lunitide user'}</h2>
              <p>{[me?.orgName, me?.department, me?.title].filter(Boolean).join(' · ') || '还没有填写组织信息'}</p>
              <small><span className={`people-dot ${me?.status || 'online'}`} aria-hidden="true" />{statusLabel(me?.status || 'online')} · {me?.discoveryEnabled ? '局域网可见' : '发现关闭'}</small>
            </div>
          </div>
        )}
      </aside>
      <div className="panel-resizer split-resizer" role="separator" aria-label="调整同事列表宽度" aria-orientation="vertical" onPointerDown={startMidResize} />

      <section className="people-thread" aria-label={rail === 'me' ? '个人资料' : '同事对话'}>
        {rail === 'me' ? (
          <ProfilePanel identity={identity} people={people} />
        ) : showThread && thread ? (
          <>
            <header>
              <div className="people-thread-ident">
                {thread.kind === 'group' ? (
                  <button type="button" className="people-title-btn" onClick={() => setMembersOpen(open => !open)} aria-expanded={membersOpen} aria-label="查看群成员">
                    <ThreadAvatar thread={thread} selfId={me?.subjectId} />
                    <span>
                      <h2>{title}</h2>
                      <p>{`群主 ${displayName(thread.members.find(m => m.subjectId === thread.ownerSubjectId) ?? { nickname: '' })} · ${thread.members.length} 人 · 只有被 @ 的同事会开口`}</p>
                    </span>
                  </button>
                ) : (
                  <div className="people-title-btn">
                    <ThreadAvatar thread={thread} selfId={me?.subjectId} />
                    <span>
                      <h2>{title}</h2>
                      <p>{`${statusLabel(peerMember?.status || card?.status || 'offline')} · 一对一 · 局域网投递，文件需对方确认`}</p>
                    </span>
                  </div>
                )}
              </div>
              <div className="people-thread-tools">
                {card && !card.self ? <button type="button" onClick={() => setMoreOpen(open => !open)}>{moreOpen ? '收起' : '更多'}</button> : null}
                <label className="people-search">搜索<input value={query} onChange={e => setQuery(e.target.value)} placeholder="本会话历史" aria-label="搜索本会话" /></label>
              </div>
            </header>
            {pendingPref ? <PendingMemoryBanner item={pendingPref} busy={prefBusy} onConfirm={() => void decidePref('confirm')} onLater={() => void decidePref('later')} /> : null}
            {thread.kind === 'group' && membersOpen && (
              <div className="people-member-drawer">
                <label className="people-search">搜索群成员<input value={memberQuery} onChange={e => setMemberQuery(e.target.value)} placeholder="昵称或备注" aria-label="搜索群成员" /></label>
                <div className="people-member-grid">
                  {drawerMembers.map(member => (
                    <button type="button" key={member.subjectId} onClick={() => { setDraft(value => `${value}${value && !/\s$/.test(value) ? ' ' : ''}@${displayName(member)} `); setMembersOpen(false) }}>
                      <PeopleFace person={member} />
                      <small>{displayName(member)}</small>
                    </button>
                  ))}
                </div>
              </div>
            )}
            {moreOpen && card && !card.self && (
              <form className="people-pair-bar people-remark-bar" onSubmit={e => { e.preventDefault(); void people.contactUpdate({ subjectId: card.subjectId, remark: card.remark ?? '' }).then(updated => { setCard(updated); void refresh() }).catch(err => setNotice(err instanceof Error ? err.message : '无法更新备注')) }}>
                <label>备注<input value={card.remark ?? ''} maxLength={64} aria-label="备注" onChange={e => setCard(cur => cur ? { ...cur, remark: e.target.value } : cur)} /></label>
                <button type="submit">保存备注</button>
                <button type="button" onClick={() => void people.contactUpdate({ subjectId: card.subjectId, blocked: !card.blocked }).then(updated => { setCard(updated); void refresh() }).catch(err => setNotice(err instanceof Error ? err.message : '无法更新屏蔽'))}>{card.blocked ? '取消屏蔽' : '屏蔽'}</button>
              </form>
            )}
            {card?.trustState === 'discovered' && (
              <form className="people-pair-bar" onSubmit={e => { e.preventDefault(); void pairPerson(card) }}>
                <span>未配对，不能进群。输入对方 6 位配对码：</span>
                <input value={pairCode} maxLength={6} inputMode="numeric" aria-label="配对码" onChange={e => setPairCode(e.target.value.replace(/\D/g, ''))} />
                <button className="primary" disabled={pairCode.length !== 6}>配对</button>
              </form>
            )}
            <div ref={scroller} className="people-log" onScroll={() => {
              const el = scroller.current
              if (!el) return
              stickToBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight <= STICK_PX
            }}>
              {visible.map(item => {
                if (item.kind === 'system') {
                  return (
                    <article key={item.messageId} className="people-bubble-row people-system-row">
                      <p className="people-system">{item.body}</p>
                    </article>
                  )
                }
                const mine = item.senderSubjectId === me?.subjectId
                const sender = thread.members.find(m => m.subjectId === item.senderSubjectId)
                const acceptedImage = item.kind === 'image' && item.offerStatus === 'accepted' && item.destPath
                return (
                  <article key={item.messageId} className={`people-bubble-row ${mine ? 'mine' : 'peer'}`}>
                    {mine ? null : <PeopleFace person={sender} />}
                    <div className={`people-bubble ${mine ? 'mine' : 'peer'}`}>
                    <small>{sender ? displayName(sender) : item.senderSubjectId}</small>
                    {acceptedImage ? <img src={`file://${item.destPath}`} alt={item.fileName} /> : null}
                    {item.kind === 'emoji' || item.kind === 'text' ? <p>{item.body}</p> : null}
                    {(item.kind === 'file' || item.kind === 'image') && (
                      <div className="people-file">
                        <b>{item.fileName || '文件'}</b>
                        <small>{fileCaption(item, mine)}</small>
                        {item.offerId && item.offerStatus === 'pending' && !mine && (
                          <div className="people-file-actions">
                            <button type="button" className="primary" onClick={() => setPendingSave(item)}>接收到本机</button>
                            <button type="button" onClick={() => void decide(item.offerId!, false)}>拒绝</button>
                          </div>
                        )}
                        {item.destPath && (item.offerStatus === 'accepted' || mine) && (
                          <div className="people-file-actions">
                            <button type="button" onClick={() => void people.fileOpen({ destPath: item.destPath! }).catch(err => showNotice(err instanceof Error ? err.message : '无法打开文件', true))}>打开</button>
                          </div>
                        )}
                      </div>
                    )}
                    </div>
                  </article>
                )
              })}
              {typingNames.length > 0 && <p className="people-typing">{typingNames.join('、')}正在输入…</p>}
              {readHint && <p className="people-read">{readHint}</p>}
            </div>
            <div className="panel-resizer people-composer-resizer" role="separator" aria-label="调整输入框高度" aria-orientation="horizontal" onPointerDown={startComposerResize} />
            <form className="people-composer" style={{ '--people-composer-height': `${composerHeight}px` } as React.CSSProperties} onSubmit={e => { e.preventDefault(); if (draft.trim()) void send('text') }} onDragOver={e => e.preventDefault()} onDrop={e => {
              e.preventDefault()
              const file = e.dataTransfer.files?.[0]
              if (file) void send(file.type.startsWith('image/') ? 'image' : 'file', '', file)
            }} onPaste={e => {
              const images = normalizePastedImages(clipboardImages(e.clipboardData))
              if (images[0]) { e.preventDefault(); void send('image', '', images[0]) }
            }}>
              <input ref={imageRef} hidden type="file" accept="image/*" onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) void send('image', '', file) }} />
              <div className="people-composer-tools">
                <button type="button" onClick={() => setEmojiOpen(v => !v)} aria-label="表情">☺</button>
                <button type="button" className="people-snip-btn" onClick={() => void grabScreen()} aria-label="框选截图" title="框选截图（Alt+A），像微信一样拖选区域后发送。Esc / 右键取消。">
                  <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
                    <rect x="3.5" y="3.5" width="17" height="17" rx="2" fill="none" stroke="currentColor" strokeWidth="1.8" strokeDasharray="3.5 2.5" />
                  </svg>
                </button>
                <button type="button" onClick={() => imageRef.current?.click()} aria-label="发送图片">🖼</button>
                <button type="button" onClick={() => void pickNative(false)} aria-label="发送本机文件" title="从这台电脑选择文件发送，需对方确认后才会保存">📎</button>
                <button type="button" onClick={() => void pickNative(true)} aria-label="发送文件夹" title="选择本机文件夹并打包为 zip 发送">📁</button>
              </div>
              {emojiOpen && <div className="people-emoji">{PEOPLE_EMOJI.map(item => <button type="button" key={item} onClick={() => { setDraft(d => d + item); setEmojiOpen(false) }}>{item}</button>)}</div>}
              {mentionNeedle !== null && (
                <div className="people-mention-list" role="listbox" aria-label="选择要@的人">
                  {mentionHits.length === 0 ? <p className="people-mention-empty">这个群还没有可 @ 的人</p> : mentionHits.map((member, index) => (
                    <button type="button" key={member.subjectId} role="option" aria-selected={index === mentionHi} className={index === mentionHi ? 'on' : ''} onClick={() => setDraft(value => insertMention(value, member.nickname))}>
                      <PeopleFace person={member} />
                      {member.nickname}
                    </button>
                  ))}
                </div>
              )}
              {(pendingTask || claimedTasks.length > 0) && <div className="people-claim-bar" role="status">
                {claimedTasks.map(item => <small key={item.task}>已认领 {item.task} → {item.owner}</small>)}
                {pendingTask && threadAgents[0] ? <button type="button" onClick={() => { setDraft(value => `${value}${value && !value.endsWith(' ') ? ' ' : ''}@${displayName(threadAgents[0])} 认领 ${pendingTask}`); }}>{`认领 ${pendingTask}`}</button> : null}
              </div>}
              <textarea value={draft} onChange={e => { setDraft(e.target.value); setMentionHi(0) }} placeholder={thread?.kind === 'group' ? '群里 @同事 才会回复。认领：@报告编写专家 认领 周报' : '发消息、拖入文件或粘贴图片。文件需对方确认。'} onKeyDown={e => {
                if (mentionNeedle !== null && mentionHits.length > 0) {
                  if (e.key === 'ArrowDown') { e.preventDefault(); setMentionHi(i => (i + 1) % mentionHits.length); return }
                  if (e.key === 'ArrowUp') { e.preventDefault(); setMentionHi(i => (i - 1 + mentionHits.length) % mentionHits.length); return }
                  if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); setDraft(value => insertMention(value, mentionHits[mentionHi]?.nickname ?? mentionHits[0].nickname)); return }
                }
                if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); if (draft.trim()) void send('text') }
              }} />
              <button className="primary" disabled={busy || !draft.trim()}>发送</button>
            </form>
          </>
        ) : rail === 'contacts' && card ? (
          <ContactCard person={card} me={me} pairCode={pairCode} setPairCode={setPairCode} recentThreads={threads.filter(item => item.members.some(member => member.subjectId === card.subjectId))} onOpenExpertCenter={isAgentContact(card) ? onOpenExpertCenter : undefined} onOpen={() => void openPeer(card)} onPair={() => void pairPerson(card)} onUpdate={async patch => {
            const updated = await people.contactUpdate({ subjectId: card.subjectId, ...patch })
            setCard(updated)
            await refresh()
          }} />
        ) : (
          <div className="people-blank">
            <h2>{rail === 'contacts' ? '选择一位同事' : '选择一个会话'}</h2>
            <p>像微信一样点开名片进入一对一。群聊需要已配对同事或同事专家。同事专家按岗位做事，跑在同一月汐引擎上，不是独立进程。生成的文件写在本机工作区或桌面。BeeBEEP 式桌面共享和默认自动收文件不会做。</p>
          </div>
        )}
        {notice && <p className={`people-notice${noticeError ? ' is-error' : ''}`} role={noticeError ? 'alert' : 'status'}>{notice}</p>}
      </section>

      {groupOpen && (
        <div className="people-modal" role="dialog" aria-label="创建群聊">
          <form onSubmit={e => { e.preventDefault(); void people.groupCreate({ title: groupTitle.trim(), ownerSubjectId: groupOwner || undefined, memberSubjectIds: groupMembers }).then(async created => { setGroupOpen(false); setGroupTitle(''); setRail('chats'); await refresh(); await openThread(created) }).catch(err => setNotice(err instanceof Error ? err.message : '无法建群')) }}>
            <h3>创建群聊</h3>
            <div className="people-group-hero">
              <span className="people-ava lg" aria-hidden="true">{initials(groupTitle || '群')}</span>
              <label>群名称<input value={groupTitle} maxLength={128} onChange={e => setGroupTitle(e.target.value)} placeholder="例如：项目评审" /></label>
            </div>
            <fieldset>
              <legend>群主（点选一人，默认为我）</legend>
              <div className="people-picker">
                {trusted.map(person => (
                  <ContactRow key={`owner-${person.subjectId}`} person={person} active={groupOwner === person.subjectId} onOpen={() => setGroupOwner(person.subjectId)} />
                ))}
              </div>
            </fieldset>
            <fieldset>
              <legend>成员（已配对同事或同事专家）</legend>
              <div className="people-picker">
                {trusted.filter(p => !p.self).map(person => (
                  <label key={person.subjectId} className={`people-pick-row ${groupMembers.includes(person.subjectId) ? 'on' : ''}`}>
                    <input type="checkbox" checked={groupMembers.includes(person.subjectId)} onChange={e => setGroupMembers(ids => e.target.checked ? [...ids, person.subjectId] : ids.filter(id => id !== person.subjectId))} />
                    <ContactRow person={person} pick />
                  </label>
                ))}
                {trusted.filter(p => !p.self).length === 0 && <p>把已配对同事或同事专家拉进群。</p>}
              </div>
            </fieldset>
            <div className="dialog-actions">
              <button type="button" onClick={() => setGroupOpen(false)}>取消</button>
              <button className="primary" disabled={!groupTitle.trim()}>创建</button>
            </div>
          </form>
        </div>
      )}

      <Dialog
        open={!!pendingSave}
        title="确认保存到本机？"
        description="局域网文件不会自动保存。确认后才会写入本机收件夹。电脑控制不会共享给对方。"
        onClose={() => setPendingSave(undefined)}
      >
        {pendingSave && (
          <>
            <p className="people-file-confirm">
              <b>{pendingSave.fileName || '文件'}</b>
              <small>{[formatBytes(pendingSave.fileSize), displayName(thread?.members.find(m => m.subjectId === pendingSave.senderSubjectId) ?? { nickname: '同事' })].filter(Boolean).join(' · ')}</small>
            </p>
            <div className="dialog-actions">
              <button type="button" onClick={() => setPendingSave(undefined)}>取消</button>
              <button type="button" onClick={() => void decide(pendingSave.offerId!, false)}>拒绝</button>
              <button type="button" className="primary" onClick={() => void decide(pendingSave.offerId!, true)}>确认保存到本机</button>
            </div>
          </>
        )}
      </Dialog>
      {cropFile ? (
        <ScreenCropOverlay
          file={cropFile}
          onCancel={() => { setCropFile(undefined); showNotice('') }}
          onConfirm={file => {
            setCropFile(undefined)
            void send('image', '', file)
          }}
        />
      ) : null}
    </div>
  )
}

function PeopleFace({ person, className = 'people-ava' }: { person?: { nickname: string; avatar?: string; remark?: string; self?: boolean }; className?: string }): React.JSX.Element {
  const name = person ? displayName(person) : '月'
  return (
    <span className={className} aria-hidden="true">
      {person?.avatar ? (contactAvatarIsImage(person.avatar) ? <img src={person.avatar} alt="" /> : person.avatar) : initials(name)}
    </span>
  )
}

function ThreadAvatar({ thread, selfId }: { thread: PeopleThreadDTO; selfId?: string }): React.JSX.Element {
  if (thread.kind === 'group') {
    const faces = (thread.members ?? []).filter(member => !member.self && member.subjectId !== selfId).slice(0, 4)
    const cells = faces.length ? faces : (thread.members ?? []).slice(0, 1)
    return (
      <span className="people-ava people-ava-mosaic" data-count={Math.max(1, cells.length)} aria-hidden="true">
        {cells.map(member => (
          <span key={member.subjectId}>{member.avatar && !contactAvatarIsImage(member.avatar) ? member.avatar : initials(displayName(member))}</span>
        ))}
        {(thread.members?.length ?? 0) > 0 ? <em className="people-ava-badge">{thread.members.length}</em> : null}
      </span>
    )
  }
  return <PeopleFace person={threadPeer(thread.members, selfId)} />
}

function ContactRow({ person, active, onOpen, pick }: { person: PeopleContactDTO; active?: boolean; onOpen?: () => void; pick?: boolean }): React.JSX.Element {
  const body = (
    <>
      <span className={`people-dot ${person.status}`} aria-hidden="true" />
      <span className="people-ava">{person.avatar ? (contactAvatarIsImage(person.avatar) ? <img src={person.avatar} alt="" /> : person.avatar) : initials(displayName(person))}</span>
      <span>
        <b>{displayName(person, true)}{person.blocked ? <em className="people-blocked">已屏蔽</em> : null}</b>
        <small>{statusLabel(person.status)} · {trustLabel(person.trustState, person.orgName)}{person.hostAddr ? ` · ${person.hostAddr}` : ''}{person.title ? ` · ${person.title}` : ''}</small>
      </span>
    </>
  )
  if (pick) return <span className="people-contact-row pick">{body}</span>
  return (
    <button type="button" className={`people-contact-row ${active ? 'on' : ''}`} onClick={onOpen}>
      {body}
    </button>
  )
}

function ContactCard({ person, me, pairCode, setPairCode, onOpen, onPair, onUpdate, recentThreads = [], onOpenExpertCenter }: {
  person: PeopleContactDTO
  me?: IdentityDTO
  pairCode: string
  setPairCode: (value: string) => void
  onOpen: () => void
  onPair: () => void
  onUpdate: (patch: { remark?: string; blocked?: boolean }) => Promise<void>
  recentThreads?: PeopleThreadDTO[]
  onOpenExpertCenter?: (expertId: string) => void
}): React.JSX.Element {
  const [remark, setRemark] = useState(person.remark ?? '')
  useEffect(() => { setRemark(person.remark ?? '') }, [person.subjectId, person.remark])
  return (
    <div className="people-card">
      <span className="people-ava lg">{person.avatar ? (contactAvatarIsImage(person.avatar) ? <img src={person.avatar} alt="" /> : person.avatar) : initials(displayName(person))}</span>
      <h2>{displayName(person, true)}</h2>
      <p>{[person.orgName, person.department, person.title].filter(Boolean).join(' · ') || '未填写组织'}</p>
      <small>{statusLabel(person.status)} · {trustLabel(person.trustState, person.orgName)}{person.hostAddr ? ` · ${person.hostAddr}` : ''}</small>
      {person.bio && <p className="people-bio">{person.bio}</p>}
      {!person.self && (
        <form className="people-remark-form" onSubmit={e => { e.preventDefault(); void onUpdate({ remark }).catch(() => {}) }}>
          <label>备注<input value={remark} maxLength={64} aria-label="备注" onChange={e => setRemark(e.target.value)} /></label>
          <button type="submit">保存备注</button>
          <button type="button" onClick={() => void onUpdate({ blocked: !person.blocked })}>{person.blocked ? '取消屏蔽' : '屏蔽'}</button>
        </form>
      )}
      {isAgentContact(person) && (
        <div className="people-expert-home">
          <p>同事专家主页。同一月汐引擎，人设和工具不同，不是独立进程。技能包装专家不会出现在花名册。</p>
          {onOpenExpertCenter ? <button type="button" onClick={() => onOpenExpertCenter(person.subjectId)}>打开专家中心</button> : null}
          {recentThreads.length > 0 ? <ul>{recentThreads.slice(0, 5).map(item => <li key={item.threadId}>{threadTitle(item, displayName(person))}</li>)}</ul> : <small>还没有一起开过的房间</small>}
        </div>
      )}
      {!person.self && !person.blocked && <button type="button" className="primary" onClick={onOpen}>发消息</button>}
      {person.trustState === 'discovered' && (
        <form onSubmit={e => { e.preventDefault(); onPair() }}>
          <p>输入对方 6 位配对码。未配对不能进群，文件永远要确认。</p>
          <input value={pairCode} maxLength={6} inputMode="numeric" aria-label="配对码" onChange={e => setPairCode(e.target.value.replace(/\D/g, ''))} />
          <button className="primary" disabled={pairCode.length !== 6}>确认配对</button>
        </form>
      )}
      {me?.subjectId === person.subjectId && <p className="setting-desc">电脑控制仍然只作用于这台电脑，不会把桌面分享给同事。</p>}
    </div>
  )
}

function fileCaption(item: PeopleMessageDTO, mine: boolean): string {
  if (item.transferPercent && item.transferPercent < 100) return `传输 ${item.transferPercent}%`
  if (item.offerStatus === 'pending') return mine ? '已发出，等待对方确认' : '等待确认，不会自动保存'
  if (item.offerStatus === 'accepted') return item.destPath || '已接收'
  if (item.offerStatus === 'rejected') return '已拒绝'
  return '已发出'
}

function readReceipt(thread: PeopleThreadDTO | undefined, selfId: string | undefined, messages: PeopleMessageDTO[]): string {
  if (!thread || thread.kind !== 'direct' || !selfId) return ''
  const peer = thread.members.find(m => !m.self)
  const lastMine = [...messages].reverse().find(m => m.senderSubjectId === selfId)
  if (!peer?.lastReadAt || !lastMine) return ''
  return peer.lastReadAt >= lastMine.createdAt ? '已读' : '未读'
}

function readBrowserFile(file: File): Promise<ArrayBuffer> {
  if (typeof file.arrayBuffer === 'function') return file.arrayBuffer()
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as ArrayBuffer)
    reader.onerror = () => reject(reader.error ?? new Error('读取文件失败'))
    reader.readAsArrayBuffer(file)
  })
}

function bytesToB64(bytes: Uint8Array): string {
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(binary)
}
