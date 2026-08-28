/** Tide Mermaid theme + layout helpers (palette tokens; not a vendored diagram product). */

export type TidePalette = {
  bg: string
  bg2: string
  bg3: string
  ink: string
  muted: string
  tide1: string
  tide2: string
  tide3: string
  glow: string
  dark: boolean
}

export const TIDE_PALETTE_FALLBACK: TidePalette = {
  bg: '#000',
  bg2: '#080b10',
  bg3: '#0d1118',
  ink: '#eaf3ff',
  muted: '#8fa3bf',
  tide1: '#3bd6ff',
  tide2: '#7f5bff',
  tide3: '#2fd6b5',
  glow: '#7fb4ff',
  dark: true,
}

const TOKEN_KEYS = ['bg', 'bg2', 'bg3', 'ink', 'muted', 'tide1', 'tide2', 'tide3', 'glow'] as const

function readToken(el: Element | null, name: string, fallback: string): string {
  if (!el || typeof getComputedStyle !== 'function') return fallback
  const value = getComputedStyle(el).getPropertyValue(name).trim()
  return value || fallback
}

export function readTidePalette(root?: Element | null): TidePalette {
  const el = root ?? (typeof document === 'undefined' ? null : document.documentElement)
  const palette: TidePalette = { ...TIDE_PALETTE_FALLBACK }
  for (const key of TOKEN_KEYS) {
    palette[key] = readToken(el, `--${key}`, TIDE_PALETTE_FALLBACK[key])
  }
  const theme =
    el?.getAttribute?.('data-theme') ??
    (typeof document === 'undefined' ? null : document.documentElement.getAttribute('data-theme'))
  palette.dark = theme !== 'light'
  return palette
}

/** Drop author init directives so stock dark/default cannot wipe the Tide theme. */
export function prepareMermaidSource(source: string): string {
  return source.replace(/%%\{\s*init[\s\S]*?\}%%/gi, '').trim()
}

/** Responsive SVG: mermaid often sets inline height=viewBox px, which leaves a huge empty band. */
export function fitMermaidSvg(svg: SVGSVGElement): void {
  svg.style.maxWidth = '100%'
  svg.style.width = '100%'
  svg.style.height = 'auto'
  svg.removeAttribute('width')
  svg.removeAttribute('height')
  svg.setAttribute('preserveAspectRatio', 'xMidYMid meet')
}

/** Parse mermaid SVG without innerHTML so a bad payload cannot break the chat tree. */
export function mountMermaidSvg(host: HTMLElement, svg: string): SVGSVGElement {
  const parsed = new DOMParser().parseFromString(svg, 'image/svg+xml')
  const root = parsed.documentElement
  if (!root || root.tagName.toLowerCase() !== 'svg' || parsed.querySelector('parsererror')) {
    throw new Error('Mermaid 返回了无法解析的 SVG')
  }
  const imported = document.importNode(root, true) as unknown as SVGSVGElement
  fitMermaidSvg(imported)
  host.replaceChildren(imported)
  return imported
}

export function tideMermaidThemeCSS(): string {
  return [
    '.node rect,.node circle,.node ellipse,.node polygon,.node path{rx:12px;ry:12px}',
    '.cluster rect{rx:16px;ry:16px;stroke-dasharray:7 5}',
    '.edgePath .path,.flowchart-link{stroke-width:1.75px;fill:none}',
    '.marker,.arrowheadPath{stroke-width:0}',
  ].join('')
}

export function tideMermaidConfig(palette: TidePalette) {
  const nodeFill = palette.dark ? palette.bg3 : palette.bg2
  const clusterFill = palette.dark ? palette.bg2 : palette.bg3
  return {
    startOnLoad: false,
    theme: 'base' as const,
    look: 'classic' as const,
    darkMode: palette.dark,
    securityLevel: 'antiscript' as const,
    suppressErrorRendering: true,
    fontFamily: 'Inter, "Noto Sans SC", "Segoe UI", "Microsoft YaHei UI", ui-sans-serif, system-ui, sans-serif',
    fontSize: 16,
    themeCSS: tideMermaidThemeCSS(),
    themeVariables: {
      darkMode: palette.dark,
      background: palette.bg,
      mainBkg: nodeFill,
      primaryColor: nodeFill,
      primaryTextColor: palette.ink,
      primaryBorderColor: palette.tide1,
      secondaryColor: clusterFill,
      secondaryTextColor: palette.ink,
      secondaryBorderColor: palette.tide2,
      tertiaryColor: palette.bg,
      tertiaryTextColor: palette.muted,
      tertiaryBorderColor: palette.tide3,
      lineColor: palette.tide2,
      textColor: palette.ink,
      titleColor: palette.ink,
      nodeTextColor: palette.ink,
      nodeBorder: palette.tide1,
      clusterBkg: clusterFill,
      clusterBorder: palette.muted,
      edgeLabelBackground: palette.bg2,
      defaultLinkColor: palette.tide2,
      fontFamily: 'Inter, "Noto Sans SC", ui-sans-serif, system-ui, sans-serif',
      actorBkg: nodeFill,
      actorBorder: palette.tide1,
      actorTextColor: palette.ink,
      actorLineColor: palette.muted,
      signalColor: palette.tide2,
      signalTextColor: palette.ink,
      labelBoxBkgColor: palette.bg2,
      labelBoxBorderColor: palette.tide1,
      labelTextColor: palette.ink,
      loopTextColor: palette.muted,
      activationBkgColor: palette.bg3,
      activationBorderColor: palette.tide1,
      sequenceNumberColor: palette.bg,
      noteBkgColor: palette.bg3,
      noteTextColor: palette.ink,
      noteBorderColor: palette.glow,
      sectionBkgColor: palette.bg2,
      altSectionBkgColor: palette.bg3,
      gridColor: palette.muted,
      cScale0: palette.tide1,
      cScale1: palette.tide2,
      cScale2: palette.tide3,
      cScale3: palette.glow,
    },
    flowchart: {
      htmlLabels: false,
      curve: 'linear' as const,
      padding: 16,
      diagramPadding: 12,
      nodeSpacing: 42,
      rankSpacing: 56,
      wrappingWidth: 200,
      useMaxWidth: false,
      defaultRenderer: 'dagre-wrapper' as const,
      subGraphTitleMargin: { top: 10, bottom: 8 },
    },
    sequence: {
      useMaxWidth: false,
      diagramMarginX: 12,
      diagramMarginY: 12,
      actorMargin: 28,
      boxMargin: 8,
    },
    class: { useMaxWidth: false },
    state: { useMaxWidth: false },
    er: { useMaxWidth: false },
  }
}

type MermaidEngine = {
  initialize: (config: ReturnType<typeof tideMermaidConfig>) => void
  render: (id: string, source: string) => Promise<{ svg: string }>
}

let enginePromise: Promise<MermaidEngine> | undefined

export async function loadMermaidEngine(): Promise<MermaidEngine> {
  if (!enginePromise) {
    enginePromise = import('mermaid')
      .then(mod => mod.default as MermaidEngine)
      .catch(err => {
        enginePromise = undefined
        throw err
      })
  }
  const mermaid = await enginePromise
  mermaid.initialize(tideMermaidConfig(readTidePalette()))
  return mermaid
}

export function resetMermaidEngineForTests(): void {
  enginePromise = undefined
}

export function mermaidInitConfig() {
  return tideMermaidConfig(readTidePalette())
}

export function mermaidThemeVariables() {
  return tideMermaidConfig(readTidePalette()).themeVariables
}
