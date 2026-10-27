import { describe, expect, it, vi } from 'vitest'
import { fileFromTemplate, goalWithAssetDraft, officeTemplates, readTemplateContent, type TemplateContentReader } from './officeAssetTemplates'
import type { OfficeTemplate } from './officeAssetTemplates'

const item = (patch: Partial<OfficeTemplate>): OfficeTemplate => ({
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAT',
  templateCode: 'TPL00001',
  name: '季度汇报',
  templateType: 'ppt',
  fileName: '季度汇报.pptx',
  status: 'enabled',
  createdAt: '2026-09-24T00:00:00Z',
  updatedAt: '2026-09-24T00:00:00Z',
  version: 1,
  ...patch,
})

describe('office asset templates', () => {
  it('keeps only enabled ppt word and excel templates', () => {
    const got = officeTemplates([
      item({}),
      item({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAU', templateType: 'word', fileName: '纪要.docx', name: '纪要' }),
      item({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', templateType: 'excel', fileName: 'ledger.xlsx', status: 'draft' }),
      item({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAY', templateType: 'ppt', fileName: 'old.ppt' }),
      item({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAW', templateType: 'document' }),
      item({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAX', templateType: 'scaffold', status: 'enabled' }),
    ])
    expect(got.map(row => row.name)).toEqual(['季度汇报', '纪要'])
  })

  it('rebuilds the uploaded file and names it in the office goal', () => {
    const file = fileFromTemplate('deck.pptx', btoa('pptx-bytes'))
    expect(file.name).toBe('deck.pptx')
    expect(file.size).toBe('pptx-bytes'.length)
    expect(goalWithAssetDraft('做一季度汇报', '季度汇报')).toContain('底稿使用资产模版「季度汇报」')
    expect(goalWithAssetDraft('做一季度汇报', '')).toBe('做一季度汇报')
  })
})

const chunkedBridge = (
  content: Uint8Array,
  overrides?: {
    slice?: (offset: number, limit: number) => { data: Uint8Array; nextOffset: number; eof: boolean }
    fileName?: string
  },
): TemplateContentReader => {
  const b64 = (bytes: Uint8Array): string => {
    let binary = ''
    for (let i = 0; i < bytes.length; i += 1) binary += String.fromCharCode(bytes[i])
    return btoa(binary)
  }
  const total = content.length
  const step = 4
  const defaultSlice = (offset: number): { data: Uint8Array; nextOffset: number; eof: boolean } => {
    const end = Math.min(offset + step, total)
    return { data: content.subarray(offset, end), nextOffset: end, eof: end >= total }
  }
  return {
    open: vi.fn().mockResolvedValue({ opened: false, fileName: 'deck.pptx', size: total, chunked: true }),
    readChunk: vi.fn(async (payload: { offset: number; limit?: number }) => {
      const out = overrides?.slice
        ? overrides.slice(payload.offset, payload.limit ?? 0)
        : defaultSlice(payload.offset)
      return {
        fileName: overrides?.fileName ?? 'deck.pptx',
        contentBase64: b64(out.data),
        nextOffset: out.nextOffset,
        total,
        eof: out.eof,
      }
    }),
  }
}

describe('readTemplateContent', () => {
  it('returns inline content without chunking when the template fits one frame', async () => {
    const readChunk = vi.fn()
    const bridge: TemplateContentReader = {
      open: vi.fn().mockResolvedValue({ opened: false, fileName: 'deck.pptx', contentBase64: btoa('pptx-bytes'), size: 10 }),
      readChunk,
    }
    const result = await readTemplateContent(bridge, '01ARZ3NDEKTSV4RRFFQ69G5FAV')
    expect(result.fileName).toBe('deck.pptx')
    expect(result.contentBase64).toBe(btoa('pptx-bytes'))
    expect(readChunk).not.toHaveBeenCalled()
  })

  it('assembles chunked templates byte-exactly across multiple slices', async () => {
    const text = '0123456789abcdef'
    const content = new TextEncoder().encode(text)
    const bridge = chunkedBridge(content)
    const result = await readTemplateContent(bridge, '01ARZ3NDEKTSV4RRFFQ69G5FAV')
    expect(result.fileName).toBe('deck.pptx')
    expect(atob(result.contentBase64)).toBe(text)
    expect(bridge.readChunk).toHaveBeenCalledTimes(4)
    expect(bridge.readChunk).toHaveBeenNthCalledWith(1, { id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', offset: 0, limit: 4 * 1024 * 1024 })
  })

  it('rejects templates above the engine limit before downloading', async () => {
    const bridge = chunkedBridge(new Uint8Array(0))
    ;(bridge.open as ReturnType<typeof vi.fn>).mockResolvedValue({ opened: false, fileName: 'deck.pptx', size: 16, chunked: true })
    await expect(readTemplateContent(bridge, '01ARZ3NDEKTSV4RRFFQ69G5FAV', 8)).rejects.toThrow('超过办公引擎导入上限')
    expect(bridge.readChunk).not.toHaveBeenCalled()
  })

  it('rejects inconsistent chunk progress', async () => {
    const bridge = chunkedBridge(new TextEncoder().encode('0123456789'), {
      slice: (offset: number, limit: number) => ({ data: new TextEncoder().encode('0123'), nextOffset: offset + limit, eof: false }),
    })
    await expect(readTemplateContent(bridge, '01ARZ3NDEKTSV4RRFFQ69G5FAV')).rejects.toThrow('资产模版读取进度不一致')
  })

  it('rejects a file name swap mid-stream', async () => {
    const bridge = chunkedBridge(new TextEncoder().encode('0123456789'), { fileName: 'other.pptx' })
    await expect(readTemplateContent(bridge, '01ARZ3NDEKTSV4RRFFQ69G5FAV')).rejects.toThrow('资产模版读取期间内容发生变化')
  })

  it('rejects chunked responses without any content', async () => {
    const bridge: TemplateContentReader = {
      open: vi.fn().mockResolvedValue({ opened: false, fileName: 'deck.pptx', size: 4, chunked: false }),
      readChunk: vi.fn(),
    }
    await expect(readTemplateContent(bridge, '01ARZ3NDEKTSV4RRFFQ69G5FAV')).rejects.toThrow('资产模版内容为空或不可用')
  })
})
