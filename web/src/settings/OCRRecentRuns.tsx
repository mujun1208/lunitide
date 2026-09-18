import React, { useEffect, useRef, useState } from 'react'
import { getOCRRunBridge, type OCRRunBridge } from '../bridge/client'
import { MarkdownMessage } from '../session/MarkdownMessage'
import { OCR_ARTIFACT_CHUNK, OCR_ARTIFACT_PREVIEW_LIMIT, ocrUserError } from './ocrCopy'

type RunItem = {
  runId?: string
  createdAt?: string
  status?: string
  artifactId?: string
  previewBytes?: number
}

function asRunItems(value: unknown): RunItem[] {
  if (!Array.isArray(value)) return []
  return value.filter(row => row && typeof row === 'object') as RunItem[]
}

function decodeBase64(value: string): string {
  try {
    const binary = atob(value)
    const bytes = Uint8Array.from(binary, char => char.charCodeAt(0))
    return new TextDecoder('utf-8', { fatal: false }).decode(bytes)
  } catch {
    return ''
  }
}

export function OCRRecentRuns({
  api = getOCRRunBridge(),
}: {
  api?: OCRRunBridge
}): React.JSX.Element {
  const [items, setItems] = useState<RunItem[]>([])
  const [error, setError] = useState('')
  const [preview, setPreview] = useState('')
  const [previewRun, setPreviewRun] = useState('')
  const [offset, setOffset] = useState(0)
  const [totalBytes, setTotalBytes] = useState(0)
  const [eof, setEof] = useState(true)
  const generation = useRef(0)

  useEffect(() => {
    const epoch = ++generation.current
    void api.list({ scopeKind: 'user' }).then(listed => {
      if (epoch !== generation.current) return
      setItems(asRunItems(listed.items))
      setError('')
    }).catch(err => {
      if (epoch !== generation.current) return
      setError(ocrUserError(err, '最近运行载入失败'))
    })
    return () => { generation.current++ }
  }, [api])

  const loadChunk = async (item: RunItem, nextOffset: number) => {
    if (!item.runId || !item.artifactId) return
    try {
      const chunk = await api.readArtifact({
        artifactId: item.artifactId,
        offset: nextOffset,
        limit: Math.min(OCR_ARTIFACT_CHUNK, OCR_ARTIFACT_PREVIEW_LIMIT),
      })
      const text = decodeBase64(chunk.base64)
      setPreviewRun(item.runId)
      setPreview(current => nextOffset === 0 ? text : current + text)
      setOffset(chunk.nextOffset)
      setTotalBytes(Math.min(chunk.totalBytes, OCR_ARTIFACT_PREVIEW_LIMIT))
      setEof(chunk.eof || chunk.nextOffset >= OCR_ARTIFACT_PREVIEW_LIMIT)
      setError('')
    } catch (err) {
      setError(ocrUserError(err, '识别结果读取失败'))
    }
  }

  return (
    <section className="ocr-recent" aria-labelledby="ocr-recent-title">
      <h4 id="ocr-recent-title">最近运行</h4>
      {error ? <p role="alert">{error}</p> : null}
      {items.length === 0 && !error ? <p className="ocr-muted">暂无识别记录</p> : null}
      <ul>
        {items.map(item => (
          <li key={item.runId ?? item.createdAt}>
            <button
              type="button"
              disabled={!item.artifactId}
              onClick={() => void loadChunk(item, 0)}
            >
              {item.createdAt ?? item.runId ?? '运行'} {item.status ?? ''}
            </button>
          </li>
        ))}
      </ul>
      {previewRun ? (
        <div className="ocr-preview">
          <MarkdownMessage text={preview} />
          {!eof && offset < OCR_ARTIFACT_PREVIEW_LIMIT ? (
            <button
              type="button"
              onClick={() => {
                const item = items.find(row => row.runId === previewRun)
                if (item) void loadChunk(item, offset)
              }}
            >
              下一页
            </button>
          ) : null}
          {totalBytes >= OCR_ARTIFACT_PREVIEW_LIMIT ? <p className="ocr-muted">预览已达 1 MiB，请导出完整结果</p> : null}
        </div>
      ) : null}
    </section>
  )
}
