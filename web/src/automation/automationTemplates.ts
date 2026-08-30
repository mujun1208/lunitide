export const AUTOMATION_CREATE_PROMPT =
  '我需要创建一个自动化任务。任务内容是：\n执行时间是：'

export type AutomationTemplate = {
  id: string
  title: string
  description: string
  cron: string
  prompt: string
}

export const AUTOMATION_TEMPLATES: AutomationTemplate[] = [
  {
    id: 'web-form-scrape',
    title: '网页填表与抓取',
    description: '在托管浏览器里 snapshot 后填写或抽取字段，再写成表格。',
    cron: '0 9 * * 1-5',
    prompt:
      '用 browser.act 完成本机网页任务（OpenClaw 闭环）：\n1. navigate 到用户指定的公开页（没有 URL 就停下来问）\n2. 使用返回的 snapshot ref 点击/填写，不要猜 CSS\n3. 登录墙、验证码、2FA、文件选择交给用户，不要猜\n4. 抽到的字段调用 structured.output（template=form 或 kv），再 excel.gen 写入工作区\n禁止远程电脑或入站 webhook。',
  },
  {
    id: 'desktop-data-flow',
    title: '本机跨应用流转',
    description: '从本机软件读数，写入表格或文档，可选推送到飞书/企微。',
    cron: '30 17 * * 1-5',
    prompt:
      '在这台 PC 上打通软件数据，不要远程控制：\n1. computer.act action=list 找到源应用，action=focus 后再 action=observe\n2. 读数优先 action=set_value 的目标框或 clipboard，不要截图盲点\n3. 用 excel.gen 或 docx.gen 写入工作区；需要 JSON 时 structured.output\n4. 若任务配置了出站 webhook，在摘要末尾说明即可（引擎会推送）\n禁止 UAC 与局域网。遇到打开/保存文件对话框请用户去点，不要代点。不要调用 cc.* 工具名。',
  },
  {
    id: 'multi-step-orchestration',
    title: '多步编排简报',
    description: '拆步搜索、核对、结构化输出，适合定时重复的调研。',
    cron: '0 8 * * 1-5',
    prompt:
      '把本任务当成可验证的多步编排，不要中途停下等确认：\n1. todo.write 列出 3–6 步\n2. 调研必须 web.search，不要去 fetch 搜索引擎首页\n3. 需要打开页面时 browser.act（先 snapshot 再 act）\n4. 最终用 structured.output template=kv 给出结论，再按重要性写简报\n同一指令只执行一次。',
  },
  {
    id: 'daily-ai-news',
    title: '每日 AI 新闻简报',
    description: '每天早上汇总 AI 行业热点新闻与趋势。',
    cron: '0 7 * * *',
    prompt:
      '搜索今日 AI 行业的热点新闻，覆盖以下方面：\n1. 重要产品发布或功能更新\n2. 融资事件与行业并购\n3. 技术突破或重要论文发布\n4. 行业标准与规范动态\n输出要求：按重要性排序，列出 5-8 条新闻。',
  },
  {
    id: 'brand-sentiment',
    title: '品牌公开新闻摘要',
    description: '用 web.search 汇总公开网页上的品牌报道，不是社交媒体爬虫。',
    cron: '0 9 * * 1',
    prompt:
      '用 web.search 检索用户指定品牌本周的公开新闻与报道（不要假装爬了微博/抖音接口）。\n1. 至少 5 条带来源 URL\n2. 分正面/中性/风险，无法核实的标「未核实」\n3. structured.output template=kv 后 docx.gen 或 excel.gen\n禁止入站 webhook，禁止说已监控全网舆情。',
  },
  {
    id: 'competitor-track',
    title: '每周竞品公开动态',
    description: '检索竞品官网与公开新闻，形成周报。',
    cron: '0 10 * * 1',
    prompt:
      '对用户给出的竞品名单：web.search 版本发布、定价页、公开新闻。每条附 URL。无法打开的页面不要编造。最后 excel.gen 一张对照表。',
  },
  {
    id: 'stock-monitor',
    title: '股价公开报道摘要',
    description: '检索公开财经报道与大致涨跌描述，不是行情源。',
    cron: '30 15 * * 1-5',
    prompt:
      '用 web.search 查用户关注标的的当日公开报道。写清：这不是交易所行情，数字以报道为准并附链接。没有搜到就说没搜到。docx.gen 一页摘要。',
  },
  {
    id: 'security-scan',
    title: '依赖与提交只读快审',
    description: '用白名单 git/go 与 workspace 只读检查，不是漏洞扫描器。',
    cron: '0 2 * * 0',
    prompt:
      '在工作区做只读安全快审：command.run 仅白名单 git status/diff/log 与 go vet/test；workspace.search 密钥形态。列出严重/建议，引用 path:line。禁止声称已扫完全部 CVE，禁止改生产配置。',
  },
  {
    id: 'commit-bug-scan',
    title: '只读审查近况提交',
    description: '用 git log/diff 看最近提交风险，不是缺陷扫描产品。',
    cron: '0 18 * * 1-5',
    prompt:
      'command.run 白名单 git --no-pager log / diff，归纳最近提交的回归风险。没有仓库或命令失败时据实说明。不要假装跑了静态分析平台。',
  },
  {
    id: 'test-coverage',
    title: '建议补测清单',
    description: '读现有测试风格，列出应补的用例，能跑再 command.run。',
    cron: '0 20 * * 3',
    prompt:
      'workspace.search 最近改动与测试文件。列出应补的回归用例。若用户已给测试命令且在白名单内，command.run 验证；否则只出清单，不要假装覆盖率数字。',
  },
  {
    id: 'daily-changelog',
    title: '每日变更摘要',
    description: '汇总本机仓库只读 git 记录为团队日报。',
    cron: '30 17 * * 1-5',
    prompt:
      'command.run 白名单 git log/diff。没有提交就写「今日无提交」。docx.gen 日报。不要编造合并请求。',
  },
]

export function cronToHuman(cron: string): string {
  if (cron.startsWith('at:')) {
    const stamp = cron.slice(3)
    const date = new Date(stamp)
    if (!Number.isNaN(date.getTime())) {
      return `一次 ${date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })}`
    }
    return '一次性'
  }
  const parts = cron.trim().split(/\s+/)
  if (parts.length < 2) return cron
  const [min, hour, , , dow] = parts
  const time = `${hour.padStart(2, '0')}:${min.padStart(2, '0')}`
  if (dow === '*') return `每天 ${time}`
  if (dow === '1-5') return `工作日 ${time}`
  if (dow === '1') return `每周一 ${time}`
  if (dow === '0') return `每周日 ${time}`
  return cron
}

export type RecurringFreq = 'daily' | 'weekdays' | 'weekly'
export type ScheduleFreq = RecurringFreq | 'once' | 'in20'

export function scheduleToCron(freq: RecurringFreq, time: string): string {
  const [hour = '9', min = '0'] = time.split(':')
  const h = String(Number(hour) || 9)
  const m = String(Number(min) || 0)
  if (freq === 'weekdays') return `${m} ${h} * * 1-5`
  if (freq === 'weekly') return `${m} ${h} * * 1`
  return `${m} ${h} * * *`
}

export function delayAtCron(minutes: number, now = new Date()): string {
  return `at:${new Date(now.getTime() + minutes * 60_000).toISOString()}`
}

export function datetimeLocalToAtCron(value: string): string {
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return delayAtCron(20)
  return `at:${d.toISOString()}`
}

export function atCronToDatetimeLocal(cron: string): string {
  if (!cron.startsWith('at:')) return ''
  const d = new Date(cron.slice(3))
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}
