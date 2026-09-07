// Development-only fixture. This entry is excluded from release build inputs.
import { createRoot } from 'react-dom/client'
import type { MeetingsBridge } from '../bridge/client'
import type { MeetingDTO } from '../generated/bridge'
import { MeetingPage } from './MeetingPage'
import '../styles.css'

const query = new URLSearchParams(location.search)
document.documentElement.dataset.theme = query.get('theme') === 'light' ? 'light' : 'dark'
const now = '2026-09-07T01:00:00.000Z'
const meeting: MeetingDTO = {
  meetingId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', title: '产品体验与发布评审', revision: 2,
  status: 'ready', audioSource: 'microphone_and_system', startedAt: now, endedAt: now,
  durationMs: 2714000, createdAt: now, updatedAt: now, transcriptRevision: 1,
  summarySourceRevision: 1, summarySourceDigest: 'a'.repeat(64), transcriptComplete: true,
  summary: '本次会议围绕产品体验、现有功能恢复与发布安排展开讨论。\n\n一、体验与稳定性\n保留现有产品逻辑，优先修复截图发送、对话恢复与会议录音问题。\n\n二、会议纪要\n同时收录系统播放的声音和麦克风，保留完整录音、逐字稿与摘要来源。\n\n三、发布安排\n完成自动化验证后，再进行真实设备验收。',
  actions: '□ 验证截图发送、点击预览和历史消息打开\n□ 验证系统声音与麦克风同时录制\n□ 复核月伴长期对话及历史记录\n□ 完成黑色与白色主题下的布局检查',
  transcript: '主持人：我们先确认这次改造的原则，原有功能必须保留。\n\n同事甲：截图发出后，需要在聊天中直接点击查看，发送失败也不能影响后续聊天。\n\n同事乙：会议录音需要把电脑里播放的声音一起收录，会议、课程和音视频都要覆盖。\n\n主持人：好的。布局可以优化，但录音、转写、人工编辑和导出这些能力都要保留。',
  segments: [], docs: [],
}
const meetings: MeetingsBridge = {
  list: async () => ({ items: query.has('empty') ? [] : [meeting, { ...meeting, meetingId: '01ARZ3NDEKTSV4RRFFQ69G5FAW', title: '设计方案讨论', durationMs: 1162000 }] }),
  get: async () => meeting,
  start: async () => { throw new Error('布局预览不录音') },
  append: async () => { throw new Error('布局预览不录音') },
  audioAppend: async () => { throw new Error('布局预览不录音') },
  loopbackPoll: async () => ({ meetingId: meeting.meetingId, pcm: "", active: false }),
  heartbeat: async () => meeting,
  stop: async () => meeting,
  catchup: async () => meeting,
  summarize: async () => meeting,
  update: async payload => ({ ...meeting, ...payload, revision: meeting.revision + 1 }),
  delete: async ({ meetingId }) => ({ meetingId }),
  exportMeeting: async () => { throw new Error('布局预览不导出文件') },
  summarySource: async ({ offset = 0 }) => ({ meetingId: meeting.meetingId, sourceDigest: meeting.summarySourceDigest!, sourceRevision: 1, title: meeting.title, transcript: meeting.transcript, offset, totalRunes: Array.from(meeting.transcript).length, nextOffset: 0 }),
  transcriptGet: async () => { throw new Error('预览逐字稿已完整载入') },
  segmentsList: async () => ({ meetingId: meeting.meetingId, items: [], nextSeq: 0, hasMore: false, throughSeq: 0, revision: meeting.revision }),
}
createRoot(document.getElementById('root')!).render(<div style={{ height: '100vh' }}><MeetingPage meetings={meetings} /></div>)
if (!query.has('empty')) {
  let tries = 0
  const select = window.setInterval(() => {
    const row = [...document.querySelectorAll<HTMLButtonElement>('.meeting-list button')].find(button => button.textContent?.includes(meeting.title))
    if (row) row.click()
    if (row || ++tries > 40) window.clearInterval(select)
  }, 50)
}
