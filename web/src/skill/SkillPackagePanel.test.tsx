import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import type {SkillBridge} from '../bridge/client'
import type {SkillPackageListResult,SkillPackageReadResult} from '../generated/bridge'
import {SkillPackagePanel} from './SkillPackagePanel'

afterEach(cleanup)
const id='01ARZ3NDEKTSV4RRFFQ69G5FAA'
const listing:SkillPackageListResult={skillId:id,rootPath:'C:/Lunitide/skills/weekly-report',revision:'rev-1',entries:[{path:'SKILL.md',kind:'file',size:30},{path:'references/example.md',kind:'file',size:10}]}
const read=(path='SKILL.md',content='# 周报技能\n按照原始内容生成。'):SkillPackageReadResult=>({skillId:id,path,content,encoding:'utf8',size:30,nextOffset:30,eof:true,digest:'digest',revision:'rev-1'})
const api=(overrides:Partial<SkillBridge>={})=>({packageList:vi.fn().mockResolvedValue(listing),packageRead:vi.fn().mockImplementation(async({path})=>read(path)),...overrides}) as unknown as SkillBridge

it('does not show raw English directory load failures',async()=>{
  render(<SkillPackagePanel skillId={id} bridge={api({packageList:vi.fn().mockRejectedValue(new Error('Failed to fetch'))})}/>)
  expect(await screen.findByRole('alert')).toHaveTextContent('技能目录读取失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('opens real package files with the listed revision and renders content as text',async()=>{
  const bridge=api({packageRead:vi.fn().mockImplementation(async({path})=>read(path,path==='SKILL.md'?'# 周报\n<script>不执行</script>':'参考资料原文'))})
  const {container}=render(<SkillPackagePanel skillId={id} bridge={bridge}/>)
  expect(await screen.findByLabelText('文件内容 SKILL.md')).toHaveTextContent('# 周报 <script>不执行</script>')
  expect(container.querySelector('script')).toBeNull()
  expect(screen.getByText(listing.rootPath)).toBeInTheDocument()
  expect(bridge.packageRead).toHaveBeenCalledWith({skillId:id,path:'SKILL.md',offset:0,limit:16384,expectedRevision:'rev-1'})
  fireEvent.click(screen.getByRole('button',{name:/example.md/}))
  expect(await screen.findByText('参考资料原文')).toBeInTheDocument()
  expect(bridge.packageRead).toHaveBeenLastCalledWith(expect.objectContaining({path:'references/example.md',offset:0}))
})

it('pages UTF8 content using engine byte offsets and keeps the prior page available',async()=>{
  const bridge=api({packageRead:vi.fn().mockImplementation(async({path,offset})=>({...read(path,offset?'第二段':'中文开头'),eof:offset>0,nextOffset:offset?30:12}))})
  render(<SkillPackagePanel skillId={id} bridge={bridge}/>)
  await screen.findByText('中文开头')
  fireEvent.click(screen.getByRole('button',{name:'下一段'}))
  await screen.findByText('第二段')
  expect(bridge.packageRead).toHaveBeenLastCalledWith(expect.objectContaining({offset:12}))
  fireEvent.click(screen.getByRole('button',{name:'上一段'}))
  await screen.findByText('中文开头')
  expect(bridge.packageRead).toHaveBeenLastCalledWith(expect.objectContaining({offset:0}))
})

it('rejects a changed package revision while loading more directory entries',async()=>{
  const bridge=api({packageList:vi.fn().mockResolvedValueOnce({...listing,nextCursor:'cursor-1'}).mockResolvedValue({...listing,revision:'rev-2',entries:[{path:'changed.md',kind:'file',size:2}]})})
  render(<SkillPackagePanel skillId={id} bridge={bridge}/>)
  fireEvent.click(await screen.findByRole('button',{name:'更多文件'}))
  expect(await screen.findByRole('alert')).toHaveTextContent('技能文件已更新')
  expect(screen.queryByRole('button',{name:/changed.md/})).not.toBeInTheDocument()
})

it('ignores a late file response after changing skills and distinguishes binary files',async()=>{
  let resolve!:(value:SkillPackageReadResult)=>void
  const second='01ARZ3NDEKTSV4RRFFQ69G5FAB'
  const bridge=api({packageList:vi.fn().mockImplementation(async({skillId})=>({...listing,skillId})),packageRead:vi.fn().mockImplementation(({skillId})=>skillId===id?new Promise<SkillPackageReadResult>(r=>{resolve=r}):Promise.resolve({...read(),skillId:second,encoding:'binary'}))})
  const view=render(<SkillPackagePanel skillId={id} bridge={bridge}/>)
  await waitFor(()=>expect(bridge.packageRead).toHaveBeenCalledOnce())
  view.rerender(<SkillPackagePanel skillId={second} bridge={bridge}/>)
  expect(await screen.findByText(/这是二进制文件/)).toBeInTheDocument()
  await act(async()=>resolve(read('SKILL.md','旧技能的迟到内容')))
  expect(screen.queryByText('旧技能的迟到内容')).not.toBeInTheDocument()
})

it('reports unreadable package files and permits a real reload instead of inventing content',async()=>{
  const bridge=api({packageRead:vi.fn().mockRejectedValueOnce(new Error('文件已变更')).mockResolvedValue(read())})
  render(<SkillPackagePanel skillId={id} bridge={bridge}/>)
  expect(await screen.findByRole('alert')).toHaveTextContent('文件已变更')
  fireEvent.click(screen.getByRole('button',{name:'刷新目录'}))
  expect(await screen.findByText(/按照原始内容生成/)).toBeInTheDocument()
  expect(bridge.packageRead).toHaveBeenCalledTimes(2)
})
