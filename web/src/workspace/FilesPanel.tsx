import React,{useEffect,useMemo,useState}from'react'
import{ontologyBridge,skillBridge,type OntologyBridge,type SkillBridge}from'../bridge/client'
import type{OntologyNodeDTO,SkillDTO}from'../generated/bridge'

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
export function FilesPanel({projectId,ontology=ontologyBridge,skills=skillBridge,preferSkills=false}:{projectId:string;ontology?:OntologyBridge;skills?:SkillBridge;preferSkills?:boolean}):React.JSX.Element{
 const[nodes,setNodes]=useState<OntologyNodeDTO[]>([]),[skillItems,setSkillItems]=useState<SkillDTO[]>([]),[loading,setLoading]=useState(true),[error,setError]=useState('')
 useEffect(()=>{let active=true;setLoading(true);setError('');Promise.all([Promise.resolve().then(()=>ontology.listNodes({projectId})),Promise.resolve().then(()=>skills.list({}))]).then(([projectResult,skillResult])=>{if(active){setNodes(projectResult.items);setSkillItems(skillResult.items)}}).catch(e=>{if(active)setError(e instanceof Error?e.message:'目录载入失败')}).finally(()=>{if(active)setLoading(false)});return()=>{active=false}},[ontology,projectId,skills])
 const roots=useMemo(()=>{if(!preferSkills)return buildDirectoryTree(nodes,skillItems);const catalog:TreeNode={name:'技能目录',path:'skills',kind:'directory',children:skillItems.map(skill=>({name:skill.displayName||skill.name,path:`skills/${skill.name}`,kind:'file' as const,children:[],meta:`${skill.version} · ${skill.status}`})).sort((a,b)=>a.name.localeCompare(b.name))};return[catalog]},[nodes,skillItems,preferSkills])
 return <section className="files-panel" aria-label={preferSkills?'技能包目录':'项目与技能目录'}><header><div><b>{preferSkills?'技能包目录':'文件目录'}</b><small>{preferSkills?'本软件已安装的全部技能':'项目索引与技能入口'}</small></div><span>{preferSkills?`${skillItems.length} 个技能`:`${nodes.length} 个项目条目 · ${skillItems.length} 个技能`}</span></header>{loading?<p role="status">正在载入目录…</p>:error?<p role="alert">{error}</p>:<ul className="file-tree">{roots.map(root=><TreeItem key={root.path} node={root}/>)}</ul>}<p className="files-panel-note">{preferSkills?'这里列出技能中心里已经安装的技能包，不是上次打开的磁盘文件夹。':'这里显示引擎已索引的项目路径和技能清单路径，不暴露应用外部的任意磁盘目录。'}</p></section>
}
