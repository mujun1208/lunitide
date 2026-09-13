import { describe, expect, it } from 'vitest'
import { registryTreeReady } from './projectDevGate'

describe('registryTreeReady', () => {
  it('is false until the project root is materialized', () => {
    expect(registryTreeReady({ treeStatus: 'none' })).toBe(false)
    expect(registryTreeReady({ rootPath: 'D:\\work\\mall', treeStatus: 'none' })).toBe(false)
    expect(registryTreeReady({ rootPath: 'D:\\work\\mall', treeStatus: 'pending' })).toBe(false)
  })

  it('is true when the tree is ready on a bound root', () => {
    expect(registryTreeReady({ rootPath: 'D:\\work\\mall', treeStatus: 'ready' })).toBe(true)
  })
})
