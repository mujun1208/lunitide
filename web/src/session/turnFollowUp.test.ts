import { expect, it } from 'vitest'
import { classifyFollowUp, companionShouldDeferInFlight, companionShouldDiscardOnExit, inFlightLiveChat, isStatusFollowUp, isStopCommand, looksLikeTaskChange } from './turnFollowUp'

it('treats 停止 as cancel, not a normal follow-up', () => {
  expect(isStopCommand('停止')).toBe(true)
  expect(isStopCommand('停止。')).toBe(true)
  expect(isStopCommand('stop')).toBe(true)
  expect(isStopCommand('做好了没有')).toBe(false)
  expect(isStopCommand('改方案用深色封面')).toBe(false)
})

it('treats 做好了吗 as a status follow-up that must attach', () => {
  expect(isStatusFollowUp('做好了吗')).toBe(true)
  expect(isStatusFollowUp('做好了没有')).toBe(true)
  expect(isStatusFollowUp('进度')).toBe(true)
  expect(isStatusFollowUp('写一份12星座爱情小说Word到桌面')).toBe(false)
})

it('classifies supplements vs task changes during in-flight work', () => {
  expect(classifyFollowUp('做好了吗', '请 PPT专家做一份介绍')).toBe('progress')
  expect(classifyFollowUp('封面先做出来', '请 PPT专家做一份介绍')).toBe('supplement')
  expect(classifyFollowUp('改方案用深色封面', '请 PPT专家做一份介绍')).toBe('supplement')
  expect(classifyFollowUp('别做PPT了帮我查天气', '请 PPT专家做一份介绍')).toBe('task_change')
  expect(classifyFollowUp('帮我打开桌面协议的文件', '请 PPT专家做一份介绍')).toBe('task_change')
})

it('detects task-change negation and independent asks', () => {
  expect(looksLikeTaskChange('别做PPT了帮我查天气', '请 PPT专家做一份介绍')).toBe(true)
  expect(looksLikeTaskChange('别做PPT了帮我查天气')).toBe(false)
  expect(looksLikeTaskChange('封面先做出来', '请 PPT专家做一份介绍')).toBe(false)
  expect(looksLikeTaskChange('做好了吗')).toBe(false)
})

it('defers the same companion sentence while a turn is live', () => {
  expect(companionShouldDeferInFlight('打开记事本', '打开记事本')).toBe(true)
  expect(companionShouldDeferInFlight(' 打开记事本 ', '打开记事本')).toBe(true)
  expect(companionShouldDeferInFlight('不是这个', '打开记事本')).toBe(false)
  expect(companionShouldDeferInFlight('', '打开记事本')).toBe(false)
})

it('keeps a spoken companion session when exiting with empty local items', () => {
  expect(companionShouldDiscardOnExit({
    initialCompanion: true, itemsLength: 0, chatActive: false, sessionEngaged: true,
  })).toBe(false)
  expect(companionShouldDiscardOnExit({
    initialCompanion: true, itemsLength: 0, chatActive: true, sessionEngaged: false,
  })).toBe(false)
  expect(companionShouldDiscardOnExit({
    initialCompanion: true, itemsLength: 0, chatActive: false, sessionEngaged: false,
  })).toBe(true)
  expect(companionShouldDiscardOnExit({
    initialCompanion: false, itemsLength: 0, chatActive: false, sessionEngaged: false,
  })).toBe(false)
})

it('detects an in-flight live chat that must not be replaced', () => {
  expect(inFlightLiveChat({ terminal: false }, false)).toBe(true)
  expect(inFlightLiveChat(undefined, true)).toBe(true)
  expect(inFlightLiveChat({ terminal: true }, false)).toBe(false)
  expect(inFlightLiveChat(undefined, false)).toBe(false)
})
