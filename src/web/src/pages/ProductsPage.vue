<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import { api, statusText } from '../api'
import { usePermissions } from '../permissions'
import { boxFromRect, placeFloatingPanel, type Box, type Point } from '../floatingPanel'
import { isBidirectionalSecondary, topoEdgeGeometry, type TopoBox } from '../topoEdge'
import { createHistory } from '../history'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import ProductImage from '../components/ProductImage.vue'
import ImageHoverPreview from '../components/ImageHoverPreview.vue'
import FilterSelect from '../components/FilterSelect.vue'

const processOptions = computed(() => processes.value.map(p => ({ value: p.id, label: `${p.process_code} · ${p.name}` })))
const availableProcessOptions = computed(() => {
  const kw = processPickerKeyword.value.trim().toLowerCase()
  return processOptions.value.filter(o => !nodes.value.some(n => n.process_id === o.value)
    && (!kw || o.label.toLowerCase().includes(kw)))
})
const orderOptions = computed(() => projects.value.map(p => ({ value: p.id, label: `${p.project_code} · ${p.product_name || p.style_no || '未命名订单'}` })))

const tab = ref<'products' | 'processes' | 'topology'>('products')
const products = ref<any[]>([])
const productKeyword = ref('')
const productFilter = ref('')
const filteredProducts = computed(() => {
  const kw = productFilter.value.trim().toLowerCase()
  if (!kw) return products.value
  return products.value.filter((p: any) =>
    [p.product_code, p.name, p.spec, p.configurer_name]
      .some(v => String(v || '').toLowerCase().includes(kw)))
})
const productPage = ref(1)
const productPageSize = ref(10)
const pagedProducts = computed(() => {
  const start = (productPage.value - 1) * productPageSize.value
  return filteredProducts.value.slice(start, start + productPageSize.value)
})
const productPageCount = computed(() => Math.max(1, Math.ceil(filteredProducts.value.length / productPageSize.value)))
function applyProductSearch() { productFilter.value = productKeyword.value; productPage.value = 1 }
const processes = ref<any[]>([])
const projects = ref<any[]>([])
const loading = ref(false)
const error = ref('')
const notice = ref('')
const showProduct = ref(false)
const showRoute = ref(false)
const saving = ref(false)
const productForm = reactive({ product_code: '', name: '', spec: '', unit: '件', sale_price: '0' })
const processForm = reactive({ process_code: '', name: '', default_unit_price: '0.00', default_report_ratio: '1', standard_work_seconds: 0 })
const current = ref<any>(null)
const editingProcessRow = ref<string | null>(null)

/* ---- 拓扑编辑状态 ---- */
type TopoNode = { key: string; id: string; process_id: string; _lastPid: string; name: string; code: string; sequence_no: number; unit_price: string; report_ratio: string; department_id: string | null; standard_work_seconds: number; is_critical: boolean; is_required: boolean; remark: string | null; node_x: number; node_y: number }
type TopoEdge = { from: string; to: string }
const nodes = ref<TopoNode[]>([])
const edges = ref<TopoEdge[]>([])
const routeMeta = ref<{ name?: string | null; at?: number | null }>({})
const selectedOrderID = ref('')
const linkSourceKey = ref('')
const selectedNodeKey = ref('')
const selectedEdgeIndex = ref(-1)
const inspectorPos = ref<Point | null>(null)
const inspectorRef = ref<HTMLElement | null>(null)
const inspectorAnchor = ref<Point | null>(null)
const inspectorAvoid = ref<Box | null>(null)
const processPickerOpen = ref(false)
const processPickerKeyword = ref('')
const processPickerSelected = ref<string[]>([])
type CtxMenu = { show: boolean; x: number; y: number; type: 'node' | 'edge'; nodeKey: string; edgeIndex: number }
const ctxMenu = ref<CtxMenu>({ show: false, x: 0, y: 0, type: 'node', nodeKey: '', edgeIndex: -1 })
const svgRef = ref<SVGSVGElement | null>(null)
const showTopoTips = ref(true)
const NODE_W = 168
const NODE_H = 72
let keySeq = 0
function newKey() { return `n-${Date.now()}-${++keySeq}` }

/* ---- 撤销/重做 ---- */
type ProdSnapshot = { nodes: TopoNode[]; edges: TopoEdge[] }
const history = createHistory<ProdSnapshot>()
const canUndo = history.canUndo
const canRedo = history.canRedo
function snapshot(): ProdSnapshot { return { nodes: nodes.value, edges: edges.value } }
function applySnapshot(s: ProdSnapshot) {
  nodes.value = s.nodes; edges.value = s.edges
  selectedNodeKey.value = ''; selectedEdgeIndex.value = -1; linkSourceKey.value = ''
  closeInspector(); closeCtxMenu(); refreshBounds()
}
function mutate(fn: () => void) { history.begin(snapshot()); fn(); history.commit(snapshot()) }
function undo() { const s = history.undo(snapshot()); if (s) applySnapshot(s) }
function redo() { const s = history.redo(snapshot()); if (s) applySnapshot(s) }

const { has } = usePermissions()
const canProductWrite = computed(() => has('product:write'))
const canProcessWrite = computed(() => has('process:write'))

async function load() {
  loading.value = true; error.value = ''; productPage.value = 1
  try {
    const [productData, processData] = await Promise.all([
      api<{ items: any[] }>('/products'),
      api<{ items: any[] }>('/process-definitions').catch(() => ({ items: [] as any[] })),
    ])
    products.value = productData.items
    processes.value = processData.items
    if (!processData.items.length) notice.value = '当前账号暂无工序库读取权限，工序相关操作不可用。'
  } catch (e) {
    products.value = []; processes.value = []
    error.value = (e as Error).message
  } finally { loading.value = false }
}
function changeProductPage(delta: number) { productPage.value = Math.min(productPageCount.value, Math.max(1, productPage.value + delta)) }
function onProductPageSizeChange() { productPage.value = 1 }
async function createProduct() {
  saving.value = true; error.value = ''
  try { await api('/products', { method: 'POST', body: JSON.stringify({ ...productForm, spec: productForm.spec || null, sale_price: productForm.sale_price || '0' }) }); showProduct.value = false; notice.value = '产品已保存为草稿，请继续配置工序。'; await load() } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
function fmtPrice(v: any) { const n = Number(v); return Number.isFinite(n) ? n.toFixed(2) : '0.00' }
function fmtRatio(v: any) { const n = Number(v); return Number.isFinite(n) ? String(Math.round(n)) : '0' }
function resetProcessForm() {
  Object.assign(processForm, { process_code: '', name: '', default_unit_price: '0.00', default_report_ratio: '1', standard_work_seconds: 0 })
}
async function startCreateProcess() {
  tab.value = 'processes'; editingProcessRow.value = '__new__'; error.value = ''; resetProcessForm()
  await nextTick()
  const input = document.querySelector('.row-editing input') as HTMLInputElement | null
  input?.scrollIntoView({ block: 'center' }); input?.focus()
}
function startEditProcess(item: any) {
  editingProcessRow.value = item.id; error.value = ''
  Object.assign(processForm, { process_code: item.process_code, name: item.name, default_unit_price: fmtPrice(item.default_unit_price), default_report_ratio: fmtRatio(item.default_report_ratio), standard_work_seconds: item.standard_work_seconds })
}
function cancelProcessRow() { editingProcessRow.value = null; error.value = '' }
async function saveProcess() {
  saving.value = true; error.value = ''
  try {
    const isNew = editingProcessRow.value === '__new__'
    const editing = isNew ? null : processes.value.find(p => p.id === editingProcessRow.value)
    if (editing) await api(`/process-definitions/${editing.id}`, { method: 'PATCH', body: JSON.stringify({ id: editing.id, ...processForm, version: editing.version }) })
    else await api('/process-definitions', { method: 'POST', body: JSON.stringify(processForm) })
    editingProcessRow.value = null; notice.value = isNew ? '工序已新增' : '工序已保存'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function deleteProcess(item: any) {
  if (!window.confirm(`确认删除工序 ${item.name}？已用于生产的工序将被拒绝。`)) return
  if (editingProcessRow.value === item.id) cancelProcessRow()
  error.value = ''
  try { await api(`/process-definitions/${item.id}`, { method: 'DELETE' }); notice.value = '工序已删除'; await load() } catch (e) { error.value = (e as Error).message }
}

/* ---- 拓扑：打开 / 载入 ---- */
/* 选订单反查产品：优先外键，其次款号匹配产品编号，最后产品名匹配 */
function resolveProduct(order: any) {
  if (order.product_id) {
    const byID = products.value.find(p => p.id === order.product_id)
    if (byID) return byID
  }
  if (order.style_no) {
    const byCode = products.value.find(p => p.product_code === order.style_no)
    if (byCode) return byCode
  }
  if (order.product_name) {
    const byName = products.value.find(p => p.name === order.product_name)
    if (byName) return byName
  }
  return null
}
async function onOrderChange() {
  error.value = ''; notice.value = ''
  const order = projects.value.find(p => p.id === selectedOrderID.value)
  if (!order) return
  const product = resolveProduct(order)
  if (!product) { error.value = '未找到该订单对应的产品（产品编号/名称均未匹配），请先在【产品资料】建立对应产品。'; return }
  await editRoutes(product)
}
async function editRoutes(product: any) {
  error.value = ''; current.value = product
  linkSourceKey.value = ''; selectedNodeKey.value = ''; selectedEdgeIndex.value = -1; inspectorPos.value = null; ctxMenu.value.show = false
  try {
    const [detail, routeData] = await Promise.all([
      api<any>(`/products/${product.id}`),
      api<{ items: any[]; edges?: any[]; configurer_name?: string | null; configured_at?: number | null }>(`/products/${product.id}/processes`),
    ])
    current.value = detail
    nodes.value = routeData.items.map((it: any) => ({
      key: newKey(), id: it.id || '', process_id: it.process_id, _lastPid: it.process_id, name: it.name, code: it.process_code,
      sequence_no: it.sequence_no, unit_price: it.unit_price, report_ratio: it.report_ratio,
      department_id: it.department_id, standard_work_seconds: it.standard_work_seconds,
      is_critical: !!it.is_critical, is_required: it.is_required !== false, remark: it.remark,
      node_x: it.node_x || 0, node_y: it.node_y || 0,
    }))
    edges.value = (routeData.edges || []).map((e: any) => ({ from: e.from, to: e.to }))
    routeMeta.value = { name: routeData.configurer_name, at: routeData.configured_at }
    if (nodes.value.length && !nodes.value.some(n => n.node_x || n.node_y)) autoLayout(false)
    else refreshBounds()
    showRoute.value = true
    history.reset()
    nextTick(zoomFit)
  } catch (e) { error.value = (e as Error).message }
}

/* ---- 拓扑：节点 / 连线操作 ---- */
function openProcessPicker() {
  if (!canProductWrite.value) { error.value = '当前账号没有工序写入权限，无法添加工序'; return }
  processPickerKeyword.value = ''
  processPickerSelected.value = []
  error.value = ''
  processPickerOpen.value = true
}
function toggleProcessPick(id: string, checked: boolean) {
  if (checked && !processPickerSelected.value.includes(id)) processPickerSelected.value.push(id)
  if (!checked) processPickerSelected.value = processPickerSelected.value.filter(v => v !== id)
}
function confirmProcessPicker() {
  const chosen = processes.value.filter(p => processPickerSelected.value.includes(p.id)
    && !nodes.value.some(n => n.process_id === p.id))
  if (!chosen.length) { error.value = '请至少选择一个工序'; return }
  mutate(() => {
    const layer = nodes.value.length ? Math.max(...nodes.value.map(n => n.node_y)) + NODE_H + 40 : 40
    chosen.forEach((process, i) => {
      nodes.value.push({
        key: newKey(), id: '', process_id: process.id, _lastPid: process.id, name: process.name, code: process.process_code,
        sequence_no: nodes.value.length + 1, unit_price: process.default_unit_price, report_ratio: process.default_report_ratio,
        department_id: process.default_department_id || null, standard_work_seconds: process.standard_work_seconds,
        is_critical: false, is_required: true, remark: null,
        node_x: 60 + (i % 4) * (NODE_W + 36), node_y: layer + Math.floor(i / 4) * (NODE_H + 30),
      })
    })
    refreshBounds()
  })
  processPickerOpen.value = false
  notice.value = `已添加 ${chosen.length} 道工序到绘图区`
}
function removeNode(key: string) {
  mutate(() => {
    const node = nodes.value.find(n => n.key === key)
    nodes.value = nodes.value.filter(n => n.key !== key)
    if (node) edges.value = edges.value.filter(e => e.from !== node.process_id && e.to !== node.process_id)
    if (selectedNodeKey.value === key) selectedNodeKey.value = ''
    refreshBounds()
  })
}
function nodeByProcess(pid: string) { return nodes.value.find(n => n.process_id === pid) }
function fillDefaults(node: TopoNode, oldProcessID?: string) {
  mutate(() => {
    /* 更换节点工序时，把边里对旧 process_id 的引用迁移到新 id，避免产生"幽灵边" */
    const old = oldProcessID || node._lastPid
    if (old && old !== node.process_id) {
      edges.value.forEach(e => {
        if (e.from === old) e.from = node.process_id
        if (e.to === old) e.to = node.process_id
      })
    }
    node._lastPid = node.process_id
    const process = processes.value.find(p => p.id === node.process_id)
    if (process) { node.name = process.name; node.code = process.process_code; node.unit_price = process.default_unit_price; node.report_ratio = process.default_report_ratio; node.standard_work_seconds = process.standard_work_seconds }
  })
}
function repositionInspector() {
  const panel = inspectorRef.value
  const anchor = inspectorAnchor.value
  if (!panel || !anchor || !inspectorPos.value) return
  const rect = panel.getBoundingClientRect()
  inspectorPos.value = placeFloatingPanel(anchor, { width: rect.width, height: rect.height }, inspectorAvoid.value)
}
function openInspectorAt(anchor: Point, avoid: Box | null) {
  inspectorAnchor.value = anchor
  inspectorAvoid.value = avoid
  inspectorPos.value = { x: anchor.x + 12, y: anchor.y + 12 }
  nextTick(repositionInspector)
}
function onNodeClick(key: string, e: MouseEvent) {
  closeCtxMenu()
  if (e.ctrlKey || e.metaKey) {
    if (!canProductWrite.value) { error.value = '当前账号没有工序写入权限，无法建立连线'; return }
    const toNode = nodes.value.find(n => n.key === key)
    if (!toNode) return
    if (!linkSourceKey.value) { linkSourceKey.value = key; selectedNodeKey.value = key; return }
    if (linkSourceKey.value === key) { linkSourceKey.value = ''; return }
    const fromNode = nodes.value.find(n => n.key === linkSourceKey.value)
    if (!fromNode) { linkSourceKey.value = key; return }
    if (fromNode.process_id === toNode.process_id) { linkSourceKey.value = ''; error.value = '同一个工序不能连接到自身'; return }
    const exists = edges.value.some(edge => edge.from === fromNode.process_id && edge.to === toNode.process_id)
    if (exists) notice.value = '该连线已存在'
    else mutate(() => { edges.value.push({ from: fromNode.process_id, to: toNode.process_id }) })
    linkSourceKey.value = ''; selectedNodeKey.value = ''
    return
  }
  selectedNodeKey.value = key
  selectedEdgeIndex.value = -1
  inspectorPos.value = null
  inspectorAnchor.value = null
  inspectorAvoid.value = null
}
function onEdgeClick(index: number, e: MouseEvent) {
  closeCtxMenu(); selectedEdgeIndex.value = index; selectedNodeKey.value = ''
  const target = e.currentTarget as Element | null
  openInspectorAt({ x: e.clientX, y: e.clientY }, boxFromRect(target?.getBoundingClientRect()))
}
function removeEdge(index: number) {
  mutate(() => {
    edges.value.splice(index, 1)
    if (selectedEdgeIndex.value === index) selectedEdgeIndex.value = -1
    else if (selectedEdgeIndex.value > index) selectedEdgeIndex.value--
  })
}
function closeCtxMenu() { ctxMenu.value.show = false }
function openCtxMenu(e: MouseEvent, type: 'node' | 'edge', nodeKey = '', edgeIndex = -1) {
  const target = e.currentTarget as Element | null
  inspectorAnchor.value = { x: e.clientX, y: e.clientY }
  inspectorAvoid.value = boxFromRect(target?.getBoundingClientRect())
  ctxMenu.value = {
    show: true, type, nodeKey, edgeIndex,
    x: Math.min(e.clientX, window.innerWidth - 150),
    y: Math.min(e.clientY, window.innerHeight - 100),
  }
}
function ctxNodeProps() {
  selectedNodeKey.value = ctxMenu.value.nodeKey; selectedEdgeIndex.value = -1
  if (inspectorAnchor.value) openInspectorAt(inspectorAnchor.value, inspectorAvoid.value)
  closeCtxMenu()
}
function ctxNodeDelete() {
  if (!canProductWrite.value) error.value = '当前账号没有工序写入权限，无法删除'
  else removeNode(ctxMenu.value.nodeKey)
  closeCtxMenu()
}
function ctxEdgeProps() {
  selectedEdgeIndex.value = ctxMenu.value.edgeIndex; selectedNodeKey.value = ''
  if (inspectorAnchor.value) openInspectorAt(inspectorAnchor.value, inspectorAvoid.value)
  closeCtxMenu()
}
function closeInspector() { selectedNodeKey.value = ''; selectedEdgeIndex.value = -1; inspectorPos.value = null; inspectorAnchor.value = null; inspectorAvoid.value = null }
function ctxEdgeDelete() {
  if (!canProductWrite.value) error.value = '当前账号没有工序写入权限，无法删除'
  else removeEdge(ctxMenu.value.edgeIndex)
  closeCtxMenu()
}
function isParallel(node: TopoNode) {
  const out = edges.value.filter(e => e.from === node.process_id).length
  const inc = edges.value.filter(e => e.to === node.process_id).length
  return out > 1 || inc > 1
}

/* 依据边做分层布局：无前驱的在最左，逐层向右 */
function autoLayout(record = true) {
  const apply = () => {
    const indeg: Record<string, number> = {}
    nodes.value.forEach(n => indeg[n.process_id] = 0)
    edges.value.forEach(e => { if (indeg[e.to] !== undefined) indeg[e.to]++ })
    const layer: Record<string, number> = {}
    nodes.value.forEach(n => layer[n.process_id] = 0)
    let queue = nodes.value.filter(n => indeg[n.process_id] === 0).map(n => n.process_id)
    let guard = 0
    const order = [...queue]
    while (queue.length && guard++ < 1000) {
      const cur = queue.shift()!
      edges.value.filter(e => e.from === cur).forEach(e => {
        layer[e.to] = Math.max(layer[e.to] || 0, (layer[cur] || 0) + 1)
        indeg[e.to]--
        if (indeg[e.to] === 0) { queue.push(e.to); order.push(e.to) }
      })
    }
    nodes.value.forEach(n => {
      const l = layer[n.process_id] || 0
      n.node_x = 40 + l * (NODE_W + 90)
    })
    const byLayer: Record<number, TopoNode[]> = {}
    nodes.value.forEach(n => { const l = layer[n.process_id] || 0; (byLayer[l] ||= []).push(n) })
    Object.values(byLayer).forEach(group => group.forEach((n, i) => { n.node_y = 40 + i * (NODE_H + 30) }))
    refreshBounds()
  }
  if (record) mutate(apply); else apply()
}
const node_x_max = ref(1000)
const node_y_max = ref(500)
const canvas_min_x = ref(0)
const canvas_min_y = ref(0)
const zoom = ref(1)
const ZOOM_MIN = 0.4, ZOOM_MAX = 2
function zoomBy(delta: number) { zoom.value = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, +(zoom.value + delta).toFixed(2))) }
function onWheel(e: WheelEvent) { zoomBy(e.deltaY < 0 ? 0.1 : -0.1) }
function zoomFit() {
  const wrap = document.querySelector('.topo-canvas-wrap') as HTMLElement | null
  if (!wrap || !nodes.value.length) { zoom.value = 1; return }
  const cw = wrap.clientWidth - 40, ch = wrap.clientHeight - 40
  const z = Math.min(cw / node_x_max.value, ch / node_y_max.value)
  zoom.value = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, +z.toFixed(2)))
}
/* 根据所有节点位置和完整连线动态计算画布尺寸，保证内容不被裁切、可滚动 */
function refreshBounds() {
  const geoms = edgeGeoms.value
  canvas_min_x.value = Math.round(Math.min(0, ...nodes.value.map(n => n.node_x), ...geoms.map(g => g.minX)) - 40)
  canvas_min_y.value = Math.round(Math.min(0, ...nodes.value.map(n => n.node_y), ...geoms.map(g => g.minY)) - 40)
  node_x_max.value = Math.round(Math.max(NODE_W, ...nodes.value.map(n => n.node_x + NODE_W), ...geoms.map(g => g.maxX)) + 120 - canvas_min_x.value)
  node_y_max.value = Math.round(Math.max(NODE_H, ...nodes.value.map(n => n.node_y + NODE_H), ...geoms.map(g => g.maxY)) + 120 - canvas_min_y.value)
}

/* SVG 拖拽 */
let drag: { key: string; startX: number; startY: number; ox: number; oy: number } | null = null
function onPointerDown(e: PointerEvent, node: TopoNode) {
  if (e.button !== 0 || e.ctrlKey || e.metaKey) return
  history.begin(snapshot())
  drag = { key: node.key, startX: e.clientX, startY: e.clientY, ox: node.node_x, oy: node.node_y }
  ;(e.currentTarget as Element).setPointerCapture?.(e.pointerId)
}
function onPointerMove(e: PointerEvent) {
  if (!drag) return
  const node = nodes.value.find(n => n.key === drag!.key)
  if (!node) return
  const scale = zoom.value || 1
  node.node_x = Math.max(0, drag.ox + (e.clientX - drag.startX) / scale)
  node.node_y = Math.max(0, drag.oy + (e.clientY - drag.startY) / scale)
  refreshBounds()
}
function onPointerUp() { if (drag) history.commit(snapshot()); drag = null }

/* 连线几何：侧边中点锚点，绕开其它节点，箭头按终点切线 */
function edgeGeometry(e: TopoEdge) {
  const boxes: TopoBox[] = nodes.value.map(n => ({
    id: n.process_id, x: n.node_x, y: n.node_y, width: NODE_W, height: NODE_H,
  }))
  return topoEdgeGeometry(e, boxes, edges.value)
}
const edgeGeoms = computed(() => edges.value.map(e => edgeGeometry(e)))
function edgePathAt(i: number) { return edgeGeoms.value[i]?.d || '' }
function edgeAngleAt(i: number) { return edgeGeoms.value[i]?.angle || '0deg' }
function edgeColor(e: TopoEdge) { return isBidirectionalSecondary(e, edges.value) ? '#f59e0b' : '#2563eb' }

async function saveRoutes() {
  if (!current.value) return
  /* 清理端点已不存在的"幽灵边"（历史数据或多次改工序遗留） */
  const validPIDs = new Set(nodes.value.map(n => n.process_id))
  const before = edges.value.length
  edges.value = edges.value.filter(e => validPIDs.has(e.from) && validPIDs.has(e.to))
  if (before !== edges.value.length) notice.value = `已自动清理 ${before - edges.value.length} 条无效连线`
  /* DAG 环检测 */
  const indeg: Record<string, number> = {}
  nodes.value.forEach(n => indeg[n.process_id] = 0)
  edges.value.forEach(e => { if (indeg[e.to] !== undefined) indeg[e.to]++ })
  const q = nodes.value.filter(n => indeg[n.process_id] === 0).map(n => n.process_id)
  let seen = 0, guard = 0
  while (q.length && guard++ < 1000) {
    const cur = q.shift()!; seen++
    edges.value.filter(e => e.from === cur).forEach(e => { indeg[e.to]--; if (indeg[e.to] === 0) q.push(e.to) })
  }
  if (seen !== nodes.value.length) { error.value = '工序拓扑存在循环依赖，请检查连线'; return }
  saving.value = true; error.value = ''
  const items = nodes.value.map((n, i) => ({
    id: n.id, process_id: n.process_id, sequence_no: i + 1, unit_price: String(n.unit_price),
    report_ratio: String(n.report_ratio), department_id: n.department_id || null,
    standard_work_seconds: Number(n.standard_work_seconds || 0),
    is_parallel: isParallel(n), is_critical: !!n.is_critical, is_required: n.is_required !== false,
    remark: n.remark || null, node_x: Math.round(n.node_x), node_y: Math.round(n.node_y),
  }))
  try {
    await api(`/products/${current.value.id}/processes`, { method: 'PUT', body: JSON.stringify({ version: current.value.version, items, edges: edges.value }) })
    showRoute.value = false; notice.value = '工序拓扑已保存为新版本，历史工单快照不受影响。'; history.reset(); await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
function onKeydown(e: KeyboardEvent) {
  if (!showRoute.value) return
  const t = e.target as HTMLElement | null
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return
  if (!(e.ctrlKey || e.metaKey)) return
  const key = e.key.toLowerCase()
  if (key === 'z') { e.preventDefault(); e.shiftKey ? redo() : undo() }
  else if (key === 'y') { e.preventDefault(); redo() }
}
onMounted(() => {
  load()
  window.addEventListener('resize', repositionInspector)
  window.addEventListener('keydown', onKeydown)
})
onUnmounted(() => {
  window.removeEventListener('resize', repositionInspector)
  window.removeEventListener('keydown', onKeydown)
})
</script>
<template>
  <div class="page-header"><div><h1>产品与工序</h1><p>按产品订单添加工序，以图形配置串行 / 并行 / 汇合拓扑</p></div><div class="header-actions"><button v-if="canProcessWrite" class="button secondary" @click="startCreateProcess">+ 新建工序</button><button v-if="canProductWrite" class="button" @click="showProduct = true">+ 新建产品</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>

  <section class="panel"><div class="tabs"><button :class="{ active: tab === 'products' }" @click="tab = 'products'">产品资料</button><button :class="{ active: tab === 'processes' }" @click="tab = 'processes'">工序库</button></div><div class="toolbar"><input v-model="productKeyword" class="search-input" placeholder="搜索产品编号或名称" @keyup.enter="applyProductSearch"><button class="button secondary" @click="applyProductSearch">检索</button><span class="spacer"></span><button class="button secondary" @click="load">刷新</button></div><PageState :loading="loading" />
    <div v-if="!loading && tab === 'products'">
      <div class="table-wrap"><table><thead><tr><th>图片</th><th>产品编号</th><th>名称/规格</th><th>工序数</th><th>理论工价</th><th>版本</th><th>状态</th><th>配置人</th><th>操作</th></tr></thead><tbody><tr v-for="product in pagedProducts" :key="product.id"><td><ImageHoverPreview :attachment-id="product.image_attachment_id"><ProductImage :attachment-id="product.image_attachment_id" :width="52" :height="52" /></ImageHoverPreview></td><td><strong>{{ product.product_code }}</strong></td><td>{{ product.name }}<small class="text-muted" style="display:block">{{ product.spec || '—' }}</small></td><td>{{ product.process_count }}</td><td class="money">¥ {{ fmtPrice(product.theoretical_price) }}</td><td>v{{ product.version }}</td><td><span class="status" :class="product.status">{{ statusText[product.status] }}</span></td><td :title="product.configured_at ? new Date(product.configured_at/1000).toLocaleString('zh-CN') : ''">{{ product.configurer_name || '—' }}</td><td><RouterLink class="button link" :to="`/products/${product.id}`">资料/图片</RouterLink><button v-if="canProductWrite" class="button link" @click="editRoutes(product)">配置工序</button></td></tr></tbody></table></div>
      <PageState :empty="!filteredProducts.length" />
      <div v-if="filteredProducts.length" class="table-pager">
        <span class="pager-info">共 {{ filteredProducts.length }} 条 · 第 {{ productPage }} / {{ productPageCount }} 页</span>
        <select v-model.number="productPageSize" class="pager-size" @change="onProductPageSizeChange"><option :value="5">5 条/页</option><option :value="10">10 条/页</option><option :value="20">20 条/页</option><option :value="50">50 条/页</option></select>
        <button class="button secondary" :disabled="productPage <= 1" @click="changeProductPage(-1)">上一页</button>
        <button class="button secondary" :disabled="productPage >= productPageCount" @click="changeProductPage(1)">下一页</button>
      </div>
    </div>
    <div v-if="!loading && tab === 'processes'" class="table-wrap"><table><thead><tr><th>工序编码</th><th>名称</th><th>默认工价</th><th>报工配比</th><th>标准工时(秒)</th><th>状态</th><th>操作</th></tr></thead><tbody>
      <template v-for="process in processes" :key="process.id">
        <tr v-if="editingProcessRow === process.id" class="row-editing">
          <td><input v-model.trim="processForm.process_code" class="cell-input" placeholder="留空自动生成"></td>
          <td><input v-model.trim="processForm.name" class="cell-input" placeholder="工序名称"></td>
          <td><input v-model="processForm.default_unit_price" class="cell-input" inputmode="decimal"></td>
          <td><input v-model="processForm.default_report_ratio" class="cell-input" inputmode="decimal"></td>
          <td><input v-model.number="processForm.standard_work_seconds" class="cell-input" type="number" min="0"></td>
          <td><span class="status active">{{ statusText[process.status] }}</span></td>
          <td class="inline-actions"><button class="button link" :disabled="saving" @click="saveProcess">保存</button><button class="button link" @click="cancelProcessRow">取消</button></td>
        </tr>
        <tr v-else>
          <td>{{ process.process_code }}</td><td>{{ process.name }}</td><td>{{ fmtPrice(process.default_unit_price) }}</td><td>{{ fmtRatio(process.default_report_ratio) }}</td><td>{{ process.standard_work_seconds }}</td><td><span class="status active">{{ statusText[process.status] }}</span></td>
          <td><button v-if="canProcessWrite" class="button link" @click="startEditProcess(process)">编辑</button><button v-if="canProcessWrite" class="button link text-danger" @click="deleteProcess(process)">删除</button></td>
        </tr>
      </template>
      <tr v-if="editingProcessRow === '__new__' && canProcessWrite" class="row-editing">
        <td><input v-model.trim="processForm.process_code" class="cell-input" placeholder="留空自动生成"></td>
        <td><input v-model.trim="processForm.name" class="cell-input" placeholder="例如 车耳朵"></td>
        <td><input v-model="processForm.default_unit_price" class="cell-input" inputmode="decimal"></td>
        <td><input v-model="processForm.default_report_ratio" class="cell-input" inputmode="decimal"></td>
        <td><input v-model.number="processForm.standard_work_seconds" class="cell-input" type="number" min="0"></td>
        <td><span class="status">草稿</span></td>
        <td class="inline-actions"><button class="button link" :disabled="saving" @click="saveProcess">保存</button><button class="button link" @click="cancelProcessRow">取消</button></td>
      </tr>
    </tbody></table><PageState :empty="!processes.length && editingProcessRow !== '__new__'" /></div>
  </section>

  <ModalDialog v-if="showProduct" title="新建产品" @close="showProduct = false"><div class="form-grid"><div class="field"><label>产品编号</label><input v-model.trim="productForm.product_code" placeholder="留空自动生成（如 CP-00001）"></div><div class="field"><label class="required">产品名称</label><input v-model.trim="productForm.name"></div><div class="field"><label>规格</label><input v-model.trim="productForm.spec"></div><div class="field"><label class="required">单位</label><input v-model.trim="productForm.unit"></div><div class="field"><label>售价</label><input v-model="productForm.sale_price" inputmode="decimal"></div></div><template #footer><button class="button secondary" @click="showProduct = false">取消</button><button class="button" :disabled="saving" @click="createProduct">保存草稿</button></template></ModalDialog>

  <ModalDialog v-if="processPickerOpen" wide title="选择工序" @close="processPickerOpen = false">
    <div class="process-picker-toolbar"><input v-model.trim="processPickerKeyword" class="search-input" placeholder="检索工序编号或名称"><span class="text-muted">可多选，已添加到绘图区的工序不再显示</span></div>
    <div class="process-picker-list">
      <label v-for="p in availableProcessOptions" :key="p.value" class="process-picker-item">
        <input type="checkbox" :checked="processPickerSelected.includes(p.value)" @change="toggleProcessPick(p.value, ($event.target as HTMLInputElement).checked)">
        <span>{{ p.label }}</span>
      </label>
      <div v-if="!availableProcessOptions.length" class="text-muted" style="padding:16px">没有可添加的工序</div>
    </div>
    <template #footer><button class="button secondary" @click="processPickerOpen = false">取消</button><button class="button" @click="confirmProcessPicker">确定添加 ({{ processPickerSelected.length }})</button></template>
  </ModalDialog>

  <ModalDialog v-if="showRoute" wide width="90vw" :title="`工序拓扑图 · ${current?.name || ''}`" @close="showRoute = false">
    <div class="topo-toolbar">
      <button class="button secondary" :disabled="!canProductWrite" @click="openProcessPicker">+ 添加工序</button>
      <button class="button secondary" :disabled="!canProductWrite" @click="autoLayout()">自动布局</button>
      <button class="button secondary small" :disabled="!canUndo" title="撤销（Ctrl/Cmd+Z）" @click="undo">↶ 撤销</button>
      <button class="button secondary small" :disabled="!canRedo" title="重做（Ctrl/Cmd+Shift+Z 或 Ctrl+Y）" @click="redo">↷ 重做</button>
      <span class="topo-zoom">
        <button class="button secondary" @click="zoomBy(-0.1)" title="缩小">−</button>
        <span>{{ Math.round(zoom * 100) }}%</span>
        <button class="button secondary" @click="zoomBy(0.1)" title="放大">＋</button>
        <button class="button secondary" @click="zoomFit" title="适应窗口">适应</button>
      </span>
      <span class="topo-legend"><i class="dot serial"></i>串行 <i class="dot parallel"></i>并行/汇合 <span class="legend-tip">左键选中节点 · 右键节点/连线属性 · Ctrl + 点两个工序建立连线（A→B）· 双向连线橙色区分 · Ctrl+Z 撤销 / Ctrl+Y 重做</span></span>
    </div>
    <div class="topo-tips">
      <button class="topo-tips-head" @click="showTopoTips = !showTopoTips">
        <span class="tips-icon">?</span> 操作提示
        <span class="tips-toggle">{{ showTopoTips ? '收起' : '展开' }}</span>
      </button>
      <div v-if="showTopoTips" class="topo-tips-body">
        <div><b>添加工序</b>：点击「+ 添加工序」打开可检索工序列表，勾选后确定添加到绘图区</div>
        <div><b>建立连线</b>：按住 <kbd>Ctrl</kbd> 依次点击工序 A、B，生成 A→B；再点同一工序可取消起点</div>
        <div><b>移动工序</b>：左键拖拽工序框；<b>节点操作</b>：右键工序框 → 属性 / 删除</div>
        <div><b>连线操作</b>：左键点连线查看属性（不会删除）；右键连线 → 属性 / 删除</div>
        <div><b>布局缩放</b>：「自动布局」分层排列；滚轮或工具栏缩放，「适应」铺满窗口</div>
      </div>
    </div>
    <div v-if="!canProductWrite" class="notice error">当前账号没有「product:write（产品工序写入）」权限，只能查看不能编辑。请联系管理员为你的角色授权后刷新页面。</div>
    <div class="notice">保存后生成产品新版本；工价与配比只影响后续下发。同层多节点为并行，多线汇入为汇合。</div>
    <p class="text-muted" style="margin:8px 0 0">最近配置：{{ routeMeta.name || '—' }}<template v-if="routeMeta.at"> · {{ new Date(routeMeta.at/1000).toLocaleString('zh-CN') }}</template></p>
    <div class="topo-canvas-wrap" @wheel.prevent="onWheel">
      <svg ref="svgRef" class="topo-canvas" :viewBox="`${canvas_min_x} ${canvas_min_y} ${node_x_max} ${node_y_max}`" :width="node_x_max * zoom" :height="node_y_max * zoom"
           @pointermove="onPointerMove" @pointerup="onPointerUp" @pointerleave="onPointerUp">
        <defs>
          <marker v-for="(e, ei) in edges" :key="'marker-'+ei" :id="'arrow-'+ei" markerWidth="10" markerHeight="8" refX="9" refY="4" :orient="edgeAngleAt(ei)"><path d="M0,0 L10,4 L0,8 Z" :fill="edgeColor(e)" /></marker>
        </defs>
        <g v-for="(e, ei) in edges" :key="'e'+ei">
          <path :d="edgePathAt(ei)" class="topo-edge-hit" :class="{ active: selectedEdgeIndex === ei }" @click.stop="onEdgeClick(ei, $event)" @contextmenu.prevent.stop="openCtxMenu($event, 'edge', '', ei)" />
          <path :d="edgePathAt(ei)" class="topo-edge" :style="{ stroke: edgeColor(e) }" :marker-end="'url(#arrow-'+ei+')'" />
        </g>
        <g v-for="n in nodes" :key="n.key" class="topo-node" :class="{ selected: selectedNodeKey === n.key, linking: linkSourceKey === n.key, parallel: isParallel(n) }"
           :transform="`translate(${n.node_x},${n.node_y})`" @pointerdown="onPointerDown($event, n)" @click.stop="onNodeClick(n.key, $event)" @contextmenu.prevent.stop="openCtxMenu($event, 'node', n.key)">
          <rect class="node-box" :width="NODE_W" :height="NODE_H" rx="8" />
          <text class="node-code" x="10" y="18">{{ n.code || '工序' }}</text>
          <text class="node-name" x="10" y="40">{{ n.name }}</text>
          <text class="node-meta" x="10" y="60">件数比 {{ n.report_ratio }} · ¥{{ n.unit_price }}</text>
          <circle v-if="isParallel(n)" class="node-flag" :cx="NODE_W-12" cy="12" r="5" />
          <text v-if="n.is_critical" class="node-star" :x="NODE_W-26" y="17">★</text>
        </g>
      </svg>
      <div v-if="!nodes.length" class="topo-empty">暂无工序，点击「+ 添加工序」开始配置；按住 Ctrl 依次点击两个工序建立 A→B 连线，右键可打开操作菜单。</div>
    </div>
    <Teleport to="body">
      <div v-if="inspectorPos && selectedNodeKey" ref="inspectorRef" class="topo-inspector floating" :style="{ left: inspectorPos.x + 'px', top: inspectorPos.y + 'px' }">
        <div class="inspector-title">节点属性<button type="button" class="inspector-close" @click="closeInspector">×</button></div>
        <template v-for="n in nodes.filter(x => x.key === selectedNodeKey)" :key="n.key">
          <div class="inspector-grid">
            <label>工序名称<input :value="n.name" disabled></label>
            <label>工序<FilterSelect v-model="n.process_id" :options="processOptions" @change="fillDefaults(n, n._lastPid)" /></label>
            <label>工价<input v-model="n.unit_price" inputmode="decimal" /></label>
            <label>报工配比<input v-model="n.report_ratio" inputmode="decimal" /></label>
            <label>标准工时(秒)<input v-model.number="n.standard_work_seconds" type="number" min="0" /></label>
            <label class="chk"><input v-model="n.is_critical" type="checkbox"> 关键工序</label>
            <label class="chk"><input v-model="n.is_required" type="checkbox"> 必做</label>
          </div>
          <button class="button link text-danger" :disabled="!canProductWrite" @click="removeNode(n.key); closeInspector()">删除该工序</button>
        </template>
      </div>
      <div v-if="inspectorPos && selectedEdgeIndex >= 0 && edges[selectedEdgeIndex]" ref="inspectorRef" class="topo-inspector floating" :style="{ left: inspectorPos.x + 'px', top: inspectorPos.y + 'px' }">
        <div class="inspector-title">连线属性<button type="button" class="inspector-close" @click="closeInspector">×</button></div>
        <div class="edge-info">
          <span>{{ nodeByProcess(edges[selectedEdgeIndex].from)?.name || edges[selectedEdgeIndex].from }}</span>
          <span class="edge-arrow">→</span>
          <span>{{ nodeByProcess(edges[selectedEdgeIndex].to)?.name || edges[selectedEdgeIndex].to }}</span>
        </div>
        <button v-if="canProductWrite" class="button link text-danger" @click="removeEdge(selectedEdgeIndex); closeInspector()">删除该连线</button>
      </div>
    </Teleport>
    <Teleport to="body">
      <div v-if="ctxMenu.show" class="ctx-overlay" @click="closeCtxMenu" @contextmenu.prevent="closeCtxMenu">
        <div class="ctx-menu" :style="{ left: ctxMenu.x + 'px', top: ctxMenu.y + 'px' }" @click.stop>
          <template v-if="ctxMenu.type === 'node'">
            <button @click="ctxNodeProps">属性</button>
            <button v-if="canProductWrite" class="text-danger" @click="ctxNodeDelete">删除</button>
          </template>
          <template v-else>
            <button @click="ctxEdgeProps">属性</button>
            <button v-if="canProductWrite" class="text-danger" @click="ctxEdgeDelete">删除</button>
          </template>
        </div>
      </div>
    </Teleport>
    <template #footer><span class="text-muted" style="margin-right:auto">共 {{ nodes.length }} 道工序 · {{ edges.length }} 条连线</span><button class="button secondary" @click="showRoute = false">取消</button><button class="button" :disabled="saving || !canProductWrite" @click="saveRoutes">保存拓扑</button></template>
  </ModalDialog>
</template>
<style scoped>
.topo-entry{padding:16px 20px;margin-bottom:16px}
.topo-entry-title{font-weight:700;margin-bottom:10px}
.topo-entry-row{display:flex;gap:14px;align-items:center}
.topo-entry-row .filter-select{min-width:360px}
.row-editing td{background:#f8fbff}
.cell-input{width:100%;min-width:72px;padding:6px 8px}
.inline-actions{white-space:nowrap}
.table-pager{display:flex;gap:10px;align-items:center;justify-content:flex-end;padding:12px 20px;border-top:1px solid var(--border);background:rgba(255,255,255,.6)}
.pager-info{margin-right:auto;color:#667085;font-size:12px}
.pager-size{padding:6px 9px;min-width:auto}
.table-pager .button{padding:6px 12px;min-width:auto}
.topo-toolbar{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-bottom:10px}
.topo-toolbar .button.active{outline:2px solid #2563eb}
.topo-zoom{display:flex;gap:6px;align-items:center;margin-left:8px}
.topo-zoom .button{padding:4px 9px;min-width:auto}
.topo-zoom span{min-width:44px;text-align:center;font-size:12px;color:#475467}
.topo-legend{display:flex;gap:6px;align-items:center;color:#667085;font-size:12px;margin-left:auto}
.topo-legend .dot{display:inline-block;width:9px;height:9px;border-radius:50%;margin-left:8px}
.topo-legend .dot.serial{background:#2563eb}
.topo-legend .dot.parallel{background:#f79009}
.topo-legend .legend-tip{margin-left:8px;color:#667085}
.topo-tips{margin-bottom:10px;border:1px solid var(--border);border-radius:8px;background:#f8fbff;overflow:hidden}
.topo-tips-head{display:flex;align-items:center;gap:8px;width:100%;border:0;background:transparent;padding:8px 12px;font-size:12px;font-weight:700;color:#17255f;cursor:pointer;text-align:left}
.tips-icon{display:inline-grid;place-items:center;width:16px;height:16px;border-radius:50%;background:#2563eb;color:#fff;font-size:11px;flex:none}
.tips-toggle{margin-left:auto;color:#2563eb;font-weight:500}
.topo-tips-body{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:4px 20px;padding:0 12px 10px;font-size:12px;color:#475467;line-height:1.6}
.topo-tips-body b{color:#17255f}
.topo-tips-body kbd{border:1px solid #c7dcff;border-bottom-width:2px;border-radius:4px;padding:0 5px;background:#fff;font-size:11px;color:#2563eb}
.topo-canvas-wrap{position:relative;overflow:auto;max-height:52vh;border:1px solid var(--border);border-radius:10px;background:
  radial-gradient(circle,#d9e2f0 1px,transparent 1px) 0 0/20px 20px,#fbfdff}
.topo-canvas{display:block}
.topo-empty{position:absolute;inset:0;display:grid;place-items:center;color:#98a2b3;font-size:13px;pointer-events:none;padding:20px;text-align:center}
.topo-edge{fill:none;stroke:#2563eb;stroke-width:2;pointer-events:none}
.topo-edge-hit{fill:none;stroke:transparent;stroke-width:14;cursor:pointer}
.topo-edge-hit.active{stroke:rgba(37,99,235,.2);stroke-width:16}
.topo-node{cursor:grab;user-select:none}
.topo-node:active{cursor:grabbing}
.topo-node .node-box{fill:#fff;stroke:#98a2b3;stroke-width:1.5;filter:drop-shadow(0 2px 4px rgba(16,24,40,.08))}
.topo-node.parallel .node-box{stroke:#f79009;stroke-width:2}
.topo-node.selected .node-box{stroke:#2563eb;stroke-width:2.5}
.topo-node.linking .node-box{stroke:#12b76a;stroke-width:2.5;stroke-dasharray:5 3}
.topo-node .node-code{font-size:11px;fill:#667085}
.topo-node .node-name{font-size:15px;font-weight:700;fill:#17255f}
.topo-node .node-meta{font-size:11px;fill:#475467}
.topo-node .node-flag{fill:#f79009}
.topo-node .node-star{fill:#f79009;font-size:13px}
.topo-inspector{margin-top:12px;padding:12px 14px;border:1px solid var(--border);border-radius:10px;background:#fbfdff}
.topo-inspector.floating{position:fixed;z-index:10001;margin-top:0;width:320px;max-width:calc(100vw - 24px);max-height:calc(100vh - 24px);overflow:auto;box-shadow:0 12px 34px rgba(16,24,40,.22)}
.inspector-title{font-weight:700;margin-bottom:8px;display:flex;align-items:center;justify-content:space-between}
.inspector-close{border:0;background:transparent;color:#98a2b3;font-size:18px;line-height:1;cursor:pointer}
.inspector-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px;align-items:end;margin-bottom:8px}
.inspector-grid label{display:flex;flex-direction:column;gap:4px;font-size:12px;color:#475467}
.inspector-grid label.chk{flex-direction:row;align-items:center;gap:6px;align-self:center}
.edge-info{display:flex;align-items:center;gap:12px;margin-bottom:8px;font-size:13px;color:#17255f}
.edge-arrow{color:#2563eb;font-weight:700}
.ctx-overlay{position:fixed;inset:0;z-index:10000}
.ctx-menu{position:fixed;min-width:120px;padding:5px;background:#fff;border:1px solid var(--border);border-radius:8px;box-shadow:0 10px 28px rgba(16,24,40,.2)}
.ctx-menu button{display:block;width:100%;text-align:left;border:0;background:transparent;border-radius:5px;padding:8px 12px;font-size:13px;color:#17255f;cursor:pointer}
.ctx-menu button:hover{background:#f2f4f7}
.ctx-menu button.text-danger{color:#d92d20}
.process-picker-toolbar{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:12px}
.process-picker-toolbar .search-input{flex:1;min-width:240px}
.process-picker-list{display:grid;grid-template-columns:repeat(2,1fr);gap:8px 16px;max-height:420px;overflow-y:auto;border:1px solid var(--border);border-radius:8px;padding:10px}
.process-picker-item{display:flex;align-items:center;gap:8px;font-size:13px;cursor:pointer}
.process-picker-item input{width:14px;height:14px;min-width:0}
@media(max-width:900px){.inspector-grid{grid-template-columns:1fr 1fr}.topo-entry-row{flex-direction:column;align-items:stretch}.topo-tips-body{grid-template-columns:1fr}.process-picker-list{grid-template-columns:1fr}}
</style>
