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
      '在这台 PC 上打通软件数据，不要远程控制：\n1. cc.window_list 找到源应用，cc.window_focus 后再 cc.observe_ui\n2. 读数优先 cc.set_value 的目标框或 cc.clipboard，不要截图盲点\n3. 用 excel.gen 或 docx.gen 写入工作区；需要 JSON 时 structured.output\n4. 若任务配置了出站 webhook，在摘要末尾说明即可（引擎会推送）\n禁止 UAC 与局域网。遇到打开/保存文件对话框请用户去点，不要代点。',
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
    title: '品牌舆情监控回馈',
    description: '爬取社交媒体品牌提及并生成周报摘要。',
    cron: '0 9 * * 1',
    prompt: '监控指定品牌在社交媒体上的提及与情绪变化，输出本周舆情摘要与风险提醒。',
  },
  {
    id: 'competitor-track',
    title: '每周竞品动态追踪',
    description: '追踪竞品更新、反馈与新闻。',
    cron: '0 10 * * 1',
    prompt: '汇总本周主要竞品的版本更新、定价变化、用户反馈与公开新闻，形成竞品周报。',
  },
  {
    id: 'stock-monitor',
    title: '股价监控与预警',
    description: '跟踪指定股票涨跌幅并推送报告。',
    cron: '30 15 * * 1-5',
    prompt: '检查我关注的股票列表，汇总当日涨跌幅、异常波动与相关新闻，给出简要风险提示。',
  },
  {
    id: 'security-scan',
    title: '安全漏洞扫描',
    description: '定期扫描代码仓库中的安全漏洞。',
    cron: '0 2 * * 0',
    prompt: '扫描项目依赖与最近提交，列出高风险安全漏洞与修复建议。',
  },
  {
    id: 'commit-bug-scan',
    title: '扫描提交发现 Bug',
    description: '分析最近代码提交中的高风险缺陷。',
    cron: '0 18 * * 1-5',
    prompt: '审查最近 24 小时的代码提交，找出可能引入回归或逻辑错误的高风险变更。',
  },
  {
    id: 'test-coverage',
    title: '补充测试覆盖',
    description: '识别缺少测试的高风险代码并建议补测。',
    cron: '0 20 * * 3',
    prompt: '找出最近改动但测试覆盖不足的高风险模块，给出应补充的测试用例清单。',
  },
  {
    id: 'daily-changelog',
    title: '每日变更摘要',
    description: '汇总代码仓库更新为团队可读日报。',
    cron: '30 17 * * 1-5',
    prompt: '汇总今日代码仓库的合并请求、重要提交与发布说明，生成团队可读的每日变更摘要。',
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
