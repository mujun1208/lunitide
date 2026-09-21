import { useEffect, useState } from 'react'

export function useBoxSize(): [(node: HTMLElement | null) => void, { w: number; h: number }] {
  const [node, setNode] = useState<HTMLElement | null>(null)
  const [size, setSize] = useState({ w: 0, h: 0 })

  useEffect(() => {
    if (!node) return
    const read = () => setSize(curr => {
      const next = { w: node.clientWidth, h: node.clientHeight }
      return curr.w === next.w && curr.h === next.h ? curr : next
    })
    read()
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(read)
    observer.observe(node)
    return () => observer.disconnect()
  }, [node])

  return [setNode, size]
}
