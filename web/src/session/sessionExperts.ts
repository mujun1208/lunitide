const key=(sessionId:string)=>`lunitide:session-experts:${sessionId}`

export type MountedExpert={expertId:string;name:string;division?:string;semver?:string;state?:string}

export function readMountedExperts(sessionId:string):MountedExpert[]{
 try{
  const raw=localStorage.getItem(key(sessionId))
  if(!raw)return[]
  const parsed=JSON.parse(raw)as unknown
  if(!Array.isArray(parsed))return[]
  return parsed.filter((item):item is MountedExpert=>!!item&&typeof item==='object'&&typeof(item as MountedExpert).expertId==='string'&&typeof(item as MountedExpert).name==='string')
 }catch{return[]}
}

export function writeMountedExperts(sessionId:string,experts:readonly MountedExpert[]):void{
 try{localStorage.setItem(key(sessionId),JSON.stringify(experts.map(item=>({expertId:item.expertId,name:item.name,division:item.division,semver:item.semver,state:item.state}))))}catch{/* quota / private mode */}
}

export function expertInitial(name:string):string{
 const trimmed=name.trim()
 if(!trimmed)return '专'
 return Array.from(trimmed)[0]??'专'
}
