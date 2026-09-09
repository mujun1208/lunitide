import type {StreamEvent} from '../bridge/client'

export type TokenUsageValue = Extract<StreamEvent, {type: 'usage'}>['usage']

export function TokenUsage({usage, enabled, zh}: {usage?: TokenUsageValue; enabled?: boolean; zh: boolean}) {
  if (!usage && enabled === undefined) return null
  const reported = usage?.cacheUsageReported === true
  return <div className="chat-usage token-usage" role="status" aria-label={zh ? 'Token 用量与精简' : 'Token usage and efficiency'}>
    {enabled !== undefined && <span>{zh ? '上下文精简' : 'Context trimming'}: {enabled ? (zh ? '已开启' : 'On') : (zh ? '已关闭' : 'Off')}</span>}
    {usage && <>
      <span>{zh ? '输入' : 'Input'} {usage.inputTokens} · {zh ? '输出' : 'Output'} {usage.outputTokens} · {zh ? '合计' : 'Total'} {usage.totalTokens}</span>
      <span>{zh ? '缓存命中' : 'Cache read'} {reported ? (usage.cachedInputTokens ?? 0) : usage.cachedInputTokens ? `${usage.cachedInputTokens} (${zh ? '部分回报' : 'partial'})` : (zh ? '未完整回报' : 'Not fully reported')}</span>
      {(reported || !!usage.cacheWriteInputTokens) && <span>{zh ? '缓存写入' : 'Cache write'} {usage.cacheWriteInputTokens ?? 0}{!reported && (zh ? ' (部分回报)' : ' (partial)')}</span>}
    </>}
  </div>
}
