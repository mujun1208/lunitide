import {expect,it,vi} from 'vitest'
import {capBridgeDeadlineMs,createPluginBridge,MCP_SETUP_DEADLINE_MS,PACK_INSTALL_DEADLINE_MS,type WebViewTransport} from './client'
it('sends a pack install envelope that can hold a cold MCP handshake',()=>{
 const sent:Array<{method:string;deadlineMs:number}>=[]
 const transport:WebViewTransport={addEventListener:vi.fn(),removeEventListener:vi.fn(),postMessage:m=>{sent.push(m as {method:string;deadlineMs:number})}}
 const bridge=createPluginBridge(transport)
 if(!bridge.packInstall||!bridge.packUninstall)throw new Error('pack bridge lost its install methods')
 void bridge.packInstall({id:'pack-browser',name:'浏览器工作包',description:'d',skills:[],mcpPresetIds:['playwright'],toolGates:[]} as never)
 void bridge.packUninstall({id:'pack-browser'} as never)
 expect(sent.map(m=>m.deadlineMs)).toEqual([PACK_INSTALL_DEADLINE_MS,PACK_INSTALL_DEADLINE_MS])
 expect(PACK_INSTALL_DEADLINE_MS).toBeGreaterThan(MCP_SETUP_DEADLINE_MS)
})
it('stops clamping pack methods to the 30s default cap',()=>{
 for(const method of ['plugin.pack.install','plugin.pack.uninstall'])expect(capBridgeDeadlineMs(method,PACK_INSTALL_DEADLINE_MS)).toBe(PACK_INSTALL_DEADLINE_MS)
 expect(capBridgeDeadlineMs('plugin.pack.list',PACK_INSTALL_DEADLINE_MS)).toBe(30000)
})
