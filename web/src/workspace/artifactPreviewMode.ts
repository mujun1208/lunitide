export type ArtifactViewMode = 'html' | 'image' | 'sheet' | 'markdown' | 'code' | 'paper' | 'pdf' | 'text'

const CODE_EXT = /\.(json|ts|tsx|js|jsx|go|py|css|sql|xml|yml|yaml|csv|log)$/i

export function artifactViewMode(kind: string, path: string): ArtifactViewMode {
  if (kind === 'html') return 'html'
  if (kind === 'image') return 'image'
  if (kind === 'xlsx') return 'sheet'
  if (kind === 'pdf') return 'pdf'
  if (kind === 'docx' || kind === 'pptx') return 'paper'
  const base = path.split(/[/\\]/).pop()?.toLowerCase() ?? ''
  if (base.endsWith('.md')) return 'markdown'
  if (CODE_EXT.test(base)) return 'code'
  return 'text'
}

export function artifactLooksLikePdfBytes(content: string): boolean {
  const trimmed = content.trim()
  if (trimmed.length < 16 || trimmed.includes(' ') || trimmed.includes('\n')) return false
  return /^[A-Za-z0-9+/]+=*$/.test(trimmed) && trimmed.startsWith('JVBERi')
}

export function previewKindFromPath(path: string): 'html' | 'xlsx' | 'docx' | 'pptx' | 'image' | 'pdf' | 'text' | 'file' {
  const ext = path.split(/[/\\]/).pop()?.split('.').pop()?.toLowerCase() ?? ''
  if (ext === 'html') return 'html'
  if (ext === 'png' || ext === 'jpg' || ext === 'jpeg' || ext === 'gif') return 'image'
  if (ext === 'xlsx') return 'xlsx'
  if (ext === 'pdf') return 'pdf'
  if (ext === 'docx') return 'docx'
  if (ext === 'pptx') return 'pptx'
  return 'text'
}

export function artifactPreviewIsReady(kind: string, path: string, content: string): boolean {
  const mode = artifactViewMode(kind, path)
  if (mode === 'pdf') return artifactLooksLikePdfBytes(content)
  if (mode === 'image') return /^data:image\/(png|jpeg|gif);base64,[A-Za-z0-9+/=]+$/.test(content)
  return content.length > 0
}
