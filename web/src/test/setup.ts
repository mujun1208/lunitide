import '@testing-library/jest-dom/vitest'

// jsdom lacks window.matchMedia, which @xterm/xterm requires on terminal.open()
if (typeof window !== 'undefined' && !window.matchMedia) {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }),
  })
}

// jsdom has no canvas backend, so @xterm/xterm's colour probing floods every
// run with "Not implemented: HTMLCanvasElement.prototype.getContext". The
// terminal is never pixel-asserted in tests; a 2D stub keeps real failures
// visible in the output.
if (typeof HTMLCanvasElement !== 'undefined') {
  const context2d = {
    fillStyle: '',
    fillRect: () => {},
    clearRect: () => {},
    getImageData: (_x: number, _y: number, w: number, h: number) => ({ data: new Uint8ClampedArray(Math.max(0, w * h * 4)) }),
    putImageData: () => {},
    createImageData: (w: number, h: number) => ({ data: new Uint8ClampedArray(Math.max(0, w * h * 4)) }),
    setTransform: () => {},
    drawImage: () => {},
    save: () => {},
    restore: () => {},
    beginPath: () => {},
    closePath: () => {},
    stroke: () => {},
    fill: () => {},
    translate: () => {},
    scale: () => {},
    rotate: () => {},
    measureText: (text: string) => ({ width: text.length * 8 }),
    fillText: () => {},
    strokeText: () => {},
    canvas: undefined as unknown,
  }
  HTMLCanvasElement.prototype.getContext = function getContext(this: HTMLCanvasElement, kind: string) {
    if (kind !== '2d') return null
    return { ...context2d, canvas: this } as unknown as CanvasRenderingContext2D
  } as HTMLCanvasElement['getContext']
}
