const suffix: Record<string, string> = {
  'parse function not configured': '未配置正文解析',
  'source digest changed': '源文件在入库后已被修改',
  'source changed during parsing': '源文件在入库后已被修改',
  'content_ref must be an absolute path': '内容路径必须是绝对路径',
  'no non-empty chunks': '没有可检索的正文',
  'empty body': '没有可检索的正文',
  'chunk body exceeds budget': '分块正文超过上限',
  'tombstone:deleted': '知识来源已删除',
}

export function localizeKBFailReason(msg: string): string {
  const text = msg.trim()
  if (!text) return text
  const prefix = '无法抽出正文：'
  const body = text.startsWith(prefix) ? text.slice(prefix.length) : text
  let mapped = suffix[body]
  if (!mapped && body.includes('parse function')) mapped = '未配置正文解析'
  if (!mapped && body.includes('source changed during parsing')) mapped = '源文件在入库后已被修改'
  if (!mapped && body.startsWith('chunk count ') && body.includes('exceeds cap')) mapped = '分块数量超过上限'
  if (!mapped && body.includes('tombstone:deleted')) mapped = '知识来源已删除'
  if (!mapped) mapped = body
  return text.startsWith(prefix) ? prefix + mapped : mapped
}

export function knowledgeUserError(msg: string | undefined, zh: boolean, fallbackZh: string, fallbackEn: string): string {
  const text = (msg ?? '').trim()
  if (!text) return zh ? fallbackZh : fallbackEn
  const localized = localizeKBFailReason(text)
  if (/[\u4e00-\u9fff]/.test(localized)) return localized
  return zh ? fallbackZh : fallbackEn
}
