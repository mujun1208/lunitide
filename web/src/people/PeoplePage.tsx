import React, { useEffect, useMemo, useRef, useState } from 'react'
import { getIdentityBridge, getPeopleBridge, type IdentityBridge, type PeopleBridge } from '../bridge/client'
import type { IdentityDTO, PeopleContactDTO, PeopleMessageDTO, PeopleThreadDTO } from '../generated/bridge'
import { ProfilePanel } from '../settings/ProfilePanel'
import { Dialog } from '../ui/Dialog'
import { captureThisPcFrame } from './peopleCapture'
import { PEOPLE_EMOJI, displayName, filterContacts, filterMessages, filterThreads, formatBytes, groupContactsByOrg, initials, lastPreview, relativeTime, statusLabel, threadTitle, trustLabel } from './peopleRoster'

const MAX_FILE = 32 * 1024 * 1024
const INLINE_MAX = 180 * 1024
const STAGE_CHUNK = 48 * 1024
type Rail = 'chats' | 'contacts' | 'me'

export function PeoplePage({
  identity = getIdentityBridge(),
  people = getPeopleBridge(),
  initialRail = 'chats',
}: {
  identity?: IdentityBridge
  people?: PeopleBridge
  initialRail?: Rail
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
  const [pendingSave, setPendingSave] = useState<PeopleMessageDTO>()
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const imageRef = useRef<HTMLInputElement>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const threadIdRef = useRef<string | undefined>(undefined)
  const sending = useRef(false)
  threadIdRef.current = thread?.threadId

  const refresh = async () => {
    const [profile, list, threadList] = await Promise.all([identity.get(), people.list(), people.threadList()])
    setMe(profile)
    setContacts(list.items)
    setThreads(threadList.items)
  }

  useEffect(() => { setRail(initialRail) }, [initialRail])
  useEffect(() => { void refresh().catch(e => setNotice(e instanceof Error ? e.message : '通讯录加载失败')) }, [identity, people])
  useEffect(() => { const el = scroller.current; if (el) el.scrollTop = el.scrollHeight }, [messages])
  useEffect(() => {
    const timer = window.setInterval(() => {
      void (async () => {
        try {
          const listed = await people.threadList()
          setThreads(listed.items)
          const id = threadIdRef.current
          if (id && rail !== 'me') {
            const opened = await people.threadOpen({ threadId: id })
            setThread(opened.thread)
            setMessages(opened.messages)
          }
          const list = await people.list()
          setContacts(list.items)
        } catch { /* poll is best-effort */ }
      })()
    }, 1500)
    return () => window.clearInterval(timer)
  }, [people, rail])
  useEffect(() => {
    if (!thread || !draft.trim()) return
    const timer = window.setTimeout(() => { void people.threadTyping({ threadId: thread.threadId }).catch(() => {}) }, 600)
    return () => window.clearTimeout(timer)
  }, [draft, thread, people])

  const visibleContacts = useMemo(() => filterContacts(contacts, rosterQuery), [contacts, rosterQuery])
  const groups = useMemo(() => groupContactsByOrg(visibleContacts), [visibleContacts])
  const visibleThreads = useMemo(() => filterThreads(threads, rosterQuery), [threads, rosterQuery])
  const visible = useMemo(() => filterMessages(messages, query), [messages, query])
  const trusted = contacts.filter(c => (c.trustState === 'trusted' || c.self) && !c.blocked)
  const typingNames = (thread?.typingSubjectIds ?? []).map(id => displayName(thread?.members.find(m => m.subjectId === id) ?? { nickname: '同事' }))
  const readHint = readReceipt(thread, me?.subjectId, messages)
  const peerMember = thread?.members.find(m => !m.self)

  const openPeer = async (peer: PeopleContactDTO) => {
    setCard(peer)
    setBusy(true)
    setNotice('')
    try {
      const opened = await people.threadOpen({ peerSubjectId: peer.subjectId })
      setThread(opened.thread)
      setMessages(opened.messages)
      await refresh()
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '无法打开会话')
    } finally {
      setBusy(false)
    }
  }

  const openThread = async (item: PeopleThreadDTO) => {
    setBusy(true)
    try {
      const opened = await people.threadOpen({ threadId: item.threadId })
      setThread(opened.thread)
      setMessages(opened.messages)
      setCard(opened.thread.members.find(m => !m.self))
      await refresh()
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '无法打开会话')
    } finally {
      setBusy(false)
    }
  }

  const send = async (kind: 'text' | 'emoji' | 'image' | 'file', body = draft, file?: File, localPath?: string, fileName?: string, fileMime?: string) => {
    const threadId = threadIdRef.current
    if (!threadId || sending.current) return
    sending.current = true
    setBusy(true)
    setNotice('')
    try {
      let payload: Parameters<PeopleBridge['threadSend']>[0] = { threadId, kind, body }
      if (localPath) {
        payload = { threadId, kind, fileName, fileMime, localPath }
      } else if (file) {
        const buf = new Uint8Array(await file.arrayBuffer())
        if (buf.length > MAX_FILE) throw new Error('文件需小于 32 MiB')
        const fileKind = kind === 'image' || file.type.startsWith('image/') ? 'image' : 'file'
        if (buf.length <= INLINE_MAX) {
          payload = { threadId, kind: fileKind, fileName: file.name, fileMime: file.type || fileMime, contentBase64: bytesToB64(buf) }
        } else {
          setNotice(`正在分片上传 ${file.name}…`)
          const staged = await stageBrowserFile(people, file)
          payload = { threadId, kind: fileKind, fileName: file.name, fileMime: file.type || fileMime, localPath: staged }
        }
      }
      const result = await people.threadSend(payload)
      setMessages(items => [...items, result.message])
      setDraft('')
      setEmojiOpen(false)
      await refresh()
      if (result.offer?.status === 'pending') setNotice(`已发出文件「${result.offer.fileName}」，对方必须确认后才会保存。`)
    } catch (e) {
      setNotice(e instanceof Error ? e.message : '发送失败')
    } finally {
      sending.current = false
      setBusy(false)
    }
  }

  const pickNative = async (folder: boolean) => {
    if (!thread || busy) return
    try {
      const picked = await people.filePick({ folder })
      const kind = !folder && picked.fileName.match(/\.(png|jpe?g|gif|webp|bmp)$/i) ? 'image' : 'file'
      await send(kind, '', undefined, picked.path, folder ? `${picked.fileName}.zip` : picked.fileName)
    } catch (e) {
      const msg = e instanceof Error ? e.message : '无法选择文件'
      if (/取消/.test(msg)) return
      setNotice(msg)
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
    if (!threadIdRef.current || sending.current) return
    try {
      const file = await captureThisPcFrame({ maxBytes: INLINE_MAX })
      await send('image', '', file)
    } catch (e) {
      const name = e instanceof DOMException ? e.name : ''
      if (name === 'AbortError' || name === 'NotAllowedError') return
      setNotice(e instanceof Error ? e.message : '无法截取本机画面')
    }
  }

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

  const title = threadTitle(thread ?? {}, '同事对话')
  const showThread = rail !== 'me' && thread

  return (
    <div className="people-shell" data-rail={rail}>
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
            {threads.length === 0 ? <p className="people-empty">还没有会话</p> : visibleThreads.length === 0 ? <p className="people-empty">没有匹配的会话</p> : visibleThreads.map(item => {
              const unread = item.unreadCount ?? 0
              return (
                <button type="button" key={item.threadId} className={`people-chat-row ${thread?.threadId === item.threadId ? 'on' : ''}`} onClick={() => { setRail('chats'); void openThread(item) }}>
                  <span className="people-ava">{item.kind === 'group' ? initials(threadTitle(item) || '群') : initials(threadTitle(item))}</span>
                  <span className="people-chat-copy">
                    <b>{threadTitle(item)}{unread > 0 ? <em className="people-unread">{unread > 99 ? '99+' : unread}</em> : null}</b>
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
            {groups.length === 0 ? <p className="people-empty">{contacts.length === 0 ? '还没有同事' : '没有匹配的同事'}</p> : groups.map(group => (
              <section key={group.key}>
                <h3>{group.label}</h3>
                {group.people.map(person => (
                  <ContactRow key={person.subjectId} person={person} active={card?.subjectId === person.subjectId} onOpen={() => void openPeer(person)} />
                ))}
              </section>
            ))}
          </>
        )}
        {rail === 'me' && (
          <div className="people-me-summary">
            <button type="button" className="people-ava lg" aria-hidden="true">{me?.avatar ? <img src={me.avatar} alt="" /> : initials(me?.nickname || '月')}</button>
            <h2>{me?.nickname || '月汐用户'}</h2>
            <p>{[me?.orgName, me?.department, me?.title].filter(Boolean).join(' · ') || '还没有填写组织信息'}</p>
            <small>{statusLabel(me?.status || 'online')} · {me?.discoveryEnabled ? '局域网可见' : '发现关闭'}</small>
          </div>
        )}
      </aside>

      <section className="people-thread" aria-label={rail === 'me' ? '个人资料' : '同事对话'}>
        {rail === 'me' ? (
          <ProfilePanel identity={identity} people={people} />
        ) : showThread ? (
          <>
            <header>
              <div>
                <h2>{title}</h2>
                <p>{thread.kind === 'group' ? `群主 ${displayName(thread.members.find(m => m.subjectId === thread.ownerSubjectId) ?? { nickname: '' })} · ${thread.members.map(m => displayName(m)).join('、')}` : `${statusLabel(peerMember?.status || card?.status || 'offline')} · 一对一 · 局域网投递，文件需对方确认`}</p>
              </div>
              <label className="people-search">搜索<input value={query} onChange={e => setQuery(e.target.value)} placeholder="本会话历史" aria-label="搜索本会话" /></label>
            </header>
            {card && !card.self && (
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
            <div ref={scroller} className="people-log">
              {visible.map(item => {
                const mine = item.senderSubjectId === me?.subjectId
                const sender = thread.members.find(m => m.subjectId === item.senderSubjectId)
                const acceptedImage = item.kind === 'image' && item.offerStatus === 'accepted' && item.destPath
                return (
                  <article key={item.messageId} className={`people-bubble ${mine ? 'mine' : 'peer'}`}>
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
                      </div>
                    )}
                  </article>
                )
              })}
              {typingNames.length > 0 && <p className="people-typing">{typingNames.join('、')}正在输入…</p>}
              {readHint && <p className="people-read">{readHint}</p>}
            </div>
            <form className="people-composer" onSubmit={e => { e.preventDefault(); if (draft.trim()) void send('text') }} onDragOver={e => e.preventDefault()} onDrop={e => {
              e.preventDefault()
              const file = e.dataTransfer.files?.[0]
              if (file) void send(file.type.startsWith('image/') ? 'image' : 'file', '', file)
            }} onPaste={e => {
              const file = [...e.clipboardData.files].find(f => f.type.startsWith('image/'))
              if (file) { e.preventDefault(); void send('image', '', file) }
            }}>
              <input ref={imageRef} hidden type="file" accept="image/*" onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) void send('image', '', file) }} />
              <div className="people-composer-tools">
                <button type="button" onClick={() => setEmojiOpen(v => !v)} aria-label="表情">☺</button>
                <button type="button" onClick={() => imageRef.current?.click()} aria-label="发送图片">🖼</button>
                <button type="button" onClick={() => void grabScreen()} aria-label="截取本机画面" title="只截取这台电脑的画面，不会把桌面共享给其他电脑">📷</button>
                <button type="button" onClick={() => void pickNative(false)} aria-label="发送本机文件" title="从这台电脑选择文件发送，需对方确认后才会保存">📎</button>
                <button type="button" onClick={() => void pickNative(true)} aria-label="发送文件夹" title="选择本机文件夹并打包为 zip 发送">📁</button>
              </div>
              {emojiOpen && <div className="people-emoji">{PEOPLE_EMOJI.map(item => <button type="button" key={item} onClick={() => void send('emoji', item)}>{item}</button>)}</div>}
              <textarea value={draft} onChange={e => setDraft(e.target.value)} placeholder="发消息、拖入文件或粘贴图片。文件需对方确认。" onKeyDown={e => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); if (draft.trim()) void send('text') } }} />
              <button className="primary" disabled={busy || !draft.trim()}>发送</button>
            </form>
          </>
        ) : rail === 'contacts' && card ? (
          <ContactCard person={card} me={me} pairCode={pairCode} setPairCode={setPairCode} onOpen={() => void openPeer(card)} onPair={() => void pairPerson(card)} onUpdate={async patch => {
            const updated = await people.contactUpdate({ subjectId: card.subjectId, ...patch })
            setCard(updated)
            await refresh()
          }} />
        ) : (
          <div className="people-blank">
            <h2>{rail === 'contacts' ? '选择一位同事' : '选择一个会话'}</h2>
            <p>像微信一样点开名片进入一对一。群聊需要先配对。BeeBEEP 式桌面共享和默认自动收文件不会做。</p>
          </div>
        )}
        {notice && <p className="people-notice" role="status">{notice}</p>}
      </section>

      {groupOpen && (
        <div className="people-modal" role="dialog" aria-label="创建群聊">
          <form onSubmit={e => { e.preventDefault(); void people.groupCreate({ title: groupTitle.trim(), ownerSubjectId: groupOwner || undefined, memberSubjectIds: groupMembers }).then(async created => { setGroupOpen(false); setGroupTitle(''); setRail('chats'); await refresh(); await openThread(created) }).catch(err => setNotice(err instanceof Error ? err.message : '无法建群')) }}>
            <h3>创建群聊</h3>
            <div className="people-group-hero">
              <span className="people-ava lg" aria-hidden="true">{initials(groupTitle || '群')}</span>
              <label>群名称<input value={groupTitle} maxLength={128} onChange={e => setGroupTitle(e.target.value)} placeholder="给群起个名字" /></label>
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
              <legend>成员（仅已配对）</legend>
              <div className="people-picker">
                {trusted.filter(p => !p.self).map(person => (
                  <label key={person.subjectId} className={`people-pick-row ${groupMembers.includes(person.subjectId) ? 'on' : ''}`}>
                    <input type="checkbox" checked={groupMembers.includes(person.subjectId)} onChange={e => setGroupMembers(ids => e.target.checked ? [...ids, person.subjectId] : ids.filter(id => id !== person.subjectId))} />
                    <ContactRow person={person} pick />
                  </label>
                ))}
                {trusted.filter(p => !p.self).length === 0 && <p>先配对同事，才能拉进群。</p>}
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
    </div>
  )
}

function ContactRow({ person, active, onOpen, pick }: { person: PeopleContactDTO; active?: boolean; onOpen?: () => void; pick?: boolean }): React.JSX.Element {
  const body = (
    <>
      <span className={`people-dot ${person.status}`} aria-hidden="true" />
      <span className="people-ava">{person.avatar ? <img src={person.avatar} alt="" /> : initials(displayName(person))}</span>
      <span>
        <b>{displayName(person, true)}{person.blocked ? <em className="people-blocked">已屏蔽</em> : null}</b>
        <small>{statusLabel(person.status)} · {trustLabel(person.trustState)}{person.hostAddr ? ` · ${person.hostAddr}` : ''}{person.title ? ` · ${person.title}` : ''}</small>
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

function ContactCard({ person, me, pairCode, setPairCode, onOpen, onPair, onUpdate }: {
  person: PeopleContactDTO
  me?: IdentityDTO
  pairCode: string
  setPairCode: (value: string) => void
  onOpen: () => void
  onPair: () => void
  onUpdate: (patch: { remark?: string; blocked?: boolean }) => Promise<void>
}): React.JSX.Element {
  const [remark, setRemark] = useState(person.remark ?? '')
  useEffect(() => { setRemark(person.remark ?? '') }, [person.subjectId, person.remark])
  return (
    <div className="people-card">
      <span className="people-ava lg">{person.avatar ? <img src={person.avatar} alt="" /> : initials(displayName(person))}</span>
      <h2>{displayName(person, true)}</h2>
      <p>{[person.orgName, person.department, person.title].filter(Boolean).join(' · ') || '未填写组织'}</p>
      <small>{statusLabel(person.status)} · {trustLabel(person.trustState)}{person.hostAddr ? ` · ${person.hostAddr}` : ''}</small>
      {person.bio && <p className="people-bio">{person.bio}</p>}
      {!person.self && (
        <form className="people-remark-form" onSubmit={e => { e.preventDefault(); void onUpdate({ remark }).catch(() => {}) }}>
          <label>备注<input value={remark} maxLength={64} aria-label="备注" onChange={e => setRemark(e.target.value)} /></label>
          <button type="submit">保存备注</button>
          <button type="button" onClick={() => void onUpdate({ blocked: !person.blocked })}>{person.blocked ? '取消屏蔽' : '屏蔽'}</button>
        </form>
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

function bytesToB64(bytes: Uint8Array): string {
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(binary)
}

function newUlid(): string {
  const alphabet = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'
  const extra = crypto.getRandomValues(new Uint8Array(10))
  let value = (BigInt(Date.now()) << 80n) | extra.reduce((n, x) => (n << 8n) | BigInt(x), 0n)
  let out = ''
  for (let i = 0; i < 26; i++) {
    out = alphabet[Number(value & 31n)] + out
    value >>= 5n
  }
  return out
}

async function stageBrowserFile(people: PeopleBridge, file: File): Promise<string> {
  const buf = new Uint8Array(await file.arrayBuffer())
  const uploadId = newUlid()
  let offset = 0
  let index = 0
  let localPath = ''
  while (offset < buf.length) {
    const end = Math.min(offset + STAGE_CHUNK, buf.length)
    const staged = await people.fileStage({
      uploadId, fileName: file.name, fileMime: file.type, index, last: end === buf.length,
      contentBase64: bytesToB64(buf.subarray(offset, end)),
    })
    localPath = staged.localPath
    offset = end
    index += 1
  }
  if (!localPath) throw new Error('文件分片上传未完成')
  return localPath
}
