import { int16ToBase64 } from '../session/companion/pcmFrames'
import { type MeetingAudioBatch, verifyMeetingAudioAck } from './meetingAudio'

type Frame = { id?: number; meetingId: string; captureId: string; pcm: string; samples: number }
type Capture = { id: string; seq: number; sample: number }
type Batch = { id?: number; meetingId: string; pcm: string; position: Omit<MeetingAudioBatch, 'digest'> }
export type PendingMeetingAudio = Batch & { id: number; identity: MeetingAudioBatch }
const MAX_SAMPLES = 19200

function request<T>(value: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => { value.onsuccess = () => resolve(value.result); value.onerror = () => reject(value.error) })
}
function committed(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onabort = () => reject(tx.error ?? new Error('本机录音队列事务未完成'))
    tx.onerror = () => reject(tx.error ?? new Error('无法写入本机录音队列'))
  })
}
function decode(pcm: string): Uint8Array { return Uint8Array.from(atob(pcm), char => char.charCodeAt(0)) }

/** No PCM leaves the renderer before its IndexedDB transaction commits. A
 * batch is immutable until the matching durable server ACK commits its removal. */
export class MeetingAudioQueue {
  private constructor(private readonly db: IDBDatabase) {}

  static async open(factory: IDBFactory = indexedDB, name = 'lunitide-meeting-audio'): Promise<MeetingAudioQueue> {
    const opened = factory.open(name, 1)
    opened.onupgradeneeded = () => {
      const db = opened.result
      db.createObjectStore('captures', { keyPath: 'id' })
      for (const name of ['frames', 'batches']) {
        db.createObjectStore(name, { keyPath: 'id', autoIncrement: true }).createIndex('meetingId', 'meetingId')
      }
    }
    const db = await request(opened)
    db.onversionchange = () => db.close()
    return new MeetingAudioQueue(db)
  }

  close(): void { this.db.close() }

  async enqueue(meetingId: string, captureId: string, samples: Int16Array): Promise<void> {
    if (!samples.length) return
    const tx = this.db.transaction(['frames', 'captures'], 'readwrite', { durability: 'strict' })
    const done = committed(tx)
    const captures = tx.objectStore('captures')
    const existing = await request(captures.get(captureId)) as Capture | undefined
    if (!existing) captures.add({ id: captureId, seq: 0, sample: 0 } satisfies Capture)
    for (let at = 0; at < samples.length; at += MAX_SAMPLES) {
      const chunk = samples.subarray(at, at + MAX_SAMPLES)
      tx.objectStore('frames').add({ meetingId, captureId, pcm: int16ToBase64(chunk), samples: chunk.length } satisfies Frame)
    }
    await done
  }

  async next(meetingId: string, activeCaptureId: string, flush: boolean): Promise<PendingMeetingAudio | undefined> {
    const tx = this.db.transaction(['frames', 'batches', 'captures'], 'readwrite', { durability: 'strict' })
    const done = committed(tx)
    const batches = tx.objectStore('batches')
    let batch = (await request(batches.index('meetingId').getAll(meetingId, 1)) as Batch[])[0]
    if (!batch) {
      const frames = tx.objectStore('frames')
      // A bounded cursor avoids loading hours of disconnected audio into RAM.
      const available = await request(frames.index('meetingId').getAll(meetingId, 13)) as Frame[]
      const first = available[0]
      const selected: Frame[] = []
      let count = 0
      for (const frame of available) {
        if (frame.captureId !== first.captureId || count + frame.samples > MAX_SAMPLES) break
        selected.push(frame); count += frame.samples
      }
      if (!first || (!flush && first.captureId === activeCaptureId && count < MAX_SAMPLES && selected.length === available.length)) {
        await done
        return undefined
      }
      const captures = tx.objectStore('captures')
      const capture = await request(captures.get(first.captureId)) as Capture
      const joined = new Uint8Array(count * 2)
      let offset = 0
      for (const frame of selected) {
        const bytes = decode(frame.pcm)
        joined.set(bytes, offset); offset += bytes.length
        frames.delete(frame.id!)
      }
      const pcm = int16ToBase64(new Int16Array(joined.buffer))
      batch = { meetingId, pcm, position: { captureSessionId: capture.id, chunkSeq: capture.seq, sampleStart: capture.sample, sampleCount: count } }
      capture.seq++; capture.sample += count
      captures.put(capture)
      batch.id = await request(batches.add(batch)) as number
    }
    await done
    const digest = await crypto.subtle.digest('SHA-256', decode(batch.pcm).buffer as ArrayBuffer)
    return { ...batch, id: batch.id!, identity: { ...batch.position, digest: Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('') } }
  }

  async acknowledge(batch: PendingMeetingAudio, ack: unknown): Promise<void> {
    verifyMeetingAudioAck(ack, batch.identity)
    const tx = this.db.transaction('batches', 'readwrite', { durability: 'strict' })
    const done = committed(tx)
    const store = tx.objectStore('batches')
    const current = await request(store.get(batch.id)) as Batch | undefined
    if (current && current.meetingId === batch.meetingId && current.pcm === batch.pcm && JSON.stringify(current.position) === JSON.stringify(batch.position)) store.delete(batch.id)
    else if (current) { tx.abort(); await done; return }
    await done
  }
}
