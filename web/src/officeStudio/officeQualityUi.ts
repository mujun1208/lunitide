import type { OfficeQuality, OfficeVersion } from './officeStudioApi'

export const OFFICE_STYLE_OPTIONS = [
  { id: 'ops-clear', label: '清晰经营' },
  { id: 'brand-pitch', label: '品牌方案' },
  { id: 'editorial-report', label: '编辑式报告' },
] as const

export const OFFICE_GENERATE_STAGES = ['整理', '设计', '生成', '检查'] as const

export function briefFieldLabel(value?: string): string {
  const trimmed = value?.trim() ?? ''
  return trimmed || '未填写'
}

export function briefLengthLabel(targetLength?: number): string {
  return targetLength && targetLength > 0 ? `约 ${targetLength} 页` : '页数未填写'
}

export function canFormalDeliver(version?: Pick<OfficeVersion, 'quality' | 'validations'>): boolean {
  if (!version || version.quality !== 'passed') return false
  return !(version.validations ?? []).some(
    (check) => check.status === 'failed' || (check.severity === 'blocking' && check.status !== 'passed'),
  )
}

export function conceptPreviewLabel(layoutPreview: boolean): string {
  return layoutPreview ? '文件排版预览' : '概念预览'
}

export function draftQualityNotice(quality: OfficeQuality): string {
  return quality === 'passed' ? '检查已通过，可作为正式交付。' : '低风险未检全版本标为草稿，正式交付前需清除硬问题。'
}

export function visualScoreNotice(): string {
  return '规则视觉分为未校准启发式，不能当作 85 分认证。'
}

export function deferredOfficeCapabilitiesNotice(): string {
  return '企业模板审批与在线协同（含 Univer）本期不做；不提供组织模板库。'
}

export function trialScopeNotice(): string {
  return '本期试验范围：可编辑简报、四格式生成、检查、局部修改、草稿或正式导出。不声称设计师已检 36 变体、校准 85 分、PowerPoint/WPS 实机通过、竞品盲评或企业模板审批。'
}

export function qualityPromiseLabels(
  version?: Pick<OfficeVersion, 'quality' | 'mode' | 'validations'>,
): string[] {
  if (!version) return []
  const checks = version.validations ?? []
  const out: string[] = []
  const factFailed = checks.some((check) => /fact|source|metric/i.test(check.id) && check.status === 'failed')
  if (checks.some((check) => check.id === 'structure' && check.status === 'passed') && !factFailed) {
    out.push('内容完整')
  }
  if (checks.some((check) => /layout|overlap|geometry|clip|overflow/i.test(check.id) && check.status === 'passed')) {
    out.push('排版已检查')
  }
  if (checks.some((check) => /source|fact|metric/i.test(check.id) && check.status === 'passed')) {
    out.push('关键数字有来源')
  }
  if (version.mode === 'managed' || version.mode === 'imported') {
    out.push('可继续编辑')
  }
  const needsWork =
    version.quality === 'blocked' ||
    version.quality === 'partial' ||
    checks.some((check) => check.status === 'failed' || (check.severity === 'blocking' && check.status !== 'passed'))
  if (needsWork) out.push('存在需处理的问题')
  return out
}

export function formalDeliverBlockedReason(version?: Pick<OfficeVersion, 'quality' | 'validations'>): string {
  if (!version) return '未选择版本，不能作为正式交付。'
  if (canFormalDeliver(version)) return ''
  const blockers = (version.validations ?? []).filter(
    (check) => check.status === 'failed' || (check.severity === 'blocking' && check.status !== 'passed'),
  )
  if (blockers.length) {
    const labels = blockers.map((check) => check.label || check.id).join('、')
    return `正式交付仍被阻断：${labels}。本机未验证的目标软件、视觉模型和 PDF/A 不单独算正式门槛。`
  }
  if (version.quality !== 'passed') {
    return '检查尚未全部通过，只能导出草稿，不能作为正式交付。'
  }
  return '正式交付仍被阻断。'
}

export function findFactRefs(
  facts: Array<{ factId: string; value: string; unit?: string }>,
  nodes: Array<{ id: string; text: string; valueType?: string }>,
): Array<{ factId: string; nodeId: string }> {
  const out: Array<{ factId: string; nodeId: string }> = []
  for (const fact of facts) {
    const id = fact.factId.trim()
    const value = fact.value.trim()
    if (!id || !value) continue
    for (const node of nodes) {
      const kind = node.valueType || ''
      if (kind.includes('formula') || kind.includes('error')) continue
      const text = node.text || ''
      if (text === value || (text.includes(value) && (!fact.unit || text.includes(fact.unit)))) {
        out.push({ factId: id, nodeId: node.id })
      }
    }
  }
  return out
}
