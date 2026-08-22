import React,{useEffect,useMemo,useState}from'react'
import{expertBridge,ontologyBridge,pluginBridge,skillBridge,type ExpertBridge,type OntologyBridge,type PluginBridge,type SkillBridge}from'../bridge/client'
import type{OntologyNodeDTO,SkillDTO}from'../generated/bridge'

export type FilesFocus='session'|'skills'|'experts'|'plugins'|'assets'

type TreeNode={name:string;path:string;kind:'directory'|'file';children:TreeNode[];meta?:string}

const cleanPath=(value:string)=>value.replace(/\\/g,'/').replace(/^\.\//,'').replace(/^\/+|\/+$/g,'')
const addPath=(root:TreeNode,path:string,meta?:string)=>{
 const parts=cleanPath(path).split('/').filter(Boolean);if(!parts.length)return
 let current=root
 parts.forEach((name,index)=>{const last=index===parts.length-1,full=parts.slice(0,index+1).join('/');let child=current.children.find(item=>item.name===name)
  if(!child){child={name,path:full,kind:last?'file':'directory',children:[],meta:last?meta:undefined};current.children.push(child)}else if(!last)child.kind='directory'
  current=child
 })
}
const ordered=(node:TreeNode):TreeNode=>({...node,children:node.children.map(ordered).sort((a,b)=>Number(a.kind==='file')-Number(b.kind==='file')||a.name.localeCompare(b.name))})
export const buildDirectoryTree=(nodes:OntologyNodeDTO[],skills:SkillDTO[]):TreeNode[]=>{
 const project:TreeNode={name:'项目目录',path:'project',kind:'directory',children:[]},skillRoot:TreeNode={name:'技能目录',path:'skills',kind:'directory',children:[]}
 nodes.filter(node=>node.fullPath.trim()).forEach(node=>addPath(project,node.fullPath,node.type))
 skills.forEach(skill=>addPath(skillRoot,cleanPath(skill.entryPoint)||skill.name,`${skill.displayName} · ${skill.status}`))
 return[ordered(project),ordered(skillRoot)]
}
function TreeItem({node,depth=0}:{node:TreeNode;depth?:number}):React.JSX.Element{
 const[open,setOpen]=useState(depth===0),directory=node.kind==='directory'
 return <li className={directory?'directory':'file'}><button type="button" style={{paddingLeft:`${10+depth*16}px`}} aria-expanded={directory?open:undefined} onClick={()=>directory&&setOpen(value=>!value)}><span aria-hidden="true">{directory?(open?'⌄':'›'):'·'}</span><b>{node.name}</b>{node.meta&&<small>{node.meta}</small>}</button>{directory&&open&&<ul>{node.children.length?node.children.map(child=><TreeItem key={child.path} node={child} depth={depth+1}/>):<li className="tree-empty">目录为空</li>}</ul>}</li>
}
export function FilesPanel({projectId,ontology=ontologyBridge,skills=skillBridge,experts=expertBridge,plugins=pluginBridge,focus='skills',preferSkills}:{projectId:string;ontology?:OntologyBridge;skills?:SkillBridge;experts?:ExpertBridge;plugins?:PluginBridge;focus?:Exclude<FilesFocus,'session'>;preferSkills?:boolean}):React.JSX.Element{
 const mode=preferSkills?'skills':focus
 const[nodes,setNodes]=useState<OntologyNodeDTO[]>([]),[skillItems,setSkillItems]=useState<SkillDTO[]>([]),[expertItems,setExpertItems]=useState<Array<{name:string;meta:string}>>([]),[pluginItems,setPluginItems]=useState<Array<{name:string;meta:string}>>([]),[loading,setLoading]=useState(true),[error,setError]=useState('')
 useEffect(()=>{let active=true;setLoading(true);setError('');const tasks:Promise<void>[]=[]
 if(mode==='skills'||mode==='assets')tasks.push(Promise.resolve().then(()=>ontology.listNodes({projectId})).then(r=>{if(active)setNodes(r.items)}))
 if(mode==='skills')tasks.push(Promise.resolve().then(()=>skills.list({})).then(r=>{if(active)setSkillItems(r.items)}))
 if(mode==='experts')tasks.push(Promise.resolve().then(()=>experts.list({})).then(r=>{if(active)setExpertItems(r.experts.map(item=>({name:item.name,meta:`${item.division} · v${item.semver}`})))}))
 if(mode==='plugins')tasks.push(Promise.resolve().then(()=>plugins.list({})).then(r=>{if(active)setPluginItems(r.plugins.map(item=>({name:item.pluginId,meta:`${item.kind} · ${item.state}`})))}).catch(()=>{if(active)setPluginItems([])}))
 Promise.all(tasks).catch(e=>{if(active)setError(e instanceof Error?e.message:'目录载入失败')}).finally(()=>{if(active)setLoading(false)});return()=>{active=false}},[ontology,projectId,skills,experts,plugins,mode])
 const roots=useMemo(()=>{
  if(mode==='experts')return[{name:'专家目录',path:'experts',kind:'directory' as const,children:expertItems.map(item=>({name:item.name,path:`experts/${item.name}`,kind:'file' as const,children:[],meta:item.meta})).sort((a,b)=>a.name.localeCompare(b.name))}]
  if(mode==='plugins')return[{name:'插件目录',path:'plugins',kind:'directory' as const,children:pluginItems.map(item=>({name:item.name,path:`plugins/${item.name}`,kind:'file' as const,children:[],meta:item.meta})).sort((a,b)=>a.name.localeCompare(b.name))}]
  if(mode==='assets')return[{name:'资产根目录',path:'assets',kind:'directory' as const,children:[{name:'项目索引',path:'assets/project',kind:'directory' as const,children:nodes.filter(node=>node.fullPath.trim()).map(node=>({name:node.fullPath.split('/').pop()||node.fullPath,path:`assets/project/${node.fullPath}`,kind:'file' as const,children:[],meta:node.type})),meta:undefined},{name:'技能包',path:'assets/skills',kind:'directory' as const,children:skillItems.map(skill=>({name:skill.displayName||skill.name,path:`assets/skills/${skill.name}`,kind:'file' as const,children:[],meta:`${skill.version} · ${skill.status}`})),meta:undefined}]}]
  if(mode==='skills'){const catalog:TreeNode={name:'技能目录',path:'skills',kind:'directory',children:skillItems.map(skill=>({name:skill.displayName||skill.name,path:`skills/${skill.name}`,kind:'file' as const,children:[],meta:`${skill.version} · ${skill.status}`})).sort((a,b)=>a.name.localeCompare(b.name))};return[catalog]}
  return buildDirectoryTree(nodes,skillItems)
 },[nodes,skillItems,expertItems,pluginItems,mode])
 const label=mode==='experts'?'专家目录':mode==='plugins'?'插件目录':mode==='assets'?'资产根目录':mode==='skills'?'技能包目录':'文件目录'
 const note=mode==='experts'?'这里列出专家中心已安装的专家配置根目录。':mode==='plugins'?'这里列出插件中心已安装的插件根目录。':mode==='assets'?'这里汇总项目资产与技能包根目录索引。':mode==='skills'?'这里列出技能中心里已经安装的技能包。':'这里显示引擎已索引的项目路径和技能清单路径。'
 return <section className="files-panel" aria-label={label}><header><div><b>{label}</b><small>{mode==='skills'?'本软件已安装的全部技能':mode==='assets'?'项目与技能资产索引':'对应模块根目录'}</small></div><span>{mode==='experts'?`${expertItems.length} 个专家`:mode==='plugins'?`${pluginItems.length} 个插件`:mode==='skills'?`${skillItems.length} 个技能`:mode==='assets'?`${nodes.length} 个项目条目 · ${skillItems.length} 个技能`:`${nodes.length} 个项目条目 · ${skillItems.length} 个技能`}</span></header>{loading?<p role="status">正在载入目录…</p>:error?<p role="alert">{error}</p>:<ul className="file-tree">{roots.map(root=><TreeItem key={root.path} node={root}/>)}</ul>}<p className="files-panel-note">{note}</p></section>
}
