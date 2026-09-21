import {describe,expect,it} from 'vitest'
import {matchByDescription,terms,type MatchCandidate} from './expertMatch'

const skill=(key:string,text:string):MatchCandidate=>({key,text})

describe('terms',()=>{
 it('keeps chinese pairs so a description can match a brief that wraps the word',()=>{
  const t=terms('负责撰写周报与月报')
  expect(t.has('周报')).toBe(true)
  expect(t.has('月报')).toBe(true)
 })
 it('splits compound latin identifiers so slide matches slide-builder',()=>{
  const t=terms('slide-builder')
  expect(t.has('slide')).toBe(true)
  expect(t.has('builder')).toBe(true)
  expect(t.has('slide-builder')).toBe(true)
 })
 it('drops the filler every brief shares',()=>{
  const t=terms('使用这个技能可以完成输出 use the tool for all')
  for(const noise of ['使用','这个','技能','完成','输出','the','tool','use','for','all'])expect(t.has(noise)).toBe(false)
 })
})

describe('matchByDescription',()=>{
 const library=[
  skill('weekly-report','周报生成 汇总本周进展并输出周报草稿'),
  skill('meeting-minutes','会议纪要 根据对话记录生成结构化纪要'),
  skill('mcp:playwright','browser automation via playwright, click and screenshot pages'),
  skill('slide-builder','slide deck builder, produces presentation outlines'),
  skill('code-review','代码审查 检查提交差异中的缺陷'),
  skill('generic-helper','通用助手 可以完成各种输出内容的技能工具'),
 ]

 it('selects the skill whose own description answers the brief',()=>{
  const picked=matchByDescription('你负责每周汇总团队进展，输出周报草稿供审阅。',library)
  expect(picked.map(p=>p.key)).toContain('weekly-report')
 })

 it('does not select a skill that only shares filler with the brief',()=>{
  const picked=matchByDescription('你负责每周汇总团队进展，输出周报草稿供审阅。',library)
  expect(picked.map(p=>p.key)).not.toContain('generic-helper')
  expect(picked.map(p=>p.key)).not.toContain('code-review')
 })

 it('matches an english mcp preset from english brief vocabulary',()=>{
  const picked=matchByDescription('Drive a browser with playwright to screenshot each page.',library)
  expect(picked.map(p=>p.key)).toContain('mcp:playwright')
 })

 it('reports the terms that earned the match so the notice can name them',()=>{
  const picked=matchByDescription('会议对话结束后生成纪要。',library)
  const minutes=picked.find(p=>p.key==='meeting-minutes')
  expect(minutes).toBeTruthy()
  expect(minutes!.hits.length).toBeGreaterThan(0)
 })

 it('returns nothing rather than guessing when the brief is empty',()=>{
  expect(matchByDescription('',library)).toEqual([])
  expect(matchByDescription('   ',library)).toEqual([])
 })

 it('caps the selection so a long brief cannot equip the whole library',()=>{
  const brief=library.map(s=>s.text).join(' ')
  expect(matchByDescription(brief,library,3).length).toBeLessThanOrEqual(3)
 })

 it('orders by score so the strongest match is first',()=>{
  const picked=matchByDescription('周报 汇总 进展 周报草稿 代码审查',library)
  expect(picked.length).toBeGreaterThan(1)
  expect(picked[0].score).toBeGreaterThanOrEqual(picked[1].score)
 })
})
