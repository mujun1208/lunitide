import type { TemplateListResult } from '../generated/bridge'

export type OfficeTemplate = TemplateListResult['items'][number]

const OFFICE_EXTENSION: Record<string, string> = { ppt: 'pptx', word: 'docx', excel: 'xlsx' }

export function officeTemplates(items: OfficeTemplate[]): OfficeTemplate[] {
  return items.filter(item => {
    const extension = OFFICE_EXTENSION[item.templateType]
    const name = (item.fileName || '').toLowerCase()
    return item.status === 'enabled' && !!extension && name.endsWith(`.${extension}`)
  })
}

export function fileFromTemplate(fileName: string, contentBase64: string): File {
  const binary = atob(contentBase64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i)
  return new File([bytes], fileName)
}

export function goalWithAssetDraft(goal: string, assetName: string): string {
  const text = goal.trim()
  if (!assetName) return text
  return `${text}\n底稿使用资产模版「${assetName}」。在这份文件上修改并输出，不要另起一份空文档。`
}
