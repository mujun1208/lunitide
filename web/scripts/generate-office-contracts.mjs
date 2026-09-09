// Office Studio's additive bridge schemas. Run this when changing the contract,
// then use the repository's canonical generate-bridge / verify-bridge commands.
import { readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
const root = resolve(import.meta.dirname, '../..')
const dir = resolve(root, 'api/bridge/v1')
const base = 'https://lunitide.local/schema/bridge/v1/'
const ref = name => ({ $ref: `${base}public.dto.schema.json#/$defs/${name}` })
const text = (max=4096,min=0) => ({type:'string',minLength:min,maxLength:max})
const integer = (min=0,max=9007199254740991) => ({type:'integer',minimum:min,maximum:max})
const bool = {type:'boolean'}
const object = (properties,required=Object.keys(properties)) => ({type:'object',additionalProperties:false,properties,required})
const array = (items,max=1000) => ({type:'array',maxItems:max,items})
const kind = {enum:['pptx','docx','xlsx','pdf']}
const quality = {enum:['unverified','checking','partial','passed','blocked','stale']}
const id = ref('ULID')
const snapshotInput = {snapshotOffset:integer(),snapshotDigest:text(64,64)}
const snapshotPage = {snapshotOffset:integer(),nextSnapshotOffset:integer(-1),totalSnapshotItems:integer(),snapshotDigest:text(64,64)}
const definitions = {
 OfficeTaskDTO: object({id,sessionId:id,projectId:id,title:text(240,1),goal:text(8192),goalTruncated:bool,revision:integer(1),status:{enum:['draft','queued','planning','running','validating','succeeded','waiting_input','waiting_approval','cancelling','cancelled','failed','interrupted']},createdAt:text(64),updatedAt:text(64)},['id','sessionId','title','goal','revision','status','createdAt','updatedAt']),
 OfficeCheckDTO: object({id:text(128),label:text(256),status:{enum:['passed','failed','unavailable','pending']},severity:{enum:['info','warning','blocking']},message:text(4096),messageTruncated:bool,nodeId:text(512)},['id','label','status','severity','message']),
 OfficeVersionDTO: object({id,versionNo:integer(1),quality,mode:{enum:['managed','imported']},size:integer(),sha256:text(64,64),createdAt:text(64),parentVersionId:id,summary:text(1024),path:text(4096),validationOffset:integer(),totalValidations:integer(),validations:array(ref('OfficeCheckDTO'),500)},['id','versionNo','quality','mode','size','sha256','createdAt']),
 OfficeArtifactDTO: object({id,revision:integer(1),name:text(256),kind,role:{enum:['reference','deliverable']},headVersionId:id,acceptedVersionId:id,versions:array(ref('OfficeVersionDTO'),1000)},['id','revision','name','kind','headVersionId','versions']),
 OfficeStepDTO: object({id:text(128),label:text(256),status:text(64),summary:text(4096),summaryTruncated:bool,createdAt:text(64)},['id','label','status']),
 OfficeSourceDTO: object({id:text(128),name:text(256),versionId:id,location:text(512),rawValue:text(1024),displayValue:text(1024),transform:text(2048),transformTruncated:bool,stale:bool},['id','name']),
 OfficeTaskDetailDTO: object({task:ref('OfficeTaskDTO'),artifacts:array(ref('OfficeArtifactDTO'),500),steps:array(ref('OfficeStepDTO'),1000),sources:array(ref('OfficeSourceDTO'),1000),...snapshotPage},['task','artifacts','steps','sources']),
 OfficeNodeDTO: object({id:text(512),label:text(512),text:text(131072),location:text(512),digest:text(64),editable:bool,valueType:text(64)},['id','label','text']),
 OfficePreviewDTO: object({versionId:id,kind,content:text(1048576),notice:text(4096),previewBasis:text(64),nodes:array(ref('OfficeNodeDTO'),1000),pdfReady:bool,truncated:bool,nodeOffset:integer(),nextNodeOffset:integer(),totalNodes:integer(),parts:array(object({name:text(512),sha256:text(64,64),size:integer()}),128)},['versionId','kind','content','previewBasis','nodes','pdfReady','truncated']),
 OfficeMetricDTO: object({id,taskId:id,name:text(256),sourceVersionId:id,sourceSha256:text(64,64),sourceNodeId:text(512),sourceNodeDigest:text(64,64),rawValue:text(131072),valueType:text(64),unit:text(64),currency:text(3),period:text(128),aggregation:text(64),roundingDigits:integer(0,12),roundingPolicy:text(64),displayValue:text(131072),createdAt:text(64)},['id','taskId','name','sourceVersionId','sourceSha256','sourceNodeId','sourceNodeDigest','rawValue','valueType','unit','currency','period','aggregation','roundingPolicy','displayValue','createdAt']),
 OfficeBundleFileDTO: object({versionId:id,artifactId:id,name:text(512),kind,sha256:text(64,64),size:integer(),quality,accepted:bool}),
 OfficeBundleDTO: object({schemaVersion:integer(1),id,taskId:id,title:text(240),files:array(ref('OfficeBundleFileDTO'),32),createdAt:text(64)}),
}
definitions.OfficeMetricDTO.properties.rawValueTruncated=bool
definitions.OfficeMetricDTO.properties.displayValueTruncated=bool
definitions.OfficeCheckDTO.properties.labelTruncated=bool
definitions.OfficeSourceDTO.properties.targetVersionId=id
definitions.OfficeSourceDTO.properties.targetLocation=text(512)
const change={enum:['added','deleted','modified']}
const nodeValue=object({kind:text(64),text:text(8192),digest:text(64),textTruncated:bool})
definitions.OfficeDiffDTO=object({baseVersionId:id,versionId:id,kind,baseSha256:text(64),sha256:text(64),comparisonBasis:text(64),bytesIdentical:bool,partHashesCompared:bool,payloadIdentical:bool,summary:object(Object.fromEntries(['addedNodes','deletedNodes','modifiedNodes','unchangedNodes','addedParts','deletedParts','modifiedParts','unchangedParts','unchangedBytes'].map(x=>[x,integer()]))),changes:array(object({change,nodeId:text(512),part:text(512),partNameDigest:text(64),partTruncated:bool,locator:text(512),locatorTruncated:bool,changedFields:array(text(32),3),before:nodeValue,after:nodeValue},['change','nodeId','part','partNameDigest','partTruncated','locator','locatorTruncated','changedFields']),500),parts:array(object({change,name:text(512),nameDigest:text(64),nameTruncated:bool,beforeSha256:text(64),sha256:text(64),beforeSize:integer(),size:integer()},['change','name','nameDigest','nameTruncated','beforeSize','size']),500),nodeOffset:integer(),partOffset:integer(),nextNodeOffset:integer(),nextPartOffset:integer(),totalNodeChanges:integer(),totalPartChanges:integer(),truncated:bool,notice:text(4096)})
const publicPath=resolve(dir,'public.dto.schema.json')
const pub=JSON.parse(await readFile(publicPath,'utf8'))
// Additive extensions keep their canonical schemas beside these base contracts.
// Preserve their node metadata when regenerating the original Office surface.
for (const key of ['image','chart']) if (pub.$defs.OfficeNodeDTO?.properties?.[key]) definitions.OfficeNodeDTO.properties[key]=pub.$defs.OfficeNodeDTO.properties[key]
Object.assign(pub.$defs,definitions)
await writeFile(publicPath,JSON.stringify(pub,null,2)+'\n')
const A='01ARZ3NDEKTSV4RRFFQ69G5FAE',B='01ARZ3NDEKTSV4RRFFQ69G5FAV'
const task={taskId:id},version={...task,versionId:id},detail=ref('OfficeTaskDetailDTO')
const contracts={
 'office.task.list':[object({query:text(256),sessionId:id,...snapshotInput},[]),object({items:array(ref('OfficeTaskDTO'),200),...snapshotPage},['items']),{}],
 'office.task.create':[object({title:text(240,1),goal:text(8192),sessionId:id,includeHistory:bool},['title']),detail,{title:'季度汇报'}],
 'office.task.get':[object({...task,...snapshotInput},['taskId']),detail,{taskId:A}],
 'office.task.sync':[object({...task,artifactPath:text(4096)},['taskId']),detail,{taskId:A}],
 'office.task.update':[object({...task,expectedRevision:integer(1),title:text(240,1),goal:text(8192)}),detail,{taskId:A,expectedRevision:1,title:'季度汇报',goal:''}],
 'office.task.cancel':[object(task),detail,{taskId:A}],
 'office.artifact.import':[object({...task,attachmentId:id,name:text(240),artifactId:id,baseVersionId:id,expectedRevision:integer(1)},['taskId','attachmentId']),detail,{taskId:A,attachmentId:B}],
 'office.artifact.preview':[object({...version,nodeOffset:integer(0,50000)},['taskId','versionId']),ref('OfficePreviewDTO'),{taskId:A,versionId:B}],
 'office.artifact.diff':[object({...version,baseVersionId:id,nodeOffset:integer(),partOffset:integer()},['taskId','versionId','baseVersionId']),ref('OfficeDiffDTO'),{taskId:A,versionId:B,baseVersionId:A}],
 'office.artifact.patch':[object({...task,artifactId:id,baseVersionId:id,expectedRevision:integer(1),nodeId:text(512,1),nodeDigest:text(64,64),text:text(131072)}),detail,{taskId:A,artifactId:B,baseVersionId:B,expectedRevision:1,nodeId:'slide-1',nodeDigest:'a'.repeat(64),text:'新的标题'}],
 'office.artifact.patchRange':[object({...task,artifactId:id,baseVersionId:id,expectedRevision:integer(1),ranges:array(object({part:text(512,1),range:text(64,1),expectedDigest:text(64,64),rows:array(array(object({type:{enum:['text','number','boolean','formula','date','blank']},value:text(32768)},['type']),128),1000)}),32)}),detail,{taskId:A,artifactId:B,baseVersionId:B,expectedRevision:1,ranges:[{part:'xl/worksheets/sheet1.xml',range:'A1:A1',expectedDigest:'a'.repeat(64),rows:[[{type:'text',value:'0012'}]]}]}],
 'office.artifact.validate':[object(version),detail,{taskId:A,versionId:B}],
 'office.artifact.accept':[object({...version,artifactId:id,expectedRevision:integer(1)}),detail,{taskId:A,artifactId:B,versionId:B,expectedRevision:1}],
 'office.artifact.restore':[object({...version,artifactId:id,expectedRevision:integer(1)}),detail,{taskId:A,artifactId:B,versionId:B,expectedRevision:1}],
 'office.artifact.export':[object({...version,name:text(240),draft:bool},['taskId','versionId','draft']),object({path:text(4096),absolutePath:text(4096),notice:text(4096)},['path']),{taskId:A,versionId:B,draft:true}],
 'office.artifact.readChunk':[object({...version,offset:integer(),limit:integer(1,32768)},['taskId','versionId','offset']),object({contentBase64:text(43692),nextOffset:integer(),total:integer(),eof:bool}),{taskId:A,versionId:B,offset:0}],
 'office.artifact.open':[object({...task,path:text(4096,1),reveal:bool},['taskId','path']),object({opened:text(4096)}),{taskId:A,path:'C:/example/report-v1.docx'}],
 'office.renderer.probe':[object({}),object({components:array(object({id:text(128),label:text(256),status:{enum:['ready','unavailable','error']},detail:text(4096)}),10)}),{}],
 'office.metric.list':[object({...task,...snapshotInput},['taskId']),object({items:array(ref('OfficeMetricDTO'),1000),...snapshotPage},['items']),{taskId:A}],
 'office.metric.capture':[object({...version,nodeId:text(512,1),nodeDigest:text(64,64),name:text(256),unit:text(64),currency:text(3),period:text(128),roundingDigits:integer(0,12)},['taskId','versionId','nodeId','nodeDigest','name']),detail,{taskId:A,versionId:B,nodeId:'node',nodeDigest:'a'.repeat(64),name:'销售额'}],
 'office.metric.apply':[object({...task,metricId:id,targetVersionId:id,targetNodeId:text(512,1),targetNodeDigest:text(64,64),template:text(131072,1),expectedRevision:integer(1)}),detail,{taskId:A,metricId:B,targetVersionId:B,targetNodeId:'node',targetNodeDigest:'a'.repeat(64),template:'收入 {{value}} {{unit}}',expectedRevision:1}],
 'office.bundle.list':[object({...task,...snapshotInput},['taskId']),object({items:array(ref('OfficeBundleDTO'),200),...snapshotPage},['items']),{taskId:A}],
 'office.bundle.create':[object({...task,title:text(240,1),versionIds:array(id,32)}),ref('OfficeBundleDTO'),{taskId:A,title:'季度交付',versionIds:[B]}],
 'office.bundle.export':[object({...task,bundleId:id}),object({bundleId:id,directory:text(4096),manifestPath:text(4096),files:array(object({versionId:id,name:text(512),path:text(4096),sha256:text(64,64),reused:bool}),32),complete:bool}),{taskId:A,bundleId:B}],
}
for (const method of ['office.storage.usage','office.storage.sweep','office.artifact.replaceImage','office.artifact.refresh','office.artifact.chart','office.artifact.patchChart','office.artifact.chart','office.artifact.patchChart']) {
 const current=JSON.parse(await readFile(resolve(dir,method+'.schema.json'),'utf8'))
 contracts[method]=[object(current.properties,current.required),current['x-result'],current['x-examples'].positive[0]]
}
for(const [method,[input,result,positive]] of Object.entries(contracts)){
 const schema={$schema:'https://json-schema.org/draft/2020-12/schema',$id:base+method+'.schema.json',title:method+' payload','x-method':method,'x-owner':'engine','x-enabled':true,...input,'x-result':result,'x-examples':{positive:[positive],negative:[{unexpected:true}]}}
 await writeFile(resolve(dir,method+'.schema.json'),JSON.stringify(schema,null,2)+'\n')
}
const envPath=resolve(dir,'envelope.schema.json'),env=JSON.parse(await readFile(envPath,'utf8'))
env.properties.method.enum=[...new Set([...env.properties.method.enum,...Object.keys(contracts)])].sort((a,b)=>a.localeCompare(b))
await writeFile(envPath,JSON.stringify(env,null,2)+'\n')
const genPath=resolve(root,'web/scripts/generate-bridge.mjs')
let gen=await readFile(genPath,'utf8')
{
 gen=gen.replace(/^  'office\.[^']+',\r?\n/gm,'')
 gen=gen.replace("  'omni.append',",Object.keys(contracts).sort((a,b)=>a.localeCompare(b)).map(x=>`  '${x}',`).join('\n')+"\n  'omni.append',")
 if(!gen.includes("  'office.task.create',"))throw new Error('Enabled method assertion insertion point missing')
 await writeFile(genPath,gen)
}
