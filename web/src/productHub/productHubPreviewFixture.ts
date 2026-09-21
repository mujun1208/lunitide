import type { ProductHubBridge } from '../bridge/client'
import { buildCatalogCards, buildCatalogGraph, catalogChanges, catalogOverview } from './hubCatalog'
import type { HubCard, HubFinding, HubNode } from './productHubTypes'

const TOKEN = 'preview-token'
const USER = 'mujun'
const PASS = '1234567890'

const musicCard: HubCard = {
  stable_key: 'feature.dialog.music.open-player',
  name: '打开音乐播放软件',
  name_en: 'Open Music Player',
  domain: 'dialog',
  module: 'companion',
  summary: '和语音对话、打字对话打开电脑上的音乐播放软件。',
  description: '可以通过打字对话和语音对话操作电脑，打开电脑安装的汽水音乐、网易云音乐等音乐播放软件，并进行播放、暂停、搜索歌曲、下一曲、上一曲等操作。支持连续语音指令，无需重复唤醒；打字对话可 @技能引用直达。',
  attributes: {
    operations: ['电脑操作'],
    tools: ['computer.control', 'process.launch', 'window.find'],
    mcps: ['mcp_windows_sysmon'],
    skills: ['skillpack.computer-ops'],
    capabilities: ['capability.stt.asr', 'capability.llm.intent', 'capability.tts.voice'],
  },
  methods: [
    { type: 'voice', entry: '唤醒月伴后直接说「打开网易云音乐」', continuous: '连续播放、暂停、下一曲，免重复唤醒' },
    { type: 'ui', entry: '对话框输入「帮我打开汽水音乐」', continuous: '@技能引用电脑操作直达' },
  ],
  chain: {
    steps: [
      { index: 1, name: '用户输入', detail: '语音 / 文字', description: '月伴说或打字；语音经 VAD 断句后触发。' },
      { index: 2, name: '语音识别', detail: 'ASR 转写', description: '本地 sherpa；在线不可用自动切本地模型。' },
      { index: 3, name: '意图理解', detail: 'LLM 解析', description: '解析 computer.open_app；低置信反问确认。' },
      { index: 4, name: '方案生成', detail: '工具绑定', description: '绑定 computer.control；FullAccess 免审批。' },
      { index: 5, name: '执行控制', detail: 'UI 自动化', description: '开始菜单搜索 → 启动进程 → 8s 内等窗口。' },
    ],
    branches: [
      { type: 'success', from_step: 5, name: '成功', description: '窗口校验·成功；提示音+TTS播报；成功链路入库' },
      {
        type: 'failure',
        from_step: 5,
        name: '失败',
        description: '错误诊断·失败',
        retry: { name: '重试·备用启动', description: '注册表 App Paths 再搜' },
        fallback: { name: '降级·播报+推荐', description: '播报失败原因，推荐替代应用' },
      },
      { type: 'retry', from_step: 5, name: '重试·备用启动', description: '注册表 App Paths；成功汇入成功路径' },
      { type: 'fallback', from_step: 5, name: '降级·播报+推荐', description: '播报失败原因，推荐替代应用（汽水音乐）' },
    ],
  },
  scaffold: {
    pages: ['home', 'media'],
    bridge: ['computer.control', 'process.launch'],
    settings: ['voice', 'computer'],
    runtime: ['desktop'],
  },
  principle: '意图直接到电脑控制，不经过项目管理。',
  logic: '输入 → 识别 → 意图 → 方案 → 执行；失败则重试，再失败降级。',
  tech: 'sherpa ASR + 电脑操作技能 + SMTC 校验。',
  analysis: '入口已齐，连续指令免唤醒，失败有闭环。',
  tags: ['场景·娱乐', '能力·语音,控制', '入口·语音,打字', '状态·核心'],
  provenance: 'manifest+probe',
  source: 'preview',
  probe: { passed: 6, total: 6 },
  version: 'v2.4.1',
}

const playCard: HubCard = {
  ...musicCard,
  stable_key: 'feature.dialog.music.play',
  name: '放歌',
  name_en: 'Play music',
  module: 'companion',
  summary: '按歌名或歌手播放本地或在线曲目。',
  description: '从对话或办公入口唤起播放器，解析曲目后走播放链。本地库优先，没有再走在线源。',
  tags: ['状态·核心', 'kind:媒体'],
  methods: [
    { type: 'voice', entry: '说「放周杰伦的晴天」', continuous: '可接着暂停 / 下一曲' },
    { type: 'ui', entry: '对话 · 放歌', continuous: '@媒体技能' },
  ],
}

const openFileCard: HubCard = {
  ...musicCard,
  stable_key: 'feature.dialog.file.open',
  name: '打开文件',
  name_en: 'Open file',
  domain: 'dialog',
  module: 'companion',
  summary: '从工作台打开本地文件并进入预览。',
  description: '选择或拖入文件后，走类型识别和预览链。',
  scaffold: { pages: ['home', 'office'], bridge: ['desktop.files.readChunk'], settings: [] },
  methods: [{ type: 'ui', entry: '主页 / 办公 · 打开文件', continuous: '预览后可分享' }, { type: 'bridge', entry: 'office.file.open' }],
  tags: ['状态·核心', 'kind:办公'],
  chain: {
    steps: [
      { index: 1, name: '选文件', detail: 'picker', description: '系统选择器或拖放。' },
      { index: 2, name: '识别类型', detail: 'detect', description: '按扩展名和内容嗅探。' },
      { index: 3, name: '打开预览', detail: 'preview', description: '进入对应阅读器。' },
    ],
    branches: [
      { type: 'success', from_step: 3, name: '成功', description: '预览打开' },
      { type: 'failure', from_step: 3, name: '失败', description: '类型不支持；权限拒绝' },
    ],
  },
}

const landscapeCard: HubCard = {
  ...playCard,
  stable_key: 'landscape.competitor.cursor',
  name: '竞品 · Cursor',
  name_en: 'Competitor · Cursor',
  domain: 'foundation',
  module: 'landscape',
  summary: '对照 IDE 内嵌代理的产品形态。',
  description: '2026-09-21 观察（来源：Cursor 公开桌面端说明）。本地工作区 + 云端模型；规则/MCP/skills 强，媒体核验与产品本体中枢弱。图景不计入健康分。',
  tags: ['状态·观察', 'kind:竞品'],
  methods: [{ type: 'note', entry: '图景卡' }],
  chain: { steps: [], branches: [] },
}

const desktopAssistantsCard: HubCard = {
  ...landscapeCard,
  stable_key: 'landscape.competitor.desktop-assistants',
  name: '竞品对照：桌面助手',
  name_en: 'Competitive desktop assistants',
  summary: '对照 Copilot / ChatGPT Desktop / Claude Desktop 的四维可核验行为。',
  description: '2026-09-21 初版对照。只比较本机优先、媒体核验、技能MCP、知识自描述。禁止无出处排名。',
}

const frontierSmtcCard: HubCard = {
  ...landscapeCard,
  stable_key: 'landscape.frontier.smtc-verification',
  name: '前沿：播放核验',
  name_en: 'Frontier SMTC verification',
  summary: '以 SMTC/owned runtime 为播放真相，而不是键发了就算成功。',
  tags: ['状态·观察', 'kind:前沿'],
}

const frontierHealCard: HubCard = {
  ...landscapeCard,
  stable_key: 'landscape.frontier.self-heal-agents',
  name: '前沿：自净化与知识图谱',
  name_en: 'Frontier self-heal graph',
  summary: '产品用本体+图谱描述自己，再用诊断报告驱动内部模型/技能修复。',
  tags: ['状态·观察', 'kind:前沿'],
}

const extraNodes: HubNode[] = [
  { id: landscapeCard.stable_key, stable_key: landscapeCard.stable_key, type: 'Scenario', name: landscapeCard.name, domain: 'foundation', module: 'landscape' },
  { id: desktopAssistantsCard.stable_key, stable_key: desktopAssistantsCard.stable_key, type: 'Scenario', name: desktopAssistantsCard.name, domain: 'foundation', module: 'landscape' },
  { id: frontierSmtcCard.stable_key, stable_key: frontierSmtcCard.stable_key, type: 'Scenario', name: frontierSmtcCard.name, domain: 'foundation', module: 'landscape' },
  { id: frontierHealCard.stable_key, stable_key: frontierHealCard.stable_key, type: 'Scenario', name: frontierHealCard.name, domain: 'foundation', module: 'landscape' },
  { id: 'expert.companion', stable_key: 'expert.companion', type: 'Expert', name: '月伴手册专家', domain: 'dialog', version: 'v3' },
  { id: 'expert.office', stable_key: 'expert.office', type: 'Expert', name: '办公助手', domain: 'office' },
  { id: 'skill.computer-ops', stable_key: 'skillpack.computer-ops', type: 'Skill', name: '技能注册表', domain: 'assets' },
  { id: 'plugin.ocr', stable_key: 'plugin.ocr-router', type: 'Plugin', name: 'ocr.router', domain: 'assets', provides: 'OCR 模型路由', version: 'v1.0', state: 'degraded' },
  { id: 'cap.asr', stable_key: 'capability.stt.asr', type: 'Capability', name: 'stt.asr', domain: 'dialog' },
  { id: 'cap.tts', stable_key: 'capability.tts.voice', type: 'Capability', name: 'tts.voice', domain: 'dialog' },
]

const extraEdges = [
  { from: 'domain.foundation', to: landscapeCard.stable_key, rel: 'contains' as const },
  { from: musicCard.stable_key, to: 'cap.asr', rel: 'uses' as const },
  { from: musicCard.stable_key, to: 'cap.tts', rel: 'uses' as const },
]

const { nodes, edges } = buildCatalogGraph(extraNodes, extraEdges)
const cards = buildCatalogCards([
  musicCard, playCard, openFileCard, landscapeCard, desktopAssistantsCard, frontierSmtcCard, frontierHealCard,
])
const changes = catalogChanges()
const overview = catalogOverview(nodes, changes)

const findings: HubFinding[] = [
  {
    severity: 'error',
    error_code: 'PH-004',
    stable_key: 'feature.dialog.music.play',
    title: '属性孤儿引用：技能包引用未找到',
    evidence: '[usesAssetsResolve] 2026-09-19 09:12:31\nprobe    → feature.dialog.music-player → attributes.skills[0]\nexpected : skillpack.computer-ops\nregistry : NOT FOUND\ninstalled: computer-ops-v2 / file-ops / web-search\nsignal   : PH-004（构建期校验 孤儿引用）',
    root_cause: 'v2.4.1 升级将技能包 computer-ops 更名为 computer-ops-v2 并迁移安装卡目录，功能 manifest 中 attributes.skills 引用未同步更新，构建期校验判定为孤儿引用。',
    fix: '① edit_manifest manifest.json：将 feature.dialog.music-player.attributes.skills 中 skillpack.computer-ops 替换为 skillpack.computer-ops-v2\n② rebuild 快照重建\n③ check_service computer-ops-v2',
    verify: '重连后 usesAssetsResolve 探针 6/6 ✓；本条自动转 fixed。',
    status: 'open',
    apply_prompt: '只改目录标签，把 computer-ops 指到 computer-ops-v2，不要改写 Go/TS。',
  },
  {
    severity: 'warn',
    error_code: 'PH-003',
    stable_key: 'capability.tts.voice',
    title: 'TTS 探针超时：capability.tts.voice',
    evidence: 'probe tts.voice timeout 8s',
    root_cause: '语音通道模块未就绪。',
    fix: '检查 tts 服务并重跑探针。',
    verify: 'capability.tts.voice 探针通过。',
    status: 'open',
  },
  {
    severity: 'warn',
    error_code: 'PH-003',
    stable_key: 'feature.foundation.settings.channels',
    title: '设置缺映射：「消息通道」未挂模块',
    evidence: 'settingsNav 18 分类 · 模块映射 · 探针 settingsCoverage',
    root_cause: '消息通道分类已注册，模块未挂接。',
    fix: 'check_service settings.channels：补模块映射后重跑覆盖探针。',
    verify: 'settingsCoverage 18/18。',
    status: 'open',
  },
  {
    severity: 'info',
    error_code: 'PH-004',
    stable_key: 'feature.office.page.office',
    title: '模块零功能卡：Office 交付',
    evidence: 'module.office.office-delivery 规则 module-no-feature-card',
    root_cause: '模块已注册，功能卡尚未编排。',
    fix: '补功能卡或从活源并入。',
    verify: '模块展开不再空。',
    status: 'open',
  },
  {
    severity: 'info',
    error_code: 'PH_000',
    stable_key: 'product.lunitide',
    title: '本轮未发现阻断问题',
    evidence: '健康分 94。',
    root_cause: '—',
    fix: '继续观察图景槽位。',
    verify: '图景不计入健康分。',
    status: 'wont_fix',
  },
]

const reportHtml = `
  <h1>月汐产品说明书（预览）</h1>
  <p>这是样式预览用的说明书正文，不是仓库里的静态 PRD。</p>
  <h2>打开音乐播放软件</h2>
  <p>语音或打字打开本机播放器，失败则重试，再失败降级推荐。</p>
`

const reportMarkdown = '# 月汐产品说明书（预览）\n\n样式预览用的 Markdown。'

const previewAuth = { unlocked: false, findings: findings.map(item => ({ ...item })) }

export function setProductHubPreviewUnlocked(unlocked: boolean): void {
  previewAuth.unlocked = unlocked
}

export function createProductHubPreviewBridge(): ProductHubBridge {
  const requireAuth = (token?: string) => {
    if (token !== TOKEN || !previewAuth.unlocked) throw new Error('未解锁')
  }
  return {
    authStatus: async ({ sessionToken }) => ({
      username: USER,
      unlocked: sessionToken === TOKEN && previewAuth.unlocked,
      visible: true,
    }),
    unlock: async ({ username, password }) => {
      if (username !== USER || password !== PASS) throw new Error('用户名或密码不正确')
      previewAuth.unlocked = true
      return { sessionToken: TOKEN, username: USER, unlocked: true }
    },
    changePassword: async ({ sessionToken, currentPassword, newPassword }) => {
      requireAuth(sessionToken)
      if (currentPassword !== PASS || newPassword.length < 8) throw new Error('改密失败')
      return { ok: true }
    },
    status: async ({ sessionToken }) => {
      requireAuth(sessionToken)
      return overview
    },
    overview: async ({ sessionToken }) => {
      requireAuth(sessionToken)
      return overview
    },
    refresh: async ({ sessionToken }) => {
      requireAuth(sessionToken)
      return {
        editionId: overview.editionId,
        generatedAt: overview.generatedAt,
        cardCount: overview.cardCount,
        healthScore: overview.healthScore,
        added: overview.added,
        updated: overview.updated,
        removed: overview.removed,
        reportMarkdown,
        reportHtml,
      }
    },
    featureCard: async ({ sessionToken, stableKey }) => {
      requireAuth(sessionToken)
      const card = cards.get(stableKey)
      if (card) return { card }
      const node = nodes.find(item => item.stable_key === stableKey)
      if (!node) throw new Error('没有这张卡')
      return { card: { ...playCard, stable_key: node.stable_key, name: node.name, name_en: node.name, domain: node.domain || 'dialog', module: node.domain || 'dialog' } }
    },
    graph: async ({ sessionToken }) => {
      requireAuth(sessionToken)
      return { nodes, edges }
    },
    node: async ({ sessionToken, id }) => {
      requireAuth(sessionToken)
      const node = nodes.find(item => item.id === id)
      if (!node) throw new Error('没有这个节点')
      return node
    },
    changelog: async ({ sessionToken }) => {
      requireAuth(sessionToken)
      return { changes }
    },
    diagnostics: async ({ sessionToken }) => {
      requireAuth(sessionToken)
      return { findings: previewAuth.findings, reportMarkdown, reportHtml }
    },
    apply: async ({ sessionToken, errorCode }) => {
      requireAuth(sessionToken)
      const item = previewAuth.findings.find(finding => !errorCode || finding.error_code === errorCode)
      if (item) {
        item.status = 'applied'
        item.plan = '预览：只改目录标签，不改写 Go/TS。'
        item.skill_name = 'debugger'
        item.skill_output = '已写任务书：补齐技能包引用。'
      }
      return {
        ok: true,
        applied: true,
        count: 1,
        status: 'applied',
        plan: item?.plan ?? '本地方案',
        skillName: item?.skill_name,
        skillOutput: item?.skill_output,
        errorCode: item?.error_code,
        stableKey: item?.stable_key,
      }
    },
    tags: async ({ sessionToken }) => {
      requireAuth(sessionToken)
      return { tags: overview.tags ?? [] }
    },
    tagSet: async ({ sessionToken }) => {
      requireAuth(sessionToken)
      return { ok: true }
    },
    exportDoc: async ({ sessionToken, format }) => {
      requireAuth(sessionToken)
      return format === 'html'
        ? { content: reportHtml, mime: 'text/html' }
        : { content: reportMarkdown, mime: 'text/markdown' }
    },
  }
}

export function unlockProductHubPreview(): string {
  return TOKEN
}
