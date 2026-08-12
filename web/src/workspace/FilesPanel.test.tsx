import{cleanup,render,screen}from'@testing-library/react'
import userEvent from'@testing-library/user-event'
import{afterEach,expect,it,vi}from'vitest'
import type{OntologyBridge,SkillBridge}from'../bridge/client'
import{FilesPanel}from'./FilesPanel'
const P='01ARZ3NDEKTSV4RRFFQ69G5FAV',now='2025-01-01T00:00:00Z'
afterEach(cleanup)
it('renders collapsible project and skill directory roots from indexed paths',async()=>{const ontology={listNodes:vi.fn().mockResolvedValue({items:[{id:'01ARZ3NDEKTSV4RRFFQ69G5FAA',projectId:P,type:'file',name:'App',fullPath:'src/App.tsx',description:'',metadataJson:'{}',version:1,createdAt:now,updatedAt:now}]})}as unknown as OntologyBridge,skills={list:vi.fn().mockResolvedValue({items:[{id:'01ARZ3NDEKTSV4RRFFQ69G5FAB',name:'review',displayName:'审查',description:'',version:'1.0.0',status:'published',permissions:['read_only'],entryPoint:'skills/review/index.js',manifestJson:'{}',createdAt:now,updatedAt:now}]})}as unknown as SkillBridge,user=userEvent.setup();render(<FilesPanel projectId={P} ontology={ontology} skills={skills}/>);expect(await screen.findByRole('button',{name:/项目目录/})).toHaveAttribute('aria-expanded','true');expect(screen.getByRole('button',{name:/技能目录/})).toBeInTheDocument();await user.click(screen.getByRole('button',{name:/技能目录/}));expect(screen.queryByRole('button',{name:/skills/})).not.toBeInTheDocument();expect(ontology.listNodes).toHaveBeenCalledWith({projectId:P});expect(skills.list).toHaveBeenCalledWith({})})
