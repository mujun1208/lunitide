import { describe, expect, test } from 'vitest'
import { leftoverArchivedMcp } from './leftoverMcp'

describe('leftoverArchivedMcp', () => {
  test('detects archived package names without matching live servers', () => {
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-github'])).toEqual(['GitHub'])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-git'])).toEqual(['Git'])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-everything'])).toEqual([])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-memory'])).toEqual([])
  })
})
