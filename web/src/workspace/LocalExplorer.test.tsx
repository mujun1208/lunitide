import{cleanup,render,screen,waitFor}from'@testing-library/react'
import userEvent from'@testing-library/user-event'
import{afterEach,expect,it,vi}from'vitest'
import{LocalExplorer}from'./LocalExplorer'
import{resetSessionWorkspaceBindings}from'./workspaceSession'
afterEach(()=>{cleanup();resetSessionWorkspaceBindings()})
it('does not show raw English workspace load or open failures, and ignores OS cancel',async()=>{
  const user=userEvent.setup()
  render(<LocalExplorer bridge={{root:vi.fn().mockRejectedValue(new Error('Failed to fetch')),select:vi.fn(),clear:vi.fn(),open:vi.fn(),list:vi.fn(),read:vi.fn()}} onPreview={vi.fn()}/>)
  expect(await screen.findByRole('alert')).toHaveTextContent('工作区载入失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  const select=vi.fn().mockRejectedValue(new Error('Failed to fetch'))
  render(<LocalExplorer bridge={{root:vi.fn().mockResolvedValue({name:'',path:'',bound:false}),select,clear:vi.fn(),open:vi.fn(),list:vi.fn(),read:vi.fn()}} onPreview={vi.fn()}/>)
  await screen.findByText('这一轮对话还没有打开本地目录。需要时再选择文件夹。')
  await user.click(screen.getByRole('button',{name:'选择文件夹'}))
  expect(await screen.findByRole('alert')).toHaveTextContent('未选择工作区')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  const cancelled=vi.fn().mockRejectedValue(new Error('用户取消了选择'))
  render(<LocalExplorer bridge={{root:vi.fn().mockResolvedValue({name:'',path:'',bound:false}),select:cancelled,clear:vi.fn(),open:vi.fn(),list:vi.fn(),read:vi.fn()}} onPreview={vi.fn()}/>)
  await user.click(await screen.findByRole('button',{name:'选择文件夹'}))
  await waitFor(()=>expect(cancelled).toHaveBeenCalled())
  expect(screen.queryByRole('alert')).toBeNull()
  expect(screen.queryByText('用户取消了选择')).toBeNull()
  cleanup()
  const read=vi.fn().mockRejectedValue(new Error('Failed to fetch'))
  render(<LocalExplorer bridge={{root:vi.fn().mockResolvedValue({name:'repo',path:'C:\\repo',bound:true}),select:vi.fn(),clear:vi.fn(),open:vi.fn(),list:vi.fn().mockResolvedValue({items:[{name:'notes.txt',path:'notes.txt',directory:false}]}),read}} onPreview={vi.fn()}/>)
  await user.click(await screen.findByRole('treeitem',{name:/notes.txt/}))
  expect(await screen.findByRole('alert')).toHaveTextContent('无法打开文件')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('expands only bridge-provided paths and previews selected supported files',async()=>{const bridge={root:vi.fn().mockResolvedValue({name:'repo',path:'C:\\repo',bound:true}),select:vi.fn(),clear:vi.fn(),open:vi.fn(),list:vi.fn().mockImplementation((path='')=>Promise.resolve({items:path==='src'?[{name:'main.ts',path:'src/main.ts',directory:false}]:[{name:'src',path:'src',directory:true}]})),read:vi.fn().mockResolvedValue({path:'src/main.ts',content:'safe',size:4})},preview=vi.fn(),user=userEvent.setup();render(<LocalExplorer bridge={bridge} onPreview={preview}/>);await user.click(await screen.findByRole('treeitem',{name:/src/}));await user.click(await screen.findByRole('treeitem',{name:/main.ts/}));await waitFor(()=>expect(bridge.list).toHaveBeenCalledWith('src'));expect(bridge.read).toHaveBeenCalledWith('src/main.ts');expect(preview).toHaveBeenCalledWith(expect.objectContaining({content:'safe'}));expect(bridge.clear).not.toHaveBeenCalled()})
it('starts a conversation unbound and only shows a folder after this session selects it',async()=>{const root=vi.fn().mockResolvedValue({name:'知域宫殿',path:'D:\\知域宫殿',bound:true}),clear=vi.fn().mockResolvedValue({cleared:true}),select=vi.fn().mockImplementation(async()=>{root.mockResolvedValue({name:'repo',path:'C:\\repo',bound:true});return{name:'repo',path:'C:\\repo'}}),bridge={root,select,clear,open:vi.fn(),list:vi.fn().mockResolvedValue({items:[]}),read:vi.fn()},user=userEvent.setup();render(<LocalExplorer bridge={bridge} sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" isolateRoot onPreview={vi.fn()}/>);expect(await screen.findByText('这一轮对话还没有打开本地目录。需要时再选择文件夹。')).toBeInTheDocument();await waitFor(()=>expect(clear).toHaveBeenCalledOnce());expect(screen.queryByText('知域宫殿')).not.toBeInTheDocument();await user.click(screen.getByRole('button',{name:'选择文件夹'}));await waitFor(()=>expect(select).toHaveBeenCalledOnce());expect(await screen.findByText('repo')).toBeInTheDocument()})
it('keeps a project workspace bound to the last selected folder',async()=>{const bridge={root:vi.fn().mockResolvedValue({name:'知域宫殿',path:'D:\\知域宫殿',bound:true}),select:vi.fn(),clear:vi.fn(),open:vi.fn(),list:vi.fn().mockResolvedValue({items:[]}),read:vi.fn()};render(<LocalExplorer bridge={bridge} sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" onPreview={vi.fn()}/>);expect(await screen.findByText('知域宫殿')).toBeInTheDocument();expect(bridge.clear).not.toHaveBeenCalled()})

