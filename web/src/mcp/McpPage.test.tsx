import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { mcBridge, type McpBridge } from '../bridge/client'
import { McpPage, __parseManualJsonForTest } from './McpPage'

afterEach(cleanup)

const catalog = [
  { id: 'everything', name: 'Everything', description: '官方测试服务器', transport: 'stdio' as const, command: 'npx' as const, args: ['-y', '@modelcontextprotocol/server-everything'], needsArgs: false, category: '测试' },
  { id: 'filesystem', name: 'Filesystem', description: '读写指定目录内的文件', transport: 'stdio' as const, command: 'npx' as const, args: ['-y', '@modelcontextprotocol/server-filesystem', '{{dir}}'], needsArgs: true, argPlaceholder: '{{dir}}', argHint: '要挂载的目录绝对路径', category: '文件' },
]

const memoryEndpoint = {
  endpointId: 'mcp-01ARZ3NDEKTSV4RRFFQ69G5FAA',
  transport: 'stdio' as const,
  state: 'ready' as const,
  enabled: true,
  origin: 'manual' as const,
  displayName: 'Memory',
  command: 'npx',
  args: ['-y', '@modelcontextprotocol/server-memory'],
}

function api(overrides: Partial<McpBridge> = {}): McpBridge {
  return {
    list: vi.fn().mockResolvedValue({ endpoints: [] }),
    add: vi.fn().mockResolvedValue({ endpointId: 'mcp-1', state: 'ready' }),
    toggle: vi.fn().mockResolvedValue({ endpointId: 'mcp-1', enabled: true, state: 'ready' }),
    health: vi.fn().mockResolvedValue({ state: 'ready', driftDetected: false, checkedAt: '2026-01-01T00:00:00Z', latencyMs: 12 }),
    marketSearch: vi.fn(),
    presets: vi.fn().mockResolvedValue({ items: catalog }),
    ...overrides,
  }
}

it('renders MCP market cards and installs with plus', async () => {
  const bridge = api()
  render(<McpPage bridge={bridge} />)
  expect(await screen.findByRole('heading', { name: 'MCP' })).toBeInTheDocument()
  expect(await screen.findByText('Everything')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '安装 Everything' }))
  await waitFor(() => expect(bridge.add).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.add).mock.calls[0][0]).toMatchObject({
    origin: 'manual',
    transport: 'stdio',
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-everything'],
    riskConfirmed: true,
  })
  await waitFor(() => expect(bridge.toggle).toHaveBeenCalledWith({ endpointId: 'mcp-1', enabled: true }))
  expect(await screen.findByRole('status')).toHaveTextContent('已安装「Everything」')
})

it('installs needsArgs presets with argDefault without a path picker', async () => {
  const bridge = api({
    presets: vi.fn().mockResolvedValue({
      items: [{
        ...catalog[1],
        argDefault: 'C:/Users/demo/AppData/Local/Lunitide/mcp/filesystem',
        argHint: '月汐会使用本机数据目录，无需手动填写',
      }],
    }),
  })
  render(<McpPage bridge={bridge} />)
  await screen.findByText('Filesystem')
  fireEvent.click(screen.getByRole('button', { name: '安装 Filesystem' }))
  await waitFor(() => expect(bridge.add).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.add).mock.calls[0][0].args).toEqual(['-y', '@modelcontextprotocol/server-filesystem', 'C:/Users/demo/AppData/Local/Lunitide/mcp/filesystem'])
  expect(screen.queryByLabelText('Filesystem 参数')).not.toBeInTheDocument()
})

it('refuses a needsArgs preset that cannot one-click install', async () => {
  const bridge = api()
  render(<McpPage bridge={bridge} />)
  await screen.findByText('Filesystem')
  fireEvent.click(screen.getByRole('button', { name: '安装 Filesystem' }))
  expect(bridge.add).not.toHaveBeenCalled()
  expect(await screen.findByRole('alert')).toHaveTextContent('无法一键安装')
  expect(screen.queryByLabelText('Filesystem 参数')).not.toBeInTheDocument()
})

it('labels a curated install with its preset id', async () => {
  const bridge = api({
    presets: vi.fn().mockResolvedValue({
      items: [{
        id: 'playwright', name: 'Playwright', description: '浏览器自动化',
        transport: 'stdio' as const, command: 'npx' as const, args: ['-y', '@playwright/mcp'],
        needsArgs: false, category: '浏览器',
      }],
    }),
    list: vi.fn().mockResolvedValue({
      endpoints: [{
        ...memoryEndpoint,
        displayName: 'Playwright',
        args: ['-y', '@playwright/mcp'],
      }],
    }),
  })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  expect(await screen.findByText(/策展预置 playwright/)).toBeInTheDocument()
})

it('shows installed display names and connection status', async () => {
  const bridge = api({ list: vi.fn().mockResolvedValue({ endpoints: [memoryEndpoint] }) })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: '已安装（1）' }))
  expect(await screen.findByText('Memory')).toBeInTheDocument()
  expect(screen.getByText('已连接')).toBeInTheDocument()
  expect(screen.queryByText(memoryEndpoint.endpointId)).not.toBeInTheDocument()
})

it('reconnects an installed MCP', async () => {
  const bridge = api({
    list: vi.fn().mockResolvedValue({ endpoints: [{ ...memoryEndpoint, enabled: false, state: 'degraded' }] }),
  })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  await screen.findByText('Memory')
  fireEvent.click(screen.getByRole('button', { name: '重新连接' }))
  await waitFor(() => expect(bridge.toggle).toHaveBeenCalledWith({ endpointId: memoryEndpoint.endpointId, enabled: true }))
  await waitFor(() => expect(bridge.health).toHaveBeenCalledWith({ endpointId: memoryEndpoint.endpointId }))
})

it('uninstalls an installed market card without switching tabs', async () => {
  const confirmToken = vi.spyOn(mcBridge, 'confirmToken').mockResolvedValue({ confirmToken: 'a'.repeat(64), expiresAt: '2026-01-01T00:00:00Z' })
  const uninstall = vi.spyOn(mcBridge, 'uninstall').mockResolvedValue({ endpointId: memoryEndpoint.endpointId, state: 'revoked' })
  const bridge = api({
    presets: vi.fn().mockResolvedValue({
      items: [{
        id: 'playwright', name: 'Playwright', description: '浏览器自动化',
        transport: 'stdio' as const, command: 'npx' as const, args: ['-y', '@playwright/mcp'],
        needsArgs: false, category: '浏览器',
      }],
    }),
    list: vi.fn().mockResolvedValue({
      endpoints: [{
        ...memoryEndpoint,
        displayName: 'Playwright',
        args: ['-y', '@playwright/mcp'],
      }],
    }),
  })
  render(<McpPage bridge={bridge} />)
  expect(await screen.findByRole('tab', { name: /MCP 市场/ })).toHaveAttribute('aria-selected', 'true')
  fireEvent.click(await screen.findByRole('button', { name: '卸载 Playwright' }))
  fireEvent.click(await screen.findByRole('button', { name: '确认删除' }))
  await waitFor(() => expect(uninstall).toHaveBeenCalledOnce())
  confirmToken.mockRestore()
  uninstall.mockRestore()
})

it('deletes an installed MCP after confirm', async () => {
  const confirmToken = vi.spyOn(mcBridge, 'confirmToken').mockResolvedValue({ confirmToken: 'a'.repeat(64), expiresAt: '2026-01-01T00:00:00Z' })
  const uninstall = vi.spyOn(mcBridge, 'uninstall').mockResolvedValue({ endpointId: memoryEndpoint.endpointId, state: 'revoked' })
  const bridge = api({ list: vi.fn().mockResolvedValue({ endpoints: [memoryEndpoint] }) })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  fireEvent.click(await screen.findByRole('button', { name: '删除' }))
  fireEvent.click(await screen.findByRole('button', { name: '确认删除' }))
  await waitFor(() => expect(uninstall).toHaveBeenCalledOnce())
  expect(confirmToken).toHaveBeenCalledWith({ method: 'mc.connector.uninstall', target: memoryEndpoint.endpointId })
  confirmToken.mockRestore()
  uninstall.mockRestore()
})

it('saves Cursor-style mcpServers JSON from the create dialog', async () => {
  const bridge = api()
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('button', { name: '＋ 创建 MCP' }))
  const dialog = await screen.findByRole('dialog', { name: '创建 MCP' })
  fireEvent.change(screen.getByLabelText('MCP JSON'), { target: { value: '{"mcpServers":{"memory":{"command":"npx","args":["-y","@modelcontextprotocol/server-memory"]}}}' } })
  fireEvent.click(dialog.querySelector('input[type="checkbox"]')!)
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(bridge.add).toHaveBeenCalledOnce())
  expect(vi.mocked(bridge.add).mock.calls[0][0]).toMatchObject({
    origin: 'manual',
    transport: 'stdio',
    command: 'npx',
    args: ['-y', '@modelcontextprotocol/server-memory'],
  })
})

it('opens installed view and marks leftover archived MCP', async () => {
  const leftover = {
    ...memoryEndpoint,
    endpointId: 'mcp-old-github',
    displayName: '@modelcontextprotocol/server-github',
    args: ['-y', '@modelcontextprotocol/server-github'],
  }
  const bridge = api({ list: vi.fn().mockResolvedValue({ endpoints: [memoryEndpoint, leftover] }) })
  render(<McpPage bridge={bridge} />)
  expect(await screen.findByText(/已下架且无法继续使用的 MCP（GitHub）/)).toBeInTheDocument()
  expect(await screen.findByText(/已下架 · GitHub/)).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: /已安装/ })).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByText('Memory')).toBeInTheDocument()
})

it('warns that Chrome attach presets are not default computer control', async () => {
  const bridge = api({
    presets: vi.fn().mockResolvedValue({
      items: [{
        id: 'chrome-devtools', name: 'Chrome DevTools', description: '官方 Chrome DevTools MCP',
        transport: 'stdio' as const, command: 'npx' as const, args: ['-y', 'chrome-devtools-mcp'],
        needsArgs: false, category: '浏览器',
      }],
    }),
  })
  render(<McpPage bridge={bridge} />)
  expect(await screen.findByText(/不是默认电脑控制/)).toBeInTheDocument()
  expect(screen.getByText(/月伴不会自动安装/)).toBeInTheDocument()
})

it('parses both mcpServers maps and single command entries', () => {
  expect(__parseManualJsonForTest('{"mcpServers":{"a":{"command":"npx","args":["-y","pkg"]}}}')).toEqual([
    { name: 'a', transport: 'stdio', command: 'npx', args: ['-y', 'pkg'], url: undefined },
  ])
  expect(__parseManualJsonForTest('{"url":"https://example.test/mcp"}')).toEqual([
    { name: 'manual', transport: 'https', command: undefined, args: undefined, url: 'https://example.test/mcp' },
  ])
})

it('reports connection failure after registration and refreshes the actual endpoint',async()=>{
 const bridge=api({toggle:vi.fn().mockRejectedValue(new Error('凭据失效，请更新'))})
 render(<McpPage bridge={bridge}/>);fireEvent.click(await screen.findByRole('button',{name:'安装 Everything'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('凭据失效，请更新')
 expect(screen.queryByRole('status')).not.toBeInTheDocument()
 expect(bridge.list).toHaveBeenCalledTimes(2)
})

it('lets a leftover remote MCP configure credentials without staying in the market',async()=>{
 const saved={endpointId:'mcp-1',transport:'https' as const,url:'https://mcp.juhe.cn/mcp?token={{credential}}',state:'probe' as const,enabled:false,securityVersion:0,displayName:'聚合日常查询'}
 const bridge=api({list:vi.fn().mockResolvedValue({endpoints:[saved]}),credentialSet:vi.fn().mockResolvedValue({configured:true,securityVersion:1})})
 render(<McpPage bridge={bridge}/>)
 fireEvent.click(await screen.findByRole('tab',{name:'已安装（1）'}))
 fireEvent.click(await screen.findByRole('button',{name:'凭据'}))
 await screen.findByRole('dialog',{name:'MCP 凭据'})
 fireEvent.change(screen.getByLabelText('MCP 凭据值'),{target:{value:'fixture-token'}});fireEvent.click(screen.getByText('保存凭据'))
 await waitFor(()=>expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
 expect(bridge.credentialSet).toHaveBeenCalledWith(expect.objectContaining({endpointId:'mcp-1',credential:'fixture-token',expectedVersion:0}))
 fireEvent.click(screen.getByRole('button',{name:'重新连接'}))
 await waitFor(()=>expect(bridge.toggle).toHaveBeenCalledWith({endpointId:'mcp-1',enabled:true}))
 fireEvent.click(screen.getByRole('tab',{name:/MCP 市场/}))
 expect(screen.queryByText('聚合日常查询')).not.toBeInTheDocument()
})

it('manual remote JSON saves configuration even when the server needs credentials',async()=>{
 const bridge=api({toggle:vi.fn().mockRejectedValue(new Error('401 requires credentials'))})
 render(<McpPage bridge={bridge}/>);fireEvent.click(await screen.findByRole('button',{name:'＋ 创建 MCP'}))
 fireEvent.change(screen.getByLabelText('MCP JSON'),{target:{value:'{"url":"https://fixture.invalid/mcp"}'}})
 fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByRole('button',{name:'保存'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
 expect(bridge.add).toHaveBeenCalledWith(expect.objectContaining({url:'https://fixture.invalid/mcp',configureOnly:true}));expect(bridge.toggle).not.toHaveBeenCalled()
})

it('shows the reconnect diagnostic instead of treating a degraded probe as a success notice', async()=>{
 const bridge=api({list:vi.fn().mockResolvedValue({endpoints:[{...memoryEndpoint,state:'degraded'}]}),health:vi.fn().mockResolvedValue({state:'degraded',driftDetected:false,checkedAt:'2026-09-07T00:00:00Z',diagnosticCode:'MCP_DEPENDENCY_FAILED',diagnosticMessage:'本地 Python 或软件依赖未能准备完成，请检查运行环境。'})})
 render(<McpPage bridge={bridge}/>);fireEvent.click(await screen.findByRole('tab',{name:'已安装（1）'}))
 fireEvent.click(await screen.findByRole('button',{name:'重新连接'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('Memory：本地 Python 或软件依赖未能准备完成')
 expect(screen.queryByText('Memory：连接异常')).not.toBeInTheDocument()
})
it('does not show raw English list failures',async()=>{
 render(<McpPage bridge={api({list:vi.fn().mockRejectedValue(new Error('Failed to fetch'))})}/>)
 expect(await screen.findByRole('alert')).toHaveTextContent('MCP 清单加载失败')
 expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('explains the OAuth requirement on a leftover Google Drive installation',async()=>{
 const bridge=api({list:vi.fn().mockResolvedValue({endpoints:[{...memoryEndpoint,displayName:'Google Drive',args:['-y','@modelcontextprotocol/server-gdrive','C:/old/folder'],state:'degraded',credentialConfigured:false}]})})
 render(<McpPage bridge={bridge}/>);fireEvent.click(await screen.findByRole('tab',{name:'已安装（1）'}))
 expect(await screen.findByText(/普通文件目录不能代替授权/)).toBeInTheDocument()
 expect(screen.getByRole('link',{name:'官方配置说明'})).toHaveAttribute('href','https://github.com/modelcontextprotocol/servers-archived/tree/main/src/gdrive')
})

it('names a broken install when displayName is empty',async()=>{
 const bridge=api({list:vi.fn().mockResolvedValue({endpoints:[{...memoryEndpoint,displayName:'',command:'',args:[],endpointId:'',state:'degraded'}]})})
 render(<McpPage bridge={bridge}/>)
 fireEvent.click(await screen.findByRole('tab',{name:'已安装（1）'}))
 expect(await screen.findByText('未命名 MCP')).toBeInTheDocument()
})

it('does not treat a failed handshake as a market install', async () => {
  const failed = {
    ...memoryEndpoint,
    endpointId: 'mcp-1',
    displayName: 'Everything',
    args: catalog[0].args,
    state: 'degraded' as const,
    enabled: false,
    diagnosticMessage: '服务器握手或工具目录响应不符合支持的 MCP 协议，请检查启动配置及服务器版本。',
  }
  const bridge = api({
    add: vi.fn().mockResolvedValue({ endpointId: 'mcp-1', state: 'degraded' }),
    list: vi.fn().mockResolvedValueOnce({ endpoints: [] }).mockResolvedValue({ endpoints: [failed] }),
  })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('button', { name: '安装 Everything' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('不符合支持的 MCP 协议')
  expect(bridge.toggle).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: '安装 Everything' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '卸载 Everything' })).not.toBeInTheDocument()
  expect(screen.queryByText('已安装「Everything」')).not.toBeInTheDocument()
})

it('uninstalls leftover Google Drive, Linear and Lark in one click', async () => {
  const leftover = [
    { ...memoryEndpoint, endpointId: 'mcp-gdrive', displayName: 'Google Drive', args: ['-y', '@modelcontextprotocol/server-gdrive'], state: 'degraded' as const },
    { ...memoryEndpoint, endpointId: 'mcp-linear', displayName: 'Linear', transport: 'https' as const, url: 'https://mcp.linear.app/mcp', command: '', args: [], state: 'degraded' as const, enabled: false },
    { ...memoryEndpoint, endpointId: 'mcp-lark', displayName: '飞书', args: ['-y', '@larksuite/lark-mcp'], state: 'degraded' as const },
  ]
  const confirmToken = vi.spyOn(mcBridge, 'confirmToken').mockResolvedValue({ confirmToken: 'a'.repeat(64), expiresAt: '2026-01-01T00:00:00Z' })
  const uninstall = vi.spyOn(mcBridge, 'uninstall').mockResolvedValue({ endpointId: 'mcp-gdrive', state: 'revoked' })
  const bridge = api({ list: vi.fn().mockResolvedValueOnce({ endpoints: leftover }).mockResolvedValue({ endpoints: [] }) })
  render(<McpPage bridge={bridge} />)
  expect(await screen.findByText(/Google Drive、Linear、飞书/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '一键卸载' }))
  fireEvent.click(await screen.findByRole('button', { name: '确认卸载' }))
  await waitFor(() => expect(uninstall).toHaveBeenCalledTimes(3))
  expect(confirmToken).toHaveBeenCalledWith({ method: 'mc.connector.uninstall', target: 'mcp-gdrive' })
  expect(confirmToken).toHaveBeenCalledWith({ method: 'mc.connector.uninstall', target: 'mcp-linear' })
  expect(confirmToken).toHaveBeenCalledWith({ method: 'mc.connector.uninstall', target: 'mcp-lark' })
  confirmToken.mockRestore()
  uninstall.mockRestore()
})

it('offers one-click uv install when a Python MCP is missing uv', async () => {
  const uvInstall = vi.fn().mockResolvedValue({ state: 'ready', percent: 100, doneBytes: 1, totalBytes: 1 })
  const bridge = api({
    uvInstall,
    list: vi.fn().mockResolvedValue({
      endpoints: [{
        ...memoryEndpoint,
        displayName: 'Fetch',
        command: 'uvx',
        args: ['mcp-server-fetch'],
        state: 'degraded',
        diagnosticCode: 'MCP_UV_UNAVAILABLE',
        diagnosticMessage: '未找到 uv。可在本页点「安装 uv」自动下载；npx 服务不受影响。',
      }],
    }),
  })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: '已安装（1）' }))
  fireEvent.click(await screen.findByRole('button', { name: '安装 uv' }))
  await waitFor(() => expect(uvInstall).toHaveBeenCalled())
  expect(await screen.findByText(/uv 已安装/)).toBeInTheDocument()
})

const youtubePreset = {
  id: 'youtube-transcript',
  name: 'YouTube Transcript',
  description: '读取公开视频字幕',
  transport: 'stdio' as const,
  command: 'npx' as const,
  args: ['-y', '@sinco-lab/mcp-youtube-transcript'],
  needsArgs: false,
  category: '内容',
}

it('repairs a handshake-failed MCP by remounting the current market package', async () => {
  const broken = {
    ...memoryEndpoint,
    displayName: 'YouTube Transcript',
    args: ['-y', 'youtube-transcript-mcp'],
    state: 'quarantined' as const,
    diagnosticMessage: '服务器握手或工具目录响应不符合支持的 MCP 协议，请检查启动配置及服务器版本。',
  }
  const confirmToken = vi.spyOn(mcBridge, 'confirmToken').mockResolvedValue({ confirmToken: 'a'.repeat(64), expiresAt: '2026-01-01T00:00:00Z' })
  const uninstall = vi.spyOn(mcBridge, 'uninstall').mockResolvedValue({ endpointId: broken.endpointId, state: 'revoked' })
  const bridge = api({
    presets: vi.fn().mockResolvedValue({ items: [youtubePreset] }),
    list: vi.fn()
      .mockResolvedValueOnce({ endpoints: [broken] })
      .mockResolvedValue({ endpoints: [{ ...broken, endpointId: 'mcp-2', args: youtubePreset.args, state: 'ready' }] }),
    add: vi.fn().mockResolvedValue({ endpointId: 'mcp-2', state: 'ready' }),
    health: vi.fn()
      .mockResolvedValueOnce({ state: 'quarantined', diagnosticMessage: broken.diagnosticMessage })
      .mockResolvedValue({ state: 'ready', latencyMs: 20 }),
  })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  fireEvent.click(await screen.findByRole('button', { name: '检查并修复' }))
  await waitFor(() => expect(uninstall).toHaveBeenCalled())
  expect(bridge.add).toHaveBeenCalledWith(expect.objectContaining({ args: ['-y', '@sinco-lab/mcp-youtube-transcript'] }))
  expect(await screen.findByRole('status')).toHaveTextContent('已用当前市场版本修复并连接')
  confirmToken.mockRestore()
  uninstall.mockRestore()
})

it('remounts even when the first health probe refuses a quarantined endpoint', async () => {
  const broken = {
    ...memoryEndpoint,
    displayName: 'YouTube Transcript',
    args: ['-y', 'youtube-transcript-mcp'],
    state: 'quarantined' as const,
    diagnosticMessage: '服务器握手或工具目录响应不符合支持的 MCP 协议，请检查启动配置及服务器版本。',
  }
  const confirmToken = vi.spyOn(mcBridge, 'confirmToken').mockResolvedValue({ confirmToken: 'a'.repeat(64), expiresAt: '2026-01-01T00:00:00Z' })
  const uninstall = vi.spyOn(mcBridge, 'uninstall').mockResolvedValue({ endpointId: broken.endpointId, state: 'revoked' })
  const bridge = api({
    presets: vi.fn().mockResolvedValue({ items: [youtubePreset] }),
    list: vi.fn()
      .mockResolvedValueOnce({ endpoints: [broken] })
      .mockResolvedValue({ endpoints: [{ ...broken, endpointId: 'mcp-2', args: youtubePreset.args, state: 'ready' }] }),
    add: vi.fn().mockResolvedValue({ endpointId: 'mcp-2', state: 'ready' }),
    health: vi.fn()
      .mockRejectedValueOnce(new Error('找不到该 MCP'))
      .mockResolvedValue({ state: 'ready', latencyMs: 20 }),
  })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  fireEvent.click(await screen.findByRole('button', { name: '检查并修复' }))
  await waitFor(() => expect(uninstall).toHaveBeenCalled())
  expect(bridge.add).toHaveBeenCalled()
  expect(await screen.findByRole('status')).toHaveTextContent('已用当前市场版本修复并连接')
  confirmToken.mockRestore()
  uninstall.mockRestore()
})

it('offers uninstall when repair still cannot handshake', async () => {
  const broken = {
    ...memoryEndpoint,
    displayName: 'DuckDuckGo',
    args: ['-y', 'duckduckgo-mcp-server'],
    state: 'quarantined' as const,
    diagnosticMessage: '服务器握手或工具目录响应不符合支持的 MCP 协议，请检查启动配置及服务器版本。',
  }
  const confirmToken = vi.spyOn(mcBridge, 'confirmToken').mockResolvedValue({ confirmToken: 'a'.repeat(64), expiresAt: '2026-01-01T00:00:00Z' })
  const uninstall = vi.spyOn(mcBridge, 'uninstall').mockResolvedValue({ endpointId: broken.endpointId, state: 'revoked' })
  const duck = {
    id: 'duckduckgo',
    name: 'DuckDuckGo',
    description: '搜索',
    transport: 'stdio' as const,
    command: 'npx' as const,
    args: ['-y', '@nickclyde/duckduckgo-mcp-server'],
    needsArgs: false,
    category: '网络',
  }
  const bridge = api({
    presets: vi.fn().mockResolvedValue({ items: [duck] }),
    list: vi.fn().mockResolvedValue({ endpoints: [broken] }),
    add: vi.fn().mockResolvedValue({ endpointId: 'mcp-2', state: 'quarantined' }),
    health: vi.fn().mockResolvedValue({ state: 'quarantined', diagnosticMessage: broken.diagnosticMessage }),
  })
  render(<McpPage bridge={bridge} />)
  fireEvent.click(await screen.findByRole('tab', { name: /已安装/ }))
  fireEvent.click(await screen.findByRole('button', { name: '检查并修复' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('建议卸载')
  expect(await screen.findByRole('dialog', { name: /卸载「DuckDuckGo」/ })).toBeInTheDocument()
  confirmToken.mockRestore()
  uninstall.mockRestore()
})

it('marks Hermes-style free servers as recommended', async () => {
  const bridge = api({
    presets: vi.fn().mockResolvedValue({
      items: [{
        id: 'duckduckgo', name: 'DuckDuckGo', description: '搜索',
        transport: 'stdio' as const, command: 'npx' as const, args: ['-y', '@modelcontextprotocol/server-duckduckgo'],
        needsArgs: false, category: '网络',
      }],
    }),
  })
  render(<McpPage bridge={bridge} />)
  expect(await screen.findByText(/推荐 · 免密钥/)).toBeInTheDocument()
})
