import{cleanup,render,screen}from'@testing-library/react'
import userEvent from'@testing-library/user-event'
import{afterEach,expect,it,vi}from'vitest'
import type{OntologyBridge,SkillBridge}from'../bridge/client'
import{FilesPanel,buildDirectoryTree}from'./FilesPanel'
const P='01ARZ3NDEKTSV4RRFFQ69G5FAV',now='2025-01-01T00:00:00Z'
afterEach(cleanup)
const node=(fullPath:string)=>({id:'01ARZ3NDEKTSV4RRFFQ69G5FAA',projectId:P,type:'file',name:'App',fullPath,description:'',metadataJson:'{}',version:1,createdAt:now,updatedAt:now})
const skill=(name:string,displayName:string,entryPoint:string)=>({id:'01ARZ3NDEKTSV4RRFFQ69G5FAB',name,displayName,description:'',version:'1.0.0',status:'published',permissions:['read_only'],entryPoint,manifestJson:'{}',createdAt:now,updatedAt:now})

it('builds project and skill roots from indexed paths, directories before files',()=>{
 const[project,skills]=buildDirectoryTree([node('src/App.tsx'),node('README.md')]as any,[skill('review','审查','skills/review/index.js')]as any)
 expect(project.name).toBe('项目目录')
 expect(project.children.map(c=>c.name)).toEqual(['src','README.md'])
 expect(project.children[0].children[0]).toMatchObject({name:'App.tsx',kind:'file',meta:'file'})
 expect(skills.name).toBe('技能目录')
 expect(skills.children.map(c=>c.name)).toEqual(['skills'])
 expect(skills.children[0].children[0].children[0]).toMatchObject({name:'index.js',kind:'file',meta:'审查 · published'})
})

// The panel itself defaults to the installed-skill catalog: project paths reach
// the workspace through SessionFolderPanel and the local tree instead.
it('renders the installed skill catalog and collapses the root on click',async()=>{const ontology={listNodes:vi.fn().mockResolvedValue({items:[node('src/App.tsx')]})}as unknown as OntologyBridge,skills={list:vi.fn().mockResolvedValue({items:[skill('review','审查','skills/review/index.js')]})}as unknown as SkillBridge,user=userEvent.setup();render(<FilesPanel projectId={P} ontology={ontology} skills={skills}/>);const root=await screen.findByRole('button',{name:/技能目录/});expect(root).toHaveAttribute('aria-expanded','true');expect(screen.getByText('审查')).toBeInTheDocument();await user.click(root);expect(root).toHaveAttribute('aria-expanded','false');expect(screen.queryByText('审查')).not.toBeInTheDocument();expect(ontology.listNodes).toHaveBeenCalledWith({projectId:P});expect(skills.list).toHaveBeenCalledWith({})})
it('puts the installed skill catalog first when creating a skill',async()=>{const ontology={listNodes:vi.fn().mockResolvedValue({items:[{id:'01ARZ3NDEKTSV4RRFFQ69G5FAA',projectId:P,type:'file',name:'App',fullPath:'src/App.tsx',description:'',metadataJson:'{}',version:1,createdAt:now,updatedAt:now}]})}as unknown as OntologyBridge,skills={list:vi.fn().mockResolvedValue({items:[{id:'01ARZ3NDEKTSV4RRFFQ69G5FAB',name:'skill-creator',displayName:'skill-creator',description:'',version:'1.0.0',status:'published',permissions:['read_write'],entryPoint:'builtin://skill-creator',manifestJson:'{}',createdAt:now,updatedAt:now}]})}as unknown as SkillBridge;render(<FilesPanel projectId={P} ontology={ontology} skills={skills} preferSkills/>);expect(await screen.findByRole('region',{name:'技能包目录'})).toBeInTheDocument();expect(screen.getByText('本软件已安装的全部技能')).toBeInTheDocument();expect(screen.getByRole('button',{name:/技能目录/})).toHaveAttribute('aria-expanded','true');expect(screen.getByText(/skill-creator/)).toBeInTheDocument();expect(screen.queryByRole('button',{name:/项目目录/})).not.toBeInTheDocument();expect(screen.queryByText('App')).not.toBeInTheDocument()})
