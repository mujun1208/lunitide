import {expect, it} from 'vitest'
import {knowledgeUserError, localizeKBFailReason} from './kbFailReason'

it('maps leftover English parser suffixes to Chinese', () => {
  expect(localizeKBFailReason('parse function not configured')).toBe('未配置正文解析')
  expect(localizeKBFailReason('无法抽出正文：source changed during parsing')).toBe('无法抽出正文：源文件在入库后已被修改')
  expect(localizeKBFailReason('source digest changed')).toBe('源文件在入库后已被修改')
  expect(localizeKBFailReason('无法抽出正文：未配置正文解析')).toBe('无法抽出正文：未配置正文解析')
  expect(localizeKBFailReason('tombstone:deleted')).toBe('知识来源已删除')
})

it('drops leftover English knowledge errors on the user surface', () => {
  expect(knowledgeUserError('Failed to fetch', true, '知识库加载失败', 'Failed to load knowledge')).toBe('知识库加载失败')
  expect(knowledgeUserError('parse function not configured', true, '知识库加载失败', 'Failed to load knowledge')).toBe('未配置正文解析')
  expect(knowledgeUserError('无法抽出正文：未配置正文解析', true, '知识库加载失败', 'Failed to load knowledge')).toBe('无法抽出正文：未配置正文解析')
})
