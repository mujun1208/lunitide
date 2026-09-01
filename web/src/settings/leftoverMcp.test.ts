import { describe, expect, test } from 'vitest'
import { leftoverArchivedMcp, leftoverArchivedNames } from './leftoverMcp'

describe('leftoverArchivedMcp', () => {
  test('detects archived package names without matching live servers', () => {
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-github'])).toEqual(['GitHub'])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-git'])).toEqual(['Git'])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-puppeteer'])).toEqual(['Puppeteer'])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-sqlite', '--db-path', 'C:/tmp/db'])).toEqual(['SQLite'])
    expect(leftoverArchivedMcp(['-y', '@playwright/mcp'])).toEqual([])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-everything'])).toEqual([])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-memory'])).toEqual([])
  })

  test('ignores revoked leftovers so settings stop nagging after MCP-page uninstall', () => {
    expect(leftoverArchivedNames([
      { state: 'revoked', args: ['-y', '@modelcontextprotocol/server-github'] },
      { state: 'ready', args: ['-y', '@modelcontextprotocol/server-puppeteer'] },
      { state: 'ready', args: ['-y', '@playwright/mcp'] },
    ])).toEqual(['Puppeteer'])
  })
})
