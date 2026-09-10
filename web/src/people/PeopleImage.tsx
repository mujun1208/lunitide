import { useEffect, useState } from 'react'
import type { PeopleBridge } from '../bridge/client'
import type { PeopleMessageDTO } from '../generated/bridge'
import { Dialog } from '../ui/Dialog'

function peopleImageUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

/** Preview from the engine avoids blocked file:// loads in the desktop webview. */
export function PeopleImage({ message, people, onError }: {
  message: PeopleMessageDTO; people: PeopleBridge; onError: (message: string) => void
}) {
  const [dataUrl, setDataUrl] = useState('')
  const [open, setOpen] = useState(false)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let live = true
    setDataUrl(''); setFailed(false); setOpen(false)
    if (message.offerId) void people.filePreview({ offerId: message.offerId }).then(result => {
      if (!live) return
      if (/^data:image\/(png|jpeg|gif);base64,/.test(result.dataUrl)) setDataUrl(result.dataUrl)
      else setFailed(true)
    }).catch(() => { if (live) setFailed(true) })
    else setFailed(true)
    return () => { live = false }
  }, [people, message.offerId, message.destPath])
  const openOriginal = () => {
    if (message.destPath) void people.fileOpen({ destPath: message.destPath, fileName: message.fileName }).catch(error => {
      const raw = error instanceof Error ? error.message.trim() : ''
      if (/取消/.test(raw)) return
      onError(peopleImageUserError(error, '无法打开文件'))
    })
  }
  const previewFailed = () => { setDataUrl(''); setFailed(true) }
  return <>
    <button type="button" className="people-image-preview" aria-label={`查看图片 ${message.fileName || '图片'}`} onClick={() => setOpen(true)}>
      {dataUrl ? <img src={dataUrl} onError={previewFailed} alt={message.fileName || '图片'} /> : <span>{failed ? '查看图片' : '正在加载图片…'}</span>}
    </button>
    <Dialog open={open} title={message.fileName || '图片预览'} wide onClose={() => setOpen(false)}>
      {dataUrl ? <div className="people-image-viewer"><img src={dataUrl} onError={previewFailed} alt={message.fileName || '图片'} /></div> : <p>{failed ? '此图片可使用本机应用打开查看。' : '正在加载图片…'}</p>}
      <div className="dialog-actions"><button type="button" onClick={openOriginal}>打开原文件</button><button type="button" onClick={() => setOpen(false)}>关闭</button></div>
    </Dialog>
  </>
}
