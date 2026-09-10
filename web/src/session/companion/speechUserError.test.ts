import { expect, it } from 'vitest'
import { BridgeClientError } from '../../bridge/client'
import { asSpeechBridgeError, speechTransportUserError } from './speechUserError'

it('wraps only transport or generic English and keeps protocol codes', () => {
  expect(speechTransportUserError(new Error('Failed to fetch'), '本地语音识别中断')).toBe('本地语音识别中断')
  expect(speechTransportUserError(new Error('NetworkError when attempting to fetch resource.'), '火山语音识别中断')).toBe('火山语音识别中断')
  expect(speechTransportUserError(new Error('sidecar exited'), '本地语音识别中断')).toBe('sidecar exited')
  expect(speechTransportUserError(new Error('FEATURE_DISABLED: talk inspect unavailable'), '本地语音识别中断')).toBe('FEATURE_DISABLED: talk inspect unavailable')
  const coded = new BridgeClientError('引擎尚未就绪', 'VOICE-004', true, 'engine')
  expect(speechTransportUserError(coded, '本地语音识别中断')).toBe('引擎尚未就绪')
  const wrapped = asSpeechBridgeError(new Error('Failed to fetch'), '本地语音识别中断')
  expect(wrapped).toBeInstanceOf(BridgeClientError)
  expect(wrapped.code).toBe('SPEECH_RECOGNITION_UNAVAILABLE')
  expect(wrapped.message).toBe('本地语音识别中断')
  const kept = asSpeechBridgeError(coded, '本地语音识别中断')
  expect(kept).toBe(coded)
  const transportCoded = asSpeechBridgeError(
    new BridgeClientError('Failed to fetch', 'VOICE-004', true, 'engine'),
    '火山语音识别中断',
  )
  expect(transportCoded.code).toBe('VOICE-004')
  expect(transportCoded.retryable).toBe(true)
  expect(transportCoded.correlationId).toBe('engine')
  expect(transportCoded.message).toBe('火山语音识别中断')
})
