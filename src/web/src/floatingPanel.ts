export type Point = { x: number; y: number }
export type Box = { left: number; top: number; right: number; bottom: number }

const MARGIN = 12
const GAP = 12

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), Math.max(min, max))
}

function overlapArea(a: Box, b: Box) {
  const w = Math.min(a.right, b.right) - Math.max(a.left, b.left)
  const h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top)
  return w > 0 && h > 0 ? w * h : 0
}

function distanceToCenter(point: Point, box: Box) {
  return Math.hypot(point.x - (box.left + box.right) / 2, point.y - (box.top + box.bottom) / 2)
}

export function boxFromRect(rect: DOMRect | null | undefined): Box | null {
  if (!rect) return null
  return { left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom }
}

/** 在视口内放置浮层：优先不遮挡触发元素，其次靠近点击位置。 */
export function placeFloatingPanel(anchor: Point, size: { width: number; height: number }, avoid?: Box | null): Point {
  const vw = window.innerWidth
  const vh = window.innerHeight
  const w = Math.max(80, Math.min(size.width, vw - MARGIN * 2))
  const h = Math.max(60, Math.min(size.height, vh - MARGIN * 2))
  const candidates: Point[] = []
  if (avoid) {
    candidates.push(
      { x: avoid.right + GAP, y: (avoid.top + avoid.bottom - h) / 2 },
      { x: avoid.left - w - GAP, y: (avoid.top + avoid.bottom - h) / 2 },
      { x: (avoid.left + avoid.right - w) / 2, y: avoid.bottom + GAP },
      { x: (avoid.left + avoid.right - w) / 2, y: avoid.top - h - GAP },
    )
  }
  candidates.push(
    { x: anchor.x + GAP, y: anchor.y - h / 2 },
    { x: anchor.x - w - GAP, y: anchor.y - h / 2 },
    { x: anchor.x - w / 2, y: anchor.y + GAP },
    { x: anchor.x - w / 2, y: anchor.y - h - GAP },
  )
  let best = { x: MARGIN, y: MARGIN }
  let bestScore = Infinity
  for (const c of candidates) {
    const x = clamp(c.x, MARGIN, vw - w - MARGIN)
    const y = clamp(c.y, MARGIN, vh - h - MARGIN)
    const box: Box = { left: x, top: y, right: x + w, bottom: y + h }
    const score = (avoid ? overlapArea(box, avoid) * 100 : 0) + distanceToCenter(anchor, box)
    if (score < bestScore) { bestScore = score; best = { x, y } }
  }
  return best
}
