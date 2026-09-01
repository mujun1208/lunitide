import { describe, expect, test } from 'vitest'
import { BridgeClientError } from '../bridge/client'
import {
  refEngineCaption,
  refLaunchPollStatus,
  refPreviewButtonLabel,
  refPreviewReady,
  refPreviewStatus,
} from './refEnginePreview'

describe('refPreviewReady', () => {
  test('blocks preview while the hosted engine is still loading', () => {
    expect(refPreviewReady({ engineState: 'available', hostState: 'launching', serverOnline: false })).toBe(false)
    expect(refPreviewReady({ engineState: 'unavailable', hostState: 'offline', serverOnline: false })).toBe(true)
    expect(refPreviewReady({ engineState: 'available' })).toBe(true)
    expect(refPreviewReady({ engineState: 'probing' })).toBe(false)
    expect(refPreviewReady({ engineState: 'available', hostState: 'online', serverOnline: true })).toBe(true)
  })
})

describe('refPreviewStatus', () => {
  test('does not call a cold load 该段语音合成失败', () => {
    expect(refPreviewStatus(new BridgeClientError('语音引擎启动中，请稍候', 'M95-001', true, 't'))).toMatch(/请稍候再试听/)
    expect(refPreviewStatus(new BridgeClientError('该段语音合成失败', 'M95-002', false, 't'))).toBe('试听失败：该段语音合成失败')
  })
})

describe('refEngineCaption', () => {
  test('keeps launching copy and surfaces last_err when offline', () => {
    expect(refEngineCaption({ host_state: 'launching', host_script: 'E:\\GPT-SoVITS\\start-api-cpu.bat' })).toMatch(/启动中/)
    expect(refEngineCaption({
      host_state: 'launching',
      host_script: 'E:\\GPT-SoVITS\\start-api-cpu.bat',
      host_last_err: '引擎未就绪：/docs 仍无响应',
    })).toMatch(/引擎未就绪/)
    expect(refEngineCaption({
      host_state: 'offline',
      host_script: 'E:\\GPT-SoVITS\\start-api-cpu.bat',
      host_last_err: 'service stopped answering',
    })).toMatch(/service stopped answering/)
    expect(refEngineCaption({ server_online: true, pack_exists: true }, 50)).toMatch(/引擎在线/)
  })
})

describe('refLaunchPollStatus', () => {
  test('stays quiet until the budget is gone', () => {
    expect(refLaunchPollStatus(3, 40)).toBeUndefined()
    expect(refLaunchPollStatus(40, 40, 'spawn failed')).toMatch(/spawn failed/)
    expect(refLaunchPollStatus(40, 40)).toMatch(/超过 2 分钟/)
  })
})

describe('refPreviewButtonLabel', () => {
  test('says 启动中 while the host is loading', () => {
    expect(refPreviewButtonLabel({ busy: false, launching: true })).toBe('启动中…')
    expect(refPreviewButtonLabel({ busy: true, launching: true })).toBe('合成中…')
    expect(refPreviewButtonLabel({ busy: false, launching: false })).toBe('试听')
  })
})
