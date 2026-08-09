import {
  BRIDGE_VERSION, type BridgeMethod, type BridgeRequest, type BridgeResponse,
  type ProviderCreatePayload, type ProviderCreateResult, type ProviderCredentialSubmitPayload,
  type ProviderCredentialSubmitResult, type ProviderDeletePayload, type ProviderDeleteResult,
  type ProviderGetPayload, type ProviderGetResult, type ProviderListPayload, type ProviderListResult,
  type ProviderModelSyncPayload, type ProviderModelSyncResult, type ProviderTestPayload,
  type ProviderTestResult, type ProviderUpdatePayload, type ProviderUpdateResult,
  type ChatStartPayload, type ChatStartResult, type StreamCancelResult,
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

export type MutationMethod = 'provider.create'|'provider.update'|'provider.delete'|'provider.model.sync'
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
const mutationMethods = new Set<BridgeMethod>(['provider.create','provider.update','provider.delete','provider.model.sync'])
function ulid(): string { const a='0123456789ABCDEFGHJKMNPQRSTVWXYZ',b=crypto.getRandomValues(new Uint8Array(10));let v=(BigInt(Date.now())<<80n)|b.reduce((n,x)=>(n<<8n)|BigInt(x),0n),r='';for(let i=0;i<26;i++){r=a[Number(v&31n)]+r;v>>=5n}return r }
const isObj=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)
const exact=(v:Record<string,unknown>,required:string[],optional:string[]=[])=>required.every(k=>k in v)&&Object.keys(v).every(k=>required.includes(k)||optional.includes(k))
const isULID=(v:unknown)=>typeof v==='string'&&/^[0-7][0-9A-HJKMNP-TV-Z]{25}$/.test(v)
const isTime=(v:unknown)=>typeof v==='string'&&!Number.isNaN(Date.parse(v))
const isModel=(v:unknown)=>isObj(v)&&exact(v,['modelId','displayName','isDefault'])&&typeof v.modelId==='string'&&/^[\x21-\x7E]{1,200}$/.test(v.modelId)&&typeof v.displayName==='string'&&v.displayName===v.displayName.trim()&&v.displayName.length>0&&new TextEncoder().encode(v.displayName).length<=200&&typeof v.isDefault==='boolean'
const isModels=(v:unknown)=>Array.isArray(v)&&v.length>=1&&v.length<=50&&v.every(isModel)&&new Set(v.map(x=>(x as {modelId:string}).modelId)).size===v.length&&v.filter(x=>(x as {isDefault:boolean}).isDefault).length===1
const isProvider=(v:unknown)=>isObj(v)&&exact(v,['id','name','protocol','baseUrl','models','status','credentialState','createdAt','updatedAt','version'])&&isULID(v.id)&&typeof v.name==='string'&&v.name===v.name.trim()&&v.name.length>0&&['openai_compatible','anthropic'].includes(String(v.protocol))&&typeof v.baseUrl==='string'&&isModels(v.models)&&['enabled','disabled'].includes(String(v.status))&&['configured','missing','unavailable','requires_reentry'].includes(String(v.credentialState))&&isTime(v.createdAt)&&isTime(v.updatedAt)&&Number.isInteger(v.version)&&Number(v.version)>=1
const guards:Partial<Record<BridgeMethod,(v:unknown)=>boolean>>={
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
