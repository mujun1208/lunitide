import type { ReviewDTO } from '../generated/bridge'

export type ReviewPresentation = {
  title: string
  crId?: string
  whatWillHappen: string
  evidence: string
  recovery: string
  approveLabel: string
  rejectLabel: string
}

const byAction: Record<string, Partial<ReviewPresentation>> = {
  'release.promote': {
    title: '发布到生产环境',
    crId: 'CR',
    whatWillHappen: '将把当前 CR 传输包晋级到生产环境，并触发目标环境的部署流水线。',
    evidence: '集成门禁、测试清单与 manifest digest 已在构建阶段校验。',
    recovery: '健康检查失败时将自动回滚到上一稳定 revision。',
    approveLabel: '批准此操作摘要',
    rejectLabel: '要求修改',
  },
  'cross_domain_send': {
    title: '导出设计 Token（跨域发送）',
    whatWillHappen: '将把设计 Token JSON 发送到外部协作域；跨域传输需人工确认。',
    evidence: '参数摘要与 policy 版本已锁定；发送前会再次校验 params_hash。',
    recovery: '可在审批中心退回并重新生成 Token 包。',
    approveLabel: '批准本次发送',
    rejectLabel: '退回',
  },
  'plan.activate': {
    title: '启动执行计划',
    whatWillHappen: '将激活计划 DAG，并按节点依赖顺序开始执行。',
    evidence: '节点风险分级与预算已在计划管理中配置。',
    recovery: '可随时暂停计划并回退未完成节点。',
    approveLabel: '批准启动计划',
    rejectLabel: '暂缓',
  },
}

export function presentReview(review: ReviewDTO): ReviewPresentation {
  const preset = byAction[review.actionType] ?? {}
  const shortId = review.id.slice(0, 8).toUpperCase()
  return {
    title: preset.title ?? review.actionType,
    crId: preset.crId ?? shortId,
    whatWillHappen: preset.whatWillHappen ?? `将执行 ${review.actionType}，参数 digest 已绑定到当前计划状态。`,
    evidence: preset.evidence ?? `action ${review.actionDigest.slice(0, 8)}… · input ${review.inputDigest.slice(0, 8)}… · state ${review.stateDigest.slice(0, 8)}…`,
    recovery: preset.recovery ?? '批准不绕过执行前重验；参数漂移时批准自动失效。',
    approveLabel: preset.approveLabel ?? '批准此操作',
    rejectLabel: preset.rejectLabel ?? '退回',
  }
}
