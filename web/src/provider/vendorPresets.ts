import type { ModelDTO, ProviderProtocol, ProviderStatus } from '../generated/bridge'
import { persistKind, type ModelKind } from './modelKind'

export type ProviderDraft = {
  name: string
  protocol: ProviderProtocol
  baseUrl: string
  status: ProviderStatus
  models: ModelDTO[]
}

export type VendorPreset = {
  id: string
  label: string
  name: string
  protocol: ProviderProtocol
  baseUrl: string
  modelId: string
  displayName: string
  tabs: readonly ModelKind[]
  urlHint: string
}

const CHAT_TABS: readonly ModelKind[] = ['llm', 'vision', 'gui']
const COMPAT_TABS: readonly ModelKind[] = ['llm', 'vision', 'image', 'video', 'embedding', 'gui']

export const VENDOR_PRESETS: readonly VendorPreset[] = [
  {
    id: 'deepseek',
    label: 'DeepSeek',
    name: 'DeepSeek',
    protocol: 'openai_compatible',
    baseUrl: 'https://api.deepseek.com',
    modelId: 'deepseek-chat',
    displayName: 'DeepSeek Chat',
    tabs: CHAT_TABS,
    urlHint: 'Chat Completions。模型 ID 按控制台填写，例如 deepseek-chat / deepseek-flash。',
  },
  {
    id: 'glm',
    label: '智谱 GLM',
    name: '智谱 GLM',
    protocol: 'openai_compatible',
    baseUrl: 'https://open.bigmodel.cn/api/paas/v4',
    modelId: 'glm-5.3',
    displayName: 'GLM-5.3',
    tabs: CHAT_TABS,
    urlHint: '智谱开放平台 OpenAI 兼容（/api/paas/v4）。不要再拼 /v1。火山套餐请用 Agent Plan / Coding Plan。',
  },
  {
    id: 'glm-agent-plan',
    label: 'GLM · Agent Plan',
    name: 'GLM',
    protocol: 'openai_compatible',
    baseUrl: 'https://ark.cn-beijing.volces.com/api/plan/v3',
    modelId: 'glm-5.3',
    displayName: 'GLM 5.3',
    tabs: CHAT_TABS,
    urlHint: '火山 Agent Plan 的 Chat Completions。不要再拼 /v1 或 /chat/completions。',
  },
  {
    id: 'ark',
    label: '火山方舟',
    name: '火山方舟',
    protocol: 'openai_compatible',
    baseUrl: 'https://ark.cn-beijing.volces.com/api/v3',
    modelId: '',
    displayName: '',
    tabs: COMPAT_TABS,
    urlHint: '方舟标准推理。模型 ID 用接入点或模型名，按控制台粘贴。',
  },
  {
    id: 'ark-agent-plan',
    label: '火山 Agent Plan',
    name: '火山 Agent Plan',
    protocol: 'openai_compatible',
    baseUrl: 'https://ark.cn-beijing.volces.com/api/plan/v3',
    modelId: '',
    displayName: '',
    tabs: CHAT_TABS,
    urlHint: 'Agent Plan Chat Completions（/api/plan/v3）。走 Responses 时请选「Agent Plan · Responses」。',
  },
  {
    id: 'ark-agent-plan-responses',
    label: 'Agent Plan · Responses',
    name: '火山 Agent Plan',
    protocol: 'openai_responses',
    baseUrl: 'https://ark.cn-beijing.volces.com/api/plan/v3',
    modelId: '',
    displayName: '',
    tabs: CHAT_TABS,
    urlHint: '同一 Plan 地址，协议改成 Responses API，请求打到 POST /responses。',
  },
  {
    id: 'ark-coding-plan',
    label: '火山 Coding Plan',
    name: '火山 Coding Plan',
    protocol: 'openai_compatible',
    baseUrl: 'https://ark.cn-beijing.volces.com/api/coding/v3',
    modelId: '',
    displayName: '',
    tabs: CHAT_TABS,
    urlHint: 'Coding Plan Chat Completions。地址已带 v3，不要再拼 /v1。',
  },
  {
    id: 'ark-coding-plan-responses',
    label: 'Coding Plan · Responses',
    name: '火山 Coding Plan',
    protocol: 'openai_responses',
    baseUrl: 'https://ark.cn-beijing.volces.com/api/coding/v3',
    modelId: '',
    displayName: '',
    tabs: CHAT_TABS,
    urlHint: 'Coding Plan 的 Responses API。',
  },
  {
    id: 'bailian',
    label: '阿里百炼',
    name: '阿里百炼',
    protocol: 'openai_compatible',
    baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    modelId: 'qwen-plus',
    displayName: 'Qwen Plus',
    tabs: COMPAT_TABS,
    urlHint: '百炼 OpenAI 兼容模式。Key 用阿里云 DashScope API-Key。',
  },
  {
    id: 'bailian-responses',
    label: '阿里百炼 · Responses',
    name: '阿里百炼',
    protocol: 'openai_responses',
    baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    modelId: 'qwen-plus',
    displayName: 'Qwen Plus',
    tabs: CHAT_TABS,
    urlHint: '百炼兼容模式走 POST /responses。生图/向量仍用 OpenAI-compatible。',
  },
  {
    id: 'anthropic',
    label: 'Anthropic',
    name: 'Anthropic',
    protocol: 'anthropic',
    baseUrl: 'https://api.anthropic.com',
    modelId: 'claude-sonnet-4-20250514',
    displayName: 'Claude Sonnet 4',
    tabs: CHAT_TABS,
    urlHint: 'Anthropic Messages API。引擎发 x-api-key，不要填成 OpenAI-compatible。',
  },
  {
    id: 'openai',
    label: 'OpenAI',
    name: 'OpenAI',
    protocol: 'openai_compatible',
    baseUrl: 'https://api.openai.com/v1',
    modelId: 'gpt-4.1',
    displayName: 'GPT-4.1',
    tabs: COMPAT_TABS,
    urlHint: 'Chat Completions。Responses 请另选 OpenAI · Responses。',
  },
  {
    id: 'openai-responses',
    label: 'OpenAI · Responses',
    name: 'OpenAI',
    protocol: 'openai_responses',
    baseUrl: 'https://api.openai.com/v1',
    modelId: 'gpt-4.1',
    displayName: 'GPT-4.1',
    tabs: CHAT_TABS,
    urlHint: 'OpenAI Responses API（POST /responses）。',
  },
]

export function protocolLabel(protocol: ProviderProtocol): string {
  if (protocol === 'anthropic') return 'Anthropic'
  if (protocol === 'volc_speech') return '火山语音'
  if (protocol === 'openai_responses') return 'Responses API'
  return 'OpenAI-compatible'
}

export function responsesKindAllowed(kind: string): boolean {
  return kind === 'llm' || kind === 'vision' || kind === 'gui'
}

export function modelsForProtocol(protocol: ProviderProtocol, models: ModelDTO[], catalogKind: ModelKind): ModelDTO[] {
  if (protocol !== 'openai_responses') {
    return models
  }
  const kept = models.filter((model) => responsesKindAllowed(persistKind(model)))
  if (kept.length === 0) {
    const kind = responsesKindAllowed(catalogKind) ? catalogKind : 'llm'
    return [{ modelId: '', displayName: '', isDefault: true, kind, kindDefault: true }]
  }
  if (kept.some((model) => model.isDefault)) {
    return kept
  }
  return kept.map((model, index) => ({ ...model, isDefault: index === 0 }))
}

export function applyVendorPreset(draft: ProviderDraft, preset: VendorPreset, catalogKind: ModelKind): ProviderDraft {
  const hasModel = draft.models.some((model) => model.modelId.trim())
  const models = hasModel
    ? draft.models
    : [
        {
          modelId: preset.modelId,
          displayName: preset.displayName,
          isDefault: true,
          kind: catalogKind === 'voice' ? 'llm' : catalogKind,
          kindDefault: true,
        },
      ]
  return {
    ...draft,
    name: draft.name.trim() ? draft.name : preset.name,
    protocol: preset.protocol,
    baseUrl: preset.baseUrl,
    models: modelsForProtocol(preset.protocol, models, catalogKind),
  }
}

export function presetsForTab(kind: ModelKind): VendorPreset[] {
  return VENDOR_PRESETS.filter((preset) => preset.tabs.includes(kind))
}

export function protocolUrlHint(protocol: ProviderProtocol): string | undefined {
  if (protocol === 'volc_speech') return undefined
  if (protocol === 'openai_responses') {
    return 'Responses API：填到版本根（/v1、/api/v3、/api/plan/v3、/compatible-mode/v1），不要带 /responses。只用于对话 / 视觉 / GUI。'
  }
  if (protocol === 'anthropic') {
    return 'Messages API：通常是 https://api.anthropic.com。不要填成 OpenAI 兼容地址。'
  }
  return 'OpenAI 兼容 Chat Completions。已带 /v1、/v3、/v4 时不要再拼一层。粘贴了 /chat/completions 保存时引擎会去掉。'
}
