import React from'react'
import{cleanup,render,screen,waitFor}from'@testing-library/react'
import userEvent from'@testing-library/user-event'
import{afterEach,expect,it,vi}from'vitest'
import{LocalExplorer}from'./LocalExplorer'
afterEach(cleanup)
it('expands only bridge-provided paths and previews selected supported files',async()=>{const bridge={root:vi.fn().mockResolvedValue({name:'repo',path:'C:\\repo',bound:true}),select:vi.fn(),list:vi.fn().mockImplementation((path='')=>Promise.resolve({items:path==='src'?[{name:'main.ts',path:'src/main.ts',directory:false}]:[{name:'src',path:'src',directory:true}]})),read:vi.fn().mockResolvedValue({path:'src/main.ts',content:'safe',size:4})},preview=vi.fn(),user=userEvent.setup();render(<LocalExplorer bridge={bridge} onPreview={preview}/>);await user.click(await screen.findByRole('treeitem',{name:/src/}));await user.click(await screen.findByRole('treeitem',{name:/main.ts/}));await waitFor(()=>expect(bridge.list).toHaveBeenCalledWith('src'));expect(bridge.read).toHaveBeenCalledWith('src/main.ts');expect(preview).toHaveBeenCalledWith(expect.objectContaining({content:'safe'}))})
