export type TopoBox = { id: string; x: number; y: number; width: number; height: number }
export type TopoEdgeLike = { from: string; to: string }
export type EdgeGeometry = { d: string; angle: string; minX: number; minY: number; maxX: number; maxY: number }

type Point = { x: number; y: number }
type Side = 'left' | 'right' | 'top' | 'bottom'
type Rect = { left: number; top: number; right: number; bottom: number }
type Curve = { p0: Point; p1: Point; p2: Point; p3: Point }
type Slot = { index: number; total: number }
type Route = { edge: TopoEdgeLike; curve: Curve; fromSide: Side; toSide: Side }
type Attachment = { edge: TopoEdgeLike; peer: TopoBox; kind: 'from' | 'to' }

const GAP = 6
const PAD = 12
const SIDES: Side[] = ['right', 'left', 'bottom', 'top']

/** 双向连线中固定一条作为次线，用于换边、错开锚点和着色 */
export function isBidirectionalSecondary(edge: TopoEdgeLike, allEdges: TopoEdgeLike[] = []): boolean {
  if (!allEdges.some(e => e.from === edge.to && e.to === edge.from)) return false
  return String(edge.from) > String(edge.to)
}

function outward(side: Side): Point {
  if (side === 'left') return { x: -1, y: 0 }
  if (side === 'right') return { x: 1, y: 0 }
  if (side === 'top') return { x: 0, y: -1 }
  return { x: 0, y: 1 }
}

function anchor(box: TopoBox, side: Side, slot: Slot = { index: 0, total: 1 }, bias = 0): Point {
  const v = outward(side)
  const base = {
    x: box.x + box.width / 2 + v.x * box.width / 2,
    y: box.y + box.height / 2 + v.y * box.height / 2,
  }
  const sideLength = v.x ? box.height : box.width
  const baseRatio = slot.total > 1 ? (slot.index + 0.5) / slot.total : 0.5
  const ratio = Math.max(0.08, Math.min(0.92, baseRatio + bias))
  const along = (ratio - 0.5) * sideLength * 0.9
  return {
    x: base.x + (v.y ? along : 0) + v.x * GAP,
    y: base.y + (v.x ? along : 0) + v.y * GAP,
  }
}

function rectOf(box: TopoBox): Rect {
  return {
    left: box.x - PAD, top: box.y - PAD,
    right: box.x + box.width + PAD, bottom: box.y + box.height + PAD,
  }
}

function pointInRect(p: Point, r: Rect) {
  return p.x > r.left && p.x < r.right && p.y > r.top && p.y < r.bottom
}

function orient(a: Point, b: Point, c: Point) {
  return (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x)
}

function onSegment(a: Point, b: Point, p: Point) {
  return Math.abs(orient(a, b, p)) < 1e-6
    && p.x >= Math.min(a.x, b.x) - 1e-6 && p.x <= Math.max(a.x, b.x) + 1e-6
    && p.y >= Math.min(a.y, b.y) - 1e-6 && p.y <= Math.max(a.y, b.y) + 1e-6
}

function segmentsIntersect(a: Point, b: Point, c: Point, d: Point) {
  const o1 = orient(a, b, c), o2 = orient(a, b, d)
  const o3 = orient(c, d, a), o4 = orient(c, d, b)
  if (o1 * o2 < 0 && o3 * o4 < 0) return true
  return onSegment(a, b, c) || onSegment(a, b, d) || onSegment(c, d, a) || onSegment(c, d, b)
}

function curveIntersectsRect(c: Curve, r: Rect, steps = 48) {
  const pts = sampleCurve(c, steps)
  const corners = [
    { x: r.left, y: r.top }, { x: r.right, y: r.top },
    { x: r.right, y: r.bottom }, { x: r.left, y: r.bottom },
  ]
  for (let i = 1; i < pts.length; i++) {
    const a = pts[i - 1], b = pts[i]
    if (pointInRect(a, r) || pointInRect(b, r)) return true
    for (let k = 0; k < 4; k++) {
      if (segmentsIntersect(a, b, corners[k], corners[(k + 1) % 4])) return true
    }
  }
  return false
}

function curveCrossings(c: Curve, others: Curve[]) {
  const a = sampleCurve(c, 24)
  let count = 0
  for (const other of others) {
    const b = sampleCurve(other, 24)
    outer: for (let i = 2; i < a.length - 2; i++) {
      for (let j = 2; j < b.length - 2; j++) {
        if (segmentsIntersect(a[i - 1], a[i], b[j - 1], b[j])) { count++; break outer }
      }
    }
  }
  return count
}

function cubicAt(c: Curve, t: number): Point {
  const mt = 1 - t
  const a = mt * mt * mt, b = 3 * mt * mt * t, d = 3 * mt * t * t, e = t * t * t
  return {
    x: a * c.p0.x + b * c.p1.x + d * c.p2.x + e * c.p3.x,
    y: a * c.p0.y + b * c.p1.y + d * c.p2.y + e * c.p3.y,
  }
}

function sampleCurve(c: Curve, steps = 32): Point[] {
  return Array.from({ length: steps + 1 }, (_, i) => cubicAt(c, i / steps))
}

function pathLength(points: Point[]) {
  let len = 0
  for (let i = 1; i < points.length; i++) len += Math.hypot(points[i].x - points[i - 1].x, points[i].y - points[i - 1].y)
  return len
}

/** 取末端一小段上各折角的单位方向，按弧长与靠近终点程度加权平均 */
function endAngle(c: Curve): number {
  const steps = 10
  const start = 0.84
  const points = Array.from({ length: steps + 1 }, (_, i) => cubicAt(c, start + (1 - start) * i / steps))
  let sx = 0, sy = 0
  for (let i = 1; i < points.length; i++) {
    const dx = points[i].x - points[i - 1].x
    const dy = points[i].y - points[i - 1].y
    const len = Math.hypot(dx, dy)
    if (len < 1e-6) continue
    const weight = len * (i / steps)
    sx += dx / len * weight
    sy += dy / len * weight
  }
  if (Math.hypot(sx, sy) < 1e-6) return Math.atan2(c.p3.y - c.p2.y, c.p3.x - c.p2.x) * 180 / Math.PI
  return Math.atan2(sy, sx) * 180 / Math.PI
}

function preferredSides(a: TopoBox, b: TopoBox): [Side, Side] {
  const dx = (b.x + b.width / 2) - (a.x + a.width / 2)
  const dy = (b.y + b.height / 2) - (a.y + a.height / 2)
  if (Math.abs(dx) >= Math.abs(dy)) return dx >= 0 ? ['right', 'left'] : ['left', 'right']
  return dy >= 0 ? ['bottom', 'top'] : ['top', 'bottom']
}

function sideKey(nodeId: string, side: Side) { return `${nodeId}|${side}` }

function sideSlot(edge: TopoEdgeLike, boxes: TopoBox[], allEdges: TopoEdgeLike[] | undefined, side: Side, kind: 'from' | 'to', chosen?: Map<string, Attachment[]>): Slot {
  if (!allEdges) return { index: 0, total: 1 }
  const nodeId = kind === 'from' ? edge.from : edge.to
  const coord = (box: TopoBox) => side === 'left' || side === 'right'
    ? box.y + box.height / 2
    : box.x + box.width / 2
  let attachments = chosen?.get(sideKey(nodeId, side))
  if (!attachments) {
    attachments = []
    allEdges.forEach(e => {
      const a = boxes.find(x => x.id === e.from), b = boxes.find(x => x.id === e.to)
      if (!a || !b || a.id === b.id) return
      const preferred = preferredSides(a, b)
      if (e.from === nodeId && preferred[0] === side) attachments!.push({ edge: e, peer: b, kind: 'from' })
      if (e.to === nodeId && preferred[1] === side) attachments!.push({ edge: e, peer: a, kind: 'to' })
    })
  }
  if (!attachments.length) return { index: 0, total: 1 }
  attachments = [...attachments].sort((x, y) => coord(x.peer) - coord(y.peer))
  const index = attachments.findIndex(item => item.edge === edge && item.kind === kind)
  return { index: Math.max(0, index), total: attachments.length }
}

function curveBetween(a: TopoBox, b: TopoBox, fromSide: Side, toSide: Side, factor: number, fromSlot: Slot, toSlot: Slot, bias = 0): Curve {
  const p0 = anchor(a, fromSide, fromSlot, bias)
  const p3 = anchor(b, toSide, toSlot, bias)
  const direct = Math.hypot(p3.x - p0.x, p3.y - p0.y)
  const bend = Math.max(36, Math.min(240, direct * factor))
  const fv = outward(fromSide), tv = outward(toSide)
  return {
    p0,
    p1: { x: p0.x + fv.x * bend, y: p0.y + fv.y * bend },
    p2: { x: p3.x + tv.x * bend, y: p3.y + tv.y * bend },
    p3,
  }
}

function selfLoop(box: TopoBox): Curve {
  const p0 = anchor(box, 'top')
  const p3 = anchor(box, 'right')
  const loop = Math.max(box.width, box.height) * 0.55 + GAP
  return {
    p0,
    p1: { x: p0.x, y: p0.y - loop },
    p2: { x: p3.x + loop, y: p3.y },
    p3,
  }
}

function fmt(n: number) { return Math.round(n * 10) / 10 }

function toGeometry(c: Curve): EdgeGeometry {
  const angle = endAngle(c)
  const points = [c.p0, c.p1, c.p2, c.p3, ...sampleCurve(c, 40)]
  const xs = points.map(p => p.x), ys = points.map(p => p.y)
  return {
    d: `M ${fmt(c.p0.x)} ${fmt(c.p0.y)} C ${fmt(c.p1.x)} ${fmt(c.p1.y)}, ${fmt(c.p2.x)} ${fmt(c.p2.y)}, ${fmt(c.p3.x)} ${fmt(c.p3.y)}`,
    angle: `${angle.toFixed(2)}deg`,
    minX: Math.min(...xs), minY: Math.min(...ys),
    maxX: Math.max(...xs), maxY: Math.max(...ys),
  }
}

function resolveCurve(edge: TopoEdgeLike, boxes: TopoBox[], allEdges: TopoEdgeLike[], considerCrossings: boolean, chosen?: Map<string, Attachment[]>, precomputed?: Route[]): Route {
  const a = boxes.find(x => x.id === edge.from)
  const b = boxes.find(x => x.id === edge.to)
  const fallback = { id: '', x: 0, y: 0, width: 0, height: 0 }
  if (!a || !b) return { edge, curve: selfLoop(a || b || fallback), fromSide: 'top', toSide: 'right' }
  if (a.id === b.id) return { edge, curve: selfLoop(a), fromSide: 'top', toSide: 'right' }

  const preferred = preferredSides(a, b)
  const reverse = allEdges.some(e => e.from === edge.to && e.to === edge.from)
  const secondary = isBidirectionalSecondary(edge, allEdges)
  const alternative: [Side, Side] = preferred[0] === 'right' || preferred[0] === 'left' ? ['bottom', 'top'] : ['right', 'left']
  const bias = reverse ? (secondary ? 0.18 : -0.18) : 0
  const obstacles = boxes.filter(x => x.id !== a.id && x.id !== b.id).map(rectOf)
  const otherCurves = considerCrossings
    ? (precomputed || []).filter(r => r.edge !== edge).map(r => r.curve)
    : []
  const factors = [0.3, 0.55, 0.85, 1.2]
  let best: Curve | null = null
  let bestFrom: Side = preferred[0]
  let bestTo: Side = preferred[1]
  let bestScore = Infinity
  for (const fromSide of SIDES) {
    for (const toSide of SIDES) {
      const fromSlot = sideSlot(edge, boxes, allEdges, fromSide, 'from', chosen)
      const toSlot = sideSlot(edge, boxes, allEdges, toSide, 'to', chosen)
      for (const factor of factors) {
        const curve = curveBetween(a, b, fromSide, toSide, factor, fromSlot, toSlot, bias)
        const points = sampleCurve(curve, 40)
        let collisions = 0
        for (const r of obstacles) {
          if (curveIntersectsRect(curve, r)) { collisions++; break }
        }
        const crossings = considerCrossings ? curveCrossings(curve, otherCurves) : 0
        const penalty = (fromSide === preferred[0] ? 0 : 1) + (toSide === preferred[1] ? 0 : 1)
        const separation = secondary && fromSide === alternative[0] && toSide === alternative[1] ? -8000 : 0
        const score = collisions * 12000 + crossings * 2500 + penalty * 500 + separation + pathLength(points) + factor * 10
        if (score < bestScore) { bestScore = score; best = curve; bestFrom = fromSide; bestTo = toSide }
      }
    }
  }
  return { edge, curve: best!, fromSide: bestFrom, toSide: bestTo }
}

/**
 * 连线优先选择最贴近两框相对方位的侧边；同一节点同一侧（含流入/流出）沿边框分散锚点，
 * 并按对端位置排序形成扇形；通过穿框检测与交叉惩罚减少交叉；箭头角度取末端折角加权平均方向。
 */
export function topoEdgeGeometry(edge: TopoEdgeLike, boxes: TopoBox[], allEdges?: TopoEdgeLike[]): EdgeGeometry {
  const a = boxes.find(x => x.id === edge.from)
  const b = boxes.find(x => x.id === edge.to)
  if (!a || !b) return { d: '', angle: '0deg', minX: 0, minY: 0, maxX: 0, maxY: 0 }
  const all = allEdges || []
  const baseRoutes = all.filter(e => boxes.some(x => x.id === e.from) && boxes.some(x => x.id === e.to))
    .map(e => resolveCurve(e, boxes, all, false))
  const chosen = new Map<string, Attachment[]>()
  baseRoutes.forEach(r => {
    const source = boxes.find(x => x.id === r.edge.from)
    const target = boxes.find(x => x.id === r.edge.to)
    if (!source || !target) return
    const fromKey = sideKey(source.id, r.fromSide)
    const toKey = sideKey(target.id, r.toSide)
    if (!chosen.has(fromKey)) chosen.set(fromKey, [])
    if (!chosen.has(toKey)) chosen.set(toKey, [])
    chosen.get(fromKey)!.push({ edge: r.edge, peer: target, kind: 'from' })
    chosen.get(toKey)!.push({ edge: r.edge, peer: source, kind: 'to' })
  })
  return toGeometry(resolveCurve(edge, boxes, all, true, chosen, baseRoutes).curve)
}
