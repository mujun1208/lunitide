import { describe, expect, test } from 'vitest'
import { buildMoonSculpture, buildNebulaField, lotusPoint } from './particleGeometry'
import {
  companionSkinConfirmSpeech,
  consumeCompanionSkinCommand,
  engageMorph,
  particleBudget,
  isEngageState,
  nextCompanionSkin,
  parseCompanionSkinCommand,
  particleMoonTargets,
  SHAPE_LOTUS,
  SHAPE_SPHERE,
  SHAPE_TORUS,
  THINK_HOLD_SEC,
  THINK_SHRINK_SEC,
  THINK_SLOT_SEC,
} from './particleMoon'

describe('particleMoonTargets', () => {
  test('listening and thinking share the same engage targets', () => {
    const idle = particleMoonTargets('idle')
    const listening = particleMoonTargets('listening', 0.4)
    const thinking = particleMoonTargets('thinking')
    expect(isEngageState('listening')).toBe(true)
    expect(isEngageState('thinking')).toBe(true)
    expect(listening.form).toBe(thinking.form)
    expect(listening.scatter).toBe(thinking.scatter)
    expect(listening.radius).toBe(thinking.radius)
    expect(idle.form).toBeLessThan(thinking.form)
    expect(thinking.scatter).toBeLessThan(idle.scatter)
    expect(thinking.radius).toBeLessThan(idle.radius)
  })

  test('engage shrinks, then lotus, then the thinking forms', () => {
    const shrink = engageMorph(0.2)
    expect(shrink.shapeA).toBe(SHAPE_SPHERE)
    expect(shrink.radius).toBeGreaterThan(0.3)
    const held = engageMorph(THINK_SHRINK_SEC + 0.05)
    expect(held.shapeA).toBe(SHAPE_SPHERE)
    expect(held.radius).toBeLessThan(0.3)
    const first = engageMorph(THINK_SHRINK_SEC + THINK_HOLD_SEC + 0.05)
    expect(first.shapeA).toBe(SHAPE_LOTUS)
    expect(first.mix).toBe(0)
    const second = engageMorph(THINK_SHRINK_SEC + THINK_HOLD_SEC + THINK_SLOT_SEC + 0.05)
    expect(second.shapeA).toBe(SHAPE_TORUS)
    const back = engageMorph(THINK_SHRINK_SEC + THINK_HOLD_SEC + THINK_SLOT_SEC * 5 + 0.05)
    expect(back.shapeA).toBe(SHAPE_LOTUS)
  })

  test('speaking pulse rises with gain', () => {
    const low = particleMoonTargets('speaking', 0.2)
    const high = particleMoonTargets('speaking', 0.9)
    expect(low.pulse).toBeLessThan(high.pulse)
    expect(low.coreScale).toBeLessThan(high.coreScale)
    expect(low.form).toBeGreaterThan(0.7)
    expect(high.radius).toBeGreaterThan(particleMoonTargets('idle').radius)
  })
})

describe('particle layers', () => {
  test('moon dots stay on a unit sphere and carry varied hues', () => {
    const moon = buildMoonSculpture(2400)
    let minR = 99
    let maxR = 0
    const buckets = new Set<number>()
    for (let i = 0; i < 2400; i++) {
      const o = i * 3
      const radius = Math.hypot(moon.position[o], moon.position[o + 1], moon.position[o + 2])
      minR = Math.min(minR, radius)
      maxR = Math.max(maxR, radius)
      buckets.add(Math.floor(moon.color[o] * 12))
    }
    expect(minR).toBeGreaterThan(0.98)
    expect(maxR).toBeLessThan(1.02)
    expect(buckets.size).toBeGreaterThan(8)
  })

  test('nebula wraps the moon with cyan-magenta-wine rings', () => {
    const field = buildNebulaField(3000, 200)
    let minDust = 99
    let cyan = 0
    let magenta = 0
    for (let i = 0; i < field.count; i++) {
      if (field.layer[i] < 0.5) continue
      const o = i * 3
      const radius = Math.hypot(field.position[o], field.position[o + 1], field.position[o + 2])
      minDust = Math.min(minDust, radius)
      const r = field.color[o]
      const g = field.color[o + 1]
      const b = field.color[o + 2]
      if (b > r && g > 0.5) cyan += 1
      if (r > 0.7 && b > 0.55 && g < 0.55) magenta += 1
    }
    expect(minDust).toBeGreaterThan(0.55)
    expect(minDust).toBeLessThan(1.1)
    expect(cyan).toBeGreaterThan(40)
    expect(magenta).toBeGreaterThan(20)
    const bands = [0.98, 1.48, 2.05, 2.68, 3.38, 4.12, 4.88]
    let near = 0
    let dust = 0
    for (let i = 0; i < field.count; i++) {
      if (field.layer[i] < 0.5) continue
      dust += 1
      const o = i * 3
      const xz = Math.hypot(field.position[o], field.position[o + 2])
      if (bands.some(band => Math.abs(xz - band) < 0.55)) near += 1
    }
    expect(near / dust).toBeGreaterThan(0.7)
  })

  test('lotus is a flattened petal form, not a unit sphere', () => {
    let minY = 99
    let maxY = -99
    let maxR = 0
    for (let i = 0; i < 400; i++) {
      const p = lotusPoint(i, 400)
      minY = Math.min(minY, p[1])
      maxY = Math.max(maxY, p[1])
      maxR = Math.max(maxR, Math.hypot(p[0], p[2]))
    }
    expect(maxY - minY).toBeLessThan(0.7)
    expect(maxR).toBeGreaterThan(0.7)
  })
})

describe('parseCompanionSkinCommand', () => {
  test('maps the locked phrases and ignores chat', () => {
    expect(parseCompanionSkinCommand('切换风格')).toBe('toggle')
    expect(parseCompanionSkinCommand('换个风格。')).toBe('toggle')
    expect(parseCompanionSkinCommand('换成另一套')).toBe('toggle')
    expect(parseCompanionSkinCommand('星尘风格')).toBe('particle')
    expect(parseCompanionSkinCommand('粒子月亮')).toBe('particle')
    expect(parseCompanionSkinCommand('贾维斯风格')).toBe('particle')
    expect(parseCompanionSkinCommand('换成粒子')).toBe('particle')
    expect(parseCompanionSkinCommand('玉盘风格')).toBe('classic')
    expect(parseCompanionSkinCommand('普通月亮')).toBe('classic')
    expect(parseCompanionSkinCommand('原来的月亮')).toBe('classic')
    expect(parseCompanionSkinCommand('换回普通月亮')).toBe('classic')
    expect(parseCompanionSkinCommand('今天合肥的天气怎么样')).toBeNull()
    expect(parseCompanionSkinCommand('你好')).toBeNull()
    expect(parseCompanionSkinCommand('换成粒子月亮看看。')).toBe('particle')
    expect(parseCompanionSkinCommand('换成粒子月亮吧')).toBe('particle')
    expect(parseCompanionSkinCommand('帮我换成粒子月亮然后查天气')).toBeNull()
  })
})

describe('particleBudget', () => {
  test('high and low stay on the locked V2.9 caps', () => {
    expect(particleBudget(true)).toEqual({ moon: 20000, nebula: 22000, stars: 5600 })
    expect(particleBudget(false)).toEqual({ moon: 9000, nebula: 9000, stars: 2400 })
  })
})

describe('companion skin helpers', () => {
  test('toggle and confirm copy', () => {
    expect(nextCompanionSkin('classic', 'toggle')).toBe('particle')
    expect(nextCompanionSkin('particle', 'toggle')).toBe('classic')
    expect(nextCompanionSkin('classic', 'classic')).toBe('classic')
    expect(companionSkinConfirmSpeech('classic', 'particle')).toBe('好，换成星尘。')
    expect(companionSkinConfirmSpeech('particle', 'classic')).toBe('好，换回玉盘。')
    expect(companionSkinConfirmSpeech('particle', 'particle')).toBe('已经是星尘了。')
    expect(companionSkinConfirmSpeech('classic', 'classic')).toBe('已经是玉盘了。')
  })
  test('consume intercepts only exact skin lines', () => {
    expect(consumeCompanionSkinCommand('切换风格', 'classic')).toEqual({
      next: 'particle',
      speech: '好，换成星尘。',
    })
    expect(consumeCompanionSkinCommand('今天合肥的天气怎么样', 'classic')).toBeNull()
  })
})
