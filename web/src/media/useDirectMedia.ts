import { useEffect, useRef, useState } from 'react'

// The element on screen is the player. Volume, pause, and seek stay on it.
export function useDirectMedia(active: boolean, src: string | null | undefined, rate?: number) {
  const ref = useRef<HTMLMediaElement>(null)
  const boxRef = useRef<HTMLDivElement>(null)
  const volumeRef = useRef(80)
  const playingRef = useRef(false)
  const [playing, setPlaying] = useState(false)
  const [positionMs, setPositionMs] = useState(0)
  const [durationMs, setDurationMs] = useState(0)
  const [volume, setVolume] = useState(80)
  const [heard, setHeard] = useState(true)
  const [full, setFull] = useState(false)
  volumeRef.current = volume

  useEffect(() => {
    const sync = () => setFull(document.fullscreenElement != null && document.fullscreenElement === boxRef.current)
    document.addEventListener('fullscreenchange', sync)
    return () => document.removeEventListener('fullscreenchange', sync)
  }, [])

  useEffect(() => {
    const node = ref.current
    if (!active || !src || !node) return
    node.volume = volumeRef.current / 100
    node.muted = false
    if (rate) node.playbackRate = rate
    const mark = () => {
      setPositionMs(Number.isFinite(node.currentTime) ? Math.round(node.currentTime * 1000) : 0)
      setDurationMs(Number.isFinite(node.duration) ? Math.round(node.duration * 1000) : 0)
    }
    const onPlaying = () => {
      playingRef.current = true
      setPlaying(true)
      mark()
    }
    const onPause = () => {
      playingRef.current = false
      setPlaying(false)
      mark()
    }
    node.addEventListener('playing', onPlaying)
    node.addEventListener('pause', onPause)
    node.addEventListener('timeupdate', mark)
    node.addEventListener('loadedmetadata', mark)
    const started = () => {
      playingRef.current = true
      setPlaying(true)
    }
    try {
      const pending = node.play()
      void Promise.resolve(pending).then(started).catch(() => {
        node.muted = true
        setHeard(false)
        void Promise.resolve(node.play()).then(started).catch(() => {})
      })
    } catch {
      setHeard(false)
    }
    return () => {
      node.removeEventListener('playing', onPlaying)
      node.removeEventListener('pause', onPause)
      node.removeEventListener('timeupdate', mark)
      node.removeEventListener('loadedmetadata', mark)
    }
  }, [active, rate, src])

  const setLevel = (next: number) => {
    const level = Math.min(100, Math.max(0, Math.round(next)))
    setVolume(level)
    setHeard(level > 0)
    const node = ref.current
    if (!node) return
    node.volume = level / 100
    node.muted = level <= 0
  }

  const toggle = () => {
    const node = ref.current
    if (!node) return
    if (!playingRef.current) {
      node.volume = volumeRef.current / 100
      node.muted = volumeRef.current <= 0
      setHeard(volumeRef.current > 0)
      void Promise.resolve(node.play()).then(() => {
        playingRef.current = true
        setPlaying(true)
      }).catch(() => {})
      return
    }
    node.pause()
    playingRef.current = false
    setPlaying(false)
  }

  const seek = (ms: number) => {
    const node = ref.current
    if (!node) return
    node.currentTime = ms / 1000
    setPositionMs(ms)
  }

  const toggleFull = () => {
    const node = boxRef.current
    if (!node) return
    if (document.fullscreenElement === node) {
      void document.exitFullscreen?.()
      return
    }
    const pending = node.requestFullscreen?.()
    void pending?.catch(() => {})
  }

  return { ref, boxRef, playing, positionMs, durationMs, volume, heard, full, setLevel, toggle, seek, toggleFull }
}
