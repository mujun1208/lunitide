import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'
import type { IdentityBridge, PeopleBridge } from '../bridge/client'
import type { IdentityDTO, PeopleContactDTO, PeopleMessageDTO, PeopleThreadDTO } from '../generated/bridge'
import { captureThisPcFrame } from './peopleCapture'
import { PeoplePage } from './PeoplePage'

vi.mock('./peopleCapture', () => ({
  captureThisPcFrame: vi.fn(),
}))

const now = '2026-08-27T00:00:00.000Z'
const me: IdentityDTO = {
  subjectId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', nickname: 'mu', avatar: '', status: 'online',
  department: '研发', title: '工程师', orgName: '月汐', bio: '', publicKey: 'aa'.repeat(32),
  pairingCode: '111111', passwordSet: false, locked: false, discoveryEnabled: true,
  createdAt: now, updatedAt: now,
}
const peer: PeopleContactDTO = {
  subjectId: '01ARZ3NDEKTSV4RRFFQ69G5FAW', nickname: '同事甲', avatar: '', status: 'online',
  department: '设计', title: '', orgName: '月汐', bio: '', publicKey: '', trustState: 'trusted',
  hostAddr: '10.0.0.8', lastSeenAt: now, self: false,
}
const selfContact: PeopleContactDTO = { ...peer, subjectId: me.subjectId, nickname: 'mu', department: '研发', trustState: 'self', hostAddr: '', self: true }
const fileMsg: PeopleMessageDTO = {
  messageId: '01ARZ3NDEKTSV4RRFFQ69G5FAY', threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAX', senderSubjectId: peer.subjectId,
  kind: 'file', body: '', fileName: 'secret.txt', offerId: '01ARZ3NDEKTSV4RRFFQ69G5FAZ',
  offerStatus: 'pending', fileSize: 4, createdAt: now,
}
const thread: PeopleThreadDTO = {
  threadId: '01ARZ3NDEKTSV4RRFFQ69G5FAX', kind: 'direct', title: '', ownerSubjectId: me.subjectId,
  members: [selfContact, peer], unreadCount: 2, lastMessage: fileMsg, createdAt: now, updatedAt: now,
}

function bridges(decide = vi.fn()) {
  const identity: IdentityBridge = { get: vi.fn().mockResolvedValue(me), update: vi.fn(), passwordSet: vi.fn(), unlock: vi.fn() }
  const people: PeopleBridge = {
    list: vi.fn().mockResolvedValue({ items: [selfContact, peer] }),
    pair: vi.fn(),
    discoveryGet: vi.fn().mockResolvedValue({ enabled: true, pairingCode: '111111' }),
    discoverySet: vi.fn(),
    threadList: vi.fn().mockResolvedValue({ items: [thread] }),
    threadOpen: vi.fn().mockResolvedValue({ thread, messages: [fileMsg] }),
    threadSend: vi.fn(),
    threadTyping: vi.fn(),
    groupCreate: vi.fn(),
    fileDecide: decide,
    fileStage: vi.fn(),
    filePick: vi.fn(),
    peerAdd: vi.fn(),
    contactUpdate: vi.fn(),
  }
  return { identity, people, decide }
}

describe('PeoplePage', () => {
  afterEach(() => {
    cleanup()
    vi.mocked(captureThisPcFrame).mockReset()
  })
  test('chat list shows last preview and unread without mixing the org tree', async () => {
    const { identity, people } = bridges()
    render(<PeoplePage identity={identity} people={people} />)
    expect(await screen.findByRole('heading', { name: '聊天' })).toBeInTheDocument()
    expect(screen.getByText('[文件] secret.txt')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
    expect(screen.queryByText('月汐 · 设计')).not.toBeInTheDocument()
  })

  test('contacts tree, file confirm, person-picker groups, and 我 share ProfilePanel', async () => {
    const decide = vi.fn().mockResolvedValue({
      ...fileMsg, status: 'accepted', destPath: 'C:/inbox/secret.txt', offerId: fileMsg.offerId,
      messageId: fileMsg.messageId, threadId: thread.threadId, fromSubjectId: peer.subjectId,
      toSubjectId: me.subjectId, fileName: 'secret.txt', fileSize: 4, createdAt: now,
    })
    const { identity, people } = bridges(decide)
    const user = userEvent.setup()
    render(<PeoplePage identity={identity} people={people} />)
    await screen.findByRole('heading', { name: '聊天' })
    expect(screen.getByRole('separator', { name: '调整同事列表宽度' })).toBeInTheDocument()

    const rail = screen.getByRole('navigation', { name: '同事工作区' })
    await user.click(within(rail).getByRole('button', { name: /通讯录/ }))
    expect(await screen.findByText('月汐 · 设计')).toBeInTheDocument()
    expect(screen.getByText('月汐 · 研发')).toBeInTheDocument()
    await user.click(screen.getAllByRole('button', { name: /同事甲/ })[0])
    expect(await screen.findByText('等待确认，不会自动保存')).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: 'secret.txt' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '接收到本机' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '接收到本机' }))
    expect(decide).not.toHaveBeenCalled()
    const sheet = screen.getByRole('dialog', { name: '确认保存到本机？' })
    expect(within(sheet).getByText(/局域网文件不会自动保存/)).toBeInTheDocument()
    await user.click(within(sheet).getByRole('button', { name: '取消' }))
    expect(decide).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: '拒绝' }))
    expect(decide).toHaveBeenCalledWith({ offerId: fileMsg.offerId, accept: false })

    await user.click(within(rail).getByRole('button', { name: /聊天/ }))
    await user.click(screen.getByRole('button', { name: '＋ 创建群聊' }))
    expect(screen.getByRole('dialog', { name: '创建群聊' })).toBeInTheDocument()
    expect(document.querySelector('select')).toBeNull()
    expect(screen.getByText('群主（点选一人，默认为我）')).toBeInTheDocument()
    expect(within(screen.getByRole('dialog', { name: '创建群聊' })).getAllByRole('button', { name: /mu/ }).length).toBeGreaterThan(0)
    await user.click(screen.getByRole('button', { name: '取消' }))

    await user.click(within(rail).getByRole('button', { name: /我/ }))
    expect(await screen.findByText('让同网段的月汐看见我')).toBeInTheDocument()
    expect(screen.getByDisplayValue(me.subjectId)).toBeInTheDocument()
    expect(screen.queryByText(/private/i)).not.toBeInTheDocument()
  })

  test('adds a peer by IP and can remark or block from the open chat', async () => {
    const { identity, people } = bridges()
    people.peerAdd = vi.fn().mockResolvedValue(peer)
    people.contactUpdate = vi.fn().mockResolvedValue({ ...peer, remark: '阿甲' })
    const user = userEvent.setup()
    render(<PeoplePage identity={identity} people={people} />)
    const rail = screen.getByRole('navigation', { name: '同事工作区' })
    await user.click(within(rail).getByRole('button', { name: /通讯录/ }))
    await user.type(await screen.findByLabelText('对方地址'), '10.0.0.8')
    await user.click(screen.getByRole('button', { name: '添加' }))
    expect(people.peerAdd).toHaveBeenCalledWith({ hostAddr: '10.0.0.8' })
    await user.click(screen.getAllByRole('button', { name: /同事甲/ })[0])
    expect(await screen.findByLabelText('备注')).toBeInTheDocument()
    await user.type(screen.getByLabelText('备注'), '阿甲')
    await user.click(screen.getByRole('button', { name: '保存备注' }))
    expect(people.contactUpdate).toHaveBeenCalledWith({ subjectId: peer.subjectId, remark: '阿甲' })
    expect(screen.getByText(/一对一 · 局域网投递，文件需对方确认/)).toBeInTheDocument()
  })

  test('file accept does not call decide until the confirm sheet is submitted', async () => {
    const decide = vi.fn().mockResolvedValue({
      ...fileMsg, status: 'accepted', destPath: 'C:/inbox/secret.txt', offerId: fileMsg.offerId,
      messageId: fileMsg.messageId, threadId: thread.threadId, fromSubjectId: peer.subjectId,
      toSubjectId: me.subjectId, fileName: 'secret.txt', fileSize: 4, createdAt: now,
    })
    const { identity, people } = bridges(decide)
    const user = userEvent.setup()
    render(<PeoplePage identity={identity} people={people} />)
    await user.click((await screen.findAllByRole('button', { name: /同事甲/ }))[0])
    await screen.findByText('等待确认，不会自动保存')
    await user.click(screen.getByRole('button', { name: '接收到本机' }))
    expect(decide).not.toHaveBeenCalled()
    const sheet = screen.getByRole('dialog', { name: '确认保存到本机？' })
    expect(within(sheet).getByText('secret.txt')).toBeInTheDocument()
    await user.click(within(sheet).getByRole('button', { name: '确认保存到本机' }))
    expect(decide).toHaveBeenCalledWith({ offerId: fileMsg.offerId, accept: true })
  })

  test('contacts search filters the org tree', async () => {
    const { identity, people } = bridges()
    const user = userEvent.setup()
    render(<PeoplePage identity={identity} people={people} />)
    const rail = screen.getByRole('navigation', { name: '同事工作区' })
    await user.click(within(rail).getByRole('button', { name: /通讯录/ }))
    await screen.findByText('月汐 · 设计')
    await user.type(screen.getByLabelText('搜索同事'), '设计')
    expect(screen.getByText('月汐 · 设计')).toBeInTheDocument()
    expect(screen.queryByText('月汐 · 研发')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /mu（我）/ })).not.toBeInTheDocument()
  })

  test('composer can send a this-PC screenshot as an image', async () => {
    const bytes = Uint8Array.from([1, 2, 3])
    const shot = new File([bytes], 'screenshot.jpg', { type: 'image/jpeg' })
    Object.defineProperty(shot, 'arrayBuffer', { value: async () => bytes.buffer.slice(0) })
    vi.mocked(captureThisPcFrame).mockResolvedValue(shot)
    const { identity, people } = bridges()
    people.threadSend = vi.fn().mockResolvedValue({
      message: { ...fileMsg, messageId: '01ARZ3NDEKTSV4RRFFQ69G5FB0', kind: 'image', fileName: 'screenshot.jpg', offerId: undefined, offerStatus: undefined },
    })
    const user = userEvent.setup()
    render(<PeoplePage identity={identity} people={people} />)
    await user.click((await screen.findAllByRole('button', { name: /同事甲/ }))[0])
    expect(await screen.findByRole('button', { name: '发送图片' })).toBeInTheDocument()
    expect(await screen.findByText(/一对一 · 局域网投递，文件需对方确认/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '截取本机画面' }))
    expect(captureThisPcFrame).toHaveBeenCalled()
    await vi.waitFor(() => expect(people.threadSend).toHaveBeenCalledWith(expect.objectContaining({ kind: 'image', fileName: 'screenshot.jpg' })))
  })

  test('folder send uses the native picker and asks the engine to zip', async () => {
    const { identity, people } = bridges()
    people.filePick = vi.fn().mockResolvedValue({ path: 'C:/docs', fileName: 'docs' })
    people.threadSend = vi.fn().mockResolvedValue({
      message: { ...fileMsg, messageId: '01ARZ3NDEKTSV4RRFFQ69G5FB1', kind: 'file', fileName: 'docs.zip', offerStatus: 'pending' },
    })
    const user = userEvent.setup()
    render(<PeoplePage identity={identity} people={people} />)
    await user.click((await screen.findAllByRole('button', { name: /同事甲/ }))[0])
    const folderBtn = await screen.findByRole('button', { name: '发送文件夹' })
    expect(folderBtn).toHaveAttribute('title', '选择本机文件夹并打包为 zip 发送')
    await user.click(folderBtn)
    expect(people.filePick).toHaveBeenCalledWith({ folder: true })
    await vi.waitFor(() => expect(people.threadSend).toHaveBeenCalledWith(expect.objectContaining({
      kind: 'file', localPath: 'C:/docs', fileName: 'docs.zip',
    })))
  })

  test('paperclip sends a native this-PC file without auto-accepting inbound offers', async () => {
    const { identity, people } = bridges()
    people.filePick = vi.fn().mockResolvedValue({ path: 'C:/docs/spec.pdf', fileName: 'spec.pdf' })
    people.threadSend = vi.fn().mockResolvedValue({
      message: { ...fileMsg, messageId: '01ARZ3NDEKTSV4RRFFQ69G5FB2', kind: 'file', fileName: 'spec.pdf', offerStatus: 'pending' },
    })
    const user = userEvent.setup()
    render(<PeoplePage identity={identity} people={people} />)
    await user.click((await screen.findAllByRole('button', { name: /同事甲/ }))[0])
    await user.click(await screen.findByRole('button', { name: '发送本机文件' }))
    expect(people.filePick).toHaveBeenCalledWith({ folder: false })
    await vi.waitFor(() => expect(people.threadSend).toHaveBeenCalledWith(expect.objectContaining({
      kind: 'file', localPath: 'C:/docs/spec.pdf', fileName: 'spec.pdf',
    })))
  })

  test('pastes a clipboard screenshot into the composer and sends it', async () => {
    const { identity, people } = bridges()
    people.threadSend = vi.fn().mockResolvedValue({
      message: { ...fileMsg, messageId: '01ARZ3NDEKTSV4RRFFQ69G5FB3', kind: 'image', fileName: 'clipboard.png', offerStatus: 'pending' },
    })
    const user = userEvent.setup()
    render(<PeoplePage identity={identity} people={people} />)
    await user.click((await screen.findAllByRole('button', { name: /同事甲/ }))[0])
    const composer = await screen.findByPlaceholderText(/粘贴图片/)
    const file = new File([new Uint8Array([9, 8, 7])], 'image.png', { type: 'image/png' })
    await user.click(composer)
    await user.paste({
      getData: () => '',
      files: [file],
      items: [{ kind: 'file', type: 'image/png', getAsFile: () => file }],
      types: ['Files'],
    } as unknown as DataTransfer)
    await vi.waitFor(() => expect(people.threadSend).toHaveBeenCalledWith(expect.objectContaining({ kind: 'image' })))
    const payload = vi.mocked(people.threadSend).mock.calls[0][0]
    expect(payload.localPath || payload.contentBase64).toBeTruthy()
  })
})
