// Audit reproductions: PASS means the defect was observed, not fixed.
// No microphone, network, or speech model is used.
import { afterEach, expect, test, vi } from 'vitest'
const mock = vi.hoisted(() => ({
  start: vi.fn(), append: vi.fn(), finish: vi.fn(), stop: vi.fn(),
  captureStop: vi.fn().mockResolvedValue(undefined), captureFlush: vi.fn(),
  emit: undefined as undefined | ((f: {base64: string; samples: Int16Array; peak: number}) => void),
}))
vi.mock('../../../../web/src/bridge/client', () => ({
 getVoiceBridge: () => mock,
 BridgeClientError: class extends Error {},
 getProviderBridge: vi.fn(), getTalkBridge: vi.fn(),
}))
vi.mock('../../../../web/src/session/companion/ttsPlayer', () => ({unlockTtsAudio:async()=>{}}))
vi.mock('../../../../web/src/session/companion/pcmCapture', () => ({
 startPcmCapture: async (opts: {onFrame: typeof mock.emit}) => {
  mock.emit = opts.onFrame
  return {stop:mock.captureStop,flush:mock.captureFlush,setMuted:vi.fn(),attachExtraStream:vi.fn()}
 },
}))
import { startLocalAsr } from '../../../../web/src/session/companion/localAsr'
import { startMeetingAudioRecorder } from '../../../../web/src/meetings/meetingAudio'
import { startCompanionTalk } from '../../../../web/src/session/companion/companionTalk'

afterEach(() => { vi.clearAllMocks(); vi.useRealTimers() })

test('reproduce local ASR startup backlog exceeding Go 32000-byte frame limit', async () => {
 let open!: (v:{sessionId:string})=>void
 mock.start.mockImplementation(() => new Promise(resolve => {open=resolve}))
 mock.append.mockResolvedValue({text:'',final:false})
 mock.stop.mockResolvedValue({})
 const opening = startLocalAsr()
 await Promise.resolve()
 for(let i=0;i<15;i++) mock.emit?.({base64:'AAAA',samples:new Int16Array(1600),peak:0.2})
 open({sessionId:'audit'})
 const handle = await opening
 expect(mock.append).toHaveBeenCalledTimes(1)
 const raw = atob(mock.append.mock.calls[0][0].pcm)
 expect(raw.length).toBe(48000)
 expect(raw.length).toBeGreaterThan(32000)
 handle.cancel()
})

test('reproduce recorder stop leaving microphone open while an append hangs', async () => {
 vi.useFakeTimers()
 const append = vi.fn(() => new Promise(()=>{}))
 const handle = await startMeetingAudioRecorder({append})
 for(let i=0;i<12;i++) mock.emit?.({base64:'AAAA',samples:new Int16Array(1600),peak:0.2})
 const stopping=handle.stop()
 await vi.advanceTimersByTimeAsync(10000)
 expect(mock.captureStop).not.toHaveBeenCalled()
 await vi.advanceTimersByTimeAsync(110000)
 await stopping
 expect(mock.captureStop).toHaveBeenCalledTimes(1)
})

test('reproduce realtime ended event leaving its microphone alive', async () => {
 let event!: (e:any)=>void
 const end=vi.fn()
 const cancel=vi.fn().mockResolvedValue(undefined)
 const handle=await startCompanionTalk({
  sessionId:'01ARZ3NDEKTSV4RRFFQ69G5FAW',onAudio:vi.fn(),onUserTranscript:vi.fn(),onAssistantTranscript:vi.fn(),onBarge:vi.fn(),onToolHandoff:vi.fn(),onError:vi.fn(),onEnded:end,
 }, {
  listProviders:async()=>({items:[{id:'01ARZ3NDEKTSV4RRFFQ69G5FAV',name:'audit',protocol:'openai_compatible',baseUrl:'https://example.com',status:'enabled',credentialState:'configured',models:[{modelId:'gpt-4o-realtime-preview',displayName:'realtime',kind:'llm',isDefault:true}]} as any]}),
  talk:{start:async (_payload:any, callback:any)=>{event=callback;return {talkId:'audit',streamId:'audit',append:vi.fn().mockResolvedValue(undefined),cancel} as any}},
 })
 expect(handle).toBeDefined()
 event({type:'ended'})
 await Promise.resolve()
 expect(end).toHaveBeenCalledTimes(1)
 expect(mock.captureStop).not.toHaveBeenCalled()
 // Production's onEnded drops the only handle. Explicit cleanup here prevents a test leak.
 await handle?.stop()
 expect(mock.captureStop).toHaveBeenCalledTimes(1)
})
