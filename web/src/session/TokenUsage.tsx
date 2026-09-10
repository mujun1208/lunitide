import type {StreamEvent} from '../bridge/client'
import type {ChatUsageSnapshot} from '../bridge/client'

export type TokenUsageValue = Extract<StreamEvent, {type: 'usage'}>['usage']

type UsageAttempt = ChatUsageSnapshot['attempts'][number]

function attemptHasSnapshot(item: UsageAttempt): boolean {
  return !!item.policyVersion || (item.bytesBefore ?? 0) > 0 || (item.bytesAfter ?? 0) > 0
}

function trimCopy(attempts: UsageAttempt[] | undefined, zh: boolean): string {
  const recorded = (attempts ?? []).filter(attemptHasSnapshot)
  if (recorded.length === 0) return zh ? '本次精简 未知' : 'Trim unknown'
  const before = recorded.reduce((n, item) => n + (item.bytesBefore ?? 0), 0)
  const after = recorded.reduce((n, item) => n + (item.bytesAfter ?? 0), 0)
  if (after < before) return zh ? `本次精简 精简了 ${before}→${after}` : `Trim applied ${before}→${after}`
  return zh ? `本次精简 未精简 ${before}→${after}` : `Trim unchanged ${before}→${after}`
}

function attemptDurationCopy(ms: number | undefined, zh: boolean): string {
  if (ms == null || ms < 0) return ''
  const label = ms >= 1000 && ms % 1000 === 0 ? `${ms / 1000}s` : `${ms}ms`
  return zh ? `耗时 ${label}` : label
}

function integrityLabel(value: string | undefined, zh: boolean): string {
  if (!zh) return value ?? ''
  switch (value) {
    case 'reported': return '已回报'
    case 'partial': return '部分'
    case 'estimated': return '估算'
    case 'unknown': return '未知'
    default: return zh ? '其他' : (value ?? '')
  }
}

function purposeLabel(value: string | undefined, zh: boolean): string {
  if (!value) return zh ? '调用' : 'call'
  if (!zh) return value
  switch (value) {
    case 'chat': return '对话'
    case 'compaction': return '压缩摘要'
    case 'plan': return '规划'
    case 'judge': return '评判'
    case 'council': return '专家会商'
    case 'subagent': return '子任务'
    case 'ocr': return '识别'
    case 'embed': return '向量'
    case 'diagnostic': return '诊断'
    case 'gui': return '界面'
    case 'vision': return '视觉'
    case 'image': return '图片'
    case 'video': return '视频'
    case 'expert': return '专家'
    case 'people': return '同事'
    case 'route': return '路由'
    case 'meeting': return '会议纪要'
    case 'automation': return '自动化'
    case 'llm': return '模型'
    case 'unknown': return '未分类'
    default: return '其他'
  }
}

function callStatusLabel(value: string | undefined, zh: boolean): string {
  if (!zh) return value ?? ''
  switch (value) {
    case 'succeeded': return '已成功'
    case 'failed': return '失败'
    case 'cancelled': return '已取消'
    case 'intent': return '已登记'
    case 'sent': return '已发送'
    case 'unknown': return '未知'
    default: return '其他'
  }
}

function attemptTrimCopy(item: UsageAttempt, zh: boolean): string {
  if (!attemptHasSnapshot(item)) return zh ? '未知' : 'unknown'
  const before = item.bytesBefore ?? 0
  const after = item.bytesAfter ?? 0
  if (after < before) return zh ? `精简了 ${before}→${after}` : `trimmed ${before}→${after}`
  return zh ? `未精简 ${before}→${after}` : `unchanged ${before}→${after}`
}

export function TokenUsage({usage, enabled, zh, ledger}: {usage?: TokenUsageValue; enabled?: boolean; zh: boolean; ledger?: ChatUsageSnapshot}) {
  const collected = ledger?.collected === true
  const display = usage ?? (collected ? {
    inputTokens: ledger.inputTokens,
    outputTokens: ledger.outputTokens,
    totalTokens: ledger.inputTokens + ledger.outputTokens,
    cachedInputTokens: ledger.cachedInputTokens,
    cacheWriteInputTokens: ledger.cacheWriteInputTokens,
    cacheUsageReported: ledger.cacheUsageReported,
  } : undefined)
  const uncollected = !!ledger && !collected && !usage
  if (!display && enabled === undefined && !uncollected) return null
  const reported = display?.cacheUsageReported === true
  return <div className="chat-usage token-usage" role="status" aria-label={zh ? 'Token 用量与精简' : 'Token usage and efficiency'}>
    {enabled !== undefined && <span>{zh ? '上下文精简' : 'Context trimming'}: {enabled ? (zh ? '已开启' : 'On') : (zh ? '已关闭' : 'Off')}{zh ? '（只作用于请求结构与同源去重，不含独立摘要或供应商缓存；改环境变量后需重启，进行中的任务不改版）' : ' (JSON/dedup only; not independent summaries or provider caches; restart required; in-flight tasks keep their version)'}</span>}
    {uncollected && <span>{zh ? '旧记录未采集' : 'Not collected for this turn'}</span>}
    {collected && ledger?.stablePrefixHash && <span>{zh ? '固定段' : 'Fixed prefix'} {ledger.stablePrefixHash.slice(0, 8)} · {zh ? '不含当前任务边界，不表示供应商命中' : 'Excludes the current-turn boundary; not a supplier cache hit'}</span>}
    {display && <>
      <span>{zh ? '输入' : 'Input'} {display.inputTokens} · {zh ? '输出' : 'Output'} {display.outputTokens} · {zh ? '合计' : 'Total'} {display.totalTokens}</span>
      <span>{zh ? '缓存命中' : 'Cache read'} {reported ? (display.cachedInputTokens ?? 0) : display.cachedInputTokens ? `${display.cachedInputTokens} (${zh ? '部分回报' : 'partial'})` : (zh ? '未完整回报' : 'Not fully reported')}</span>
      {(reported || !!display.cacheWriteInputTokens) && <span>{zh ? '缓存写入' : 'Cache write'} {display.cacheWriteInputTokens ?? 0}{!reported && (zh ? ' (部分回报)' : ' (partial)')}</span>}
    </>}
    {ledger?.integrity && collected && <span>{zh ? '完整性' : 'Integrity'} {integrityLabel(ledger.integrity, zh)}</span>}
    {collected && ledger && <span>{trimCopy(ledger.attempts, zh)}</span>}
    {collected && ledger && ledger.attempts.length > 0 && <details>
      <summary>{zh ? '调用详情' : 'Call details'} ({ledger.attempts.length})</summary>
      {ledger.attempts.map(item => {
        const duration = attemptDurationCopy(item.durationMs, zh)
        return <div key={`${item.callId}:${item.attemptId}`}>
          {purposeLabel(item.purpose, zh)} · {callStatusLabel(item.status, zh)} · {zh ? '输入' : 'in'} {item.inputTokens} / {zh ? '输出' : 'out'} {item.outputTokens}{duration && ` · ${duration}`} · {attemptTrimCopy(item, zh)}
        </div>
      })}
    </details>}
  </div>
}
