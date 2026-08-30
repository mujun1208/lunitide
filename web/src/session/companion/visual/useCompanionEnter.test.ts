import { describe, expect, test } from 'vitest'
import { launchAuroraProps, LAUNCH_AURORA_DARK, LAUNCH_AURORA_LIGHT, orbProps } from './moonVisual'
import { COMPANION_ENTER_MS, easeCompanionEnter } from './useCompanionEnter'

describe('easeCompanionEnter', () => {
  test('starts at the jade disc and finishes fully opened', () => {
    expect(easeCompanionEnter(0)).toBe(0)
    expect(easeCompanionEnter(COMPANION_ENTER_MS)).toBe(1)
    expect(easeCompanionEnter(COMPANION_ENTER_MS * 0.25)).toBeLessThan(0.2)
    expect(easeCompanionEnter(COMPANION_ENTER_MS / 2)).toBeCloseTo(0.5, 5)
    expect(easeCompanionEnter(COMPANION_ENTER_MS * 0.9)).toBeGreaterThan(0.9)
  })
})

describe('launchAuroraProps', () => {
  test('uses the official dark aurora on black and lightMode on white', () => {
    const dark = launchAuroraProps('dark')
    expect(dark.lightMode).toBe(false)
    expect(dark.colorStops).toEqual(LAUNCH_AURORA_DARK)
    const light = launchAuroraProps('light')
    expect(light.lightMode).toBe(true)
    expect(light.colorStops).toEqual(LAUNCH_AURORA_LIGHT)
  })
})

describe('orbProps enter', () => {
  test('starts as a jade disc and settles as a circular rainbow face', () => {
    const start = orbProps('idle', 0, 0)
    expect(start.moonFill).toBe(1)
    expect(start.forceHoverState).toBe(false)
    expect(start.moonRadius).toBe(0.8)
    const settled = orbProps('idle', 0, 1)
    expect(settled.moonFill).toBe(0)
    expect(settled.forceHoverState).toBe(true)
    expect(settled.hoverIntensity).toBeLessThan(0.4)
    expect(settled.rotateOnHover).toBe(false)
    expect(settled.moonRadius).toBe(0.8)
  })
})
