import{cleanup,render,screen,waitFor}from'@testing-library/react'
import userEvent from'@testing-library/user-event'
import{afterEach,describe,expect,it,vi}from'vitest'
import type{AttachmentBridge}from'../bridge/client'
import type{BrowserBridge}from'../bridge/client'
import{Workspace,workspaceTabForTool,autoRevealWorkspaceTab,autoRevealWorkspaceForHtmlTool}from'./Workspace'
import{resetSessionWorkspaceBindings}from'./workspaceSession'
const P='01ARZ3NDEKTSV4RRFFQ69G5FAV',S='01ARZ3NDEKTSV4RRFFQ69G5FAA',OTHER='01ARZ3NDEKTSV4RRFFQ69G5FAB',A='01ARZ3NDEKTSV4RRFFQ69G5FAC',NOW='2025-01-01T00:00:00Z'
const item=(id=A,sessionId=S,name='notes.txt')=>({attachmentId:id,projectId:P,sessionId,originalName:name,mime:'text/plain',size:12,sha256:'hash',parseStatus:'succeeded' as const,parseErrorCode:'',parsedTextBytes:12,createdAt:NOW})
const bridge=(text='hello https://safe.example/path and javascript:alert(1) http://plain.test')=>({list:vi.fn().mockResolvedValue({items:[item(),item(OTHER,OTHER,'hidden.txt')]}),get:vi.fn().mockResolvedValue({...item(),parsedText:text}),ingest:vi.fn(),delete:vi.fn(),begin:vi.fn(),chunk:vi.fn(),commit:vi.fn(),abort:vi.fn()} as unknown as AttachmentBridge)
afterEach(()=>{cleanup();resetSessionWorkspaceBindings()})
it('does not show raw English attachment or browser failures',async()=>{
  const attachments={list:vi.fn().mockRejectedValue(new Error('Failed to fetch')),get:vi.fn(),ingest:vi.fn(),delete:vi.fn(),begin:vi.fn(),chunk:vi.fn(),commit:vi.fn(),abort:vi.fn()} as unknown as AttachmentBridge
  render(<Workspace attachments={attachments} projectId={P} sessionId={S} onClose={vi.fn()}/>)
  expect(await screen.findByText('附件载入失败')).toBeInTheDocument()
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  const browser={open:vi.fn().mockRejectedValue(new Error('Failed to fetch')),close:vi.fn()} as unknown as BrowserBridge
  const user=userEvent.setup()
  render(<Workspace attachments={bridge()} browser={browser} projectId={P} sessionId={S} onClose={vi.fn()}/>)
  await user.click(screen.getByRole('tab',{name:'浏览器'}))
  const input=screen.getByLabelText('浏览器地址')
  await user.clear(input)
  await user.type(input,'https://example.com/')
  await user.click(screen.getByRole('button',{name:'打开独立浏览器'}))
  expect(await screen.findByLabelText('浏览器状态')).toHaveTextContent('打开失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

describe('Workspace',()=>{
 it('routes web search to the browser, office files to files, and does not auto-reveal the terminal or subagents tab',()=>{expect(workspaceTabForTool('browser.open')).toBe('browser');expect(workspaceTabForTool('web.search')).toBe('browser');expect(workspaceTabForTool('web.fetch')).toBe('browser');expect(workspaceTabForTool('html.gen')).toBe('browser');expect(workspaceTabForTool('subagent.spawn')).toBeUndefined();expect(workspaceTabForTool('subagent.join')).toBeUndefined();expect(workspaceTabForTool('command.run')).toBe('terminal');expect(autoRevealWorkspaceTab('command.run')).toBeUndefined();expect(autoRevealWorkspaceTab('web.search')).toBeUndefined();expect(autoRevealWorkspaceTab('web.search',true)).toBe('browser');expect(autoRevealWorkspaceTab('web.fetch')).toBeUndefined();expect(autoRevealWorkspaceTab('browser.open')).toBe('browser');expect(autoRevealWorkspaceTab('subagent.spawn')).toBeUndefined();expect(autoRevealWorkspaceForHtmlTool('web.search')).toBeUndefined();expect(autoRevealWorkspaceForHtmlTool('web.search',true)).toBe('browser');expect(autoRevealWorkspaceTab('pptx.gen')).toBeUndefined();expect(workspaceTabForTool('workspace.write')).toBe('code');expect(autoRevealWorkspaceTab('workspace.write')).toBeUndefined();expect(workspaceTabForTool('answer.with.https-link')).toBeUndefined()})
 it('adds user-facing workspace tabs without exposing the unfinished Agent runtime console',()=>{render(<Workspace attachments={bridge()} projectId={P} sessionId={S} onClose={vi.fn()}/>);expect(screen.getAllByRole('tab').map(tab=>tab.textContent)).toEqual(expect.arrayContaining(['文件','代码','浏览器','终端','计划','变更']));expect(screen.queryByRole('tab',{name:'子智能体'})).toBeNull();expect(screen.queryByRole('tab',{name:'自动化'})).toBeNull();expect(screen.queryByRole('tab',{name:'Agent'})).toBeNull();expect(screen.queryByRole('button',{name:'启动 Run'})).toBeNull()})
 it('opens on the requested terminal tab, shows command activity and keeps start retryable when the bridge is unavailable',async()=>{render(<Workspace attachments={bridge()} projectId={P} sessionId={S} targetTab="terminal" toolActivities={[{callId:'call-1',name:'command.run',status:'tool_started',summary:'go test ./...'}]} onClose={vi.fn()}/>);expect(screen.getByRole('tab',{name:/终端/})).toHaveAttribute('aria-selected','true');expect(screen.getByText('自动审批')).toBeInTheDocument();expect(screen.getByLabelText('命令调用情况')).toHaveTextContent('command.run');expect(screen.getByLabelText('命令调用情况')).toHaveTextContent('go test ./...');expect(screen.getByRole('button',{name:'启动交互终端'})).toBeEnabled()})
 it('closes, filters to the current session, selects an attachment, and renders parsed text',async()=>{const onClose=vi.fn(),attachments=bridge(),user=userEvent.setup();render(<Workspace attachments={attachments} projectId={P} sessionId={S} onClose={onClose}/>);expect(await screen.findByText('notes.txt')).toBeInTheDocument();expect(screen.queryByText('hidden.txt')).toBeNull();await waitFor(()=>expect(attachments.get).toHaveBeenCalledWith({attachmentId:A}));expect(screen.getByText(/hello/)).toBeInTheDocument();await user.click(screen.getByRole('button',{name:'关闭工作区'}));expect(onClose).toHaveBeenCalledOnce()})
 it('has independent bounded minus and plus zoom controls',async()=>{const user=userEvent.setup();render(<Workspace attachments={bridge()} projectId={P} sessionId={S} onClose={vi.fn()}/>);await screen.findByText('notes.txt');const minus=screen.getByRole('button',{name:'缩小预览'}),plus=screen.getByRole('button',{name:'放大预览'});for(let i=0;i<10;i++)await user.click(minus);expect(screen.getByLabelText('预览缩放')).toHaveTextContent('50%');expect(minus).toBeDisabled();expect(plus).toBeEnabled();for(let i=0;i<10;i++)await user.click(plus);expect(screen.getByLabelText('预览缩放')).toHaveTextContent('200%');expect(plus).toBeDisabled();expect(minus).toBeEnabled()})
 it('linkifies only HTTPS text and leaves unsafe schemes inert',async()=>{render(<Workspace attachments={bridge()} projectId={P} sessionId={S} onClose={vi.fn()}/>);const link=await screen.findByRole('link',{name:'https://safe.example/path'});expect(link).toHaveAttribute('href','https://safe.example/path');expect(link).toHaveAttribute('rel',expect.stringContaining('noopener'));expect(screen.queryByRole('link',{name:/javascript|http:\/\/plain/})).toBeNull();expect(screen.getByText(/javascript:alert/)).toBeInTheDocument()})
 it('opens and closes only through the isolated browser bridge and reports opening',async()=>{const browser={open:vi.fn().mockResolvedValue({status:'opening',url:'https://example.com/'}),close:vi.fn().mockResolvedValue({status:'closed'})}as BrowserBridge,user=userEvent.setup();render(<Workspace attachments={bridge()} browser={browser} projectId={P} sessionId={S} onClose={vi.fn()}/>);await user.click(screen.getByRole('tab',{name:'浏览器'}));expect(screen.queryByRole('tab',{name:'安全浏览器'})).toBeNull();const input=screen.getByLabelText('浏览器地址');await user.clear(input);await user.type(input,'http://unsafe.example');expect(screen.getByRole('button',{name:'打开独立浏览器'})).toBeDisabled();await user.clear(input);await user.type(input,'https://example.com/');await user.click(screen.getByRole('button',{name:'打开独立浏览器'}));await waitFor(()=>expect(browser.open).toHaveBeenCalledWith({url:'https://example.com/'}));expect(screen.getByLabelText('浏览器状态')).toHaveTextContent('已接受，正在打开');expect(screen.queryByTitle('应用内页面 https://example.com/')).toBeNull();expect(screen.getByText('已在浏览器窗口打开')).toBeInTheDocument();await user.click(screen.getByRole('button',{name:'关闭浏览器'}));await waitFor(()=>expect(browser.close).toHaveBeenCalledOnce())})
 it('opens skill creation on the files tab with the installed skill catalog',async()=>{render(<Workspace attachments={bridge()} projectId={P} sessionId={S} targetTab="files" filesFocus="skills" isolateRoot onClose={vi.fn()}/>);expect(screen.getByRole('tab',{name:'文件'})).toHaveAttribute('aria-selected','true');expect(await screen.findByRole('region',{name:'技能包目录'})).toBeInTheDocument()})
 it('lets the skill catalog expand and previews a package file beside the tree',async()=>{
  const skillId='01ARZ3NDEKTSV4RRFFQ69G5FAB'
  const skills={
    list:vi.fn().mockResolvedValue({items:[{id:skillId,name:'review',displayName:'审查',description:'',version:'1.0.0',status:'published',permissions:['read_only'],entryPoint:'skills/review/SKILL.md',manifestJson:'{}',createdAt:NOW,updatedAt:NOW}]}),
    packageList:vi.fn().mockResolvedValue({skillId,revision:'rev-1',rootPath:'C:/skills/review',entries:[{path:'SKILL.md',kind:'file',size:20},{path:'scripts/check.py',kind:'file',size:8}]}),
    packageRead:vi.fn().mockResolvedValue({skillId,path:'SKILL.md',content:'# 审查规则正文',encoding:'utf8',size:20,nextOffset:20,eof:true,digest:'d',revision:'rev-1'}),
  }
  const user=userEvent.setup()
  render(<Workspace attachments={bridge()} projectId={P} sessionId={S} targetTab="files" filesFocus="skills" skills={skills as never} onClose={vi.fn()}/>)
  expect(await screen.findByRole('region',{name:'技能包目录'})).toBeInTheDocument()
  expect(screen.getByLabelText('文件树')).toBeInTheDocument()
  expect(screen.getByRole('separator',{name:'调整文件树宽度'})).toBeInTheDocument()
  expect(screen.getByText('从右侧技能目录展开技能并选择文件即可预览')).toBeInTheDocument()
  await user.click(await screen.findByRole('button',{name:/审查/}))
  await user.click(await screen.findByRole('button',{name:/SKILL.md/}))
  expect(await screen.findByText('审查规则正文')).toBeInTheDocument()
  expect(skills.packageRead).toHaveBeenCalledWith(expect.objectContaining({skillId,path:'SKILL.md'}))
 })
 it('renders image bytes from attachment.get and does not claim the workspace lacks them',async()=>{
 const jpeg='aaaa'
 const attachments=bridge()
 attachments.list=vi.fn().mockResolvedValue({items:[{...item(),originalName:'BIRETURN.jpg',mime:'image/jpeg',parseStatus:'succeeded' as const,parseErrorCode:'',parsedTextBytes:0}]})
 attachments.get=vi.fn().mockResolvedValue({...item(),originalName:'BIRETURN.jpg',mime:'image/jpeg',parseStatus:'succeeded',parseErrorCode:'',parsedTextBytes:0,parsedText:'',contentBase64:jpeg})
 render(<Workspace attachments={attachments} projectId={P} sessionId={S} onClose={vi.fn()}/>)
 expect(await screen.findByRole('img',{name:'BIRETURN.jpg'})).toHaveAttribute('src',expect.stringContaining('data:image/jpeg;base64,'))
 expect(screen.getAllByText('图片 · 可供视觉分析').length).toBeGreaterThan(0)
 expect(screen.queryByText(/尚未取得原图字节/)).toBeNull()
 expect(screen.queryByText(/^failed$/)).toBeNull()
})

it('refreshes attachments when the upload revision changes',async()=>{const attachments=bridge(),view=render(<Workspace attachments={attachments} projectId={P} sessionId={S} refreshRevision={0} onClose={vi.fn()}/>);await waitFor(()=>expect(attachments.list).toHaveBeenCalledOnce());view.rerender(<Workspace attachments={attachments} projectId={P} sessionId={S} refreshRevision={1} onClose={vi.fn()}/>);await waitFor(()=>expect(attachments.list).toHaveBeenCalledTimes(2))})
 it('opens a canvas document in the workspace canvas tab',()=>{
  render(<Workspace attachments={bridge()} projectId={P} sessionId={S} targetTab="canvas" toolActivities={[{callId:'canvas-1',name:'canvas.present',status:'tool_completed',summary:'canvas ready',artifact:{kind:'html',path:'canvas.html',content:'<!doctype html><h1>能力对照</h1>'}}]} onClose={vi.fn()}/>)
  expect(screen.getByRole('tab',{name:'画布'})).toHaveAttribute('aria-selected','true')
  expect(screen.getByTitle('画布')).toHaveAttribute('srcdoc',expect.stringContaining('能力对照'))
  expect(workspaceTabForTool('canvas.present')).toBe('canvas')
  expect(autoRevealWorkspaceTab('canvas.present')).toBe('canvas')
  expect(autoRevealWorkspaceForHtmlTool('canvas.present')).toBe('canvas')
 })
 it('shows search progress in the browser tab before HTML results arrive',()=>{
  render(<Workspace attachments={bridge()} projectId={P} sessionId={S} targetTab="browser" toolActivities={[{callId:'search-1',name:'web.search',status:'tool_started',summary:'搜索：周杰伦'}]} onClose={vi.fn()}/>)
  expect(screen.getByText('正在检索网页…')).toBeInTheDocument()
 })
 it('renders web.search HTML results in the isolated browser preview',()=>{
  const url='https://cn.bing.com/search?q=%E5%91%A8%E6%9D%B0%E4%BC%A6'
  render(<Workspace attachments={bridge()} projectId={P} sessionId={S} targetTab="browser" toolActivities={[{callId:'search-1',name:'web.search',status:'tool_completed',summary:`query: 周杰伦\nresults_url: ${url}`,artifact:{kind:'html',path:'search.html',content:'<!doctype html><h1>搜索结果 · 周杰伦</h1><ol class="serp"><li class="serp-hit"><a href="https://news.example/jay">1. 周杰伦新闻</a><small>https://news.example/jay</small><p>动态</p></li></ol>'}}]} onClose={vi.fn()}/>)
  expect(screen.getByLabelText('浏览器地址')).toHaveValue(url)
  expect(screen.getByRole('button',{name:/周杰伦新闻/})).toBeInTheDocument()
  expect(screen.queryByTitle(/HTML 预览/)).toBeNull()
  expect(screen.queryByText('尚无 HTML 预览')).toBeNull()
 })
 it('shows a fetched-page extract instead of the empty placeholder',()=>{
  render(<Workspace attachments={bridge()} projectId={P} sessionId={S} targetTab="browser" toolActivities={[{callId:'fetch-1',name:'web.fetch',status:'tool_completed',summary:'url: https://tags.sina.com.cn/star_gutianle',artifact:{kind:'html',path:'fetch.html',content:'<h1>古天乐</h1><small>https://tags.sina.com.cn/star_gutianle</small><pre>最新动态</pre>'}}]} onClose={vi.fn()}/>)
  expect(screen.getByLabelText('浏览器地址')).toHaveValue('https://tags.sina.com.cn/star_gutianle')
  expect(screen.queryByTitle('应用内页面 https://tags.sina.com.cn/star_gutianle')).toBeNull()
  expect(screen.queryByText('尚无页面')).toBeNull()
 })
 it('opens a bare https address in the real browser window',async()=>{
  const browser={open:vi.fn().mockResolvedValue({status:'opening',url:'https://tags.sina.com.cn/star_gutianle'}),close:vi.fn()} as BrowserBridge
  render(<Workspace attachments={bridge()} browser={browser} projectId={P} sessionId={S} targetTab="browser" toolActivities={[{callId:'fetch-1',name:'web.fetch',status:'tool_completed',summary:'url: https://tags.sina.com.cn/star_gutianle'}]} onClose={vi.fn()}/>)
  expect(screen.getByLabelText('浏览器地址')).toHaveValue('https://tags.sina.com.cn/star_gutianle')
  await waitFor(()=>expect(browser.open).toHaveBeenCalledWith({url:'https://tags.sina.com.cn/star_gutianle'}))
  expect(screen.queryByTitle(/应用内页面/)).toBeNull()
  expect(screen.getByLabelText('浏览器状态')).toHaveTextContent('已在浏览器窗口打开')
 })
 it('does not open youku from a tool result', () => {
  const browser = { open: vi.fn(), close: vi.fn() } as BrowserBridge
  render(<Workspace attachments={bridge()} browser={browser} projectId={P} sessionId={S} targetTab="browser" toolActivities={[{ callId: 'yk', name: 'media.play', status: 'tool_completed', summary: 'url: https://so.youku.com/search_video/q_test\n' }]} onClose={vi.fn()} />)
  expect(browser.open).not.toHaveBeenCalled()
 })
 it('plays a media-center film without opening the system browser', () => {
  const browser = { open: vi.fn(), close: vi.fn() } as BrowserBridge
  render(<Workspace attachments={bridge()} browser={browser} projectId={P} sessionId={S} targetTab="browser" toolActivities={[{ callId: 'play-center', name: 'media.play', status: 'tool_completed', summary: '已交给媒体中心播放。\nMEDIA_CENTER\nurl: https://upload.wikimedia.org/wikipedia/commons/c/c1/Night_of_the_Living_Dead_%281968%29.webm\nkind: video\n' }]} onClose={vi.fn()} />)
  expect(browser.open).not.toHaveBeenCalled()
  expect(screen.queryByText('已在浏览器窗口打开')).toBeNull()
 })
 it('opens the official film page once so the member can log in', async () => {
  const url = 'https://www.iqiyi.com/so/q_%E5%A4%A7%E8%AF%9D%E8%A5%BF%E6%B8%B8'
  const browser = { open: vi.fn().mockResolvedValue({ status: 'opening', url }), close: vi.fn() } as BrowserBridge
  render(<Workspace attachments={bridge()} browser={browser} projectId={P} sessionId={S} targetTab="browser" toolActivities={[{ callId: 'play-1', name: 'media.play', status: 'tool_completed', summary: `url: ${url}\n` }]} onClose={vi.fn()} />)
  await waitFor(() => expect(browser.open).toHaveBeenCalledWith({ url }))
 })
 it('opens a search result in the panel',async()=>{
  const browser={open:vi.fn().mockResolvedValue({status:'opening',url:'https://news.example/jay'}),close:vi.fn().mockResolvedValue({status:'closed'})}as BrowserBridge
  const user=userEvent.setup()
  render(<Workspace attachments={bridge()} browser={browser} projectId={P} sessionId={S} targetTab="browser" toolActivities={[{callId:'search-1',name:'web.search',status:'tool_completed',summary:'query: jay\nresults_url: https://cn.bing.com/search?q=jay',artifact:{kind:'html',path:'search.html',content:'<h1>搜索结果 · jay</h1><ol class="serp"><li class="serp-hit"><a href="https://news.example/jay">1. 周杰伦新闻</a><small>https://news.example/jay</small><p>动态</p></li></ol>'}}]} onClose={vi.fn()}/>)
  await user.click(screen.getByRole('button',{name:/周杰伦新闻/}))
  await waitFor(()=>expect(browser.open).toHaveBeenCalledWith({url:'https://news.example/jay'}))
  expect(screen.getByLabelText('浏览器地址')).toHaveValue('https://news.example/jay')
  expect(screen.queryByTitle('应用内页面 https://news.example/jay')).toBeNull()
 })
 it('expands and restores the conversation from the workspace chrome',async()=>{
  const onToggleExpand=vi.fn()
  const user=userEvent.setup()
  const view=render(<Workspace attachments={bridge()} projectId={P} sessionId={S} expanded={false} onToggleExpand={onToggleExpand} onClose={vi.fn()}/>)
  await user.click(screen.getByRole('button',{name:'放大工作区'}))
  expect(onToggleExpand).toHaveBeenCalledOnce()
  view.rerender(<Workspace attachments={bridge()} projectId={P} sessionId={S} expanded onToggleExpand={onToggleExpand} onClose={vi.fn()}/>)
  expect(screen.getByRole('button',{name:'恢复对话'})).toBeInTheDocument()
 })
 it('offers a small download for the selected file',async()=>{
  const attachments=bridge()
  render(<Workspace attachments={attachments} projectId={P} sessionId={S} onClose={vi.fn()}/>)
  expect(await screen.findByText('notes.txt')).toBeInTheDocument()
  await waitFor(()=>expect(attachments.get).toHaveBeenCalled())
  expect(screen.getByRole('button',{name:'下载文件'})).toBeEnabled()
 })
 it('does not download extracted text under an office filename',async()=>{
  const attachments=bridge()
  attachments.list=vi.fn().mockResolvedValue({items:[{...item(),originalName:'notes.docx',mime:'application/vnd.openxmlformats-officedocument.wordprocessingml.document',size:2048}]})
  attachments.get=vi.fn().mockResolvedValue({...item(),originalName:'notes.docx',mime:'application/vnd.openxmlformats-officedocument.wordprocessingml.document',size:2048,parsedText:'extracted paragraphs'})
  render(<Workspace attachments={attachments} projectId={P} sessionId={S} onClose={vi.fn()}/>)
  expect(await screen.findByText('notes.docx')).toBeInTheDocument()
  await waitFor(()=>expect(attachments.get).toHaveBeenCalled())
  expect(screen.getByRole('button',{name:'下载文件'})).toBeDisabled()
 })
 it('lets the code tree collapse from a small chrome button',async()=>{
  const user=userEvent.setup()
  render(<Workspace attachments={bridge()} projectId={P} sessionId={S} targetTab="code" onClose={vi.fn()}/>)
  const hide=screen.getByRole('button',{name:'折叠文件树'})
  expect(screen.getByLabelText('文件树')).toBeInTheDocument()
  await user.click(hide)
  expect(screen.queryByLabelText('文件树')).toBeNull()
  await user.click(screen.getByRole('button',{name:'显示文件树'}))
  expect(screen.getByLabelText('文件树')).toBeInTheDocument()
 })
 it('keeps a clean file preview and a resizable directory tree on the right',async()=>{
  render(<Workspace attachments={bridge()} projectId={P} sessionId={S} onClose={vi.fn()}/>)
  expect(await screen.findByText('notes.txt')).toBeInTheDocument()
  expect(screen.getByLabelText('文件树')).toBeInTheDocument()
  expect(screen.getByRole('separator',{name:'调整文件树宽度'})).toBeInTheDocument()
  expect(screen.queryByText('当前会话没有附件。')).toBeNull()
  const stage=document.querySelector('.workspace-files-stage') as HTMLElement
  expect(stage.style.getPropertyValue('--tree-width')||getComputedStyle(stage).getPropertyValue('--tree-width')).toBeTruthy()
 })
 it('can show only the local project tree without a session folder',async()=>{
  const localWorkspace={
    root:vi.fn().mockResolvedValue({name:'mall',path:'D:/mall',bound:true}),
    select:vi.fn(),
    clear:vi.fn(),
    open:vi.fn(),
    list:vi.fn().mockResolvedValue({items:[{name:'src',path:'src',directory:true}],truncated:false}),
    read:vi.fn(),
  }
  render(<Workspace attachments={bridge()} projectId={P} sessionId={S} filesFocus="local" projectRoot="D:/mall" localWorkspace={localWorkspace} onClose={vi.fn()}/>)
  expect(await screen.findByRole('treeitem',{name:/src/})).toBeInTheDocument()
  expect(screen.getByRole('region',{name:'本地工作区目录'})).toBeInTheDocument()
  expect(screen.queryByText('会话目录载入失败')).toBeNull()
 })
it('opens a session HTML file on the preview origin so its buttons can run', async () => {
  const { artifactReviewBridge, sessionFolderBridge } = await import('../bridge/client')
  const interactiveUrl = 'https://preview.lunitide.local/p/ticket0000000000000001/index.html'
  const get = vi.spyOn(sessionFolderBridge, 'get').mockResolvedValue({ path: 'E:/sessions/demo' })
  const list = vi.spyOn(sessionFolderBridge, 'list').mockResolvedValue({ items: [{ name: 'index.html', path: 'poc/it-crm/index.html', directory: false }] })
  const preview = vi.spyOn(artifactReviewBridge, 'preview').mockResolvedValue({
    kind: 'html', path: 'poc/it-crm/index.html',
    content: '<nav id="nav"></nav><button data-action="opp:new">新建商机</button><script>void 0</script>',
    size: 90, interactiveUrl,
  })
  const user = userEvent.setup()
  render(<Workspace attachments={bridge()} projectId={P} sessionId={S} onClose={vi.fn()} />)
  await user.click(await screen.findByRole('treeitem', { name: /index.html/ }))
  const frame = await screen.findByTitle('产物预览 poc/it-crm/index.html')
  expect(frame).toHaveAttribute('src', interactiveUrl)
  expect(frame.getAttribute('sandbox')).toContain('allow-scripts')
  expect(frame.getAttribute('sandbox')).toContain('allow-same-origin')
  expect(frame).not.toHaveAttribute('srcdoc')
  get.mockRestore(); list.mockRestore(); preview.mockRestore()
})
it('runs scripts in a generated page instead of a dead snapshot',()=>{
  render(<Workspace attachments={bridge()} projectId={P} sessionId={S} targetTab="browser" toolActivities={[{callId:'html-1',name:'html.gen',status:'tool_completed',summary:'wrote index.html',artifact:{kind:'html',path:'index.html',content:'<button onclick="openMenu()">菜单</button><script>function openMenu(){}</script>'}}]} onClose={vi.fn()}/>)
  const frame=screen.getByTitle('HTML 预览 index.html')
  expect(frame.getAttribute('sandbox')).toContain('allow-scripts')
  expect(frame.getAttribute('sandbox')).not.toContain('allow-same-origin')
  expect(frame.getAttribute('srcdoc')).toContain('openMenu')
 })
it('opens the first news link in the isolated browser',async()=>{
  const browser={open:vi.fn().mockResolvedValue({status:'opening',url:'https://news.example/first'}),close:vi.fn()} as BrowserBridge
  render(<Workspace attachments={bridge()} browser={browser} projectId={P} sessionId={S} targetTab="browser" toolActivities={[{callId:'news-1',name:'web.fetch',status:'tool_completed',summary:'url: https://news.example/first\nfirst_hit: true\n已打开第一条。'}]} onClose={vi.fn()}/>)
  await waitFor(()=>expect(browser.open).toHaveBeenCalledWith({url:'https://news.example/first'}))
  expect(screen.getByLabelText('浏览器地址')).toHaveValue('https://news.example/first')
})
})
