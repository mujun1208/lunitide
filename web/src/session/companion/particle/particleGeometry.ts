import { particleBudget } from './particleMoon'

export const LAYER_STAR = 0
export const LAYER_DUST = 1

const RINGS: Array<{
  r: number
  thick: number
  amp: number
  freq: number
  phase: number
  lift: number
  color: [number, number, number]
}> = [
  { r: 0.98, thick: 0.055, amp: 0.07, freq: 3, phase: 0.3, lift: -0.08, color: [0.4, 0.98, 1] },
  { r: 1.48, thick: 0.06, amp: 0.09, freq: 5, phase: 1.2, lift: 0.1, color: [0.32, 0.5, 1] },
  { r: 2.05, thick: 0.065, amp: 0.1, freq: 4, phase: 0.5, lift: -0.12, color: [0.52, 0.24, 0.96] },
  { r: 2.68, thick: 0.07, amp: 0.12, freq: 6, phase: 1.8, lift: 0.12, color: [0.72, 0.16, 0.88] },
  { r: 3.38, thick: 0.08, amp: 0.13, freq: 3, phase: 0.9, lift: -0.08, color: [0.78, 0.12, 0.42] },
  { r: 4.12, thick: 0.09, amp: 0.14, freq: 5, phase: 2.1, lift: 0.1, color: [0.72, 0.08, 0.24] },
  { r: 4.88, thick: 0.1, amp: 0.14, freq: 4, phase: 0.2, lift: -0.08, color: [0.58, 0.06, 0.18] },
]

export function hash(i: number, n: number): number {
  const x = Math.sin(i * 127.1 + n * 311.7) * 43758.5453
  return x - Math.floor(x)
}

function mix3(a: [number, number, number], b: [number, number, number], t: number): [number, number, number] {
  return [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t, a[2] + (b[2] - a[2]) * t]
}

export function spherePoint(i: number, count: number): [number, number, number] {
  const last = Math.max(count - 1, 1)
  const y = 1 - (i / last) * 2
  const r = Math.sqrt(Math.max(0, 1 - y * y))
  const theta = Math.PI * (3 - Math.sqrt(5)) * i
  return [Math.cos(theta) * r, y, Math.sin(theta) * r]
}

export function lotusPoint(i: number, count: number): [number, number, number] {
  const petals = 5
  const s = Math.sqrt(hash(i, 1))
  const v = hash(i, 2)
  const petal = (i + Math.floor(hash(i, 3) * petals)) % petals
  const a = (petal / petals) * Math.PI * 2
  const w = (v - 0.5) * 0.52 * (0.22 + s)
  const reach = 0.2 + s * 0.82
  const lift = Math.sin(s * Math.PI) * 0.24
  const x = Math.cos(a) * reach + Math.cos(a + Math.PI / 2) * w
  const z = Math.sin(a) * reach + Math.sin(a + Math.PI / 2) * w
  const y = lift - 0.06 + (hash(i, 4) - 0.5) * 0.035
  return [x, y, z]
}

export function torusPoint(i: number): [number, number, number] {
  const u = hash(i, 11) * Math.PI * 2
  const v = hash(i, 12) * Math.PI * 2
  const R = 0.58
  const r = 0.2
  return [(R + r * Math.cos(v)) * Math.cos(u), r * Math.sin(v), (R + r * Math.cos(v)) * Math.sin(u)]
}

export function helixPoint(i: number, count: number): [number, number, number] {
  const strand = i % 2
  const last = Math.max(Math.floor(count / 2) - 1, 1)
  const k = Math.floor(i / 2) / last
  const t = k * Math.PI * 2 * 3.6 + strand * Math.PI
  return [Math.cos(t) * 0.42, k * 1.62 - 0.81, Math.sin(t) * 0.42]
}

export function starPoint(i: number, count: number): [number, number, number] {
  const [nx, ny, nz] = spherePoint(i, count)
  const lon = Math.atan2(nz, nx)
  const lat = Math.asin(Math.max(-1, Math.min(1, ny)))
  const spike = Math.pow(Math.abs(Math.cos(lon * 2.5) * Math.cos(lat * 1.15)), 1.28)
  const rad = 0.36 + 0.64 * spike
  return [nx * rad, ny * rad, nz * rad]
}

export function buildMoonSculpture(count: number): {
  position: Float32Array
  sphere: Float32Array
  lotus: Float32Array
  torus: Float32Array
  helix: Float32Array
  star: Float32Array
  color: Float32Array
  size: Float32Array
  seed: Float32Array
} {
  const sphere = new Float32Array(count * 3)
  const lotus = new Float32Array(count * 3)
  const torus = new Float32Array(count * 3)
  const helix = new Float32Array(count * 3)
  const star = new Float32Array(count * 3)
  const color = new Float32Array(count * 3)
  const size = new Float32Array(count)
  const seed = new Float32Array(count * 3)

  for (let i = 0; i < count; i++) {
    const o = i * 3
    const s = spherePoint(i, count)
    const l = lotusPoint(i, count)
    const t = torusPoint(i)
    const h = helixPoint(i, count)
    const k = starPoint(i, count)
    sphere[o] = s[0]
    sphere[o + 1] = s[1]
    sphere[o + 2] = s[2]
    lotus[o] = l[0]
    lotus[o + 1] = l[1]
    lotus[o + 2] = l[2]
    torus[o] = t[0]
    torus[o + 1] = t[1]
    torus[o + 2] = t[2]
    helix[o] = h[0]
    helix[o + 1] = h[1]
    helix[o + 2] = h[2]
    star[o] = k[0]
    star[o + 1] = k[1]
    star[o + 2] = k[2]

    const spark = hash(i, 4) > 0.92 ? 1 : 0
    color[o] = hash(i, 21)
    color[o + 1] = 0.78 + hash(i, 22) * 0.2
    color[o + 2] = 0.9 + spark * 0.1
    size[i] = 1.35 + spark * 0.35
    seed[o] = hash(i, 31)
    seed[o + 1] = hash(i, 32)
    seed[o + 2] = hash(i, 33)
  }

  return { position: sphere, sphere, lotus, torus, helix, star, color, size, seed }
}

export function buildNebulaField(nebulaCount: number, starCount: number): {
  position: Float32Array
  color: Float32Array
  size: Float32Array
  seed: Float32Array
  layer: Float32Array
  ring: Float32Array
  count: number
} {
  const count = nebulaCount + starCount
  const position = new Float32Array(count * 3)
  const color = new Float32Array(count * 3)
  const size = new Float32Array(count)
  const seed = new Float32Array(count * 3)
  const layer = new Float32Array(count)
  const ring = new Float32Array(count)

  for (let i = 0; i < nebulaCount; i++) {
    const o = i * 3
    const kind = hash(i, 1)
    if (kind < 0.94) {
      const bias = Math.pow(hash(i, 2), 0.62)
      const ri = Math.min(RINGS.length - 1, Math.floor(bias * RINGS.length))
      const band = RINGS[ri]
      const theta = hash(i, 3) * Math.PI * 2
      const clump = 0.28 + 0.72 * Math.pow(hash(i, 4), 0.42)
      const wave =
        band.amp * Math.sin(band.freq * theta + band.phase) + band.amp * 0.42 * Math.sin((band.freq + 2) * theta + band.phase * 1.6)
      const r = band.r + wave + (hash(i, 5) - 0.5) * band.thick * clump
      position[o] = Math.cos(theta) * r
      position[o + 1] = band.lift + (hash(i, 6) - 0.5) * 0.1 + Math.sin(theta * 2.2 + band.phase) * 0.05
      position[o + 2] = Math.sin(theta) * r
      const spark = hash(i, 7) > 0.88 ? 0.22 : 0
      const c = mix3(band.color, [0.86, 0.94, 1], spark)
      color[o] = c[0]
      color[o + 1] = c[1]
      color[o + 2] = c[2]
      size[i] = 1.2 + hash(i, 8) * 1.8
      ring[i] = ri + 1
    } else {
      const theta = hash(i, 13) * Math.PI * 2
      const rad = 2.3 + hash(i, 14) * 3.6
      position[o] = Math.cos(theta) * rad
      position[o + 1] = (hash(i, 15) - 0.5) * 0.22
      position[o + 2] = Math.sin(theta) * rad
      const haze = mix3([0.42, 0.1, 0.52], [0.62, 0.08, 0.22], hash(i, 16))
      color[o] = haze[0]
      color[o + 1] = haze[1]
      color[o + 2] = haze[2]
      size[i] = 1.6 + hash(i, 17) * 2
      ring[i] = 4
    }
    layer[i] = LAYER_DUST
    seed[o] = hash(i, 31)
    seed[o + 1] = hash(i, 32)
    seed[o + 2] = hash(i, 33)
  }

  for (let i = 0; i < starCount; i++) {
    const idx = nebulaCount + i
    const o = idx * 3
    const z = hash(idx, 41) * 2 - 1
    const t = hash(idx, 42) * Math.PI * 2
    const r = 8.5 + hash(idx, 43) * 11
    const s = Math.sqrt(Math.max(0, 1 - z * z))
    position[o] = Math.cos(t) * s * r
    position[o + 1] = z * r * 0.42
    position[o + 2] = Math.sin(t) * s * r
    const starHue = hash(idx, 44)
    const star = starHue < 0.28
      ? [0.35, 0.85, 1]
      : starHue < 0.55
        ? [0.95, 0.35, 0.95]
        : starHue < 0.78
          ? [1, 0.72, 0.28]
          : [0.55, 1, 0.45]
    color[o] = star[0]
    color[o + 1] = star[1]
    color[o + 2] = star[2]
    size[idx] = 0.7 + hash(idx, 46) * 1.3
    layer[idx] = LAYER_STAR
    ring[idx] = 0
    seed[o] = hash(idx, 31)
    seed[o + 1] = hash(idx, 32)
    seed[o + 2] = hash(idx, 33)
  }

  return { position, color, size, seed, layer, ring, count }
}

export function buildHaloRing(inner = 0.22, outer = 0.34, segs = 80): Float32Array {
  const data = new Float32Array(segs * 6 * 3)
  let w = 0
  for (let i = 0; i < segs; i++) {
    const a0 = (i / segs) * Math.PI * 2
    const a1 = ((i + 1) / segs) * Math.PI * 2
    const quad: Array<[number, number, number]> = [
      [Math.cos(a0) * inner, 0, Math.sin(a0) * inner],
      [Math.cos(a0) * outer, 0, Math.sin(a0) * outer],
      [Math.cos(a1) * outer, 0, Math.sin(a1) * outer],
      [Math.cos(a0) * inner, 0, Math.sin(a0) * inner],
      [Math.cos(a1) * outer, 0, Math.sin(a1) * outer],
      [Math.cos(a1) * inner, 0, Math.sin(a1) * inner],
    ]
    for (const p of quad) {
      data[w++] = p[0]
      data[w++] = p[1]
      data[w++] = p[2]
    }
  }
  return data
}

export function buildPreviewCloud(high: boolean): {
  moon: ReturnType<typeof buildMoonSculpture>
  nebula: ReturnType<typeof buildNebulaField>
} {
  const budget = particleBudget(high)
  return {
    moon: buildMoonSculpture(budget.moon),
    nebula: buildNebulaField(budget.nebula, budget.stars),
  }
}
