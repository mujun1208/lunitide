import { describe, expect, test } from 'vitest'
import { leftoverArchivedMcp, leftoverArchivedNames, mcpCountsAsInstalled } from './leftoverMcp'

describe('leftoverArchivedMcp', () => {
  test('detects archived package names without matching live servers', () => {
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-github'])).toEqual(['GitHub'])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-git'])).toEqual(['Git'])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-puppeteer'])).toEqual(['Puppeteer'])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-sqlite', '--db-path', 'C:/tmp/db'])).toEqual(['SQLite'])
    expect(leftoverArchivedMcp(['-y', '@playwright/mcp'])).toEqual([])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-everything'])).toEqual([])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-memory'])).toEqual([])
    expect(leftoverArchivedMcp(['-y', '@modelcontextprotocol/server-gdrive'])).toEqual(['Google Drive'])
    expect(leftoverArchivedMcp(['-y', '@larksuite/lark-mcp'])).toEqual(['飞书'])
    expect(leftoverArchivedMcp(undefined, 'https://mcp.linear.app/mcp')).toEqual(['Linear'])
  })

  test('ignores revoked leftovers so settings stop nagging after MCP-page uninstall', () => {
    expect(leftoverArchivedNames([
      { state: 'revoked', args: ['-y', '@modelcontextprotocol/server-github'] },
      { state: 'ready', args: ['-y', '@modelcontextprotocol/server-puppeteer'] },
      { state: 'ready', args: ['-y', '@playwright/mcp'] },
      { state: 'degraded', url: 'https://mcp.linear.app/mcp' },
    ])).toEqual(['Puppeteer', 'Linear'])
  })

  test('does not treat a failed first handshake as a successful market install', () => {
    expect(mcpCountsAsInstalled({ state: 'degraded', enabled: false })).toBe(false)
    expect(mcpCountsAsInstalled({ state: 'probe', enabled: false })).toBe(false)
    expect(mcpCountsAsInstalled({ state: 'ready', enabled: false })).toBe(true)
    expect(mcpCountsAsInstalled({ state: 'degraded', enabled: true })).toBe(true)
  })
})
