import {
  BRIDGE_VERSION, type BridgeMethod, type BridgeRequest, type BridgeResponse,
  type ProviderCreatePayload, type ProviderCreateResult, type ProviderCredentialSubmitPayload,
  type ProviderCredentialSubmitResult, type ProviderDeletePayload, type ProviderDeleteResult,
  type ProviderGetPayload, type ProviderGetResult, type ProviderListPayload, type ProviderListResult,
  type ProviderModelSyncPayload, type ProviderModelSyncResult, type ProviderTestPayload,
  type ProviderTestResult, type ProviderUpdatePayload, type ProviderUpdateResult,
  type ChatStartPayload, type ChatStartResult, type StreamCancelResult,
  type ProjectCreatePayload, type ProjectCreateResult, type ProjectListPayload, type ProjectListResult,
  type SessionCreatePayload, type SessionCreateResult, type SessionListPayload, type SessionListResult,
  type MessageAppendPayload, type MessageAppendResult, type MessageListPayload, type MessageListResult,
  type StageCreatePayload, type StageCreateResult, type StageListPayload, type StageListResult,
  type PlanGetPayload, type PlanGetResult, type PlanListPayload, type PlanListResult,
  type PlanCreatePayload, type PlanCreateResult,
  type PlanActivatePayload, type PlanActivateResult, type PlanCompletePayload, type PlanCompleteResult,
  type PlanPausePayload, type PlanPauseResult, type PlanResumePayload, type PlanResumeResult,
  type NodeListPayload, type NodeListResult, type NodeStartPayload, type NodeStartResult,
  type NodeCreatePayload, type NodeCreateResult,
  type NodeCompletePayload, type NodeCompleteResult, type NodeFailPayload, type NodeFailResult,
  type ReviewListPayload, type ReviewListResult, type ReviewApprovePayload, type ReviewApproveResult,
  type ReviewRejectPayload, type ReviewRejectResult,
  type MemoryGetPayload, type MemoryGetResult, type MemoryListPayload, type MemoryListResult,
  type MemorySearchPayload, type MemorySearchResult, type MemoryUpdatePayload, type MemoryUpdateResult,
  type MemoryDeletePayload, type MemoryDeleteResult, type MemoryCreatePayload, type MemoryCreateResult,
  type OntologyNodeGetPayload, type OntologyNodeGetResult, type OntologyNodeListPayload, type OntologyNodeListResult,
  type OntologyNodeSearchPayload, type OntologyNodeSearchResult, type OntologyEdgeListPayload, type OntologyEdgeListResult,
  type OntologyNodeCreatePayload, type OntologyNodeCreateResult, type OntologyNodeUpdatePayload, type OntologyNodeUpdateResult,
  type OntologyNodeDeletePayload, type OntologyNodeDeleteResult,
  type OntologyEdgeCreatePayload, type OntologyEdgeCreateResult, type OntologyEdgeUpdatePayload, type OntologyEdgeUpdateResult,
  type OntologyEdgeDeletePayload, type OntologyEdgeDeleteResult,
  type SkillGetPayload, type SkillGetResult, type SkillListPayload, type SkillListResult,
  type SkillMatchPayload, type SkillMatchResult, type SkillPublishPayload, type SkillPublishResult,
  type SkillDeprecatePayload, type SkillDeprecateResult, type SkillDisablePayload, type SkillDisableResult,
  type SkillCreatePayload, type SkillCreateResult, type SkillUpdatePayload, type SkillUpdateResult,
  type SkillDeletePayload, type SkillDeleteResult,
  type PlanDTO, type PlanNodeDTO, type ReviewDTO, type MemoryDTO, type OntologyNodeDTO, type OntologyEdgeDTO, type SkillDTO, type SkillMatchDTO,
  type PlanStatus, type NodeStatus, type RiskLevel, type ReviewStatus, type MemoryLayer, type MemoryScope,
  type OntologyNodeType, type OntologyEdgeType, type SkillStatus, type SkillPermission,
} from '../generated/bridge'

export class BridgeClientError extends Error {
  constructor(message: string, public readonly code: string, public readonly retryable: boolean, public readonly correlationId: string) {
    super(message); this.name = 'BridgeClientError'
  }
}
export interface WebViewTransport {
  postMessage(value: unknown): void
  addEventListener(type: 'message', listener: (event: MessageEvent<BridgeResponse>) => void): void
  removeEventListener(type: 'message', listener: (event: MessageEvent<BridgeResponse>) => void): void
}
declare global { interface Window { chrome?: { webview?: WebViewTransport } } }

export type MutationMethod = 'project.create'|'session.create'|'message.append'|'provider.create'|'provider.update'|'provider.delete'|'provider.model.sync'|'stage.create'|'plan.create'|'node.create'|'memory.create'|'ontology.node.create'|'ontology.node.update'|'ontology.node.delete'|'ontology.edge.create'|'ontology.edge.update'|'ontology.edge.delete'|'skill.create'|'skill.update'|'skill.delete'
export type MutationOptions<T extends object> = { attempt?: MutationAttempt<T> }
export interface MutationAttempt<T extends object> { readonly method: MutationMethod; readonly payload: Readonly<T>; readonly idempotencyKey: string; readonly fingerprint: string }
const stable = (value: unknown): string => value === null || typeof value !== 'object' ? JSON.stringify(value) : Array.isArray(value) ? `[${value.map(stable).join(',')}]` : `{${Object.keys(value as object).sort().map(k=>`${JSON.stringify(k)}:${stable((value as Record<string,unknown>)[k])}`).join(',')}}`
const clone = <T>(value:T):T => structuredClone(value)
const freeze = <T>(value:T):T => { if(value && typeof value==='object'){Object.freeze(value);Object.values(value as object).forEach(freeze)}return value }
export function createMutationAttempt<T extends object>(method: MutationMethod, payload: T): MutationAttempt<T> { const copy=freeze(clone(payload)); return Object.freeze({method,payload:copy,idempotencyKey:ulid(),fingerprint:stable(copy)}) }
const deeplyFrozen=(value:unknown):boolean=>!value||typeof value!=='object'||Object.isFrozen(value)&&Object.values(value).every(deeplyFrozen)
function checkedAttempt<T extends object>(method:MutationMethod,payload:T,attempt?:MutationAttempt<T>):{payload:T;key:string} {
 if(!attempt)return{payload:clone(payload),key:ulid()}
 const ownFingerprint=stable(attempt.payload)
 if(!Object.isFrozen(attempt)||!deeplyFrozen(attempt.payload)||attempt.method!==method||attempt.fingerprint!==ownFingerprint||ownFingerprint!==stable(payload)||!isULID(attempt.idempotencyKey))throw new BridgeClientError('MutationAttempt 与请求负载不匹配','MUTATION_ATTEMPT_MISMATCH',false,'renderer')
 return{payload:clone(attempt.payload) as T,key:attempt.idempotencyKey}
}

export interface ProviderBridge {
  get(payload: ProviderGetPayload): Promise<ProviderGetResult>; list(payload?: ProviderListPayload): Promise<ProviderListResult>
  create(payload: ProviderCreatePayload, options?:MutationOptions<ProviderCreatePayload>): Promise<ProviderCreateResult>
  update(payload: ProviderUpdatePayload, options?:MutationOptions<ProviderUpdatePayload>): Promise<ProviderUpdateResult>
  delete(payload: ProviderDeletePayload, options?:MutationOptions<ProviderDeletePayload>): Promise<ProviderDeleteResult>
  submitCredential(payload: ProviderCredentialSubmitPayload): Promise<ProviderCredentialSubmitResult>
  syncModels(payload: ProviderModelSyncPayload, options?:MutationOptions<ProviderModelSyncPayload>): Promise<ProviderModelSyncResult>
  test(payload: ProviderTestPayload): Promise<ProviderTestResult>
}
export interface ProjectBridge {
  list(payload?:ProjectListPayload):Promise<ProjectListResult>
  create(payload:ProjectCreatePayload,options?:MutationOptions<ProjectCreatePayload>):Promise<ProjectCreateResult>
}
export interface SessionBridge { list(payload:SessionListPayload):Promise<SessionListResult>; create(payload:SessionCreatePayload,options?:MutationOptions<SessionCreatePayload>):Promise<SessionCreateResult> }
export interface MessageBridge { list(payload:MessageListPayload):Promise<MessageListResult>; append(payload:MessageAppendPayload,options?:MutationOptions<MessageAppendPayload>):Promise<MessageAppendResult> }
const mutationMethods = new Set<BridgeMethod>(['project.create','session.create','message.append','provider.create','provider.update','provider.delete','provider.model.sync','stage.create','plan.create','node.create','memory.create','ontology.node.create','ontology.node.update','ontology.node.delete','ontology.edge.create','ontology.edge.update','ontology.edge.delete','skill.create','skill.update','skill.delete'])
function ulid(): string { const a='0123456789ABCDEFGHJKMNPQRSTVWXYZ',b=crypto.getRandomValues(new Uint8Array(10));let v=(BigInt(Date.now())<<80n)|b.reduce((n,x)=>(n<<8n)|BigInt(x),0n),r='';for(let i=0;i<26;i++){r=a[Number(v&31n)]+r;v>>=5n}return r }
const isObj=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)
const exact=(v:Record<string,unknown>,required:string[],optional:string[]=[])=>required.every(k=>k in v)&&Object.keys(v).every(k=>required.includes(k)||optional.includes(k))
const isULID=(v:unknown)=>typeof v==='string'&&/^[0-7][0-9A-HJKMNP-TV-Z]{25}$/.test(v)
const isTime=(v:unknown)=>{if(typeof v!=='string')return false;const m=/^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(\.\d+)?(Z|[+-]\d\d:\d\d)$/.exec(v);if(!m)return false;const y=+m[1],mo=+m[2],d=+m[3],h=+m[4],mi=+m[5],s=+m[6];if(mo<1||mo>12||d<1||d>31||h>23||mi>59||s>59)return false;const days=[31,28+(y%4===0&&(y%100!==0||y%400===0)?1:0),31,30,31,30,31,31,30,31,30,31];if(d>days[mo-1])return false;const parsed=Date.parse(v);if(Number.isNaN(parsed))return false;const iso=new Date(parsed).toISOString();const roundtrip=iso.replace('T',' ').replace(/\.\d{3}Z$/,'Z').replace(/[+-]\d\d:\d\d$/,'Z');const original=v.replace('T',' ').replace(/\.\d+Z?/,'Z').replace(/[+-]\d\d:\d\d$/,'Z');return roundtrip===original}
const normalizedProjectName=(v:string)=>v.split(/\p{White_Space}+/u).filter(Boolean).join(' ')
const isProject=(v:unknown)=>isObj(v)&&exact(v,['id','name','status','createdAt','updatedAt','version'])&&isULID(v.id)&&typeof v.name==='string'&&v.name===normalizedProjectName(v.name)&&Array.from(v.name).length>=1&&Array.from(v.name).length<=200&&['active','archived'].includes(String(v.status))&&isTime(v.createdAt)&&isTime(v.updatedAt)&&Date.parse(String(v.updatedAt))>=Date.parse(String(v.createdAt))&&Number.isInteger(v.version)&&Number(v.version)>=1
const isSession=(v:unknown,projectId:string)=>isObj(v)&&exact(v,['id','projectId','title','status','createdAt','updatedAt','version'])&&isULID(v.id)&&v.projectId===projectId&&typeof v.title==='string'&&v.title===normalizedProjectName(v.title)&&Array.from(v.title).length>=1&&Array.from(v.title).length<=200&&v.status==='active'&&isTime(v.createdAt)&&v.createdAt===v.updatedAt&&v.version===1
const isModel=(v:unknown)=>isObj(v)&&exact(v,['modelId','displayName','isDefault'])&&typeof v.modelId==='string'&&/^[\x21-\x7E]{1,200}$/.test(v.modelId)&&typeof v.displayName==='string'&&v.displayName===v.displayName.trim()&&v.displayName.length>0&&new TextEncoder().encode(v.displayName).length<=200&&typeof v.isDefault==='boolean'
const isModels=(v:unknown)=>Array.isArray(v)&&v.length>=1&&v.length<=50&&v.every(isModel)&&new Set(v.map(x=>(x as {modelId:string}).modelId)).size===v.length&&v.filter(x=>(x as {isDefault:boolean}).isDefault).length===1
const isProvider=(v:unknown)=>isObj(v)&&exact(v,['id','name','protocol','baseUrl','models','status','credentialState','createdAt','updatedAt','version'])&&isULID(v.id)&&typeof v.name==='string'&&v.name===v.name.trim()&&v.name.length>0&&['openai_compatible','anthropic'].includes(String(v.protocol))&&typeof v.baseUrl==='string'&&isModels(v.models)&&['enabled','disabled'].includes(String(v.status))&&['configured','missing','unavailable','requires_reentry'].includes(String(v.credentialState))&&isTime(v.createdAt)&&isTime(v.updatedAt)&&Number.isInteger(v.version)&&Number(v.version)>=1
const guards:Partial<Record<BridgeMethod,(v:unknown)=>boolean>>={
 'project.create':isProject,
 'project.list':v=>isObj(v)&&exact(v,['items'])&&Array.isArray(v.items)&&v.items.length<=100&&v.items.every(isProject),
 'provider.get':isProvider,'provider.create':isProvider,'provider.update':isProvider,
 'provider.list':v=>isObj(v)&&exact(v,['items'])&&Array.isArray(v.items)&&v.items.every(isProvider),
 'provider.delete':v=>isObj(v)&&exact(v,['deleted'])&&v.deleted===true,
 'provider.credential.submit':v=>isObj(v)&&exact(v,['credentialSubmissionId','expiresAt','providerId','expiresInSeconds'])&&isULID(v.credentialSubmissionId)&&isULID(v.providerId)&&isTime(v.expiresAt)&&Number.isInteger(v.expiresInSeconds)&&Number(v.expiresInSeconds)>=1&&Number(v.expiresInSeconds)<=300,
 'provider.model.sync':v=>isObj(v)&&exact(v,['models','warnings','version'])&&isModels(v.models)&&Array.isArray(v.warnings)&&v.warnings.every(x=>typeof x==='string')&&Number.isInteger(v.version)&&Number(v.version)>=1,
 'provider.test':v=>isObj(v)&&exact(v,['status','stage','latencyMs','retryable','testedAt'],['httpStatus','errorCode','sanitizedMessage'])&&['passed','failed'].includes(String(v.status))&&['resolve','connect','authenticate','request','response'].includes(String(v.stage))&&Number.isInteger(v.latencyMs)&&Number(v.latencyMs)>=0&&typeof v.retryable==='boolean'&&isTime(v.testedAt)&&(!('httpStatus'in v)||(Number.isInteger(v.httpStatus)&&Number(v.httpStatus)>=100&&Number(v.httpStatus)<=599))&&(!('errorCode'in v)||typeof v.errorCode==='string')&&(!('sanitizedMessage'in v)||typeof v.sanitizedMessage==='string')
}
function validEnvelope(v:unknown):v is BridgeResponse { if(!isObj(v)||v.v!==BRIDGE_VERSION||v.kind!=='response'||!exact(v,['v','kind','id','requestId','ok'],v.ok===true?['payload']:['error']))return false;if(!isULID(v.id)||!isULID(v.requestId)||typeof v.ok!=='boolean')return false;return v.ok===true||isObj(v.error)&&exact(v.error,['code','message','retryable','correlationId'],['details'])&&typeof v.error.code==='string'&&typeof v.error.message==='string'&&typeof v.error.retryable==='boolean'&&typeof v.error.correlationId==='string' }
export function createProviderBridge(transport: WebViewTransport, defaultDeadlineMs=8_000): ProviderBridge {
 const pending=new Map<string,{method:BridgeMethod;resolve(v:unknown):void;reject(e:Error):void;timer:number}>()
 transport.addEventListener('message',event=>{const raw:unknown=event.data;if(!isObj(raw)||typeof raw.requestId!=='string'||!pending.has(raw.requestId))return;const requestId=raw.requestId;const waiting=pending.get(requestId)!;clearTimeout(waiting.timer);pending.delete(requestId);if(!validEnvelope(raw)){waiting.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,requestId));return}if(raw.ok){if(!guards[waiting.method]?.(raw.payload)){waiting.reject(new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,raw.id));return}waiting.resolve(raw.payload)}else waiting.reject(new BridgeClientError(raw.error.message,raw.error.code,raw.error.retryable,raw.error.correlationId))})
 const request=<T>(method:BridgeMethod,payload:object,deadlineMs=defaultDeadlineMs,attempt?:MutationAttempt<object>):Promise<T>=>{const id=ulid();const mutation=mutationMethods.has(method)?checkedAttempt(method as MutationMethod,payload,attempt):undefined;const secretSubmission=method==='provider.credential.submit';const outgoing=mutation?.payload??(secretSubmission?payload:clone(payload));const message:BridgeRequest<object>={v:BRIDGE_VERSION,kind:'request',id,traceId:ulid(),method,sentAt:new Date().toISOString(),payload:outgoing,deadlineMs:Math.min(30_000,Math.max(1,deadlineMs)),...(mutation?{idempotencyKey:mutation.key}:{})};return new Promise((resolve,reject)=>{const timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,message.traceId))},message.deadlineMs+250);pending.set(id,{method,resolve,reject,timer});try{transport.postMessage(message);if(secretSubmission&&isObj(outgoing)&&typeof outgoing.credential==='string')outgoing.credential=''}catch{clearTimeout(timer);pending.delete(id);if(secretSubmission&&isObj(outgoing)&&typeof outgoing.credential==='string')outgoing.credential='';reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,message.traceId))}})}
 return {get:p=>request('provider.get',p),list:(p={})=>request('provider.list',p),create:(p,o)=>request('provider.create',p,defaultDeadlineMs,o?.attempt),update:(p,o)=>request('provider.update',p,defaultDeadlineMs,o?.attempt),delete:(p,o)=>request('provider.delete',p,defaultDeadlineMs,o?.attempt),submitCredential:p=>request('provider.credential.submit',p),syncModels:(p,o)=>request('provider.model.sync',p,30_000,o?.attempt),test:p=>request('provider.test',p,30_000)}
}
function webview():WebViewTransport{const v=window.chrome?.webview;if(!v)throw new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,'renderer');return v}
let singleton:ProviderBridge|undefined
export function getProviderBridge():ProviderBridge{return singleton??=createProviderBridge(webview())}
export const providerBridge:ProviderBridge={get:p=>getProviderBridge().get(p),list:p=>getProviderBridge().list(p),create:(p,o)=>getProviderBridge().create(p,o),update:(p,o)=>getProviderBridge().update(p,o),delete:(p,o)=>getProviderBridge().delete(p,o),submitCredential:p=>getProviderBridge().submitCredential(p),syncModels:(p,o)=>getProviderBridge().syncModels(p,o),test:p=>getProviderBridge().test(p)}

export function createProjectBridge(transport:WebViewTransport,defaultDeadlineMs=8_000):ProjectBridge{
 const pending=new Map<string,{method:BridgeMethod;resolve(v:unknown):void;reject(e:Error):void;timer:number}>()
 transport.addEventListener('message',event=>{const raw:unknown=event.data;if(!isObj(raw)||typeof raw.requestId!=='string'||!pending.has(raw.requestId))return;const requestId=raw.requestId,waiting=pending.get(requestId)!;clearTimeout(waiting.timer);pending.delete(requestId);if(!validEnvelope(raw)){waiting.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,requestId));return}if(!raw.ok){waiting.reject(new BridgeClientError(raw.error.message,raw.error.code,raw.error.retryable,raw.error.correlationId));return}if(!guards[waiting.method]?.(raw.payload)){waiting.reject(new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,raw.id));return}waiting.resolve(raw.payload)})
 const request=<T>(method:'project.create'|'project.list',payload:object,attempt?:MutationAttempt<object>):Promise<T>=>{const id=ulid(),mutation=method==='project.create'?checkedAttempt(method,payload,attempt):undefined,traceId=ulid(),deadlineMs=Math.min(30_000,Math.max(1,defaultDeadlineMs)),message:BridgeRequest<object>={v:BRIDGE_VERSION,kind:'request',id,traceId,method,sentAt:new Date().toISOString(),payload:mutation?.payload??clone(payload),deadlineMs,...(mutation?{idempotencyKey:mutation.key}:{})};return new Promise((resolve,reject)=>{const timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,traceId))},deadlineMs+250);pending.set(id,{method,resolve,reject,timer});try{transport.postMessage(message)}catch{clearTimeout(timer);pending.delete(id);reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,traceId))}})}
 return{list:(p={})=>request('project.list',p),create:(p,o)=>request('project.create',p,o?.attempt)}
}
let projectSingleton:ProjectBridge|undefined
export function getProjectBridge():ProjectBridge{return projectSingleton??=createProjectBridge(webview())}
export const projectBridge:ProjectBridge={list:p=>getProjectBridge().list(p),create:(p,o)=>getProjectBridge().create(p,o)}

export function createSessionBridge(transport:WebViewTransport,defaultDeadlineMs=8_000):SessionBridge{
 const pending=new Map<string,{method:'session.create'|'session.list';projectId:string;resolve(v:unknown):void;reject(e:Error):void;timer:number}>()
 transport.addEventListener('message',event=>{const raw:unknown=event.data;if(!isObj(raw)||typeof raw.requestId!=='string'||!pending.has(raw.requestId))return;const waiting=pending.get(raw.requestId)!;clearTimeout(waiting.timer);pending.delete(raw.requestId);if(!validEnvelope(raw)){waiting.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,raw.requestId));return}if(!raw.ok){waiting.reject(new BridgeClientError(raw.error.message,raw.error.code,raw.error.retryable,raw.error.correlationId));return}const valid=waiting.method==='session.create'?isSession(raw.payload,waiting.projectId):isObj(raw.payload)&&exact(raw.payload,['items'])&&Array.isArray(raw.payload.items)&&raw.payload.items.length<=100&&raw.payload.items.every(v=>isSession(v,waiting.projectId));if(!valid){waiting.reject(new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,raw.id));return}waiting.resolve(raw.payload)})
 const request=<T>(method:'session.create'|'session.list',payload:SessionCreatePayload|SessionListPayload,attempt?:MutationAttempt<object>):Promise<T>=>{const id=ulid(),mutation=method==='session.create'?checkedAttempt(method,payload,attempt):undefined,traceId=ulid(),deadlineMs=Math.min(30000,Math.max(1,defaultDeadlineMs)),message:BridgeRequest<object>={v:BRIDGE_VERSION,kind:'request',id,traceId,method,sentAt:new Date().toISOString(),payload:mutation?.payload??clone(payload),deadlineMs,...(mutation?{idempotencyKey:mutation.key}:{})};return new Promise((resolve,reject)=>{const timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,traceId))},deadlineMs+250);pending.set(id,{method,projectId:payload.projectId,resolve,reject,timer});try{transport.postMessage(message)}catch{clearTimeout(timer);pending.delete(id);reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,traceId))}})}
 return{list:p=>request('session.list',p),create:(p,o)=>request('session.create',p,o?.attempt)}
}
let sessionSingleton:SessionBridge|undefined
export function getSessionBridge():SessionBridge{return sessionSingleton??=createSessionBridge(webview())}
export const sessionBridge:SessionBridge={list:p=>getSessionBridge().list(p),create:(p,o)=>getSessionBridge().create(p,o)}

const textValid=(v:unknown)=>typeof v==='string'&&v.length>0&&!v.includes('\0')&&Array.from(v).length<=2048&&new TextEncoder().encode(v).length<=8192
const isMessage=(v:unknown,sessionId:string)=>isObj(v)&&exact(v,['id','sessionId','role','status','sequence','text','createdAt'])&&isULID(v.id)&&v.sessionId===sessionId&&v.role==='user'&&v.status==='completed'&&Number.isSafeInteger(v.sequence)&&Number(v.sequence)>0&&textValid(v.text)&&isTime(v.createdAt)
export function createMessageBridge(transport:WebViewTransport,defaultDeadlineMs=8_000):MessageBridge{
 type Waiting={method:'message.append'|'message.list';sessionId:string;direction:'forward'|'backward';cursor?:string;resolve(v:unknown):void;reject(e:Error):void;timer:number}
 const pending=new Map<string,Waiting>(),cursors=new Map<string,{sessionId:string;direction:'forward'|'backward';snapshot:number}>()
 transport.addEventListener('message',event=>{const raw:unknown=event.data;if(!isObj(raw)||typeof raw.requestId!=='string'||!pending.has(raw.requestId))return;const waiting=pending.get(raw.requestId)!;clearTimeout(waiting.timer);pending.delete(raw.requestId);if(!validEnvelope(raw)){waiting.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,raw.requestId));return}if(!raw.ok){waiting.reject(new BridgeClientError(raw.error.message,raw.error.code,raw.error.retryable,raw.error.correlationId));return}let valid=isMessage(raw.payload,waiting.sessionId);if(waiting.method==='message.list'){const p=raw.payload as Record<string,unknown>,known=waiting.cursor?cursors.get(waiting.cursor):undefined;valid=isObj(p)&&exact(p,['items','hasMore','nextCursor','snapshotSequence'])&&Array.isArray(p.items)&&p.items.length<=256&&p.items.every(x=>isMessage(x,waiting.sessionId))&&typeof p.hasMore==='boolean'&&Number.isSafeInteger(p.snapshotSequence)&&Number(p.snapshotSequence)>=0&&(p.nextCursor===null||typeof p.nextCursor==='string'&&p.nextCursor.length>=1&&p.nextCursor.length<=1024)&&p.hasMore===(p.nextCursor!==null)&&(!p.hasMore||p.items.length>0)&&(!known||known.sessionId===waiting.sessionId&&known.direction===waiting.direction&&known.snapshot===p.snapshotSequence);if(valid){const messageItems=p.items as unknown[],seq=messageItems.map((x:unknown)=>Number((x as Record<string,unknown>).sequence));for(let i=1;i<seq.length;i++)if(seq[i]!==seq[i-1]+(waiting.direction==='forward'?1:-1))valid=false;if(seq.some((x:number)=>x>Number(p.snapshotSequence)))valid=false;if(valid&&typeof p.nextCursor==='string')cursors.set(p.nextCursor,{sessionId:waiting.sessionId,direction:waiting.direction,snapshot:Number(p.snapshotSequence)})}}
 if(!valid){waiting.reject(new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,raw.id));return}waiting.resolve(raw.payload)})
 const request=<T>(method:'message.append'|'message.list',payload:MessageAppendPayload|MessageListPayload,attempt?:MutationAttempt<object>):Promise<T>=>{const id=ulid(),mutation=method==='message.append'?checkedAttempt(method,payload,attempt):undefined,traceId=ulid(),deadlineMs=Math.min(30000,Math.max(1,defaultDeadlineMs)),listPayload=payload as MessageListPayload,appendPayload=payload as MessageAppendPayload,direction=method==='message.list'?(listPayload.direction??'backward'):'backward';if(method==='message.list'&&(listPayload.cursor!==undefined&&(listPayload.cursor.length<1||listPayload.cursor.length>1024)||listPayload.limit!==undefined&&(!Number.isInteger(listPayload.limit)||listPayload.limit<1||listPayload.limit>256)||listPayload.byteBudget!==undefined&&(!Number.isInteger(listPayload.byteBudget)||listPayload.byteBudget<16384||listPayload.byteBudget>245760)))return Promise.reject(new BridgeClientError('消息分页参数无效','INVALID_BRIDGE_REQUEST',false,'renderer'));if(method==='message.append'&&!textValid(appendPayload.text))return Promise.reject(new BridgeClientError('消息文本无效','INVALID_BRIDGE_REQUEST',false,'renderer'));const message:BridgeRequest<object>={v:BRIDGE_VERSION,kind:'request',id,traceId,method,sentAt:new Date().toISOString(),payload:mutation?.payload??clone(payload),deadlineMs,...(mutation?{idempotencyKey:mutation.key}:{})};return new Promise((resolve,reject)=>{const timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,traceId))},deadlineMs+250);pending.set(id,{method,sessionId:payload.sessionId,direction,cursor:method==='message.list'?listPayload.cursor:undefined,resolve,reject,timer});try{transport.postMessage(message)}catch{clearTimeout(timer);pending.delete(id);reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,traceId))}})}
 return{list:p=>request('message.list',p),append:(p,o)=>request('message.append',p,o?.attempt)}
}
let messageSingleton:MessageBridge|undefined
export function getMessageBridge():MessageBridge{return messageSingleton??=createMessageBridge(webview())}
export const messageBridge:MessageBridge={list:p=>getMessageBridge().list(p),append:(p,o)=>getMessageBridge().append(p,o)}

const stageStatuses=['not_started','in_progress','waiting_review','approved','completed','rejected','stale','paused','blocked','cancelled']
const isStage=(v:unknown,projectId:string)=>isObj(v)&&exact(v,['id','projectId','phase','title','status','createdAt','updatedAt','version'])&&isULID(v.id)&&v.projectId===projectId&&Number.isInteger(v.phase)&&Number(v.phase)>=1&&Number(v.phase)<=9&&typeof v.title==='string'&&v.title===normalizedProjectName(v.title)&&Array.from(v.title).length>=1&&Array.from(v.title).length<=200&&stageStatuses.includes(String(v.status))&&isTime(v.createdAt)&&isTime(v.updatedAt)&&v.version===1
export interface StageBridge { list(payload:StageListPayload):Promise<StageListResult>; create(payload:StageCreatePayload,options?:MutationOptions<StageCreatePayload>):Promise<StageCreateResult> }
export function createStageBridge(transport:WebViewTransport,defaultDeadlineMs=8_000):StageBridge{
 const pending=new Map<string,{method:'stage.create'|'stage.list';projectId:string;resolve(v:unknown):void;reject(e:Error):void;timer:number}>()
 transport.addEventListener('message',event=>{const raw:unknown=event.data;if(!isObj(raw)||typeof raw.requestId!=='string'||!pending.has(raw.requestId))return;const waiting=pending.get(raw.requestId)!;clearTimeout(waiting.timer);pending.delete(raw.requestId);if(!validEnvelope(raw)){waiting.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,raw.requestId));return}if(!raw.ok){waiting.reject(new BridgeClientError(raw.error.message,raw.error.code,raw.error.retryable,raw.error.correlationId));return}const valid=waiting.method==='stage.create'?isStage(raw.payload,waiting.projectId):isObj(raw.payload)&&exact(raw.payload,['items'])&&Array.isArray(raw.payload.items)&&raw.payload.items.length<=9&&raw.payload.items.every(v=>isStage(v,waiting.projectId));if(!valid){waiting.reject(new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,raw.id));return}waiting.resolve(raw.payload)})
 const request=<T>(method:'stage.create'|'stage.list',payload:StageCreatePayload|StageListPayload,attempt?:MutationAttempt<object>):Promise<T>=>{const id=ulid(),mutation=method==='stage.create'?checkedAttempt(method,payload,attempt):undefined,traceId=ulid(),deadlineMs=Math.min(30000,Math.max(1,defaultDeadlineMs)),message:BridgeRequest<object>={v:BRIDGE_VERSION,kind:'request',id,traceId,method,sentAt:new Date().toISOString(),payload:mutation?.payload??clone(payload),deadlineMs,...(mutation?{idempotencyKey:mutation.key}:{})};return new Promise((resolve,reject)=>{const timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,traceId))},deadlineMs+250);pending.set(id,{method,projectId:payload.projectId,resolve,reject,timer});try{transport.postMessage(message)}catch{clearTimeout(timer);pending.delete(id);reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,traceId))}})}
 return{list:p=>request('stage.list',p),create:(p,o)=>request('stage.create',p,o?.attempt)}
}
let stageSingleton:StageBridge|undefined
export function getStageBridge():StageBridge{return stageSingleton??=createStageBridge(webview())}
export const stageBridge:StageBridge={list:p=>getStageBridge().list(p),create:(p,o)=>getStageBridge().create(p,o)}

// P3/P4 Bridge — 简化模式：envelope 校验 + 基本 request/response
function createSimpleBridge<TMethods extends Record<string, BridgeMethod>>(
  transport: WebViewTransport,
  methods: TMethods,
  defaultDeadlineMs = 8_000
) {
  const pending = new Map<string, { resolve(v: unknown): void; reject(e: Error): void; timer: number }>()
  transport.addEventListener('message', event => {
    const raw: unknown = event.data
    if (!isObj(raw) || typeof raw.requestId !== 'string' || !pending.has(raw.requestId)) return
    const requestId = raw.requestId
    const waiting = pending.get(requestId)!
    clearTimeout(waiting.timer)
    pending.delete(requestId)
    if (!validEnvelope(raw)) { waiting.reject(new BridgeClientError('Bridge 响应格式无效', 'INVALID_BRIDGE_RESPONSE', false, requestId)); return }
    if (raw.ok) waiting.resolve(raw.payload)
    else waiting.reject(new BridgeClientError(raw.error.message, raw.error.code, raw.error.retryable, raw.error.correlationId))
  })
  const request = <T>(method: BridgeMethod, payload: object, deadlineMs = defaultDeadlineMs, attempt?: MutationAttempt<object>): Promise<T> => {
    const id = ulid(), traceId = ulid()
    const mutation = mutationMethods.has(method) ? checkedAttempt(method as MutationMethod, payload, attempt) : undefined
    const message: BridgeRequest<object> = { v: BRIDGE_VERSION, kind: 'request', id, traceId, method, sentAt: new Date().toISOString(), payload: mutation?.payload ?? clone(payload), deadlineMs: Math.min(30_000, Math.max(1, deadlineMs)), ...(mutation ? { idempotencyKey: mutation.key } : {}) }
    return new Promise((resolve, reject) => {
      const timer = window.setTimeout(() => { pending.delete(id); reject(new BridgeClientError('Bridge 请求超时', 'REQUEST_DEADLINE_EXCEEDED', true, traceId)) }, message.deadlineMs + 250)
      pending.set(id, { resolve, reject, timer })
      try { transport.postMessage(message) } catch { clearTimeout(timer); pending.delete(id); reject(new BridgeClientError('WebView2 Bridge 当前不可用', 'BRIDGE_UNAVAILABLE', true, traceId)) }
    })
  }
  return { request }
}

export interface PlanBridge {
  get(payload: PlanGetPayload): Promise<PlanGetResult>
  list(payload: PlanListPayload): Promise<PlanListResult>
  create(payload: PlanCreatePayload, options?: MutationOptions<PlanCreatePayload>): Promise<PlanCreateResult>
  activate(payload: PlanActivatePayload): Promise<PlanActivateResult>
  complete(payload: PlanCompletePayload): Promise<PlanCompleteResult>
  pause(payload: PlanPausePayload): Promise<PlanPauseResult>
  resume(payload: PlanResumePayload): Promise<PlanResumeResult>
  listNodes(payload: NodeListPayload): Promise<NodeListResult>
  createNode(payload: NodeCreatePayload, options?: MutationOptions<NodeCreatePayload>): Promise<NodeCreateResult>
  startNode(payload: NodeStartPayload): Promise<NodeStartResult>
  completeNode(payload: NodeCompletePayload): Promise<NodeCompleteResult>
  failNode(payload: NodeFailPayload): Promise<NodeFailResult>
}
export function createPlanBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): PlanBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    get: p => core.request('plan.get', p),
    list: p => core.request('plan.list', p),
    create: (p, o) => core.request('plan.create', p, defaultDeadlineMs, o?.attempt),
    activate: p => core.request('plan.activate', p),
    complete: p => core.request('plan.complete', p),
    pause: p => core.request('plan.pause', p),
    resume: p => core.request('plan.resume', p),
    listNodes: p => core.request('node.list', p),
    createNode: (p, o) => core.request('node.create', p, defaultDeadlineMs, o?.attempt),
    startNode: p => core.request('node.start', p),
    completeNode: p => core.request('node.complete', p),
    failNode: p => core.request('node.fail', p),
  }
}
let planSingleton: PlanBridge | undefined
export function getPlanBridge(): PlanBridge { return planSingleton ??= createPlanBridge(webview()) }
export const planBridge: PlanBridge = { get: p => getPlanBridge().get(p), list: p => getPlanBridge().list(p), create: (p, o) => getPlanBridge().create(p, o), activate: p => getPlanBridge().activate(p), complete: p => getPlanBridge().complete(p), pause: p => getPlanBridge().pause(p), resume: p => getPlanBridge().resume(p), listNodes: p => getPlanBridge().listNodes(p), createNode: (p, o) => getPlanBridge().createNode(p, o), startNode: p => getPlanBridge().startNode(p), completeNode: p => getPlanBridge().completeNode(p), failNode: p => getPlanBridge().failNode(p) }

export interface ReviewBridge {
  list(payload: ReviewListPayload): Promise<ReviewListResult>
  approve(payload: ReviewApprovePayload): Promise<ReviewApproveResult>
  reject(payload: ReviewRejectPayload): Promise<ReviewRejectResult>
}
export function createReviewBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): ReviewBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return { list: p => core.request('review.list', p), approve: p => core.request('review.approve', p), reject: p => core.request('review.reject', p) }
}
let reviewSingleton: ReviewBridge | undefined
export function getReviewBridge(): ReviewBridge { return reviewSingleton ??= createReviewBridge(webview()) }
export const reviewBridge: ReviewBridge = { list: p => getReviewBridge().list(p), approve: p => getReviewBridge().approve(p), reject: p => getReviewBridge().reject(p) }

export interface MemoryBridge {
  get(payload: MemoryGetPayload): Promise<MemoryGetResult>
  list(payload: MemoryListPayload): Promise<MemoryListResult>
  create(payload: MemoryCreatePayload, options?: MutationOptions<MemoryCreatePayload>): Promise<MemoryCreateResult>
  search(payload: MemorySearchPayload): Promise<MemorySearchResult>
  update(payload: MemoryUpdatePayload): Promise<MemoryUpdateResult>
  delete(payload: MemoryDeletePayload): Promise<MemoryDeleteResult>
}
export function createMemoryBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): MemoryBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return { get: p => core.request('memory.get', p), list: p => core.request('memory.list', p), create: (p, o) => core.request('memory.create', p, defaultDeadlineMs, o?.attempt), search: p => core.request('memory.search', p), update: p => core.request('memory.update', p), delete: p => core.request('memory.delete', p) }
}
let memorySingleton: MemoryBridge | undefined
export function getMemoryBridge(): MemoryBridge { return memorySingleton ??= createMemoryBridge(webview()) }
export const memoryBridge: MemoryBridge = { get: p => getMemoryBridge().get(p), list: p => getMemoryBridge().list(p), create: (p, o) => getMemoryBridge().create(p, o), search: p => getMemoryBridge().search(p), update: p => getMemoryBridge().update(p), delete: p => getMemoryBridge().delete(p) }

export interface OntologyBridge {
  getNode(payload: OntologyNodeGetPayload): Promise<OntologyNodeGetResult>
  listNodes(payload: OntologyNodeListPayload): Promise<OntologyNodeListResult>
  searchNodes(payload: OntologyNodeSearchPayload): Promise<OntologyNodeSearchResult>
  createNode(payload: OntologyNodeCreatePayload): Promise<OntologyNodeCreateResult>
  updateNode(payload: OntologyNodeUpdatePayload): Promise<OntologyNodeUpdateResult>
  deleteNode(payload: OntologyNodeDeletePayload): Promise<OntologyNodeDeleteResult>
  listEdges(payload: OntologyEdgeListPayload): Promise<OntologyEdgeListResult>
  createEdge(payload: OntologyEdgeCreatePayload): Promise<OntologyEdgeCreateResult>
  updateEdge(payload: OntologyEdgeUpdatePayload): Promise<OntologyEdgeUpdateResult>
  deleteEdge(payload: OntologyEdgeDeletePayload): Promise<OntologyEdgeDeleteResult>
}
export function createOntologyBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): OntologyBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return { getNode: p => core.request('ontology.node.get', p), listNodes: p => core.request('ontology.node.list', p), searchNodes: p => core.request('ontology.node.search', p), createNode: p => core.request('ontology.node.create', p), updateNode: p => core.request('ontology.node.update', p), deleteNode: p => core.request('ontology.node.delete', p), listEdges: p => core.request('ontology.edge.list', p), createEdge: p => core.request('ontology.edge.create', p), updateEdge: p => core.request('ontology.edge.update', p), deleteEdge: p => core.request('ontology.edge.delete', p) }
}
let ontologySingleton: OntologyBridge | undefined
export function getOntologyBridge(): OntologyBridge { return ontologySingleton ??= createOntologyBridge(webview()) }
export const ontologyBridge: OntologyBridge = { getNode: p => getOntologyBridge().getNode(p), listNodes: p => getOntologyBridge().listNodes(p), searchNodes: p => getOntologyBridge().searchNodes(p), createNode: p => getOntologyBridge().createNode(p), updateNode: p => getOntologyBridge().updateNode(p), deleteNode: p => getOntologyBridge().deleteNode(p), listEdges: p => getOntologyBridge().listEdges(p), createEdge: p => getOntologyBridge().createEdge(p), updateEdge: p => getOntologyBridge().updateEdge(p), deleteEdge: p => getOntologyBridge().deleteEdge(p) }

export interface SkillBridge {
  get(payload: SkillGetPayload): Promise<SkillGetResult>
  list(payload: SkillListPayload): Promise<SkillListResult>
  create(payload: SkillCreatePayload, options?: MutationOptions<SkillCreatePayload>): Promise<SkillCreateResult>
  update(payload: SkillUpdatePayload, options?: MutationOptions<SkillUpdatePayload>): Promise<SkillUpdateResult>
  delete(payload: SkillDeletePayload, options?: MutationOptions<SkillDeletePayload>): Promise<SkillDeleteResult>
  match(payload: SkillMatchPayload): Promise<SkillMatchResult>
  publish(payload: SkillPublishPayload): Promise<SkillPublishResult>
  deprecate(payload: SkillDeprecatePayload): Promise<SkillDeprecateResult>
  disable(payload: SkillDisablePayload): Promise<SkillDisableResult>
}
export function createSkillBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): SkillBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return { get: p => core.request('skill.get', p), list: p => core.request('skill.list', p), create: (p, o) => core.request('skill.create', p, defaultDeadlineMs, o?.attempt), update: (p, o) => core.request('skill.update', p, defaultDeadlineMs, o?.attempt), delete: (p, o) => core.request('skill.delete', p, defaultDeadlineMs, o?.attempt), match: p => core.request('skill.match', p), publish: p => core.request('skill.publish', p), deprecate: p => core.request('skill.deprecate', p), disable: p => core.request('skill.disable', p) }
}
let skillSingleton: SkillBridge | undefined
export function getSkillBridge(): SkillBridge { return skillSingleton ??= createSkillBridge(webview()) }
export const skillBridge: SkillBridge = { get: p => getSkillBridge().get(p), list: p => getSkillBridge().list(p), create: (p, o) => getSkillBridge().create(p, o), update: (p, o) => getSkillBridge().update(p, o), delete: (p, o) => getSkillBridge().delete(p, o), match: p => getSkillBridge().match(p), publish: p => getSkillBridge().publish(p), deprecate: p => getSkillBridge().deprecate(p), disable: p => getSkillBridge().disable(p) }

export type StreamEvent =
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'delta';delta:{text:string}}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'usage';usage:{inputTokens:number;outputTokens:number;totalTokens:number}}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'completed'|'cancelled'}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'failed';error:{code:string;message:string;retryable:boolean}}
export interface ChatStream { readonly streamId:string; cancel():Promise<boolean>; dispose():void }
export interface ChatBridge { start(payload:ChatStartPayload,onEvent:(event:StreamEvent)=>void):Promise<ChatStream>; dispose():void }
const nonnegativeInt=(v:unknown)=>Number.isInteger(v)&&Number(v)>=0
const isStreamEvent=(v:unknown):v is StreamEvent=>{
 if(!isObj(v)||v.v!==BRIDGE_VERSION||v.kind!=='event'||!isULID(v.id)||!isULID(v.streamId)||!Number.isInteger(v.sequence)||Number(v.sequence)<1||typeof v.type!=='string')return false
 const base=['v','kind','id','streamId','sequence','type']
 switch(v.type){
  case'delta':return exact(v,[...base,'delta'])&&isObj(v.delta)&&exact(v.delta,['text'])&&typeof v.delta.text==='string'&&v.delta.text.length>0
  case'usage':return exact(v,[...base,'usage'])&&isObj(v.usage)&&exact(v.usage,['inputTokens','outputTokens','totalTokens'])&&nonnegativeInt(v.usage.inputTokens)&&nonnegativeInt(v.usage.outputTokens)&&nonnegativeInt(v.usage.totalTokens)&&v.usage.totalTokens===Number(v.usage.inputTokens)+Number(v.usage.outputTokens)
  case'completed':case'cancelled':return exact(v,base)
  case'failed':return exact(v,[...base,'error'])&&isObj(v.error)&&exact(v.error,['code','message','retryable'])&&typeof v.error.code==='string'&&v.error.code.length>0&&typeof v.error.message==='string'&&v.error.message.length>0&&typeof v.error.retryable==='boolean'
  default:return false
 }
}
export function createChatBridge(transport:WebViewTransport,deadlineMs=30_000):ChatBridge {
 type Pending={resolve(v:unknown):void;reject(e:Error):void;timer:number}
 type Active={listener:(e:StreamEvent)=>void;next:number;terminal:boolean}
 const pending=new Map<string,Pending>(),active=new Map<string,Active>(),early=new Map<string,StreamEvent[]>(),tombstones=new Map<string,number>();let disposed=false
 const tombstone=(id:string)=>{tombstones.delete(id);tombstones.set(id,Date.now());while(tombstones.size>128)tombstones.delete(tombstones.keys().next().value!)}
 const failStream=(id:string)=>{active.delete(id);early.delete(id);tombstone(id)}
 const failActive=(id:string,state:Active,code:string,message:string)=>{if(state.terminal)return;state.terminal=true;const sequence=state.next;failStream(id);state.listener({v:BRIDGE_VERSION,kind:'event',id:ulid(),streamId:id,sequence,type:'failed',error:{code,message,retryable:false}})}
 const deliver=(state:Active,event:StreamEvent)=>{if(state.terminal)return;if(event.sequence!==state.next){failActive(event.streamId,state,'BRIDGE_EVENT_SEQUENCE_INVALID','流事件顺序无效，已安全终止');return}state.next++;if(['completed','cancelled','failed'].includes(event.type)){state.terminal=true;failStream(event.streamId)}state.listener(event)}
 const route=(event:MessageEvent<BridgeResponse>)=>{const value:unknown=event.data;if(disposed)return;if(isObj(value)&&typeof value.requestId==='string'&&pending.has(value.requestId)){const p=pending.get(value.requestId)!;pending.delete(value.requestId);clearTimeout(p.timer);if(!validEnvelope(value))p.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,value.requestId));else if(value.ok)p.resolve(value.payload);else p.reject(new BridgeClientError(value.error.message,value.error.code,value.error.retryable,value.error.correlationId));return}const candidateId=isObj(value)&&typeof value.streamId==='string'&&isULID(value.streamId)?value.streamId:undefined;if(!isStreamEvent(value)){if(candidateId){const state=active.get(candidateId);if(state)failActive(candidateId,state,'INVALID_BRIDGE_EVENT','流事件格式无效，已安全终止');else{early.delete(candidateId);tombstone(candidateId)}}return}if(tombstones.has(value.streamId))return;const state=active.get(value.streamId);if(state){deliver(state,value);return}const buffered=early.get(value.streamId)??[];if(buffered.length>=32||early.size>=32&&!early.has(value.streamId)){early.delete(value.streamId);tombstone(value.streamId);return}if(value.sequence!==buffered.length+1||buffered.some(e=>['completed','cancelled','failed'].includes(e.type))){early.delete(value.streamId);tombstone(value.streamId);return}buffered.push(value);early.set(value.streamId,buffered)}
 transport.addEventListener('message',route)
 const request=<T>(method:BridgeMethod,payload:object)=>new Promise<T>((resolve,reject)=>{if(disposed){reject(new BridgeClientError('Chat Bridge 已释放','BRIDGE_UNAVAILABLE',false,'renderer'));return}const id=ulid(),traceId=ulid(),ms=Math.min(30_000,Math.max(1,deadlineMs)),timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,traceId))},ms+250);pending.set(id,{resolve,reject,timer});try{transport.postMessage({v:BRIDGE_VERSION,kind:'request',id,traceId,method,sentAt:new Date().toISOString(),payload,deadlineMs:ms})}catch{clearTimeout(timer);pending.delete(id);reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,traceId))}})
 const cancelLocal=(id:string)=>{if(!active.has(id)&&!early.has(id))return;failStream(id);try{void request<StreamCancelResult>('stream.cancel',{streamId:id}).catch(()=>{})}catch{/* best effort */}}
 return {async start(payload,onEvent){const result=await request<ChatStartResult>('chat.start',payload);if(!isObj(result)||!exact(result,['streamId'])||!isULID(result.streamId))throw new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,'renderer');if(disposed){try{void request('stream.cancel',{streamId:result.streamId})}catch{}throw new BridgeClientError('Chat Bridge 已释放','BRIDGE_UNAVAILABLE',false,'renderer')}const state:Active={listener:onEvent,next:1,terminal:false};active.set(result.streamId,state);if(tombstones.has(result.streamId)){failActive(result.streamId,state,'BRIDGE_EARLY_EVENT_INVALID','流在建立前收到无效事件，已安全终止')}else{const buffered=early.get(result.streamId)??[];early.delete(result.streamId);for(const event of buffered){if(!active.has(result.streamId))break;deliver(state,event)}}return{streamId:result.streamId,cancel:async()=>{if(!active.has(result.streamId))return false;const r=await request<StreamCancelResult>('stream.cancel',{streamId:result.streamId});return isObj(r)&&exact(r,['cancelled'])&&r.cancelled===true},dispose:()=>cancelLocal(result.streamId)}},dispose(){if(disposed)return;for(const id of [...active.keys()])cancelLocal(id);disposed=true;early.clear();for(const [id,p]of pending){clearTimeout(p.timer);p.reject(new BridgeClientError('Chat Bridge 已释放','BRIDGE_UNAVAILABLE',false,id))}pending.clear();transport.removeEventListener('message',route)}}
}
