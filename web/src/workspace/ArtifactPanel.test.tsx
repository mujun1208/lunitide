import{cleanup,fireEvent,render,screen,waitFor}from'@testing-library/react'
import{afterEach,expect,it,vi}from'vitest'
import type{ArtifactReviewBridge}from'../bridge/client'
import{ArtifactPanel,type ArtifactCard}from'./ArtifactPanel'
const P='01ARZ3NDEKTSV4RRFFQ69G5FAV',emptyList={items:[],acceptedPaths:[]}
afterEach(cleanup)
const card=(kind:ArtifactCard['kind']='xlsx',path='reports/q3.xlsx'):ArtifactCard=>({callId:`call-${path}`,toolName:'excel.gen',kind,path,content:''})
const bridge=(list=emptyList,preview?:{kind:string;path:string;size:number;content:string}):{b:ArtifactReviewBridge;append:ReturnType<typeof vi.fn>}=>{const append=vi.fn().mockResolvedValue({ok:true});return{b:{list:vi.fn().mockResolvedValue(list),append,preview:vi.fn().mockImplementation(()=>preview?Promise.resolve(preview):Promise.reject(new Error('no preview')))}as unknown as ArtifactReviewBridge,append}}
it('shows empty hint when no artifacts exist',()=>{render(<ArtifactPanel sessionId={P} artifacts={[]} bridge={bridge().b}/>);expect(screen.getByText(/还没有生成的产物/)).toBeInTheDocument()})
it('renders artifact cards with kind label and drives comment→revise→accept loop',async()=>{const accepted:string[]=[];const list=vi.fn().mockImplementation(()=>Promise.resolve({items:[],acceptedPaths:[...accepted]}));const append=vi.fn().mockImplementation((_p:{action:string})=>{if(_p.action==='accept')accepted.push('reports/q3.xlsx');return Promise.resolve({ok:true})});const b={list,append,preview:vi.fn()}as unknown as ArtifactReviewBridge;const onRevise=vi.fn();render(<ArtifactPanel sessionId={P} artifacts={[card()]} bridge={b} onRevise={onRevise}/>)
await waitFor(()=>expect(b.list).toHaveBeenCalledWith({sessionId:P}))
// comment requires a note first.
fireEvent.click(screen.getByRole('button',{name:'评论'}));expect(screen.getByText('请先填写评论内容')).toBeInTheDocument();expect(append).not.toHaveBeenCalled()
// comment with note.
fireEvent.change(screen.getByLabelText(/产物备注/),{target:{value:'补一列环比'}});fireEvent.click(screen.getByRole('button',{name:'评论'}))
await waitFor(()=>expect(append).toHaveBeenCalledWith({sessionId:P,callId:'call-reports/q3.xlsx',toolName:'excel.gen',kind:'xlsx',path:'reports/q3.xlsx',action:'comment',note:'补一列环比'}))
// revise propagates the onRevise callback for re-generation.
fireEvent.change(screen.getByLabelText(/产物备注/),{target:{value:'图表改成折线'}});fireEvent.click(screen.getByRole('button',{name:'修改'}))
await waitFor(()=>expect(onRevise).toHaveBeenCalledWith('reports/q3.xlsx','图表改成折线'))
// accept lands the accepted badge via the reloaded list (accept needs no note).
fireEvent.click(screen.getByRole('button',{name:'验收'}))
await waitFor(()=>expect(screen.getByText('已验收',{selector:'.artifact-accepted-badge'})).toBeInTheDocument())})
it('renders kind-aware previews: xlsx grid vs docx text vs error state',async()=>{const{b}=bridge(emptyList,{kind:'xlsx',path:'reports/q3.xlsx',size:10,content:JSON.stringify({sheets:[{name:'销售',rows:12,cols:3,truncated:false,preview:[['月份','销量'],['1月',10]],header:['月份','销量']}]})});render(<ArtifactPanel sessionId={P} artifacts={[card()]} bridge={b}/>)
fireEvent.click(await screen.findByRole('button',{name:'预览'}))
expect(await screen.findByText(/销售 · 12×3/)).toBeInTheDocument();expect(screen.getByText('1月')).toBeInTheDocument()
// docx falls to the plain text lane.
const docxBridge=bridge(emptyList,{kind:'docx',path:'周报.docx',size:8,content:'进展\n完成闭环'});render(<ArtifactPanel sessionId={P} artifacts={[card('docx','周报.docx')]} bridge={docxBridge.b}/>)
fireEvent.click(await screen.findByRole('button',{name:'预览'}))
expect(await screen.findByText(/完成闭环/)).toBeInTheDocument()
// preview failure surfaces an alert without crashing.
const fail=bridge(emptyList);render(<ArtifactPanel sessionId={P} artifacts={[card('pptx','deck.pptx')]} bridge={fail.b}/>)
fireEvent.click(await screen.findByRole('button',{name:'预览'}))
expect(await screen.findByRole('alert')).toBeInTheDocument()})
it('exports artifacts to the selected target and validates custom dirs',async()=>{const exportArtifact=vi.fn().mockResolvedValue({exportedPath:'C:/Users/u/Desktop/q3.xlsx',size:1024});const b={list:vi.fn().mockResolvedValue(emptyList),append:vi.fn().mockResolvedValue({ok:true}),preview:vi.fn(),exportArtifact}as unknown as ArtifactReviewBridge
render(<ArtifactPanel sessionId={P} artifacts={[card()]} bridge={b}/>)
fireEvent.click(screen.getByRole('button',{name:'导出'}))
await waitFor(()=>expect(exportArtifact).toHaveBeenCalledWith({sessionId:P,path:'reports/q3.xlsx',target:'desktop',overwrite:false}));expect(await screen.findByText(/已导出到 C:\/Users\/u\/Desktop\/q3\.xlsx/)).toBeInTheDocument()
// custom target demands an explicit absolute path first.
fireEvent.change(screen.getByLabelText('导出目录'),{target:{value:'custom'}});fireEvent.click(screen.getByRole('button',{name:'导出'}))
expect(screen.getByText('请先填写自定义导出目录的绝对路径')).toBeInTheDocument()
fireEvent.change(screen.getByLabelText('自定义导出目录'),{target:{value:'D:\\客户交付'}});fireEvent.click(screen.getByRole('button',{name:'导出'}))
await waitFor(()=>expect(exportArtifact).toHaveBeenLastCalledWith({sessionId:P,path:'reports/q3.xlsx',target:'D:\\客户交付',overwrite:false}))})
