import { describe, expect, it } from 'vitest'
import firstRecording from './fixtures/continuous-mandarin-1.json'
import secondRecording from './fixtures/continuous-mandarin-2.json'
import firstRepeat from './fixtures/repeated-mandarin-1.json'
import secondRepeat from './fixtures/repeated-mandarin-2.json'
import firstMusic from './fixtures/music-command-1.json'
import secondMusic from './fixtures/music-command-2.json'
import { createVolcTranscriptCursor } from './volcTranscript'

describe.each([firstMusic, secondMusic])('recorded music command and following turns', recording => {
  it('submits the complete named-player command and both subsequent sentences once', () => {
    const cursor = createVolcTranscriptCursor()
    const turns: string[] = []
    for (const event of recording.events) {
      const next = cursor.update(event.Text, event.Final)
      if (next.final && next.text) turns.push(cursor.commit())
    }
    expect(turns).toEqual(recording.requests)
    expect(turns[0]).toBe('帮我打开汽水音乐，随机播放一首歌曲。')
    expect(cursor.current().text).toBe('')
  })
})

// Real provider responses to three synthetic Mandarin sentences, recorded on
// one websocket with 100 ms PCM pacing and 1.6 s silence between sentences.
// These contain no user audio, credentials, account IDs or conversation data.
// Unlike the timestamped fixtures, these providers return cumulative text only.
describe.each([firstRecording, secondRecording])('recorded continuous Mandarin ASR', recording => {
  it('submits exactly three complete independent turns, ignoring the duplicate final', () => {
    const cursor = createVolcTranscriptCursor()
    const turns: string[] = []
    const visible: string[][] = [[], [], []]
    for (const event of recording.events) {
      const next = cursor.update(event.Text, event.Final)
      if (!next.text) continue
      expect(turns.length).toBeLessThan(3)
      visible[turns.length].push(next.text)
      if (next.final) turns.push(cursor.commit())
    }
    expect(turns).toEqual(recording.requests)
    for (let i = 0; i < 3; i++) {
      expect(visible[i].length).toBeGreaterThan(1)
      expect(visible[i].at(-1)).toBe(recording.requests[i])
      expect(visible[i].every(text => recording.requests[i].startsWith(text))).toBe(true)
    }
    expect(cursor.current().text).toBe('')
  })

  it('does not resurrect an already submitted sentence after a silence-timer commit', () => {
    const cursor = createVolcTranscriptCursor()
    const turns: string[] = []
    for (const event of recording.events) {
      const next = cursor.update(event.Text, event.Final)
      if (!next.text) continue
      const expected = recording.requests[turns.length]
      expect(expected).toBeDefined()
      // The product may finish a turn just before the provider adds punctuation.
      if (next.text.replace(/[\p{P}\s]/gu, '') === expected.replace(/[\p{P}\s]/gu, '')) {
        turns.push(cursor.commit())
      }
    }
    const normalize = (text: string) => text.replace(/[\p{P}\s]/gu, '')
    expect(turns.map(normalize)).toEqual(recording.requests.map(normalize))
    expect(cursor.current().text).toBe('')
  })
})

describe.each([firstRepeat, secondRepeat])('recorded repeated Mandarin phrase', recording => {
  // The provider did not recognize the preceding two single-syllable SAPI
  // "停" clips; that live-ASR failure is recorded separately, not counted as
  // a frontend pass. Both full "请再说一遍" utterances were recognized.
  it.each([true, false])('retains both recognized utterances (audio positions: %s)', withPositions => {
    const cursor = createVolcTranscriptCursor()
    const turns: string[] = []
    for (const event of recording.events) {
      const next = cursor.update(event.Text, event.Final, withPositions ? event.Utterances : undefined)
      if (next.final && next.text) turns.push(cursor.commit())
    }
    expect(turns).toEqual(['请再说一遍。', '请再说一遍。'])
    expect(cursor.current().text).toBe('')
  })
})
