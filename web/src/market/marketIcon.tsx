import React from 'react'

const RULES: [RegExp, string][] = [
  [/excel|xlsx|表格/i, 'excel'],
  [/pptx|幻灯|演示文稿|ppt/i, 'ppt'],
  [/docx|word|文档撰写|报告编写|报告写作|文档产出/i, 'word'],
  [/playwright/i, 'edge'],
  [/chrome|浏览器/i, 'chrome'],
  [/python/i, 'python'],
  [/powershell|pwsh/i, 'powershell'],
  [/duckduckgo/i, 'duck'],
  [/filesystem|文件系统|工作区/i, 'folder'],
  [/fetch|抓取/i, 'fetch'],
  [/deepseek/i, 'deepseek'],
  [/网页搜索|find-skill|搜索|调研/i, 'search'],
  [/git/i, 'git'],
  [/figma|ui专家|ui 专家|审美/i, 'figma'],
  [/产品经理|产品规划|pm-skill|brainstorm/i, 'product'],
  [/测试|test/i, 'test'],
  [/安全|shield/i, 'shield'],
  [/记忆|memory/i, 'brain'],
  [/语音合成|tts/i, 'speaker'],
  [/语音识别|stt/i, 'mic'],
  [/定时|cron|\btime\b|本地任务/i, 'clock'],
  [/剪贴板/i, 'clipboard'],
  [/通知/i, 'bell'],
  [/数据库/i, 'database'],
  [/架构|结构规范|blueprint/i, 'blueprint'],
  [/硬件|cpu/i, 'cpu'],
  [/机务|适航|plane/i, 'plane'],
  [/航材|components/i, 'components'],
  [/维修计划|会议纪要|calendar/i, 'calendar'],
  [/化工|wrench/i, 'wrench'],
  [/小说|context7|book/i, 'book'],
  [/开发|规范|代码|bash|cmd/i, 'code'],
  [/翻译/i, 'translate'],
  [/视频|影片/i, 'video'],
  [/pdf/i, 'pdf'],
  [/youtube/i, 'youtube'],
  [/markdown|markitdown/i, 'markdown'],
  [/计算|calculator/i, 'calc'],
  [/react/i, 'react'],
  [/golang|\bgo\b/i, 'go'],
  [/终端|terminal/i, 'terminal'],
  [/思考|thinking|sequential/i, 'thinking'],
  [/设计/i, 'design'],
  [/会话|同事|users/i, 'users'],
  [/新闻/i, 'news'],
  [/skill-creator|技能创造|agent 循环|spark/i, 'spark2'],
  [/技能|everything|puzzle|能力包/i, 'puzzle'],
  [/办公交付|brief/i, 'brief'],
  [/图表|chart|antv/i, 'chart'],
]

export function marketIconSrc(label: string): string | null {
  const hit = RULES.find(([pattern]) => pattern.test(label))
  return hit ? `/market/${hit[1]}.png` : null
}

export function MarketMark({ label, fallback }: { label: string; fallback: React.ReactNode }): React.JSX.Element {
  const src = marketIconSrc(label)
  if (!src) return <>{fallback}</>
  return <img className="market-mark" src={src} alt="" />
}
