import {validMessageText as validText,MESSAGE_LIMIT_ERROR,messageSize} from './messageLimits'
import {SessionSkillArtifacts,useSessionSkillArtifacts} from '../skill/SessionSkillArtifacts'
import {SubagentActivityRow} from './SubagentActivityRow'
import {TaskOutcomePanel} from './TaskOutcomePanel'
import {coerceTaskOutcome, type TaskOutcome} from './taskOutcome'
import {DurableMessageProcess} from './DurableMessageProcess'
import React,{useCallback,useEffect,useRef,useState}from'react'
import{readSessionComposerDraft,writeSessionComposerDraft,acknowledgeSessionComposerDraft}from'./sessionComposerDraft'
import{useZh}from'../i18n/language'
import{asUserBridgeError}from'../bridge/bridgeUserError'
import{BridgeClientError,contextBridge,createLocalWorkspaceBridge,createMutationAttempt,attachmentBridge as defaultAttachmentBridge,expertBridge as defaultExpertBridge,feedbackBridge as defaultFeedbackBridge,getPeopleBridge,memoryBridge as defaultMemoryBridge,memoryOpsBridge as defaultMemoryOpsBridge,messageBridge,newBridgeULID,sessionFolderBridge,skillBridge as defaultSkillBridge,type AttachmentBridge,type ChatBridge,type ContextBridge,type ExpertBridge,type FeedbackBridge,type MemoryBridge,type MemoryOpsBridge,type MessageBridge,type MutationAttempt,type ProviderBridge,type SessionBridge,type SkillBridge,type StageBridge,type StreamEvent}from'../bridge/client'
import type{AttachmentIngestResult,AttachmentListResult,ContextCompactPreviewResult,ContextHandoffImportResult,ContextHandoffInspectResult,ContextHandoffListResult,ContextStatusResult,ExpertListResult,MessageAppendPayload,MessageDTO,ProjectDTO,ProviderDTO,SessionCreatePayload,SessionDTO,SessionDeletePayload,SkillDTO,StageDTO}from'../generated/bridge'
import{normalizeProjectName}from'../project/ProjectPage'
import{ensureProjectStages,inferActivePhase,phaseStepClass,phasesForProjectType,PROJECT_TYPE_SHORT,readPreferredPhase,writePreferredPhase}from'../project/projectPhases'
import{chipsForPhase,isDevWorkflowPhase,isPhase1Workflow}from'../project/devWorkflowChips'
import{ConfirmDialog}from'../ui/Dialog'
import{useConfirmDialog,usePromptDialog}from'../ui/useAskDialog'
import{ATTACHMENT_ACCEPT,ATTACHMENT_FILE_MAX,clipboardImages,importLocalAttachments,ingestAttachments,normalizePastedImages,prepareAttachmentFiles,type AttachmentProgress}from'./attachments'
import{attachmentChipMeta}from'./attachmentChip'
import{preferHostDrop,subscribeHostFileDrop}from'./hostFileDrop'
import{noteMediaCenterPlay}from'../media/mediaCenterPlay'
import{composerPickNotice,pickComposerFiles,type DesktopFilesBridge,type DesktopPickItem}from'./composerPlusPick'
import{composerPickerEmpty,composerPickerFailed,composerPickerIdle,composerPickerLoading,composerPickerReady,type ComposerPickerState}from'./composerPickerState'
import{composeChatPrompt,embedPendingAttachments,parseComposer,userBubbleParts}from'./composerParser'
import{resolveTalkHandoffMessage,validTalkHandoffText,talkHandoffMessages}from'./companion/talkHandoff'
import{historicalToolText}from'./historicalToolText'
import{MessageAttachmentStrip,AttachmentViewer}from'./MessageAttachmentStrip'
import{excerptFromMessages,parseCreatedArtifactId,initialSkillPackageId}from'./catalogCreated'
import{applyConversationUserScroll,conversationNearBottom,conversationPinTop,pauseFollowOnUserIntent,pinConversationScroll}from'./streamScroll'
import{FILE_PICKER_ASK,filePickerHandoffKey,looksLikeFilePickerHandoff,looksLikeUACHandoff,parseUserAskSummary,UAC_ASK,uacHandoffKey,USER_ASK_TOOL,userAskActivitySummary,type UserAskPack}from'./userAsk'
import{PendingMemoryBanner}from'./PendingMemoryBanner'
import{pendingMroFromMessages,type PendingMemoryItem}from'./pendingMemory'
import{compactPreviewDescription,compactUsageLabel,contextNeedsCompact}from'./contextCompact'
import{UserAskWizard}from'./UserAskWizard'
import{latestTodoSummary,parseTaskSteps,TaskStepsFromSummary}from'./TaskSteps'
import{sessionWorkspaceBound}from'../workspace/workspaceSession'
import{llmReadyProviders,pickCompanionFlashModel,pickDefaultLLM,modelSupportsFunctionCalling}from'../provider/modelKind'
import{userWantsBrowserPanel}from'../workspace/browserAddress'
import{browserClickTarget,browserFollowURL,openBrowserFilePath,promptWithBrowserPage,readOpenBrowserPage}from'../workspace/browserPage'
import{composerWithPreviewQuote}from'./previewQuote'
import{buildSubagentChatPolicy,loadSubagentSettings}from'../settings/subagentSettings'
import{chatStartReplyFields}from'../settings/replySettings'
import{chatStartToolProfile}from'../settings/toolProfile'
import{ComposerReplyChips}from'./ComposerReplyChips'
import{ComposerAccessChips,laneWordFromGuidance}from'./ComposerAccessChips'
import{ComposerReasoningSlider}from'./ComposerReasoningSlider'
import{loadReasoningLevel,type ReasoningLevel}from'./composerReasoning'
import{Workspace,autoRevealWorkspaceForHtmlTool,autoRevealWorkspaceTab,type WorkspaceTab}from'../workspace/Workspace'
import{extractTaskFiles}from'../workspace/codePanelUtils'
import type{WorkbenchNav,WorkbenchStats}from'../project/projectWorkbenchNav'
import type{FilesFocus}from'../workspace/FilesPanel'
import{usePanelResize}from'../ui/usePanelResize'
import{workspaceCoversChat,workspaceDragLimit,workspaceWidthAfterCover}from'./workspaceSplit'
import{AssistantMessageBody,MarkdownMessage,ThinkingPanel,thinkingDuplicatesBody}from'./MarkdownMessage'
import{microphoneConstraints,saveMicrophoneId,selectedMicrophoneId}from'../settings/microphone'
import{systemSettingsBridge}from'../bridge/client'
import{ConversationSnapshot,selectedMessages}from'./ConversationSnapshot'
import{QueueStrip,useInputQueue}from'./inputQueue'
import{CcStatusBar,useCcStatus}from'./ccStatus'
import{CompanionStage}from'./companion/CompanionStage'
import{markVoiceTiming,startVoiceTurn}from'./companion/voiceTiming'
import{isCompanionInfraBusy,withInfraBusyRetry}from'./companion/companionBusy'
import{ensureCompanionCapabilities,CC_CONFIG_EVENT}from'./companion/ensureCompanionCapabilities'
import{ChatArtifactCards,type ChatArtifact}from'./ChatArtifactCards'
import{focusOfficeArtifact}from'../officeStudio/officeNavigation'
import{ArtifactInspector}from'../workspace/ArtifactInspector'
import{ChatFollowUps}from'./ChatFollowUps'
import{splitChatSuggestions}from'./chatSuggestions'
import{applyLiveChatEvent,cancelLiveChatTurn,failLiveChat,listActiveTurns,liveChatEntry,startLiveChat,subscribeLiveChat,type LiveChatEntry}from'./liveChat'
import{cancelLivePaint,scheduleLivePaint}from'./livePaint'
import {SessionUsageBar} from './SessionUsageBar'
import {SessionOperationsBar} from './SessionOperations'
import {SessionWidgetsBar} from './SessionWidgets'
import {type TokenUsageValue} from './TokenUsage'
import {composerSizeHint} from './composerHint'
import{composerPrimaryAction,composerPrimaryLabel,showComposerStopButton,showSegmentStopControl}from'./turnControl'
import{inFlightLiveChat,isStopCommand,classifyFollowUp,classifyCompanionInFlight,companionShouldDiscardOnExit}from'./turnFollowUp'
import{countAssistantMessages,turnHistorySettlement}from'./turnHistory'
import{companionMayAutoApprove,isDangerousCompanionTool,sessionMayAutoApprove}from'./approvalProfile'
import{sessionMessageAtItems,atMenuPlaceholder,atQuery,filterComposerAtItems,insertComposerAtPick,type ComposerAtItem}from'./composerAtMenu'
import{isCompanionChatTitle,isProtectedSidebarChat,isRenameableChatTitle,titleFromFirstTurn}from'./sessionTitle'
import{formatMroContextStrip,parseMroContext}from'../mro/mroContext'
import{PERSIST_RETRY_SENTINEL,clearPersistFailed,readPersistFailed,writePersistFailed}from'./persistRetry'
import{bannersFromTurnState}from'./sessionResume'
import{isAgentContact}from'../people/peopleRoster'
export{ATTACHMENT_FILE_MAX}from'./attachments'
import { MessagePanel } from './SessionMessagePanel'
import {
  sessionUserError,
  problem,
  toChatArtifacts,
  enterToSendEnabled,
  ordered,
  orderedMessages,
  Retained,
  MessageAttempt,
  ExecutionMode,
  modeKey,
  isExecutionMode,
  generalDefaultExecutionMode,
  persistedExecutionMode,
  MODE_INFO,
  MODE_INFO_EN,
  modeInfoFor,
  TURN_RESUME_PROMPT,
  TURN_INTERRUPT_NOTICE,
  TURN_ERROR_NOTICE,
  TURN_ERROR_TIMEOUT_CAUSE,
  TURN_ERROR_TOO_LARGE_CAUSE,
  TURN_ERROR_INCOMPLETE_CAUSE,
  isChatStreamFailure,
  turnFailureNotice,
  appendTurnNotice,
  turnStorageKey,
  ActiveTurn,
  readActiveTurn,
  writeActiveTurn,
  clearActiveTurn,
  SpeechRecognitionEventLike,
  SpeechRecognitionLike,
  speechRecognitionConstructor,
  idleSpeechLevels,
  SKILL_CATEGORY_ICONS,
  toolLabel,
  toolActivityDetail,
  companionActivityLine,
  iconPaths,
  ActionIcon,
  lastAssistantExcerpt,
} from './sessionPageShared'
export {
  type ExecutionMode,
  isExecutionMode,
  generalDefaultExecutionMode,
  persistedExecutionMode,
  MODE_INFO,
  MODE_INFO_EN,
  modeInfoFor,
  TURN_RESUME_PROMPT,
  isChatStreamFailure,
  turnFailureNotice,
} from './sessionPageShared'
export function SessionPage({project,bridge,messages=messageBridge,onBack,backLabel,keepEmptySession=false,onDeleted,onSessionEngaged,stages,chat,providers,providersRevision=0,attachments=defaultAttachmentBridge,skills=defaultSkillBridge,desktopFiles,experts,initialSession,officeTaskId,initialPrompt,initialNoAutoSend,initialUploadFiles,initialLocalItems,initialProviderId,initialModelId,initialExecutionMode,initialComposerTrigger,initialWorkspaceTab,initialWorkspacePath,initialWorkspaceFocus,initialReferencedSkills,initialCompanion,onActivityChange,onManageModels,onCatalogCreated,onSaveAsSkill,onOpenMemory,onOpenMcp,personal=false,homeChat=false,readOnly=false,feedback=defaultFeedbackBridge,memory=defaultMemoryBridge,memoryOps=defaultMemoryOpsBridge,projectSidePanel,projectSideLabel,projectApprovalPanel,pendingApprovalCount=0,onRegisterWorkbenchNav,onWorkbenchStatsChange,projectPhase,projectPhaseLabel,currentTaskBrief,currentTaskPath,projectRoot,context=contextBridge}:{project:ProjectDTO;bridge:SessionBridge;messages?:MessageBridge;onBack:()=>void;backLabel?:string;keepEmptySession?:boolean;onDeleted?:(id:string)=>void;onSessionEngaged?:()=>void;stages?:StageBridge;chat?:ChatBridge;providers?:ProviderBridge;providersRevision?:number;attachments?:AttachmentBridge;skills?:SkillBridge;desktopFiles?:DesktopFilesBridge;experts?:ExpertBridge;initialSession?:SessionDTO;officeTaskId?:string;initialPrompt?:string;initialNoAutoSend?:boolean;initialUploadFiles?:readonly File[];initialLocalItems?:readonly DesktopPickItem[];initialProviderId?:string;initialModelId?:string;initialExecutionMode?:ExecutionMode;initialComposerTrigger?:'@'|'/'|'expert';initialWorkspaceTab?:WorkspaceTab;initialWorkspacePath?:string;initialWorkspaceFocus?:FilesFocus;initialReferencedSkills?:SkillDTO[];initialCompanion?:boolean;onActivityChange?:(active:boolean)=>void;onManageModels?:()=>void;onCatalogCreated?:(kind:'skill'|'expert'|'plugin',id?:string)=>void;onSaveAsSkill?:(excerpt:string)=>void;onOpenMemory?:()=>void;onOpenMcp?:()=>void;personal?:boolean;homeChat?:boolean;readOnly?:boolean;feedback?:FeedbackBridge;memory?:MemoryBridge;memoryOps?:MemoryOpsBridge;projectSidePanel?:React.ReactNode;projectSideLabel?:string;projectApprovalPanel?:React.ReactNode;pendingApprovalCount?:number;onRegisterWorkbenchNav?:(nav:WorkbenchNav|undefined)=>void;onWorkbenchStatsChange?:(stats:WorkbenchStats)=>void;projectPhase?:number;projectPhaseLabel?:string;currentTaskBrief?:string;currentTaskPath?:string;projectRoot?:string;context?:ContextBridge}):React.JSX.Element{
 const chrome=personal||homeChat
 const zh=useZh()
 const[items,setItems]=useState<SessionDTO[]>(initialSession?[initialSession]:[]),[title,setTitle]=useState(''),[loading,setLoading]=useState(true),[busy,setBusy]=useState(false),[error,setError]=useState<BridgeClientError>(),[selected,setSelected]=useState<SessionDTO|undefined>(initialSession),[deleteTarget,setDeleteTarget]=useState<SessionDTO>(),[stageItems,setStageItems]=useState<StageDTO[]>([]),[activePhase,setActivePhase]=useState<number>(()=>readPreferredPhase(project.id)??1),mounted=useRef(true),token=useRef(0),busyRef=useRef(false),deletingSessionRef=useRef(false),retained=useRef<Retained|undefined>(undefined),projectRef=useRef(project.id),leaveChatRef=useRef<(()=>void)|undefined>(undefined);projectRef.current=project.id
 const load=async()=>{if(busyRef.current)return;const current=++token.current;setLoading(true);try{const r=await bridge.list({projectId:project.id});if(mounted.current&&current===token.current){setItems(ordered(r.items));setError(undefined)}}catch(e){if(mounted.current&&current===token.current)setError(problem(e))}finally{if(mounted.current&&current===token.current)setLoading(false)}}
 useEffect(()=>{mounted.current=true;token.current++;busyRef.current=false;retained.current=undefined;setItems(initialSession?[initialSession]:[]);setSelected(initialSession);setTitle('');setBusy(false);setError(undefined);setActivePhase(readPreferredPhase(project.id)??1);void load();if(stages){let cancelled=false;void ensureProjectStages(stages,project.id,project.type).then(items=>{if(cancelled||!mounted.current)return;setStageItems(items);const map=new Map(items.map(s=>[s.phase,s]));setActivePhase(v=>inferActivePhase(phasesForProjectType(project.type),map,v??readPreferredPhase(project.id)))}).catch(()=>{});return()=>{cancelled=true}};return()=>{mounted.current=false;token.current++}},[bridge,project.id,project.type,stages,initialSession])
 const submit=async(e:React.FormEvent)=>{e.preventDefault();if(busyRef.current)return;const normalized=normalizeProjectName(title);if(!normalized||Array.from(normalized).length>200)return;const submittedProject=project.id,payload:SessionCreatePayload={projectId:submittedProject,title:normalized},signature=JSON.stringify(payload),attempt=retained.current?.signature===signature?retained.current.attempt:createMutationAttempt('session.create',payload),current=++token.current;retained.current={signature,attempt};busyRef.current=true;setLoading(false);setBusy(true);setError(undefined);setTitle(normalized);try{const created=await bridge.create(attempt.payload as SessionCreatePayload,{attempt});if(mounted.current&&current===token.current&&projectRef.current===submittedProject){retained.current=undefined;setItems(v=>ordered([...v.filter(x=>x.id!==created.id),created]));setTitle('')}}catch(x){const issue=problem(x);if(mounted.current&&current===token.current&&projectRef.current===submittedProject){setError(issue);if(!issue.retryable)retained.current=undefined}}finally{if(mounted.current&&current===token.current&&projectRef.current===submittedProject){busyRef.current=false;setBusy(false)}}}
 const delSession=async()=>{if(!deleteTarget||busyRef.current||isProtectedSidebarChat(deleteTarget.title)){if(deleteTarget&&isProtectedSidebarChat(deleteTarget.title))setDeleteTarget(undefined);return}const{id}=deleteTarget,current=++token.current;busyRef.current=true;setBusy(true);setError(undefined);try{const attempt=createMutationAttempt('session.delete',{id} as SessionDeletePayload);await bridge.delete(attempt.payload as SessionDeletePayload,{attempt});if(mounted.current&&current===token.current){setItems(v=>v.filter(x=>x.id!==id));if(selected?.id===id)setSelected(undefined);setDeleteTarget(undefined)}}catch(e){if(mounted.current&&current===token.current)setError(problem(e))}finally{if(mounted.current&&current===token.current){busyRef.current=false;setBusy(false)}}}
 const deleteEmptySession=async(id:string)=>{if(deletingSessionRef.current||!mounted.current)return;const titles=[selected?.id===id?selected.title:undefined,items.find(value=>value.id===id)?.title,initialSession?.id===id?initialSession.title:undefined].filter((title):title is string=>Boolean(title));if(titles.some(isProtectedSidebarChat))return;deletingSessionRef.current=true;try{const attempt=createMutationAttempt('session.delete',{id}as SessionDeletePayload);await bridge.delete(attempt.payload as SessionDeletePayload,{attempt})}catch{/* still drop the sidebar row */}finally{if(mounted.current){setItems(values=>values.filter(value=>value.id!==id));setSelected(undefined);onDeleted?.(id);if(!onDeleted&&chrome)onBack()}deletingSessionRef.current=false}}
 const phaseDefs=phasesForProjectType(project.type),stageMap=new Map(stageItems.map(s=>[s.phase,s])),activePhaseLabel=phaseDefs.find(p=>p.phase===activePhase)?.label??'',selectPhase=(phase:number)=>{setActivePhase(phase);writePreferredPhase(project.id,phase)},projectStatusLabel=project.status==='active'?'进行中':project.status==='closed'?'只读':project.status==='archived'?'已归档':'创建',messagePanel=(session:SessionDTO)=><MessagePanel key={session.id} session={session} bridge={messages} sessions={bridge} onClose={chrome?onBack:()=>setSelected(undefined)} onBindLeave={fn=>{leaveChatRef.current=fn}} onEmpty={async()=>{if(keepEmptySession||officeTaskId){onBack();return}await deleteEmptySession(session.id)}} onSessionEngaged={onSessionEngaged} chat={chat} providers={providers} providersRevision={providersRevision} context={context} attachments={attachments} skills={skills} desktopFiles={desktopFiles} experts={experts} projectId={project.id} projectCode={project.projectCode} officeTaskId={officeTaskId} initialPrompt={session.id===initialSession?.id?initialPrompt:undefined} initialNoAutoSend={session.id===initialSession?.id?initialNoAutoSend:undefined} initialUploadFiles={session.id===initialSession?.id?initialUploadFiles:undefined} initialLocalItems={session.id===initialSession?.id?initialLocalItems:undefined} initialComposerTrigger={session.id===initialSession?.id?initialComposerTrigger:undefined} initialWorkspaceTab={session.id===initialSession?.id?initialWorkspaceTab:undefined} initialWorkspacePath={session.id===initialSession?.id?initialWorkspacePath:undefined} initialWorkspaceFocus={session.id===initialSession?.id?initialWorkspaceFocus:undefined} initialReferencedSkills={session.id===initialSession?.id?initialReferencedSkills:undefined} initialCompanion={session.id===initialSession?.id?initialCompanion:undefined} onActivityChange={onActivityChange} initialProviderId={initialProviderId} initialModelId={initialModelId} personal={personal} homeChat={chrome} initialExecutionMode={initialExecutionMode} onManageModels={onManageModels} onCatalogCreated={onCatalogCreated} onSaveAsSkill={onSaveAsSkill} onOpenMemory={onOpenMemory} onOpenMcp={onOpenMcp} feedback={feedback} memory={memory} memoryOps={memoryOps} phaseLabel={projectPhaseLabel||(!chrome?activePhaseLabel:undefined)} readOnly={readOnly} projectSidePanel={projectSidePanel} projectSideLabel={projectSideLabel} projectApprovalPanel={projectApprovalPanel} pendingApprovalCount={pendingApprovalCount} onRegisterWorkbenchNav={onRegisterWorkbenchNav} onWorkbenchStatsChange={onWorkbenchStatsChange} projectPhase={projectPhase} projectPhaseLabel={projectPhaseLabel} currentTaskBrief={currentTaskBrief} currentTaskPath={currentTaskPath} projectRoot={projectRoot??project.rootPath}/>
 return <main className={`main ${chrome?'personal-chat-page':''} ${selected&&!chrome?'project-workspace-shell':''}`}><div className="topbar"><button onClick={()=>leaveChatRef.current?leaveChatRef.current():onBack()}>← {backLabel??(personal?(zh?'返回主页':'Home'):(zh?'返回项目管理':'Project management'))}</button><div className="tb-sep"></div><div className="tb-proj"><span className="dot"></span>{personal?(selected?.title??(zh?'普通对话':'Chat')):project.name}</div>{!chrome&&selected&&activePhaseLabel&&<div className="tb-phase" role="status">阶段 {activePhase} · {activePhaseLabel}</div>}{!chrome&&<div className="tb-model"><span className="pulse"></span>本地引擎</div>}{chrome&&selected&&<button type="button" className="session-open-folder" aria-label={zh?'打开对话文件夹':'Open conversation folder'} title={zh?'打开本对话的产物文件夹':'Open this conversation folder'} onClick={()=>void sessionFolderBridge.open({sessionId:selected.id}).catch(e=>setError(problem(e)))}><span aria-hidden="true">📁</span>{zh?'打开':'Open'}</button>}</div>{!chrome&&stages&&!selected&&<div className="pipeline" role="navigation" aria-label="阶段流水线">{phaseDefs.map(def=>{const s=stageMap.get(def.phase),cls=phaseStepClass(s?.status),on=def.phase===activePhase;return <button type="button" key={def.phase} className={`pp-step ${cls} ${on?'on':''}`} aria-current={on?'step':undefined} onClick={()=>selectPhase(def.phase)}><div className="n">{s?.status==='completed'||s?.status==='approved'?'✓':def.phase}</div><div className="l">{def.label}</div></button>})}</div>}<div className={`session-content ${selected&&!chrome?'session-content-workspace':''}`}>{!chrome&&!selected&&<><h1>{project.name} · 会话</h1><div className="project-layout"><section className="project-create"><h2>新建会话</h2><form onSubmit={e=>void submit(e)}><fieldset disabled={busy}><label>会话标题<input value={title} onChange={e=>{setTitle(e.target.value);retained.current=undefined}}/></label><button className="primary" disabled={busy}>{busy?'创建中…':'创建会话'}</button></fieldset></form>{error&&<div className="error" role="alert"><b>{error.message}</b>{error.retryable&&<button disabled={busy||loading} onClick={()=>void load()}>重试</button>}</div>}</section><section className="project-list"><h2>会话列表</h2>{loading&&!items.length?<p role="status">正在载入会话…</p>:items.length?<div className="project-cards">{items.map(v=><article className="project-card" key={v.id}><button className="session-card-open" onClick={()=>setSelected(v)}><h3>{v.title}</h3><p>活跃 · {new Date(v.createdAt).toLocaleString()}</p></button>{!isProtectedSidebarChat(v.title)&&<button className="card-delete" aria-label={`删除会话 ${v.title}`} disabled={busy} onClick={()=>setDeleteTarget(v)}>删除</button>}</article>)}</div>:<div className="empty"><b>还没有会话</b></div>}</section></div></>}{selected&&!chrome&&<div className="project-workspace"><aside className="phase-nav" aria-label="项目阶段导航"><div className="phase-nav-head"><div className="phase-nav-logo"><span className="moon-logo sm" aria-hidden="true"></span><b>LUNITIDE</b></div><div className="project-switch"><b>{project.name}</b><span>{PROJECT_TYPE_SHORT[project.type]} · {projectStatusLabel}</span></div><div className="phase-nav-title">项目阶段</div></div><div className="phase-list">{phaseDefs.map(def=>{const s=stageMap.get(def.phase),cls=phaseStepClass(s?.status),on=def.phase===activePhase;return <button type="button" key={def.phase} className={`phase-item ${cls} ${on?'on':''}`} aria-current={on?'step':undefined} onClick={()=>selectPhase(def.phase)}><span className="phase-num">{s?.status==='completed'||s?.status==='approved'?'✓':def.phase}</span><span className="phase-label">{def.label}</span></button>})}</div><p className="phase-contract-note"><b>类型投影</b>{project.type==='operations'?' · 运维六阶段':' · 实施/增强八阶段'}</p></aside>{messagePanel(selected)}</div>}{selected&&chrome&&messagePanel(selected)}{chrome&&!selected&&<div className="empty" style={{padding:'40px 20px'}}><b>正在打开对话…</b></div>}</div><ConfirmDialog open={!!deleteTarget} title={`删除会话「${deleteTarget?.title??''}」？`} description="所有消息将被永久删除，此操作不可撤销。" busy={busy} error={error?.message} onCancel={()=>{setDeleteTarget(undefined);setError(undefined)}} onConfirm={()=>void delSession()}/></main>
}
