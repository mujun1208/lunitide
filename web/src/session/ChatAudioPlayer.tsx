import React, { useEffect, useRef, useState } from 'react'
import { artifactReviewBridge } from '../bridge/client'

const SPEEDS = [0.75, 1, 1.25, 1.5] as const
const HONEST_CAPTION = '朗读合成，点播放即可听。这不是演唱成曲。'

export function audioMimeFromPath(path: string): string {
  return path.toLowerCase().endsWith('.mp3') ? 'audio/mpeg' : 'audio/wav'
}

export function audioObjectUrlFromPreview(content: string, path: string): string {
  const trimmed = content.trim()
  if (!trimmed) return ''
  try {
    const binary = atob(trimmed)
    const bytes = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
    return URL.createObjectURL(new Blob([bytes], { type: audioMimeFromPath(path) }))
  } catch {
    return ''
  }
}

function fileName(path: string): string {
  return path.split(/[/\\]/).pop() || path
}

export function ChatAudioPlayer({
  sessionId,
  path,
  content,
  onError,
  autoPlay = true,
}: {
  sessionId: string
  path: string
  content?: string
  onError?: (message: string) => void
  autoPlay?: boolean
}): React.JSX.Element {
  const audioRef = useRef<HTMLAudioElement>(null)
  const [url, setUrl] = useState('')
  const [speed, setSpeed] = useState<(typeof SPEEDS)[number]>(1)
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>('loading')
  useEffect(() => {
    let active = true
    let objectUrl = ''
    const apply = (raw: string) => {
      objectUrl = audioObjectUrlFromPreview(raw, path)
      if (!objectUrl) {
        setStatus('error')
        onError?.('无法生成可播放音频')
        return
      }
      setUrl(objectUrl)
      setStatus('ready')
    }
    setStatus('loading')
    if (content) {
      apply(content)
      return () => {
        active = false
        if (objectUrl) URL.revokeObjectURL(objectUrl)
      }
    }
    artifactReviewBridge.preview({ sessionId, path }).then(result => {
      if (!active) return
      apply(result.content)
    }).catch(cause => {
      if (!active) return
      setStatus('error')
      const detail = cause instanceof Error ? cause.message.trim() : ''
      onError?.(/[\u4e00-\u9fff]/.test(detail) ? detail : '无法加载可听音频')
    })
    return () => {
      active = false
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [sessionId, path, content])
  useEffect(() => {
    const node = audioRef.current
    if (!node) return
    node.playbackRate = speed
  }, [speed, url])
  useEffect(() => {
    const node = audioRef.current
    if (!autoPlay || !node || status !== 'ready') return
    try {
      const playing = node.play()
      if (playing && typeof playing.catch === 'function') void playing.catch(() => undefined)
    } catch {
      // Autoplay can be blocked; native controls still work.
    }
  }, [autoPlay, status, url])
  return (
    <article className="chat-audio-player" role="listitem" aria-label="可听语音">
      <header className="chat-audio-head">
        <b>{fileName(path)}</b>
        <small>{HONEST_CAPTION}</small>
      </header>
      {status === 'loading' ? <p role="status">正在准备可听音频…</p> : null}
      {status === 'error' ? <p role="alert">音频还不能直接播放，可在产物里打开原文件。</p> : null}
      {url ? (
        <audio ref={audioRef} controls preload="auto" src={url} aria-label="播放朗读">
          浏览器不支持内嵌播放
        </audio>
      ) : null}
      <div className="chat-audio-speeds" role="group" aria-label="播放速度">
        {SPEEDS.map(value => (
          <button
            key={value}
            type="button"
            aria-pressed={speed === value}
            onClick={() => setSpeed(value)}
          >
            {value === 1 ? '1×' : `${value}×`}
          </button>
        ))}
      </div>
    </article>
  )
}
