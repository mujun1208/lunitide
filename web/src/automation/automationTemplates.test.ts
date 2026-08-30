import { describe, expect, test } from 'vitest'
import { AUTOMATION_TEMPLATES, cronToHuman, delayAtCron, datetimeLocalToAtCron } from './automationTemplates'

describe('automation templates OpenClaw alignment', () => {
  test('includes browser, desktop data-flow, and multi-step recipes', () => {
    const ids = AUTOMATION_TEMPLATES.map(t => t.id)
    expect(ids).toEqual(expect.arrayContaining(['web-form-scrape', 'desktop-data-flow', 'multi-step-orchestration']))
    const scrape = AUTOMATION_TEMPLATES.find(t => t.id === 'web-form-scrape')
    expect(scrape?.prompt).toMatch(/browser\.act/)
    expect(scrape?.prompt).toMatch(/snapshot/)
    const flow = AUTOMATION_TEMPLATES.find(t => t.id === 'desktop-data-flow')
    expect(flow?.prompt).toMatch(/computer\.act/)
    expect(flow?.prompt).toMatch(/不要远程/)
    expect(flow?.prompt).not.toMatch(/cc\.window_/)
  })

  test('renders at: stamps as one-shot times', () => {
    expect(cronToHuman('at:2026-08-27T12:00:00Z')).toMatch(/一次/)
  })

  test('builds a 20-minute at: stamp', () => {
    expect(delayAtCron(20, new Date('2026-08-27T12:00:00Z'))).toBe('at:2026-08-27T12:20:00.000Z')
    const cron = datetimeLocalToAtCron('2026-08-27T20:00')
    expect(cron.startsWith('at:')).toBe(true)
    expect(Number.isNaN(new Date(cron.slice(3)).getTime())).toBe(false)
  })
})
