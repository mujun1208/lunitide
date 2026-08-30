// Portions from React Bits Orb, MIT, https://github.com/DavidHDev/react-bits
import { Mesh, Program, Renderer, Triangle, Vec3 } from 'ogl'
import { useEffect, useRef } from 'react'

const vert = /* glsl */ `
precision highp float;
attribute vec2 position;
attribute vec2 uv;
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = vec4(position, 0.0, 1.0);
}
`

const frag = /* glsl */ `
precision highp float;

uniform float iTime;
uniform vec3 iResolution;
uniform float hue;
uniform float hover;
uniform float rot;
uniform float hoverIntensity;
uniform vec3 backgroundColor;
uniform float moonFill;
uniform float ringGain;
uniform float moonRadius;
uniform float idleDrift;
varying vec2 vUv;

vec3 rgb2yiq(vec3 c) {
  float y = dot(c, vec3(0.299, 0.587, 0.114));
  float i = dot(c, vec3(0.596, -0.274, -0.322));
  float q = dot(c, vec3(0.211, -0.523, 0.312));
  return vec3(y, i, q);
}

vec3 yiq2rgb(vec3 c) {
  float r = c.x + 0.956 * c.y + 0.621 * c.z;
  float g = c.x - 0.272 * c.y - 0.647 * c.z;
  float b = c.x - 1.106 * c.y + 1.703 * c.z;
  return vec3(r, g, b);
}

vec3 adjustHue(vec3 color, float hueDeg) {
  float hueRad = hueDeg * 3.14159265 / 180.0;
  vec3 yiq = rgb2yiq(color);
  float cosA = cos(hueRad);
  float sinA = sin(hueRad);
  float i = yiq.y * cosA - yiq.z * sinA;
  float q = yiq.y * sinA + yiq.z * cosA;
  yiq.y = i;
  yiq.z = q;
  return yiq2rgb(yiq);
}

vec3 hash33(vec3 p3) {
  p3 = fract(p3 * vec3(0.1031, 0.11369, 0.13787));
  p3 += dot(p3, p3.yxz + 19.19);
  return -1.0 + 2.0 * fract(vec3(
    p3.x + p3.y,
    p3.x + p3.z,
    p3.y + p3.z
  ) * p3.zyx);
}

float snoise3(vec3 p) {
  const float K1 = 0.333333333;
  const float K2 = 0.166666667;
  vec3 i = floor(p + (p.x + p.y + p.z) * K1);
  vec3 d0 = p - (i - (i.x + i.y + i.z) * K2);
  vec3 e = step(vec3(0.0), d0 - d0.yzx);
  vec3 i1 = e * (1.0 - e.zxy);
  vec3 i2 = 1.0 - e.zxy * (1.0 - e);
  vec3 d1 = d0 - (i1 - K2);
  vec3 d2 = d0 - (i2 - K1);
  vec3 d3 = d0 - 0.5;
  vec4 h = max(0.6 - vec4(
    dot(d0, d0),
    dot(d1, d1),
    dot(d2, d2),
    dot(d3, d3)
  ), 0.0);
  vec4 n = h * h * h * h * vec4(
    dot(d0, hash33(i)),
    dot(d1, hash33(i + i1)),
    dot(d2, hash33(i + i2)),
    dot(d3, hash33(i + 1.0))
  );
  return dot(vec3(31.316), n.xyz);
}

const vec3 auroraA = vec3(0.322, 0.153, 1.0);
const vec3 auroraB = vec3(0.486, 1.0, 0.831);
const vec3 auroraC = vec3(0.247, 0.839, 1.0);

vec3 glassSpectrum(float t) {
  return 0.5 + 0.5 * cos(6.2831853 * (t + vec3(0.00, 0.18, 0.36)));
}

vec3 glowOrb(vec2 uv, float radius) {
  float nd = clamp(length(uv) / max(radius, 0.001), 0.0, 1.0);
  float z = sqrt(max(1.0 - nd * nd, 0.0));
  vec3 nrm = normalize(vec3(uv / radius, z));
  float t = iTime * (0.32 + idleDrift * 0.08 + hover * hoverIntensity * 0.06);

  float n = snoise3(vec3(nrm.x * 0.38, nrm.y * 0.38, t * 0.15));
  float roll = nrm.x * 0.92 + nrm.y * 0.5 + nrm.z * 0.12 - t + n * 0.08;
  vec3 irid = glassSpectrum(roll * 0.7);
  irid = mix(irid, vec3(1.0), 0.2);
  float ridge = pow(0.5 + 0.5 * sin(roll * 2.6), 2.0);

  vec3 col = irid * (0.38 + 0.62 * z);
  col = mix(col * 0.62, col, smoothstep(1.0, 0.22, nd));
  col += irid * ridge * 0.28 * z;
  col += mix(irid, vec3(1.0), 0.45) * pow(1.0 - z, 2.5) * 0.5;

  vec3 lightDir = normalize(vec3(-0.52, 0.64, 0.56));
  float spec = pow(max(dot(nrm, lightDir), 0.0), 34.0);
  float sheen = pow(max(dot(nrm, lightDir), 0.0), 7.0);
  col += vec3(1.0) * spec * 0.9;
  col += vec3(0.88, 0.96, 1.0) * sheen * 0.16;
  return col;
}

vec4 drawMoon(vec2 uv, vec3 rimColor) {
  float len = length(uv);
  float radius = max(moonRadius, 0.2);
  float mask = 1.0 - smoothstep(radius - 0.018, radius + 0.01, len);
  if (mask <= 0.0 || moonFill <= 0.0) return vec4(0.0);

  float nd = clamp(len / radius, 0.0, 1.0);
  float z = sqrt(max(1.0 - nd * nd, 0.0));
  vec3 nrm = normalize(vec3(uv / radius, z));
  vec3 lightDir = normalize(vec3(-0.38, 0.52, 0.78));
  float diff = max(dot(nrm, lightDir), 0.0);
  float fres = pow(1.0 - z, 2.4);
  float grain = snoise3(vec3(uv * 2.35, iTime * 0.12)) * 0.04;
  vec3 jade = vec3(0.97, 0.95, 0.91);
  vec3 limb = vec3(0.70, 0.80, 0.94);
  vec3 col = mix(jade, limb, nd * 0.58 + fres * 0.22);
  col += vec3(0.62, 0.70, 0.82) * pow(diff, 5.0) * 0.24;
  col += rimColor * fres * 0.32;
  col += grain;
  return vec4(col, mask * moonFill);
}

vec4 draw(vec2 uv) {
  float radius = max(moonRadius, 0.2);
  float mask = 1.0 - smoothstep(radius - 0.01, radius + 0.006, length(uv));
  if (mask <= 0.0) return vec4(0.0);

  vec4 jade = drawMoon(uv, auroraC);
  vec3 face = glowOrb(uv, radius);
  vec3 rgb = mix(jade.rgb, face, 1.0 - moonFill);
  return vec4(rgb, mask);
}

vec4 mainImage(vec2 fragCoord) {
  vec2 center = iResolution.xy * 0.5;
  float size = min(iResolution.x, iResolution.y);
  vec2 uv = (fragCoord - center) / size * 2.0;
  return draw(uv);
}

void main() {
  vec2 fragCoord = vUv * iResolution.xy;
  vec4 col = mainImage(fragCoord);
  gl_FragColor = vec4(col.rgb * col.a, col.a);
}
`

function parseRgb(color: string): [number, number, number] {
  if (color === 'transparent') return [0, 0, 0]
  const hex = color.replace('#', '')
  if (hex.length === 6) {
    return [
      parseInt(hex.slice(0, 2), 16) / 255,
      parseInt(hex.slice(2, 4), 16) / 255,
      parseInt(hex.slice(4, 6), 16) / 255,
    ]
  }
  return [0, 0, 0]
}

export type OrbProps = {
  hue?: number
  hoverIntensity?: number
  rotateOnHover?: boolean
  forceHoverState?: boolean
  backgroundColor?: string
  active?: boolean
  moonFill?: number
  ringGain?: number
  moonRadius?: number
  idleDrift?: number
}

export function Orb({
  hue = 205,
  hoverIntensity = 0.2,
  rotateOnHover = true,
  forceHoverState = false,
  backgroundColor = 'transparent',
  active = true,
  moonFill = 1,
  ringGain = 1,
  moonRadius = 0.72,
  idleDrift = 0.1,
}: OrbProps) {
  const ctnRef = useRef<HTMLDivElement>(null)
  const propsRef = useRef({
    hue, hoverIntensity, rotateOnHover, forceHoverState, backgroundColor, active,
    moonFill, ringGain, moonRadius, idleDrift,
  })
  propsRef.current = {
    hue, hoverIntensity, rotateOnHover, forceHoverState, backgroundColor, active,
    moonFill, ringGain, moonRadius, idleDrift,
  }

  useEffect(() => {
    const container = ctnRef.current
    if (!container) return

    let renderer: Renderer
    try {
      renderer = new Renderer({
        alpha: true,
        premultipliedAlpha: false,
        preserveDrawingBuffer: true,
        antialias: true,
        dpr: Math.min(2, window.devicePixelRatio || 1),
      })
    } catch {
      return
    }
    const gl = renderer.gl
    if (!gl) return
    gl.clearColor(0, 0, 0, 0)
    gl.canvas.style.cssText = 'width:100%;height:100%;display:block;'
    container.appendChild(gl.canvas)

    const geometry = new Triangle(gl)
    let program: Program
    try {
      program = new Program(gl, {
        vertex: vert,
        fragment: frag,
        uniforms: {
          iTime: { value: 0 },
          iResolution: {
            value: new Vec3(gl.canvas.width, gl.canvas.height, gl.canvas.width / Math.max(1, gl.canvas.height)),
          },
          hue: { value: hue },
          hover: { value: 0 },
          rot: { value: 0 },
          hoverIntensity: { value: hoverIntensity },
          backgroundColor: { value: new Vec3(...parseRgb(backgroundColor)) },
          moonFill: { value: moonFill },
          ringGain: { value: ringGain },
          moonRadius: { value: moonRadius },
          idleDrift: { value: idleDrift },
        },
      })
    } catch {
      if (gl.canvas.parentNode === container) container.removeChild(gl.canvas)
      return
    }

    const mesh = new Mesh(gl, { geometry, program })
    let resizeRaf = 0
    function resize() {
      if (!container) return
      const width = Math.max(1, container.clientWidth)
      const height = Math.max(1, container.clientHeight)
      renderer.setSize(width, height)
      program.uniforms.iResolution.value.set(gl.canvas.width, gl.canvas.height, gl.canvas.width / Math.max(1, gl.canvas.height))
    }
    const onResize = () => {
      cancelAnimationFrame(resizeRaf)
      resizeRaf = requestAnimationFrame(resize)
    }
    window.addEventListener('resize', onResize)
    resize()

    let targetHover = 0
    let lastTime = 0
    let currentRot = 0
    const rotationSpeed = 0.3

    const handleMouseMove = (e: MouseEvent) => {
      const rect = container.getBoundingClientRect()
      const x = e.clientX - rect.left
      const y = e.clientY - rect.top
      const width = rect.width
      const height = rect.height
      const size = Math.min(width, height)
      const centerX = width / 2
      const centerY = height / 2
      const uvX = ((x - centerX) / size) * 2.0
      const uvY = ((y - centerY) / size) * 2.0
      targetHover = Math.sqrt(uvX * uvX + uvY * uvY) < 0.8 ? 1 : 0
    }
    const handleMouseLeave = () => {
      targetHover = 0
    }
    container.addEventListener('mousemove', handleMouseMove)
    container.addEventListener('mouseleave', handleMouseLeave)

    let rafId: number
    const update = (t: number) => {
      rafId = requestAnimationFrame(update)
      const dt = (t - lastTime) * 0.001
      lastTime = t
      const p = propsRef.current
      program.uniforms.iTime.value = t * 0.001
      program.uniforms.hue.value = p.hue
      program.uniforms.hoverIntensity.value = p.hoverIntensity
      program.uniforms.backgroundColor.value.set(...parseRgb(p.backgroundColor))
      program.uniforms.moonFill.value = p.moonFill
      program.uniforms.ringGain.value = p.ringGain
      program.uniforms.moonRadius.value = p.moonRadius
      program.uniforms.idleDrift.value = p.idleDrift

      const effectiveHover = p.forceHoverState ? 1 : targetHover
      program.uniforms.hover.value += (effectiveHover - program.uniforms.hover.value) * 0.1

      currentRot += dt * (0.05 + (p.rotateOnHover && effectiveHover > 0.5 ? rotationSpeed : 0))
      program.uniforms.rot.value = currentRot

      if (p.active) renderer.render({ scene: mesh })
    }
    rafId = requestAnimationFrame(update)

    return () => {
      cancelAnimationFrame(rafId)
      cancelAnimationFrame(resizeRaf)
      window.removeEventListener('resize', onResize)
      container.removeEventListener('mousemove', handleMouseMove)
      container.removeEventListener('mouseleave', handleMouseLeave)
      try {
        gl.getExtension('WEBGL_lose_context')?.loseContext()
      } catch {
        /* ignore */
      }
      if (gl.canvas.parentNode === container) container.removeChild(gl.canvas)
    }
  }, [])

  return <div ref={ctnRef} className="companion-orb-canvas" aria-hidden />
}
