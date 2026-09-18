import React, { useCallback, useEffect, useRef, useState } from 'react'
import { getOCRPackBridge, type OCRPackBridge } from '../bridge/client'
import type { OcrPackGetResult, OcrPackNoticeListResult } from '../generated/bridge'
import { OCR_NOTICE_READ_LIMIT, OCR_PACK_ID, ocrUserError } from './ocrCopy'

const POLL_MS = 750

type PackOperation = {
  operationId?: string
  phase?: string
  terminal?: boolean
  cancelRequested?: boolean
}

function operationInFlight(operation: unknown): boolean {
  if (!operation || typeof operation !== 'object') return false
  const row = operation as PackOperation
  if (row.terminal) return false
  const phase = row.phase ?? ''
  return phase !== 'succeeded' && phase !== 'failed' && phase !== 'cancelled'
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

export function OCRModelPackCard({
  pack,
  api = getOCRPackBridge(),
}: {
  pack: OcrPackGetResult | null
  api?: OCRPackBridge
}): React.JSX.Element {
  const [live, setLive] = useState<OcrPackGetResult | null>(null)
  const [notices, setNotices] = useState<OcrPackNoticeListResult['items']>([])
  const [noticeOpen, setNoticeOpen] = useState(false)
  const [noticeText, setNoticeText] = useState('')
  const [noticeError, setNoticeError] = useState('')
  const [selectedDigest, setSelectedDigest] = useState('')
  const generation = useRef(0)
  const snapshot = live ?? pack

  useEffect(() => {
    setLive(null)
  }, [pack])

  const load = useCallback(async () => {
    const epoch = ++generation.current
    try {
      const next = await api.get({ packId: OCR_PACK_ID })
      if (epoch !== generation.current) return
      setLive(next)
    } catch {
      /* parent owns core routing; pack card keeps last snapshot */
    }
  }, [api])

  useEffect(() => {
    if (!operationInFlight(snapshot?.operation)) return
    const poll = () => {
      if (document.hidden) return
      void load()
    }
    const timer = window.setInterval(poll, POLL_MS)
    const onVis = () => { if (!document.hidden) poll() }
    document.addEventListener('visibilitychange', onVis)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [load, snapshot?.operation])

  const installAllowed = snapshot?.gate.installAllowed === true
  const reason = snapshot?.gate.reasonCode
  const installLabel = installAllowed ? '安装' : '安装（不可用）'

  const openNotices = async () => {
    setNoticeOpen(true)
    setNoticeError('')
    try {
      const listed = await api.noticeList({ packId: OCR_PACK_ID })
      setNotices(listed.items)
    } catch (err) {
      setNoticeError(ocrUserError(err, '许可证列表载入失败'))
    }
  }

  const readNotice = async (manifestDigest: string) => {
    setSelectedDigest(manifestDigest)
    setNoticeError('')
    try {
      const chunk = await api.noticeRead({
        packId: OCR_PACK_ID,
        manifestDigest,
        offset: 0,
        limit: OCR_NOTICE_READ_LIMIT,
      })
      setNoticeText(decodeBase64(chunk.base64))
    } catch (err) {
      setNoticeError(ocrUserError(err, '许可证读取失败'))
    }
  }

  return (
    <section className="ocr-pack-card" aria-labelledby="ocr-pack-title">
      <h3 id="ocr-pack-title">PaddleOCR-VL-1.6</h3>
      <p className="ocr-muted">复杂文档增强。未通过经验证的 Windows 运行包前不会安装，也不会参与自动路由。</p>
      <p>{snapshot?.pack.availability === 'ready' && snapshot.gate.autoRouteAllowed ? '已就绪，可参与复杂文档' : '当前不可用'}</p>
      {reason === 'NO_VERIFIED_RUNTIME_PROFILE' ? <p className="ocr-muted">尚无经验证的 Windows 运行包</p> : null}
      <button
        type="button"
        disabled={!installAllowed}
        onClick={() => {
          /* installAllowed is false until a verified runtime profile exists */
        }}
      >
        {installLabel}
      </button>
      <div className="ocr-license">
        <button type="button" className="ocr-disclose" aria-expanded={noticeOpen} onClick={() => { void (noticeOpen ? setNoticeOpen(false) : openNotices()) }}>
          许可证与声明
        </button>
        {noticeOpen ? (
          <div className="ocr-license-body">
            {noticeError ? <p role="alert">{noticeError}</p> : null}
            {notices.length === 0 && !noticeError ? <p className="ocr-muted">暂无已验证声明</p> : null}
            <ul>
              {notices.map(item => (
                <li key={item.noticeDigest}>
                  <button type="button" onClick={() => void readNotice(item.manifestDigest)}>
                    {item.version} · {item.noticeDigest.slice(0, 12)}
                  </button>
                  {item.uninstalledAt ? <span className="ocr-muted"> 已卸载，仍可离线查看</span> : null}
                </li>
              ))}
            </ul>
            {selectedDigest && noticeText ? <pre className="ocr-notice">{noticeText}</pre> : null}
          </div>
        ) : null}
      </div>
    </section>
  )
}
