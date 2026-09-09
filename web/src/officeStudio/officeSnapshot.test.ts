import { describe, expect, it, vi } from 'vitest'
import { collectOfficeDetail, collectOfficeItems } from './officeSnapshot'
import type { OfficeTaskDetail } from './officeStudioApi'

function page(offset=0,next=-1,digest='same'):OfficeTaskDetail {
 return {task:{id:'task',sessionId:'session',title:'报告',goal:'',revision:1,status:'succeeded',createdAt:'',updatedAt:''},artifacts:[],steps:[],sources:[],snapshotOffset:offset,nextSnapshotOffset:next,snapshotDigest:digest}
}
function artifact(check:string,version='v1') {
 return {id:'artifact',name:'报告.docx',kind:'docx' as const,revision:1,headVersionId:'v1',versions:[{id:version,versionNo:1,quality:'partial' as const,mode:'managed' as const,sha256:'a',size:100,createdAt:'',validations:[{id:check,label:check,status:'unavailable' as const,severity:'warning' as const,message:'未实测'}]}]}
}
describe('Office snapshot transport paging',()=>{
 it('merges split versions, validation fragments, steps and sources without losing accepted history',async()=>{
  const first={...page(0,2),artifacts:[artifact('package')]}
  const next={...page(2,4),artifacts:[artifact('render')],steps:[{id:'s1',label:'生成',status:'succeeded'}]}
  const last={...page(4),artifacts:[artifact('old','v0')],sources:[{id:'source',name:'原始数据'}]}
  const read=vi.fn().mockResolvedValueOnce(next).mockResolvedValueOnce(last)
  const result=await collectOfficeDetail(Promise.resolve(first),read)
  expect(result.artifacts).toHaveLength(1)
  expect(result.artifacts[0].versions).toHaveLength(2)
  expect(result.artifacts[0].versions[0].validations?.map(x=>x.id)).toEqual(['package','render'])
  expect(result.steps).toHaveLength(1);expect(result.sources).toHaveLength(1)
  expect(read).toHaveBeenLastCalledWith('task',{snapshotOffset:4,snapshotDigest:'same'})
 })
 it('keeps committed mutation result when later read fails; never repeats the write',async()=>{
  const create=vi.fn().mockResolvedValue({...page(0,1),artifacts:[artifact('package')]})
  const read=vi.fn().mockRejectedValue(new Error('network unavailable'))
  const result=await collectOfficeDetail(create(),read)
  expect(create).toHaveBeenCalledTimes(1)
  expect(result.loadNotice).toContain('操作结果已保留')
  expect(result.artifacts[0].id).toBe('artifact')
 })
 it('restarts reading a changed snapshot once without replaying the mutation',async()=>{
  const read=vi.fn().mockRejectedValueOnce({code:'OFFICE_SNAPSHOT_CHANGED'}).mockResolvedValueOnce({...page(0,2,'new'),artifacts:[artifact('new')]}).mockResolvedValueOnce(page(2,-1,'new'))
  const result=await collectOfficeDetail(Promise.resolve(page(0,1)),read)
  expect(read).toHaveBeenNthCalledWith(2,'task')
  expect(result.artifacts[0].versions[0].validations?.[0].id).toBe('new')
  expect(result.loadNotice).toBeUndefined()
 })
 it('stops invalid cursors and cross-task pages with a visible partial-read notice',async()=>{
  const read=vi.fn().mockResolvedValue({...page(1),task:{...page().task,id:'other'}})
  expect((await collectOfficeDetail(Promise.resolve(page(0,1)),read)).loadNotice).toBeTruthy()
  expect((await collectOfficeDetail(Promise.resolve(page(1,1)),read)).loadNotice).toBeTruthy()
 })
 it('loads all list pages, restarts on a changed snapshot, and does not silently return a partial list',async()=>{
  const read=vi.fn().mockResolvedValueOnce({...page(0,1),items:[{id:'old'}]}).mockRejectedValueOnce({code:'OFFICE_SNAPSHOT_CHANGED'}).mockResolvedValueOnce({...page(0,1,'new'),items:[{id:'new1'}]}).mockResolvedValueOnce({...page(1,-1,'new'),items:[{id:'new2'}]})
  expect((await collectOfficeItems(read)).items.map(x=>x.id)).toEqual(['new1','new2'])
  const failing=vi.fn().mockResolvedValueOnce({...page(0,1),items:[{id:'first'}]}).mockRejectedValue(new Error('network'))
  await expect(collectOfficeItems(failing)).rejects.toThrow('network')
 })
})
