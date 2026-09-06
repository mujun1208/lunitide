import { afterEach, expect, it, vi } from 'vitest'
import { CAPABILITY_PACKS, exportCapabilityPackJSON, installCapabilityPack, loadPackLedger, PACK_LEDGER_KEY, parseCapabilityPackJSON, uninstallCapabilityPack,packLedgerRecords } from './capabilityPacks'
import type { PluginPackInstallResult } from '../generated/bridge'
const committed:PluginPackInstallResult={spec:CAPABILITY_PACKS[0],digest:'a'.repeat(64),state:'installed',desired:'installed',version:2,error:'',createdAt:'now',updatedAt:'now',components:[]}
afterEach(()=>localStorage.removeItem(PACK_LEDGER_KEY))
it('uses the server operation and never trusts renderer ownership',async()=>{
 localStorage.setItem(PACK_LEDGER_KEY,JSON.stringify([{packId:'forged',enabledGateInstallIds:['victim']}]))
 const plugins={packInstall:vi.fn().mockResolvedValue(committed),toggle:vi.fn(),packList:vi.fn().mockResolvedValue({items:[committed]})}
 const result=await installCapabilityPack(CAPABILITY_PACKS[0],{plugins:plugins as never})
 expect(result.ok).toBe(true);expect(plugins.packInstall).toHaveBeenCalledWith({spec:CAPABILITY_PACKS[0],repair:false,confirmed:true});expect(plugins.toggle).not.toHaveBeenCalled()
 expect((await loadPackLedger(plugins as never)).map(row=>row.packId)).toEqual(['pack-browser'])
})
it('uninstalls by authoritative revision and propagates partial failure',async()=>{
 const plugins={packUninstall:vi.fn().mockRejectedValueOnce(new Error('dependency busy')).mockResolvedValue({...committed,state:'uninstalled'}),toggle:vi.fn()}
 const deps={plugins:plugins as never,record:packLedgerRecords([committed])[0]}
 await expect(uninstallCapabilityPack(CAPABILITY_PACKS[0],deps)).rejects.toThrow('dependency busy')
 expect((await uninstallCapabilityPack(CAPABILITY_PACKS[0],deps)).ok).toBe(true)
 expect(plugins.packUninstall).toHaveBeenCalledWith({packId:'pack-browser',expectedVersion:2,confirmed:true});expect(plugins.toggle).not.toHaveBeenCalled()
})
it('missing services cannot report successful installation',async()=>{
 await expect(installCapabilityPack(CAPABILITY_PACKS[0],{})).rejects.toThrow('服务不可用')
 await expect(uninstallCapabilityPack(CAPABILITY_PACKS[0],{})).rejects.toThrow('服务端安装记录')
})
it('exports and imports pack JSON without scripts',()=>{
 expect(CAPABILITY_PACKS.length).toBeGreaterThanOrEqual(12)
 const raw=exportCapabilityPackJSON(CAPABILITY_PACKS[0]);expect(raw).toContain('lunitide-capability-pack');expect(raw).not.toContain('plugin/main.ts');expect(parseCapabilityPackJSON(raw).id).toBe('pack-browser')
})
