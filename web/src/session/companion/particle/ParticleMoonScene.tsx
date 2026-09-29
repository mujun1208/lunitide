import { Camera, Geometry, Mesh, Program, Renderer, Sphere, Transform, Triangle } from 'ogl'
import { useEffect, useRef } from 'react'
import type { ParticleMoonState } from './particleMoon'
import { engageMorph, isEngageState, particleMoonTargets } from './particleMoon'
import { buildHaloRing, buildPreviewCloud } from './particleGeometry'

const AURORA_VERT = `#version 300 es
in vec2 position;
void main() {
  gl_Position = vec4(position, 0.0, 1.0);
}
`

const AURORA_FRAG = `#version 300 es
precision highp float;
uniform vec2 uResolution;
uniform float uTime;
uniform float uAura;
out vec4 fragColor;

float hash21(vec2 p) {
  return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453);
}

float noise(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  float a = hash21(i);
  float b = hash21(i + vec2(1.0, 0.0));
  float c = hash21(i + vec2(0.0, 1.0));
  float d = hash21(i + vec2(1.0, 1.0));
  vec2 u = f * f * (3.0 - 2.0 * f);
  return mix(a, b, u.x) + (c - a) * u.y * (1.0 - u.x) + (d - b) * u.x * u.y;
}

float fbm(vec2 p) {
  float v = 0.0;
  float a = 0.5;
  for (int i = 0; i < 5; i++) {
    v += a * noise(p);
    p *= 2.03;
    a *= 0.5;
  }
  return v;
}

void main() {
  vec2 uv = gl_FragCoord.xy / uResolution;
  vec2 p = uv * 2.0 - 1.0;
  p.x *= uResolution.x / max(uResolution.y, 1.0);
  float ang = uTime * 0.016;
  float ca = cos(ang);
  float sa = sin(ang);
  vec2 q = vec2(p.x * ca - p.y * sa, p.x * sa + p.y * ca) * 1.08;
  q.x += uTime * 0.008;
  float n = fbm(q + 2.7);
  float n2 = fbm(q * 1.85 + vec2(-uTime * 0.012, 8.2));
  float n3 = fbm(q * 0.62 + vec2(4.1, -uTime * 0.008));
  float n4 = fbm(q * 0.92 + vec2(-6.4, uTime * 0.007));

  vec2 starUv = uv * vec2(260.0, 148.0);
  vec2 starId = floor(starUv);
  vec2 starF = fract(starUv) - 0.5;
  float starHit = step(0.9964, hash21(starId));
  float starDot = exp(-dot(starF, starF) * 96.0) * starHit;
  float twinkle = 0.5 + 0.5 * sin(uTime * (1.2 + hash21(starId + 3.1) * 3.8) + hash21(starId) * 18.0);
  vec3 night = vec3(0.92, 0.95, 1.0) * starDot * twinkle;

  float rr = length(p);
  float wash = exp(-rr * rr * 0.72) * 0.04;
  vec3 river = mix(vec3(0.04, 0.03, 0.1), vec3(0.03, 0.08, 0.16), n3) * wash;

  float purpleVeil = pow(n, 2.1) * mix(0.12, 0.7, pow(n3, 1.25));
  vec3 purple = mix(vec3(0.72, 0.1, 0.58), vec3(0.28, 0.06, 0.52), n2);
  vec3 smoke = purple * purpleVeil * (0.24 + uAura * 0.08);

  float cyanVeil = pow(n2, 2.4) * mix(0.04, 0.28, pow(n, 1.6));
  vec3 cyan = vec3(0.16, 0.48, 0.86) * cyanVeil * 0.16;

  float whiteVeil = pow(n4, 2.4) * mix(0.04, 0.35, pow(n2, 1.5));
  vec3 fog = vec3(0.78, 0.84, 0.94) * whiteVeil * 0.12;

  float grain = hash21(uv + uTime * 0.01) * 0.004;
  fragColor = vec4(night + river * 0.35 + smoke + cyan + fog + grain, 1.0);
}
`

const NEBULA_VERT = `#version 300 es
in vec3 position;
in vec3 color;
in float size;
in float layer;
in float ring;
in vec3 seed;
uniform mat4 projectionMatrix;
uniform mat4 modelViewMatrix;
uniform float uTime;
uniform float uAura;
uniform vec2 uMouse;
uniform vec2 uResolution;
out vec3 vColor;
out float vBright;

vec3 rotateY(vec3 p, float a) {
  float c = cos(a);
  float s = sin(a);
  return vec3(c * p.x + s * p.z, p.y, -s * p.x + c * p.z);
}

vec3 rotateX(vec3 p, float a) {
  float c = cos(a);
  float s = sin(a);
  return vec3(p.x, c * p.y - s * p.z, s * p.y + c * p.z);
}

vec3 rotateZ(vec3 p, float a) {
  float c = cos(a);
  float s = sin(a);
  return vec3(c * p.x - s * p.y, s * p.x + c * p.y, p.z);
}

void main() {
  float orbit = mix(0.014, 0.024, clamp(ring * 0.12, 0.0, 1.0));
  orbit *= mix(0.35, 1.0, step(0.5, layer));
  float tilt = 0.66 + sin(uTime * 0.055 + ring * 0.18) * 0.07;
  vec3 p = rotateX(position, tilt);
  p = rotateY(p, uTime * orbit);
  p = rotateZ(p, sin(uTime * 0.04 + ring * 0.3) * 0.06);
  p.y += sin(uTime * 0.16 + seed.x * 8.0 + ring) * 0.025;
  p.xy += uMouse * 0.06 * (0.3 + seed.y);
  vColor = color;
  vBright = mix(0.5, 0.7 + uAura * 0.08, step(0.5, layer));
  vBright *= 0.84 + 0.16 * sin(uTime * (1.1 + seed.z * 4.0) + seed.x * 10.0);
  vec4 clip = projectionMatrix * modelViewMatrix * vec4(p, 1.0);
  float px = uResolution.y / 900.0;
  gl_PointSize = clamp(size * (11.6 / max(clip.w, 0.7)) * px, 1.2, layer < 0.5 ? 3.4 : 10.5);
  gl_Position = clip;
}
`

const NEBULA_FRAG = `#version 300 es
precision highp float;
in vec3 vColor;
in float vBright;
out vec4 fragColor;
void main() {
  vec2 uv = gl_PointCoord * 2.0 - 1.0;
  float d = dot(uv, uv);
  if (d > 1.0) discard;
  float glow = exp(-d * 4.6) + exp(-d * 1.05) * 0.32;
  fragColor = vec4(vColor * vBright, glow * vBright);
}
`

const MOON_VERT = `#version 300 es
in vec3 sphere;
in vec3 lotus;
in vec3 torus;
in vec3 helix;
in vec3 star;
in vec3 color;
in float size;
in vec3 seed;
uniform mat4 projectionMatrix;
uniform mat4 modelViewMatrix;
uniform float uTime;
uniform float uForm;
uniform float uScatter;
uniform float uSpin;
uniform float uRadius;
uniform float uPulse;
uniform float uA;
uniform float uB;
uniform float uMix;
uniform vec2 uMouse;
uniform vec2 uResolution;
out vec3 vColor;
out float vBright;

vec3 rotateY(vec3 p, float a) {
  float c = cos(a);
  float s = sin(a);
  return vec3(c * p.x + s * p.z, p.y, -s * p.x + c * p.z);
}

vec3 rotateX(vec3 p, float a) {
  float c = cos(a);
  float s = sin(a);
  return vec3(p.x, c * p.y - s * p.z, s * p.y + c * p.z);
}

vec3 rotateZ(vec3 p, float a) {
  float c = cos(a);
  float s = sin(a);
  return vec3(c * p.x - s * p.y, s * p.x + c * p.y, p.z);
}

vec3 pose(float id) {
  if (id < 0.5) return sphere;
  if (id < 1.5) return lotus;
  if (id < 2.5) return torus;
  if (id < 3.5) return helix;
  return star;
}

float smoother(float x) {
  x = clamp(x, 0.0, 1.0);
  return x * x * x * (x * (x * 6.0 - 15.0) + 10.0);
}

vec3 hsv2rgb(vec3 c) {
  vec4 K = vec4(1.0, 2.0 / 3.0, 1.0 / 3.0, 3.0);
  vec3 p = abs(fract(c.xxx + K.xyz) * 6.0 - K.www);
  return c.z * mix(K.xxx, clamp(p - K.xxx, 0.0, 1.0), c.y);
}

vec3 morphTo(vec3 a, vec3 b, float t) {
  vec3 linear = mix(a, b, t);
  float la = max(length(a), 0.06);
  float lb = max(length(b), 0.06);
  vec3 blended = mix(a / la, b / lb, t);
  float bl = length(blended);
  vec3 mid = (bl > 0.001 ? blended / bl : a / la) * mix(la, lb, t);
  float lift = sin(t * 3.14159265);
  return mix(linear, mid, 0.64) * (1.0 + lift * 0.1);
}

void main() {
  float form = clamp(uForm, 0.0, 1.0);
  float delay = seed.x * 0.26;
  float span = max(1.0 - delay * 0.7, 0.22);
  float t = smoother(clamp((clamp(uMix, 0.0, 1.0) - delay) / span, 0.0, 1.0));
  vec3 base = morphTo(pose(uA), pose(uB), t);
  float puff = mix(0.22, 0.03, form) * uScatter;
  vec3 p = base * (uRadius * (1.0 + puff * (0.3 + seed.x * 0.55)));
  p *= 1.0 + uPulse * (0.2 + seed.y * 0.08);
  float lotusAmt = 1.0 - min(abs(uA - 1.0), abs(uB - 1.0));
  p.y += sin(uTime * 1.15 + seed.x * 8.0) * 0.022 * lotusAmt;
  p = rotateY(p, uSpin + uMouse.x * 0.28);
  p = rotateX(p, sin(uSpin * 0.42) * 0.1 - uMouse.y * 0.16);
  float nestTilt = 0.66 + sin(uTime * 0.055) * 0.07;
  p = rotateX(p, nestTilt);
  p = rotateY(p, uTime * 0.018);
  p = rotateZ(p, sin(uTime * 0.04) * 0.06);

  float flicker = 0.82 + 0.18 * sin(uTime * mix(2.8, 16.0, uPulse) + seed.z * 22.0);
  float hue = fract(color.x * 0.95 + uTime * 0.12 + seed.z * 0.28);
  vec3 neon = hsv2rgb(vec3(hue, 0.96, 0.88));
  vColor = neon;
  vBright = (0.82 + uPulse * 0.28) * flicker;

  vec4 clip = projectionMatrix * modelViewMatrix * vec4(p, 1.0);
  float px = uResolution.y / 900.0;
  gl_PointSize = clamp(size * (8.4 / max(clip.w, 0.45)) * px * (1.0 + uPulse * 0.18), 1.8, 6.4);
  gl_Position = clip;
}
`

const MOON_FRAG = `#version 300 es
precision highp float;
in vec3 vColor;
in float vBright;
out vec4 fragColor;
void main() {
  vec2 uv = gl_PointCoord * 2.0 - 1.0;
  float d = dot(uv, uv);
  if (d > 1.0) discard;
  float core = exp(-d * 9.2);
  float halo = exp(-d * 2.2) * 0.48;
  float glow = core + halo;
  vec3 rgb = vColor * (0.95 + core * 0.7);
  fragColor = vec4(rgb * vBright, glow * vBright);
}
`

const PLANET_VERT = `#version 300 es
in vec3 position;
in vec3 normal;
uniform mat4 projectionMatrix;
uniform mat4 modelViewMatrix;
out vec3 vN;
out vec3 vV;
void main() {
  vec4 mv = modelViewMatrix * vec4(position, 1.0);
  vN = mat3(modelViewMatrix) * normal;
  vV = -mv.xyz;
  gl_Position = projectionMatrix * mv;
}
`

const PLANET_FRAG = `#version 300 es
precision highp float;
in vec3 vN;
in vec3 vV;
out vec4 fragColor;
void main() {
  vec3 n = normalize(vN);
  vec3 view = normalize(vV);
  vec3 light = normalize(vec3(0.42, 0.62, 0.78));
  float diff = max(dot(n, light), 0.0);
  float rim = pow(1.0 - max(dot(n, view), 0.0), 2.8);
  vec3 base = mix(vec3(0.07, 0.065, 0.085), vec3(0.3, 0.28, 0.32), diff);
  vec3 spec = vec3(0.7, 0.74, 0.8) * pow(diff, 18.0) * 0.32;
  vec3 glow = vec3(0.42, 0.62, 0.82) * rim * 0.38;
  fragColor = vec4(base + spec + glow, 1.0);
}
`

const RING_VERT = `#version 300 es
in vec3 position;
uniform mat4 projectionMatrix;
uniform mat4 modelViewMatrix;
out float vR;
void main() {
  vR = length(position.xz);
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`

const RING_FRAG = `#version 300 es
precision highp float;
in float vR;
out vec4 fragColor;
void main() {
  float band = 1.0 - abs(vR - 0.215) / 0.05;
  float glow = pow(max(band, 0.0), 1.45);
  vec3 rgb = mix(vec3(0.26, 0.5, 0.82), vec3(0.78, 0.86, 1.0), glow);
  fragColor = vec4(rgb, glow * 0.4);
}
`

export type ParticleMoonSceneProps = {
  state: ParticleMoonState
  gain: number
  levels: number[]
  burstToken: number
  high: boolean
  onFps?: (fps: number) => void
}

export function ParticleMoonScene({ state, gain, burstToken, high, onFps }: ParticleMoonSceneProps): React.JSX.Element {
  const hostRef = useRef<HTMLDivElement>(null)
  const live = useRef({ state, gain, burstToken, onFps, mouse: { x: 0, y: 0 } })
  live.current = { ...live.current, state, gain, burstToken, onFps }

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const renderer = new Renderer({
      dpr: Math.min(window.devicePixelRatio || 1, 1.5),
      webgl: 2,
      alpha: false,
      antialias: false,
    })
    const gl = renderer.gl
    gl.clearColor(0, 0, 0, 1)
    host.appendChild(gl.canvas)
    Object.assign(gl.canvas.style, { width: '100%', height: '100%', display: 'block' })

    const camera = new Camera(gl, { fov: 40, near: 0.1, far: 80 })
    camera.position.set(0, 2.15, 5.5)
    camera.lookAt([0, 0, 0])

    const aurora = new Program(gl, {
      vertex: AURORA_VERT,
      fragment: AURORA_FRAG,
      uniforms: {
        uResolution: { value: [1, 1] },
        uTime: { value: 0 },
        uAura: { value: 0.62 },
      },
      depthTest: false,
      depthWrite: false,
    })
    const auroraMesh = new Mesh(gl, { geometry: new Triangle(gl), program: aurora })

    const cloud = buildPreviewCloud(high)
    const nebulaProg = new Program(gl, {
      vertex: NEBULA_VERT,
      fragment: NEBULA_FRAG,
      uniforms: {
        uTime: { value: 0 },
        uAura: { value: 0.62 },
        uMouse: { value: [0, 0] },
        uResolution: { value: [1, 1] },
      },
      transparent: true,
      depthTest: false,
      depthWrite: false,
    })
    nebulaProg.setBlendFunc(gl.SRC_ALPHA, gl.ONE)
    const nebulaGeom = new Geometry(gl, {
      position: { size: 3, data: cloud.nebula.position },
      color: { size: 3, data: cloud.nebula.color },
      size: { size: 1, data: cloud.nebula.size },
      layer: { size: 1, data: cloud.nebula.layer },
      ring: { size: 1, data: cloud.nebula.ring },
      seed: { size: 3, data: cloud.nebula.seed },
    })
    const nebulaMesh = new Mesh(gl, { geometry: nebulaGeom, program: nebulaProg, mode: gl.POINTS, frustumCulled: false })
    const nebulaScene = new Transform()
    nebulaMesh.setParent(nebulaScene)

    const planetProg = new Program(gl, {
      vertex: PLANET_VERT,
      fragment: PLANET_FRAG,
      uniforms: {},
      transparent: false,
      depthTest: true,
      depthWrite: true,
    })
    const ringProg = new Program(gl, {
      vertex: RING_VERT,
      fragment: RING_FRAG,
      uniforms: {},
      transparent: true,
      depthTest: false,
      depthWrite: false,
      cullFace: false,
    })
    ringProg.setBlendFunc(gl.SRC_ALPHA, gl.ONE)
    const planetMesh = new Mesh(gl, { geometry: new Sphere(gl, { radius: 0.16, widthSegments: 48, heightSegments: 32 }), program: planetProg })
    const ringMesh = new Mesh(gl, {
      geometry: new Geometry(gl, { position: { size: 3, data: buildHaloRing(0.17, 0.26) } }),
      program: ringProg,
    })
    const planetRoot = new Transform()
    const planetScene = new Transform()
    planetMesh.setParent(planetRoot)
    ringMesh.setParent(planetRoot)
    ringMesh.rotation.x = 0.72
    planetRoot.setParent(planetScene)

    const moonProg = new Program(gl, {
      vertex: MOON_VERT,
      fragment: MOON_FRAG,
      uniforms: {
        uTime: { value: 0 },
        uForm: { value: 0.58 },
        uScatter: { value: 1.34 },
        uSpin: { value: 0 },
        uRadius: { value: 0.52 },
        uPulse: { value: 0 },
        uA: { value: 0 },
        uB: { value: 0 },
        uMix: { value: 0 },
        uMouse: { value: [0, 0] },
        uResolution: { value: [1, 1] },
      },
      transparent: true,
      depthTest: false,
      depthWrite: false,
    })
    moonProg.setBlendFunc(gl.SRC_ALPHA, gl.ONE)
    const moonGeom = new Geometry(gl, {
      position: { size: 3, data: cloud.moon.sphere },
      sphere: { size: 3, data: cloud.moon.sphere },
      lotus: { size: 3, data: cloud.moon.lotus },
      torus: { size: 3, data: cloud.moon.torus },
      helix: { size: 3, data: cloud.moon.helix },
      star: { size: 3, data: cloud.moon.star },
      color: { size: 3, data: cloud.moon.color },
      size: { size: 1, data: cloud.moon.size },
      seed: { size: 3, data: cloud.moon.seed },
    })
    const moonMesh = new Mesh(gl, { geometry: moonGeom, program: moonProg, mode: gl.POINTS, frustumCulled: false })
    const moonScene = new Transform()
    moonMesh.setParent(moonScene)

    let form = 0.58
    let scatter = 1.34
    let radius = 0.52
    let pulse = 0
    let aura = 0.58
    let held = 0
    let spin = 0
    let thinkAge = 0
    let fromShape = 0
    let speakFrom = 0
    let speakMix = 1
    let lastShape = 0
    let prevState: ParticleMoonState = 'idle'
    let last = performance.now()
    let frames = 0
    let fpsWindow = 0
    let raf = 0
    let resizeRaf = 0

    const resize = () => {
      const w = host.clientWidth || 1
      const h = host.clientHeight || 1
      renderer.setSize(w, h)
      camera.perspective({ aspect: w / h })
      aurora.uniforms.uResolution.value = [w, h]
      nebulaProg.uniforms.uResolution.value = [w, h]
      moonProg.uniforms.uResolution.value = [w, h]
    }
    const onResize = () => {
      cancelAnimationFrame(resizeRaf)
      resizeRaf = requestAnimationFrame(resize)
    }
    const onMove = (e: PointerEvent) => {
      const rect = host.getBoundingClientRect()
      live.current.mouse.x = ((e.clientX - rect.left) / rect.width) * 2 - 1
      live.current.mouse.y = -(((e.clientY - rect.top) / rect.height) * 2 - 1)
    }
    const pointerRoot = (host.closest('.companion-stage') ?? host) as HTMLElement
    window.addEventListener('resize', onResize)
    pointerRoot.addEventListener('pointermove', onMove)
    resize()

    const ease = (current: number, target: number, dt: number, tau: number) => current + (target - current) * (1 - Math.exp(-dt / tau))

    const frame = (now: number) => {
      raf = requestAnimationFrame(frame)
      const dt = Math.min(0.05, (now - last) / 1000)
      last = now
      frames += 1
      fpsWindow += dt
      if (fpsWindow >= 0.5) {
        live.current.onFps?.(frames / fpsWindow)
        frames = 0
        fpsWindow = 0
      }

      const snap = live.current
      const engage = isEngageState(snap.state)
      if (snap.state !== prevState) {
        if (engage && !isEngageState(prevState)) {
          thinkAge = 0
          fromShape = lastShape
        }
        if (snap.state === 'speaking') {
          speakFrom = lastShape
          speakMix = 0
        }
        prevState = snap.state
      }
      if (engage) thinkAge += dt

      const target = particleMoonTargets(snap.state, snap.gain)
      const morph = engage ? engageMorph(thinkAge, fromShape) : null
      const tau = engage ? 0.22 : snap.state === 'speaking' ? 0.12 : 0.36
      form = ease(form, target.form, dt, tau)
      scatter = ease(scatter, target.scatter, dt, snap.state === 'speaking' ? 0.1 : 0.28)
      radius = ease(radius, morph ? morph.radius : target.radius, dt, snap.state === 'speaking' ? 0.22 : 0.32)
      pulse = ease(pulse, target.pulse, dt, 0.08)
      aura = ease(aura, target.aura, dt, 0.22)
      spin += dt * target.spinSpeed * Math.PI * 2
      held = ease(held, target.shape, dt, 0.55)
      if (snap.state === 'speaking') speakMix = ease(speakMix, 1, dt, 0.28)

      let shapeA = 0
      let shapeB = 0
      let shapeMix = 0
      if (morph) {
        shapeA = morph.shapeA
        shapeB = morph.shapeB
        shapeMix = morph.mix
        lastShape = shapeMix > 0.5 ? shapeB : shapeA
      } else if (snap.state === 'speaking') {
        shapeA = speakFrom
        shapeB = 0
        shapeMix = speakMix
        lastShape = 0
      } else {
        shapeA = Math.floor(held)
        shapeB = Math.ceil(held)
        shapeMix = held - shapeA
        lastShape = 0
      }

      const mx = snap.mouse.x
      const my = snap.mouse.y
      const orbit = now * 0.00006
      camera.position.set(Math.sin(orbit) * 0.18 + mx * 0.04, 2.15 + my * 0.05, 5.5)
      camera.lookAt([0, 0, 0])

      const planetT = now * 0.00014
      planetRoot.position.set(Math.cos(planetT) * 3.55, -0.72 + Math.sin(planetT * 0.7) * 0.16, Math.sin(planetT) * 3.55 - 1.35)
      planetRoot.rotation.y = now * 0.00035

      aurora.uniforms.uTime.value = now * 0.001
      aurora.uniforms.uAura.value = aura
      nebulaProg.uniforms.uTime.value = now * 0.001
      nebulaProg.uniforms.uAura.value = aura
      nebulaProg.uniforms.uMouse.value = [mx, my]
      moonProg.uniforms.uTime.value = now * 0.001
      moonProg.uniforms.uForm.value = form
      moonProg.uniforms.uScatter.value = scatter
      moonProg.uniforms.uSpin.value = spin
      moonProg.uniforms.uRadius.value = radius
      moonProg.uniforms.uPulse.value = pulse
      moonProg.uniforms.uA.value = shapeA
      moonProg.uniforms.uB.value = shapeB
      moonProg.uniforms.uMix.value = shapeMix
      moonProg.uniforms.uMouse.value = [mx, my]

      renderer.render({ scene: auroraMesh })
      gl.clear(gl.DEPTH_BUFFER_BIT)
      renderer.render({ scene: planetScene, camera, clear: false })
      renderer.render({ scene: moonScene, camera, clear: false })
      renderer.render({ scene: nebulaScene, camera, clear: false })
    }
    raf = requestAnimationFrame(frame)

    return () => {
      cancelAnimationFrame(raf)
      cancelAnimationFrame(resizeRaf)
      window.removeEventListener('resize', onResize)
      pointerRoot.removeEventListener('pointermove', onMove)
      try {
        gl.getExtension('WEBGL_lose_context')?.loseContext()
      } catch {
        /* ignore */
      }
      if (gl.canvas.parentNode === host) host.removeChild(gl.canvas)
    }
  }, [high])

  return <div ref={hostRef} className="stardust-sim-canvas" aria-hidden="true" />
}
