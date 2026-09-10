import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {LanguageProvider} from '../i18n/language'
import {ExpertKnowledgePanel} from './ExpertKnowledgePanel'
import type {KnowledgeSource,KnowledgeStats} from './knowledgeTypes'

afterEach(cleanup)
const expertId='01ARZ3NDEKTSV4RRFFQ69G5FAV'
const source:KnowledgeSource={sourceId:'01ARZ3NDEKTSV4RRFFQ69G5FAX',collectionId:'01ARZ3NDEKTSV4RRFFQ69G5FAW',path:'C:\\manuals\\guide.md',mediaType:'text/markdown',sourceLocator:'mro://AMM/42',sha256:'a'.repeat(64),state:'stale',version:2,revision:5,checkedAt:'2026-09-06T11:00:00Z',error:'原文件已变更，请刷新索引',createdAt:'2026-09-06T10:00:00Z',versions:[{version:1,sha256:'b'.repeat(64),state:'fresh',error:'',createdAt:'2026-09-06T10:00:00Z'}]}
const stats:KnowledgeStats={collectionId:source.collectionId,documentCount:0,readyCount:0,chunkCount:0,nodeCount:0,memoryCount:0,missing:false,sources:[source]}

it('refreshes the exact source with observed revision and shows version provenance',async()=>{
 const knowledgeGet=vi.fn().mockResolvedValueOnce(stats).mockResolvedValue({...stats,sources:[{...source,state:'fresh',version:3,revision:7,error:''}]})
 const knowledgeIngest=vi.fn().mockResolvedValue({documents:[{indexState:'ready',preview:['New verified text']}]})
 render(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId={expertId} knowledgeGet={knowledgeGet} knowledgeIngest={knowledgeIngest}/></LanguageProvider>)
 expect(await screen.findByText(/原文件已变更，请刷新索引/)).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'刷新来源'}))
 await waitFor(()=>expect(knowledgeIngest).toHaveBeenCalledWith({expertId,path:source.path,mediaType:source.mediaType,sourceLocator:source.sourceLocator,expectedRevision:5}))
 expect(await screen.findByText(/v3 · 已索引/)).toBeInTheDocument()
 expect(screen.getByText(/b{64}/)).toBeInTheDocument()
})

it('reloads durable failure after a refresh rejects instead of keeping ready state',async()=>{
 const knowledgeGet=vi.fn().mockResolvedValueOnce({...stats,sources:[{...source,state:'fresh',error:''}]}).mockResolvedValue({...stats,sources:[{...source,state:'missing',error:'原文件已删除'}]})
 const knowledgeIngest=vi.fn().mockRejectedValue(new Error('KB_INDEX_FAILED'))
 render(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId={expertId} knowledgeGet={knowledgeGet} knowledgeIngest={knowledgeIngest}/></LanguageProvider>)
 fireEvent.click(await screen.findByRole('button',{name:'刷新来源'}))
 expect(await screen.findByText('原文件已删除')).toBeInTheDocument()
 expect(screen.getByRole('alert')).toHaveTextContent('无法抽出正文或刷新来源')
 expect(screen.queryByText(/已索引，使用时校验/)).toBeNull()
})

it('ignores an earlier expert upload when the selected expert changes',async()=>{
 let resolve!:(value:{documents:Array<{indexState:string;preview:string[]}>})=>void
 const knowledgeIngest=vi.fn().mockReturnValue(new Promise(r=>{resolve=r}))
 const knowledgeGet=vi.fn().mockResolvedValue(stats)
 const view=render(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId={expertId} knowledgeGet={knowledgeGet} knowledgeIngest={knowledgeIngest}/></LanguageProvider>)
 fireEvent.click(await screen.findByRole('button',{name:'刷新来源'}))
 view.rerender(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId="01ARZ3NDEKTSV4RRFFQ69G5FAY" knowledgeGet={knowledgeGet} knowledgeIngest={knowledgeIngest}/></LanguageProvider>)
 await act(async()=>{resolve({documents:[{indexState:'ready',preview:['Wrong expert old reply']}]})})
 expect(screen.queryByText('Wrong expert old reply')).toBeNull()
 expect(screen.queryByText('来源已校验，索引已更新。')).toBeNull()
 expect(knowledgeGet).toHaveBeenCalledTimes(2)
})

it('imports a local path without reading the entire file in the renderer',async()=>{
 const knowledgeGet=vi.fn().mockResolvedValue(stats)
 const knowledgeIngest=vi.fn().mockResolvedValue({documents:[{indexState:'ready'}]})
 render(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId={expertId} knowledgeGet={knowledgeGet} knowledgeIngest={knowledgeIngest}/></LanguageProvider>)
 const input=await screen.findByLabelText('文件完整路径')
 fireEvent.change(input,{target:{value:source.path}})
 fireEvent.click(screen.getByRole('button',{name:'导入此路径'}))
 await waitFor(()=>expect(knowledgeIngest).toHaveBeenCalledWith({expertId,path:source.path,mediaType:undefined}))
})

it('navigates source pages using the returned cursor and can return to the first page',async()=>{
 const cursor='01ARZ3NDEKTSV4RRFFQ69G5FAZ'
 const knowledgeGet=vi.fn().mockImplementation(async (payload:{sourcesAfter?:string})=>payload.sourcesAfter?{...stats,sources:[{...source,path:'C:\\manuals\\second-page.md'}],nextSourceCursor:''}:{...stats,nextSourceCursor:cursor})
 render(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId={expertId} knowledgeGet={knowledgeGet}/></LanguageProvider>)
 fireEvent.click(await screen.findByRole('button',{name:'下一页来源'}))
 expect(await screen.findByText(/second-page.md/)).toBeInTheDocument()
 expect(knowledgeGet).toHaveBeenLastCalledWith({expertId,sourcesAfter:cursor})
 fireEvent.click(screen.getByRole('button',{name:'上一页来源'}))
 expect(await screen.findByText(/guide.md/)).toBeInTheDocument()
 expect(knowledgeGet).toHaveBeenLastCalledWith({expertId})
})

it('deletes a source with the observed revision and reloads the tombstone in Chinese',async()=>{
 const knowledgeGet=vi.fn().mockResolvedValueOnce(stats).mockResolvedValue({...stats,sources:[{...source,state:'failed',error:'知识来源已删除'}]})
 const knowledgeDelete=vi.fn().mockResolvedValue({sourceId:source.sourceId,state:'failed',error:'知识来源已删除'})
 render(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId={expertId} knowledgeGet={knowledgeGet} knowledgeDelete={knowledgeDelete}/></LanguageProvider>)
 fireEvent.click(await screen.findByRole('button',{name:'删除来源'}))
 await waitFor(()=>expect(knowledgeDelete).toHaveBeenCalledWith({expertId,sourceId:source.sourceId,expectedRevision:5}))
 await waitFor(()=>expect(screen.getAllByText('知识来源已删除').length).toBeGreaterThan(0))
 expect(screen.queryByText('tombstone:deleted')).toBeNull()
})

it('loads older history as a separate bounded page and returns to recent versions',async()=>{
 const current={...source,version:53,nextBeforeVersion:4,versions:[{version:53,sha256:'c'.repeat(64),state:'fresh',error:'',createdAt:'2026-09-06'}]}
 const knowledgeGet=vi.fn().mockImplementation(async(payload:{historyBeforeVersion?:number})=>({...stats,sources:payload.historyBeforeVersion?[{...current,nextBeforeVersion:0,versions:[{version:1,sha256:'d'.repeat(64),state:'fresh',error:'',createdAt:'2026-01-01'}]}]:[current]}))
 render(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId={expertId} knowledgeGet={knowledgeGet}/></LanguageProvider>)
 fireEvent.click(await screen.findByText('来源版本记录（每页最多 50 版）'))
 fireEvent.click(screen.getByRole('button',{name:'更早版本'}))
 expect(await screen.findByText(/d{64}/)).toBeInTheDocument()
 expect(screen.queryByText(/c{64}/)).toBeNull()
 expect(knowledgeGet).toHaveBeenLastCalledWith({expertId,historySourceId:source.sourceId,historyBeforeVersion:4})
 fireEvent.click(screen.getByRole('button',{name:'返回最新版本'}))
 expect(await screen.findByText(/c{64}/)).toBeInTheDocument()
 expect(knowledgeGet).toHaveBeenLastCalledWith({expertId,historySourceId:source.sourceId})
})
