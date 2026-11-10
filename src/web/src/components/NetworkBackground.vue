<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { bgAnimEnabled } from '../bgAnim'

/**
 * 动态背景画布
 * theme:
 *  - aurora: 浅蓝青极光 + 星网（一智算）
 *  - vivid : 高饱和蓝青渐变 + 光带 + 光环 + 星网（门户首页）
 *  - ocean : 蓝绿深海 + 星网（二撮合）
 *  - night : 深紫夜色 + 流光波浪 + 城市光斑（三生万）
 * 动效统一跟随全局开关 bgAnimEnabled（默认关闭）；关闭时仍绘制一帧静态背景。
 * 注意：不要再声明 Boolean 类型的 animated 属性——Vue 3 会把未传入的
 * Boolean 属性自动转成 false，导致全局开关被永久覆盖。
 */
const props = withDefaults(defineProps<{ theme?: 'aurora' | 'vivid' | 'ocean' | 'night' }>(), { theme: 'aurora' })

const canvasRef = ref<HTMLCanvasElement | null>(null)
let raf = 0
let running = false
let ctx: CanvasRenderingContext2D | null = null
let W = 0, H = 0, dpr = 1

/* 静态帧使用的固定时间戳：保证每次重绘的光团/光环位置一致 */
const STATIC_TIME = 1600

/* 可复现随机 */
function mulberry32(seed: number) {
  return function () {
    seed |= 0; seed = (seed + 0x6d2b79f5) | 0
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

interface Particle { x: number; y: number; vx: number; vy: number; r: number; glow: boolean }
interface Blob { bx: number; by: number; r: number; c: string; ax: number; ay: number; sp: number; ph: number }
interface Light { x: number; y: number; w: number; h: number; c: string; ph: number; sp: number }
interface Bokeh { x: number; y: number; r: number; c: string; ph: number; sp: number; vy: number }
interface Ring { x: number; y: number; r: number; w: number; ph: number; sp: number; node: boolean }

let particles: Particle[] = []
let blobs: Blob[] = []
let lights: Light[] = []
let bokehs: Bokeh[] = []
let rings: Ring[] = []

const THEMES = {
  aurora: {
    bg: ['#d7e7ff', '#ecf4ff', '#e0f6fa'],
    bgAngle: 150,
    dot: [94, 156, 240] as const,
    line: [110, 150, 240] as const,
    glow: '255,255,255',
    blobColors: ['rgba(147,190,255,.50)', 'rgba(134,225,238,.45)', 'rgba(255,255,255,.75)', 'rgba(190,214,255,.45)'],
    linkDist: 130,
    density: 13000,
  },
  /* 门户：高饱和蓝青（左上留亮区保证深色文字可读，右下沉入饱和蓝） */
  vivid: {
    bg: ['#eaf7ff', '#9fd3fd', '#54a2f5', '#2f74e6', '#1f52bd'],
    bgAngle: 140,
    dot: [255, 255, 255] as const,
    line: [226, 244, 255] as const,
    glow: '255,255,255',
    blobColors: ['rgba(96,214,247,.55)', 'rgba(150,232,255,.50)', 'rgba(255,255,255,.80)', 'rgba(64,110,235,.42)'],
    linkDist: 140,
    density: 11000,
  },
  ocean: {
    bg: ['#0d3ea6', '#0f68c9', '#0ca3a3', '#14a862'],
    bgAngle: 135,
    dot: [255, 255, 255] as const,
    line: [255, 255, 255] as const,
    glow: '255,255,255',
    blobColors: ['rgba(255,255,255,.10)', 'rgba(84,224,200,.16)', 'rgba(70,140,255,.20)', 'rgba(52,211,153,.14)'],
    linkDist: 140,
    density: 12000,
  },
  night: {
    bg: ['#0a0518', '#1a0e3a', '#2a1650', '#0e0824'],
    bgAngle: 160,
    dot: [196, 181, 253] as const,
    line: [167, 139, 250] as const,
    glow: '240,200,104',
    blobColors: ['rgba(124,58,237,.22)', 'rgba(240,200,104,.10)', 'rgba(76,29,149,.30)'],
    linkDist: 0,
    density: 999999,
  },
} as const

function setup() {
  const canvas = canvasRef.value
  if (!canvas) return
  const rand = mulberry32(20261107)
  const theme = THEMES[props.theme]
  const rect = canvas.getBoundingClientRect()
  dpr = Math.min(window.devicePixelRatio || 1, 2)
  W = Math.max(rect.width, 1)
  H = Math.max(rect.height, 1)
  canvas.width = W * dpr
  canvas.height = H * dpr
  ctx = canvas.getContext('2d')
  if (ctx) ctx.scale(dpr, dpr)

  /* 漂浮光团 */
  blobs = theme.blobColors.map((c, i) => ({
    bx: rand() * W, by: rand() * H,
    r: Math.max(W, H) * (0.22 + rand() * 0.2),
    c,
    ax: W * (0.10 + rand() * 0.12), ay: H * (0.10 + rand() * 0.12),
    sp: 0.00012 + rand() * 0.00012, ph: rand() * Math.PI * 2,
  }))

  if (props.theme === 'vivid') {
    const base = Math.min(W, H)
    rings = [
      { x: W * 0.79, y: H * 0.16, r: base * 0.10, w: 2.2, ph: 0.4, sp: 0.00055, node: true },
      { x: W * 0.60, y: H * 0.02, r: base * 0.26, w: 1.2, ph: 2.1, sp: 0.00030, node: false },
      { x: W * 0.94, y: H * 0.66, r: base * 0.07, w: 1.6, ph: 3.6, sp: 0.00045, node: true },
      { x: W * 0.30, y: H * 0.98, r: base * 0.16, w: 1.4, ph: 1.2, sp: 0.00035, node: true },
    ]
  }

  if (props.theme === 'night') {
    /* 城市灯光：远景竖条窗光 */
    lights = []
    const n = Math.floor(W / 9)
    for (let i = 0; i < n; i++) {
      const yBase = H * (0.18 + rand() * 0.4)
      lights.push({
        x: rand() * W, y: yBase,
        w: 1 + rand() * 2.4, h: 3 + rand() * 9,
        c: rand() > 0.72 ? '240,200,104' : rand() > 0.5 ? '196,181,253' : '147,197,253',
        ph: rand() * Math.PI * 2, sp: 0.0008 + rand() * 0.002,
      })
    }
    /* 底部透视光点 */
    for (let i = 0; i < 46; i++) {
      const t = rand()
      lights.push({
        x: rand() * W, y: H * (0.72 + t * 0.26),
        w: 1 + t * 3, h: 1 + t * 2,
        c: rand() > 0.6 ? '240,200,104' : '196,181,253',
        ph: rand() * Math.PI * 2, sp: 0.0008 + rand() * 0.0022,
      })
    }
    /* 漂浮光斑 */
    bokehs = Array.from({ length: 26 }, () => ({
      x: rand() * W, y: rand() * H,
      r: 1 + rand() * 2.6,
      c: rand() > 0.7 ? '240,200,104' : '196,181,253',
      ph: rand() * Math.PI * 2, sp: 0.0006 + rand() * 0.0016,
      vy: 0.008 + rand() * 0.03,
    }))
  } else {
    /* 星网粒子 */
    const count = Math.min(90, Math.floor((W * H) / theme.density))
    particles = Array.from({ length: count }, () => ({
      x: rand() * W, y: rand() * H,
      vx: (rand() - 0.5) * 0.24, vy: (rand() - 0.5) * 0.24,
      r: 1.2 + rand() * 2.4,
      glow: rand() > 0.82,
    }))
  }
}

function drawBg(theme: (typeof THEMES)[keyof typeof THEMES], now: number) {
  if (!ctx) return
  const rad = (theme.bgAngle * Math.PI) / 180
  const g = ctx.createLinearGradient(
    W / 2 - (Math.cos(rad) * W) / 2, H / 2 - (Math.sin(rad) * H) / 2,
    W / 2 + (Math.cos(rad) * W) / 2, H / 2 + (Math.sin(rad) * H) / 2)
  const n: number = theme.bg.length
  theme.bg.forEach((c, i) => g.addColorStop(i / (n - 1), c))
  ctx.fillStyle = g
  ctx.fillRect(0, 0, W, H)
  /* 漂浮光团 */
  for (const b of blobs) {
    const x = b.bx + Math.cos(now * b.sp + b.ph) * b.ax
    const y = b.by + Math.sin(now * b.sp * 1.3 + b.ph) * b.ay
    const rg = ctx.createRadialGradient(x, y, 0, x, y, b.r)
    rg.addColorStop(0, b.c)
    rg.addColorStop(1, 'rgba(255,255,255,0)')
    ctx.fillStyle = rg
    ctx.fillRect(x - b.r, y - b.r, b.r * 2, b.r * 2)
  }
}

function drawPlexus(theme: (typeof THEMES)[keyof typeof THEMES], move: boolean) {
  if (!ctx) return
  const { linkDist } = theme
  if (move) {
    for (const p of particles) {
      p.x += p.vx; p.y += p.vy
      if (p.x < -20) p.x = W + 20; if (p.x > W + 20) p.x = -20
      if (p.y < -20) p.y = H + 20; if (p.y > H + 20) p.y = -20
    }
  }
  ctx.lineWidth = 1
  for (let i = 0; i < particles.length; i++) {
    for (let j = i + 1; j < particles.length; j++) {
      const a = particles[i], b = particles[j]
      const dx = a.x - b.x, dy = a.y - b.y
      const d2 = dx * dx + dy * dy
      if (d2 < linkDist * linkDist) {
        const alpha = (1 - Math.sqrt(d2) / linkDist) * 0.5
        ctx.strokeStyle = `rgba(${theme.line[0]},${theme.line[1]},${theme.line[2]},${alpha})`
        ctx.beginPath(); ctx.moveTo(a.x, a.y); ctx.lineTo(b.x, b.y); ctx.stroke()
      }
    }
  }
  for (const p of particles) {
    if (p.glow) {
      const rg = ctx.createRadialGradient(p.x, p.y, 0, p.x, p.y, p.r * 6)
      rg.addColorStop(0, `rgba(${theme.glow},.85)`)
      rg.addColorStop(1, 'rgba(255,255,255,0)')
      ctx.fillStyle = rg
      ctx.beginPath(); ctx.arc(p.x, p.y, p.r * 6, 0, Math.PI * 2); ctx.fill()
    }
    ctx.fillStyle = `rgba(${theme.dot[0]},${theme.dot[1]},${theme.dot[2]},.95)`
    ctx.beginPath(); ctx.arc(p.x, p.y, p.r, 0, Math.PI * 2); ctx.fill()
  }
}

function drawNight(t: number, move: boolean) {
  if (!ctx) return
  /* 城市灯光（闪烁） */
  for (const l of lights) {
    const a = 0.25 + 0.75 * Math.abs(Math.sin(t * l.sp + l.ph))
    ctx.fillStyle = `rgba(${l.c},${a})`
    ctx.fillRect(l.x, l.y, l.w, l.h)
  }
  /* 流光波浪：紫金渐变三层 */
  const layers = [
    { base: 0.62, amp: 46, k: 0.0042, sp: 0.00045, w: 2.4, o: 0.9 },
    { base: 0.68, amp: 38, k: 0.0034, sp: -0.00035, w: 1.5, o: 0.55 },
    { base: 0.56, amp: 34, k: 0.0050, sp: 0.00028, w: 1.1, o: 0.35 },
  ]
  ctx.lineCap = 'round'
  for (const L of layers) {
    const grad = ctx.createLinearGradient(0, 0, W, 0)
    grad.addColorStop(0, 'rgba(124,58,237,0)')
    grad.addColorStop(0.42, `rgba(167,139,250,${L.o})`)
    grad.addColorStop(0.75, `rgba(240,200,104,${L.o})`)
    grad.addColorStop(1, 'rgba(240,200,104,0)')
    ctx.strokeStyle = grad
    ctx.lineWidth = L.w
    ctx.beginPath()
    for (let x = 0; x <= W; x += 4) {
      const y = H * L.base
        + Math.sin(x * L.k + t * L.sp) * L.amp
        + Math.sin(x * L.k * 2.3 + t * L.sp * 1.7) * L.amp * 0.35
      if (x === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y)
    }
    ctx.stroke()
    /* 波峰上的光点 */
    for (let i = 1; i <= 3; i++) {
      const x = (W * i) / 3.6 + Math.sin(t * 0.0002 + i * 2) * 30
      const y = H * L.base + Math.sin(x * L.k + t * L.sp) * L.amp + Math.sin(x * L.k * 2.3 + t * L.sp * 1.7) * L.amp * 0.35
      const rg = ctx.createRadialGradient(x, y, 0, x, y, 10)
      rg.addColorStop(0, `rgba(240,200,104,${0.8 * L.o})`)
      rg.addColorStop(1, 'rgba(240,200,104,0)')
      ctx.fillStyle = rg
      ctx.beginPath(); ctx.arc(x, y, 10, 0, Math.PI * 2); ctx.fill()
    }
  }
  /* 漂浮光斑 */
  for (const b of bokehs) {
    if (move) {
      b.y -= b.vy
      if (b.y < -6) b.y = H + 6
    }
    const a = 0.2 + 0.6 * Math.abs(Math.sin(t * b.sp + b.ph))
    ctx.fillStyle = `rgba(${b.c},${a})`
    ctx.beginPath(); ctx.arc(b.x, b.y, b.r, 0, Math.PI * 2); ctx.fill()
  }
}

function drawVivid(t: number) {
  if (!ctx) return
  /* 斜向光带 */
  ctx.save()
  ctx.globalCompositeOperation = 'lighter'
  const beams = [
    { off: 0.46, w: 130, a: 0.12 },
    { off: 0.66, w: 240, a: 0.09 },
    { off: 0.90, w: 90, a: 0.15 },
  ]
  for (const b of beams) {
    const x0 = W * b.off - H * 0.18
    const g = ctx.createLinearGradient(x0, 0, x0 - H * 0.30, H)
    g.addColorStop(0, `rgba(255,255,255,${b.a})`)
    g.addColorStop(0.55, `rgba(214,246,255,${b.a * 0.5})`)
    g.addColorStop(1, 'rgba(255,255,255,0)')
    ctx.fillStyle = g
    ctx.beginPath()
    ctx.moveTo(x0, 0)
    ctx.lineTo(x0 + b.w, 0)
    ctx.lineTo(x0 + b.w - H * 0.30, H)
    ctx.lineTo(x0 - H * 0.30, H)
    ctx.closePath()
    ctx.fill()
  }
  /* 光环 + 环上光点 */
  for (const R of rings) {
    const pulse = 0.5 + 0.5 * Math.sin(t * R.sp + R.ph)
    const halo = ctx.createRadialGradient(R.x, R.y, 0, R.x, R.y, R.r)
    halo.addColorStop(0, `rgba(255,255,255,${0.10 + 0.10 * pulse})`)
    halo.addColorStop(1, 'rgba(255,255,255,0)')
    ctx.fillStyle = halo
    ctx.beginPath(); ctx.arc(R.x, R.y, R.r, 0, Math.PI * 2); ctx.fill()

    ctx.strokeStyle = `rgba(255,255,255,${0.30 + 0.40 * pulse})`
    ctx.lineWidth = R.w
    ctx.beginPath(); ctx.arc(R.x, R.y, R.r, 0, Math.PI * 2); ctx.stroke()

    if (R.node) {
      const ang = t * R.sp * 5 + R.ph
      const nx = R.x + Math.cos(ang) * R.r
      const ny = R.y + Math.sin(ang) * R.r
      const nr = Math.max(10, R.r * 0.22)
      const ng = ctx.createRadialGradient(nx, ny, 0, nx, ny, nr)
      ng.addColorStop(0, 'rgba(255,255,255,.95)')
      ng.addColorStop(1, 'rgba(255,255,255,0)')
      ctx.fillStyle = ng
      ctx.beginPath(); ctx.arc(nx, ny, nr, 0, Math.PI * 2); ctx.fill()
    }
  }
  ctx.restore()
}

/** 绘制一帧；move=true 时推进粒子/光斑位置（动效），false 时定格（静态背景） */
function renderFrame(t: number, move: boolean) {
  const theme = THEMES[props.theme]
  drawBg(theme, move ? Date.now() : STATIC_TIME)
  if (props.theme === 'night') drawNight(t, move)
  else {
    if (props.theme === 'vivid') drawVivid(t)
    drawPlexus(theme, move)
  }
}

function loop(t: number) {
  if (!running) return
  renderFrame(t, true)
  raf = requestAnimationFrame(loop)
}

function start() {
  if (running || !ctx) return
  running = true
  raf = requestAnimationFrame(loop)
}

function stop() {
  running = false
  cancelAnimationFrame(raf)
}

function renderStatic() { renderFrame(STATIC_TIME, false) }

function resize() {
  setup()
  if (!bgAnimEnabled.value) renderStatic()
}

onMounted(() => {
  setup()
  if (bgAnimEnabled.value) start()
  else renderStatic()
  window.addEventListener('resize', resize)
})

watch(bgAnimEnabled, (on) => {
  if (on) start()
  else { stop(); renderStatic() }
})

onBeforeUnmount(() => {
  stop()
  window.removeEventListener('resize', resize)
})
</script>

<template>
  <canvas ref="canvasRef" class="net-bg" aria-hidden="true"></canvas>
</template>

<style scoped>
.net-bg { position: absolute; inset: 0; width: 100%; height: 100%; display: block; }
</style>
