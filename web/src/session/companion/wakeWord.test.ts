import { expect, it } from 'vitest'
import { matchWakeWord } from './wakeWord'

it('matches the canonical wake phrase with punctuation and spacing', () => {
  for (const phrase of ['你好，月汐！', '你好 月汐', '你好月汐', '你好，月汐。', '  你好，月汐！  ']) {
    expect(matchWakeWord(phrase).hit).toBe(true)
  }
})

it('matches wake variants case-insensitively', () => {
  expect(matchWakeWord('Hello 月汐').hit).toBe(true)
  expect(matchWakeWord('嗨，月汐').hit).toBe(true)
  expect(matchWakeWord('哈喽月汐').hit).toBe(true)
})

it('extracts the trailing request as the companion prompt', () => {
  expect(matchWakeWord('你好，月汐！帮我看看今天的天气')).toEqual({ hit: true, prompt: '帮我看看今天的天气' })
  expect(matchWakeWord('你好月汐，现在几点了？')).toEqual({ hit: true, prompt: '现在几点了' })
  expect(matchWakeWord('你好月汐')).toEqual({ hit: true, prompt: '' })
})

it('does not match ordinary speech or look-alike phrases', () => {
  for (const phrase of ['今天天气不错', '你好，世界', '月汐你好', '再见月汐', '你好月']) {
    expect(matchWakeWord(phrase)).toEqual({ hit: false, prompt: '' })
  }
})
