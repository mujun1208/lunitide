import { describe, expect, it } from 'vitest'
import { registryTreeReady } from './projectDevGate'

describe('registryTreeReady', () => {
  it('is false until a project directory is selected', () => {
    expect(registryTreeReady({ treeStatus: 'none' })).toBe(false)
    expect(registryTreeReady({ rootPath: '   ', treeStatus: 'ready' })).toBe(false)
  })

  it('is true for the selected directory, including before any generated tree', () => {
    expect(registryTreeReady({ rootPath: 'D:\\work\\mall', treeStatus: 'none' })).toBe(true)
    expect(registryTreeReady({ rootPath: 'D:\\work\\mall', treeStatus: 'pending' })).toBe(true)
    expect(registryTreeReady({ rootPath: 'D:\\work\\mall', treeStatus: 'ready' })).toBe(true)
  })
})
