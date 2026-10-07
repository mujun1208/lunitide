import {validMessageText as validText,MESSAGE_LIMIT_ERROR,messageSize} from './messageLimits'
import {TOOL_LABELS} from './toolLabels'
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

export function sessionUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}
export const problem=(e:unknown)=>e instanceof BridgeClientError?asUserBridgeError(e,'请求失败'):new BridgeClientError(sessionUserError(e,'请求失败'),'CLIENT_ERROR',false,'renderer')
export const toChatArtifacts=(items:MessageDTO['artifacts']):ChatArtifact[]=>(items??[]).map(item=>({kind:item.kind,path:item.path,content:'',callId:item.callId,toolName:item.toolName}))
export const enterToSendEnabled=()=>{try{const raw=localStorage.getItem('lunitide:general');if(raw)return JSON.parse(raw).enterToSend!==false}catch{}return true}
export const ordered=(v:SessionDTO[])=>[...v].sort((a,b)=>a.createdAt.localeCompare(b.createdAt)||a.id.localeCompare(b.id))
export const orderedMessages=(v:MessageDTO[])=>[...new Map(v.map(x=>[x.id,x])).values()].sort((a,b)=>a.sequence-b.sequence||a.createdAt.localeCompare(b.createdAt)||a.id.localeCompare(b.id))
export type Retained={signature:string;attempt:MutationAttempt<SessionCreatePayload>};export type MessageAttempt={signature:string;attempt:MutationAttempt<MessageAppendPayload>}
export type ExecutionMode='approval'|'auto-edit'|'full-access'
export const modeKey=(sessionId:string)=>`lunitide:execution-mode:${sessionId}`
// Legacy "plan" values stored in localStorage or older sessions are mapped
// to "approval" (plan mode is now system-automatic via complexity routing).
export const isExecutionMode=(value:unknown):value is ExecutionMode=>value==='approval'||value==='auto-edit'||value==='full-access'
// generalDefaultExecutionMode reads the "默认工作模式" setting so a brand-new
// session opens in the mode the user picked. Only a valid ExecutionMode counts;
// anything else (unset, or a legacy auto/collab/code value) means full-access —
// the historical default when nothing read this setting at all.
export const generalDefaultExecutionMode=():ExecutionMode=>{try{const raw=localStorage.getItem('lunitide:general');if(raw){const m=(JSON.parse(raw) as {defaultMode?:unknown}).defaultMode;if(isExecutionMode(m))return m}}catch{}return 'full-access'}
export const persistedExecutionMode=(value:unknown):ExecutionMode=>isExecutionMode(value)?value:value==='plan'?'approval':value==null||value===undefined?generalDefaultExecutionMode():'approval'
export const MODE_INFO:Record<ExecutionMode,{label:string;description:string}>={approval:{label:'手动审批',description:'工具和命令需要你逐项批准后执行。'},'auto-edit':{label:'自动审批',description:'自动执行常规操作，高风险操作仍需你确认。'},'full-access':{label:'完全访问',description:'免审批，直接操作电脑文件、命令和工具。'}}
export const MODE_INFO_EN:Record<ExecutionMode,{label:string;description:string}>={approval:{label:'Ask first',description:'Tools and commands wait for your approval.'},'auto-edit':{label:'Auto-approve',description:'Routine actions run automatically; high-risk ones still need you.'},'full-access':{label:'Full access',description:'No approval gate; files, commands, and tools run directly.'}}
export const modeInfoFor=(zh:boolean)=>zh?MODE_INFO:MODE_INFO_EN
export const TURN_RESUME_PROMPT = '继续上次未完成的工作。结合任务清单、已完成步骤和我补充过的说明，接着做到完成。'
export const TURN_INTERRUPT_NOTICE='终止打断了'
export const TURN_ERROR_NOTICE='无法执行。'
export const TURN_ERROR_TIMEOUT_CAUSE='请求超时，请稍后重试。'
export const TURN_ERROR_TOO_LARGE_CAUSE='回复或工具参数过大，请减少内容后重试。'
export const TURN_ERROR_INCOMPLETE_CAUSE='模型结果不完整，请重试。'
export const isChatStreamFailure=(code:string)=>/^(UPSTREAM_|PROVIDER_|REQUEST_TOO_LARGE|ASSISTANT_RESPONSE_TOO_LARGE|MESSAGE_STORAGE|HOST_BUSY|ENGINE_BUSY|BUDGET_EXHAUSTED|CONTEXT_WINDOW_EXCEEDED)/.test(code)
export function turnFailureNotice(err?: {code?: string} | null): string {
  const code=err?.code??''
  if(code==='CONTEXT_WINDOW_EXCEEDED')return TURN_ERROR_NOTICE+'这一轮的内容太长，较早的对话已经收起。请再试一次。'
  if(code==='BUDGET_EXHAUSTED')return TURN_ERROR_NOTICE+'这一轮先记下已完成的内容，请再试一次。'
  if(code==='UPSTREAM_UNAVAILABLE'||code==='PROVIDER_RATE_LIMITED')return TURN_ERROR_NOTICE+'供应商暂时不可用，请稍后重试。'
  if(code==='UPSTREAM_TIMEOUT'||code.includes('TIMEOUT'))return TURN_ERROR_NOTICE+TURN_ERROR_TIMEOUT_CAUSE
  if(code==='ASSISTANT_RESPONSE_TOO_LARGE'||code==='REQUEST_TOO_LARGE')return TURN_ERROR_NOTICE+TURN_ERROR_TOO_LARGE_CAUSE
  return TURN_ERROR_NOTICE+TURN_ERROR_INCOMPLETE_CAUSE
}
export const appendTurnNotice=(prev:string,notice:string)=>{if(prev.includes(notice))return prev;if(notice.startsWith(TURN_ERROR_NOTICE)&&prev.includes(TURN_ERROR_NOTICE))return prev;const t=prev.trim();return t?`${t}\n${notice}`:notice}
export const turnStorageKey = (id: string) => `lunitide:active-turn:${id}`
export type ActiveTurn = { status: 'running' | 'interrupted' | 'cancelled'; resumeCount: number }
export const readActiveTurn = (id: string): ActiveTurn | undefined => {
  try {
    const v = JSON.parse(localStorage.getItem(turnStorageKey(id)) || '')
    if (v && typeof v === 'object' && (v.status === 'running' || v.status === 'interrupted' || v.status === 'cancelled')) return v
  } catch { /* ignore */ }
}
export const writeActiveTurn = (id: string, patch: Partial<ActiveTurn>) => {
  if (!id) return
  const prev = readActiveTurn(id) ?? { status: 'running' as const, resumeCount: 0 }
  try { localStorage.setItem(turnStorageKey(id), JSON.stringify({ ...prev, ...patch })) } catch { /* quota / private mode */ }
}
export const clearActiveTurn = (id: string) => localStorage.removeItem(turnStorageKey(id))
// Auto-resume budget (2026-10-07): an interrupted task re-sends the resume
// prompt by itself, but at most maxTurnAutoResumes consecutive times per
// active-turn record. A manual user send resets resumeCount to 0 and a
// completed turn clears the record, so the budget only bounds fully
// automatic retry chains.
export const maxTurnAutoResumes = 3
export const turnAutoResumesLeft = (id: string) => (readActiveTurn(id)?.resumeCount ?? 0) < maxTurnAutoResumes
export const countTurnAutoResume = (id: string) => writeActiveTurn(id, { resumeCount: (readActiveTurn(id)?.resumeCount ?? 0) + 1 })
export type SpeechRecognitionEventLike={results:ArrayLike<{0:{transcript:string};isFinal:boolean}>}
export type SpeechRecognitionLike={lang:string;continuous:boolean;interimResults:boolean;onresult:((event:SpeechRecognitionEventLike)=>void)|null;onerror:((event?:{error?:string})=>void)|null;onend:(()=>void)|null;start:()=>void;stop:()=>void}
export const speechRecognitionConstructor=()=>((window as typeof window&{SpeechRecognition?:new()=>SpeechRecognitionLike;webkitSpeechRecognition?:new()=>SpeechRecognitionLike}).SpeechRecognition??(window as typeof window&{webkitSpeechRecognition?:new()=>SpeechRecognitionLike}).webkitSpeechRecognition)
export const idleSpeechLevels=[.18,.32,.24,.44,.28,.38,.2,.3,.22,.36,.26,.2]
export const SKILL_CATEGORY_ICONS:Record<string,string>={efficiency:'⚡',writing:'✎',development:'⌨',data:'▤',design:'◈',research:'⌕',lifestyle:'☘',education:'❑',business:'▣',automation:'⚙',security:'⛨',other:'✦'}
export const toolLabel=(name:string)=>name.startsWith('fs.')?'读取':name.startsWith('mcp_')?`MCP ${name.slice(4)}`:TOOL_LABELS[name]??name
export const toolActivityDetail=(name:string,summary?:string)=>{if(name===USER_ASK_TOOL)return userAskActivitySummary(summary);if(name==='todo.write'){const steps=parseTaskSteps(summary);if(!steps.length)return undefined;const done=steps.filter(step=>step.status==='completed').length;return `${done}/${steps.length}`}return summary}
export const companionActivityLine=(activities:Array<{name:string;status:string;summary?:string}>):string|undefined=>{if(!activities.length)return undefined;const t=activities[activities.length-1];if(t.status==='approval_required')return'等你确认…';if(t.status==='tool_started'||t.status==='tool_output')return`${toolLabel(t.name)}中…`;if(t.status==='tool_completed'){const s=t.summary?.trim()??'';if(!s||/"frameId"/.test(s)||s.startsWith('{'))return undefined;return s.slice(0,40)};if(t.summary?.trim()){const s=t.summary.trim();if(/"frameId"/.test(s)||s.startsWith('{'))return undefined;return s.slice(0,28)}return undefined}
export const iconPaths={copy:<><rect x="5" y="5" width="11" height="13" rx="2"/><path d="M8 5V4a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-2"/></>,share:<><circle cx="18" cy="5" r="2"/><circle cx="6" cy="12" r="2"/><circle cx="18" cy="19" r="2"/><path d="m8 11 8-5M8 13l8 5"/></>,rewind:<><path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/></>,remove:<><path d="M4 7h16M9 7V4h6v3M7 7l1 14h8l1-14M10 11v6M14 11v6"/></>,retry:<><path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 4v7h-7"/></>,mic:<><path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z"/><path d="M5 10v2a7 7 0 0 0 14 0v-2M12 19v3M8 22h8"/></>,stop:<><circle cx="12" cy="12" r="9"/><rect className="icon-fill" x="9" y="9" width="6" height="6" rx="0.5"/></>,bookmark:<><path d="M6 3h12a1 1 0 0 1 1 1v17l-7-4-7 4V4a1 1 0 0 1 1-1Z"/></>,thumbUp:<><path d="M7 10v12"/><path d="M15 5.88 14 10h5.83a2 2 0 0 1 1.92 2.56l-2.33 8A2 2 0 0 1 17.5 22H4a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2h2.76a2 2 0 0 0 1.79-1.11L12 2a3.13 3.13 0 0 1 3 3.88Z"/></>,thumbDown:<><path d="M17 14V2"/><path d="M9 18.12 10 14H4.17a2 2 0 0 1-1.92-2.56l2.33-8A2 2 0 0 1 6.5 2H20a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-2.76a2 2 0 0 0-1.79 1.11L12 22a3.13 3.13 0 0 1-3-3.88Z"/></>}
export function ActionIcon({name}:{name:keyof typeof iconPaths}){return <svg className="action-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">{iconPaths[name]}</svg>}
export function lastAssistantExcerpt(items:MessageDTO[],streaming:string):string{
 const live=streaming.trim()
 if(live)return live.slice(0,2000)
 for(let i=items.length-1;i>=0;i--){
  if(items[i].role==='assistant'&&items[i].text?.trim())return items[i].text.trim().slice(0,2000)
 }
 return ''
}
