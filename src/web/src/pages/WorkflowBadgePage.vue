<script setup lang="ts">
/**
 * 流程待办配置：以拓扑图方式配置流程。环节 = 生产管理菜单项，节点之间的连线表达串行/并行；
 * 同一节点多条出边即并行分支，多条入边即汇合。数据落到 workflow_badge_flow/step/edge 三表。
 */
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import { usePermissions } from '../permissions'
import { productionMenuItems, type NavItem } from '../nav'
import { boxFromRect, placeFloatingPanel, type Box, type Point } from '../floatingPanel'
import { isBidirectionalSecondary, topoEdgeGeometry, type TopoBox } from '../topoEdge'
import { createHistory } from '../history'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'

interface Flow { id: string; code: string; name: string; status: string; sort_no: number }
interface Step { id: string; flow_id: string; step_code: string; name: string; menu_path: string; permission_code: string; recipient_role_codes: string[]; count_kind: string; trigger_event: string; enabled: boolean; sort_no: number; node_x: number; node_y: number }
interface Edge { id: string; flow_id: string; from_step_id: string; to_step_id: string }

const { has } = usePermissions()
const canWrite = computed(() => has('settings:write'))

const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const flows = ref<Flow[]>([])
const steps = ref<Step[]>([])
const edges = ref<Edge[]>([])
const currentFlowID = ref('')

const countKinds = [
  ['none', '不计数（仅作为流程起点）'],
  ['material_issue_pending_confirm', '待确认领料（material_issue）'],
  ['project_confirmed_pending_receipt', '已确认订单待入库（project）'],
  ['material_receipt_draft', '待提交入库单（material_receipt）'],
]

/* ---- 拓扑状态 ---- */
type TopoNode = { key: string; step: Step }
type TopoEdge = { from: string; to: string }
const nodes = ref<TopoNode[]>([])
const topoEdges = ref<TopoEdge[]>([])
const selectedNodeKey = ref('')
const selectedEdgeIndex = ref(-1)
const linkSourceKey = ref('')
const inspectorOpen = ref(false)
const inspectorPos = ref<Point | null>(null)
const inspectorRef = ref<HTMLElement | null>(null)
const inspectorAnchor = ref<Point | null>(null)
const inspectorAvoid = ref<Box | null>(null)
const pickerOpen = ref(false)
const pickerKeyword = ref('')
const pickerSelected = ref<string[]>([])
const svgRef = ref<SVGSVGElement | null>(null)
const NODE_W = 180
const NODE_H = 72
const node_x_max = ref(1000)
const node_y_max = ref(500)
const canvas_min_x = ref(0)
const canvas_min_y = ref(0)
const zoom = ref(1)
const ZOOM_MIN = 0.4
const ZOOM_MAX = 2
let keySeq = 0
let stepSeq = 0
function newKey() { return `n-${Date.now()}-${++keySeq}` }
function newStepId() { return `wbs-${Date.now()}-${++stepSeq}-${Math.random().toString(36).slice(2, 6)}` }

/* ---- 撤销/重做 ---- */
type WfSnapshot = { flows: Flow[]; steps: Step[]; edges: Edge[]; currentFlowID: string; nodes: TopoNode[]; topoEdges: TopoEdge[] }
const history = createHistory<WfSnapshot>()
const canUndo = history.canUndo
const canRedo = history.canRedo
function snapshot(): WfSnapshot {
  return { flows: flows.value, steps: steps.value, edges: edges.value, currentFlowID: currentFlowID.value, nodes: nodes.value, topoEdges: topoEdges.value }
}
function applySnapshot(s: WfSnapshot) {
  flows.value = s.flows; steps.value = s.steps; edges.value = s.edges
  currentFlowID.value = s.currentFlowID
  nodes.value = s.nodes; topoEdges.value = s.topoEdges
  clearSelection(); refreshBounds()
}
function mutate(fn: () => void) { history.begin(snapshot()); fn(); history.commit(snapshot()) }
function undo() { const s = history.undo(snapshot()); if (s) applySnapshot(s) }
function redo() { const s = history.redo(snapshot()); if (s) applySnapshot(s) }

const menuOptions = computed(() => productionMenuItems())
const flowOptions = computed(() => flows.value.filter(f => f.status === 'active'))
const currentFlow = computed(() => flows.value.find(f => f.id === currentFlowID.value) || null)

async function load() {
  loading.value = true; error.value = ''
  try {
    const res = await api<{ flows: Flow[]; steps: Step[]; edges: Edge[] }>('/workflow-badge-config')
    flows.value = res.flows || []; steps.value = res.steps || []; edges.value = res.edges || []
    if (!flows.value.some(f => f.id === currentFlowID.value)) currentFlowID.value = flowOptions.value[0]?.id || flows.value[0]?.id || ''
    syncTopo(); history.reset()
  } catch (e) { error.value = (e as Error).message } finally { loading.value = false }
}

/* 把当前流程的 step/edge 映射为拓扑节点/边 */
function syncTopo() {
  clearSelection()
  nodes.value = steps.value.filter(s => s.flow_id === currentFlowID.value).map(s => ({ key: newKey(), step: s }))
  topoEdges.value = edges.value.filter(e => e.flow_id === currentFlowID.value).map(e => ({ from: e.from_step_id, to: e.to_step_id }))
  if (nodes.value.length && nodes.value.every(n => !n.step.node_x && !n.step.node_y)) autoLayout(false)
  else refreshBounds()
}
function selectFlow(id: string) {
  currentFlowID.value = id
  syncTopo(); history.reset()
}
function setFlowName(value: string) { if (currentFlow.value) currentFlow.value.name = value }
function setFlowStatus(value: string) { if (currentFlow.value) currentFlow.value.status = value }
function addFlow() {
  if (!canWrite.value) return
  mutate(() => {
    const f: Flow = { id: '', code: `flow_${flows.value.length + 1}`, name: `新流程 ${flows.value.length + 1}`, status: 'active', sort_no: flows.value.length + 1 }
    flows.value.push(f)
    currentFlowID.value = '' // 保存后由后端分配 id
    clearSelection()
    nodes.value = []; topoEdges.value = []
  })
}
function removeFlow() {
  if (!canWrite.value || !currentFlow.value) return
  if (!window.confirm(`确定删除流程「${currentFlow.value.name}」及其所有环节与连线？`)) return
  mutate(() => {
    const id = currentFlowID.value
    flows.value = flows.value.filter(f => f.id !== id)
    steps.value = steps.value.filter(s => s.flow_id !== id)
    edges.value = edges.value.filter(e => e.flow_id !== id)
    currentFlowID.value = flows.value[0]?.id || ''
    syncTopo()
  })
}

function nodeByStep(id: string) { return nodes.value.find(n => n.step.id === id) }
function menuByPath(path: string): NavItem | undefined { return menuOptions.value.find(m => m.path === path) }
function nodeLabel(n: TopoNode | undefined) { return n ? (n.step.name || menuByPath(n.step.menu_path)?.label || n.step.step_code || '环节') : '—' }
function isParallel(n: TopoNode) {
  const out = topoEdges.value.filter(e => e.from === n.step.id).length
  const inc = topoEdges.value.filter(e => e.to === n.step.id).length
  return out > 1 || inc > 1
}

function openPicker() {
  if (!canWrite.value) { error.value = '当前账号没有配置写入权限'; return }
  pickerKeyword.value = ''
  pickerSelected.value = []
  pickerOpen.value = true
}
const pickerItems = computed(() => {
  const kw = pickerKeyword.value.trim().toLowerCase()
  const used = new Set(nodes.value.map(n => n.step.menu_path))
  return menuOptions.value.filter(m => !used.has(m.path) && (!kw || m.label.toLowerCase().includes(kw) || m.path.toLowerCase().includes(kw)))
})
function togglePicker(path: string) {
  const i = pickerSelected.value.indexOf(path)
  if (i >= 0) pickerSelected.value.splice(i, 1)
  else pickerSelected.value.push(path)
}
function confirmPicker() {
  if (!currentFlowID.value) { error.value = '请先新建并保存流程，再添加环节'; return }
  mutate(() => {
    const startX = nodes.value.length ? Math.min(...nodes.value.map(n => n.step.node_x)) : 40
    const startY = nodes.value.length ? Math.max(...nodes.value.map(n => n.step.node_y)) + NODE_H + 30 : 40
    pickerSelected.value.forEach((path, i) => {
      const m = menuByPath(path)
      if (!m) return
      const step: Step = {
        id: newStepId(), flow_id: currentFlowID.value, step_code: path, name: m.label,
        menu_path: m.path, permission_code: (m.permission || '').split(',')[0] || '',
        recipient_role_codes: [], count_kind: 'none', trigger_event: '', enabled: true,
        sort_no: steps.value.length + 1, node_x: startX, node_y: startY + i * (NODE_H + 24),
      }
      nodes.value.push({ key: newKey(), step })
    })
    refreshBounds()
  })
  pickerOpen.value = false
  notice.value = `已添加 ${pickerSelected.value.length} 个环节`
}

function removeNode(key: string) {
  mutate(() => {
    const n = nodes.value.find(x => x.key === key)
    nodes.value = nodes.value.filter(x => x.key !== key)
    if (n) topoEdges.value = topoEdges.value.filter(e => e.from !== n.step.id && e.to !== n.step.id)
    if (selectedNodeKey.value === key) selectedNodeKey.value = ''
    if (linkSourceKey.value === key) linkSourceKey.value = ''
    refreshBounds()
  })
}
function repositionInspector() {
  const panel = inspectorRef.value
  const anchor = inspectorAnchor.value
  if (!panel || !anchor || !inspectorOpen.value) return
  const rect = panel.getBoundingClientRect()
  inspectorPos.value = placeFloatingPanel(anchor, { width: rect.width, height: rect.height }, inspectorAvoid.value)
}
function openInspectorAt(e: MouseEvent) {
  const target = e.currentTarget as Element | null
  inspectorAnchor.value = { x: e.clientX, y: e.clientY }
  inspectorAvoid.value = boxFromRect(target?.getBoundingClientRect())
  inspectorPos.value = { x: e.clientX + 12, y: e.clientY + 12 }
  inspectorOpen.value = true
  nextTick(repositionInspector)
}
function hasEdge(from: string, to: string) { return topoEdges.value.some(e => e.from === from && e.to === to) }
function linkSteps(from: string, to: string) {
  error.value = ''
  if (hasEdge(from, to)) { notice.value = '该连线已存在'; return false }
  mutate(() => { topoEdges.value.push({ from, to }) })
  notice.value = `已连接「${nodeLabel(nodeByStep(from))}」→「${nodeLabel(nodeByStep(to))}」；可继续连接其他目标形成并行或回路`
  return true
}
function openNodeInspector(key: string, e: MouseEvent) {
  selectedNodeKey.value = key
  selectedEdgeIndex.value = -1
  linkSourceKey.value = ''
  openInspectorAt(e)
  error.value = ''
}
function cancelLink() {
  if (!linkSourceKey.value) return
  linkSourceKey.value = ''
  notice.value = '已退出连续连线'
}
function onNodeClick(key: string, e: MouseEvent) {
  if (e.ctrlKey || e.metaKey) {
    inspectorOpen.value = false
    error.value = ''
    if (!canWrite.value) { error.value = '当前账号没有配置写入权限，无法建立连线'; return }
    const to = nodes.value.find(n => n.key === key)
    if (!to) return
    if (!linkSourceKey.value) {
      linkSourceKey.value = key
      selectedNodeKey.value = key
      selectedEdgeIndex.value = -1
      notice.value = '已选择起点：Ctrl/Cmd+点击多个目标可连续建立并行连线；再次点击起点结束'
      return
    }
    if (linkSourceKey.value === key) { cancelLink(); return }
    const from = nodes.value.find(n => n.key === linkSourceKey.value)
    if (!from) { linkSourceKey.value = key; return }
    linkSteps(from.step.id, to.step.id)
    selectedNodeKey.value = from.key
    return
  }
  selectedNodeKey.value = key
  selectedEdgeIndex.value = -1
  linkSourceKey.value = ''
  inspectorOpen.value = false
}
function onEdgeClick(index: number, e: MouseEvent) {
  selectedEdgeIndex.value = index
  selectedNodeKey.value = ''
  linkSourceKey.value = ''
  openInspectorAt(e)
}
function removeEdge(index: number) {
  mutate(() => {
    topoEdges.value.splice(index, 1)
    if (selectedEdgeIndex.value === index) selectedEdgeIndex.value = -1
    else if (selectedEdgeIndex.value > index) selectedEdgeIndex.value--
  })
}
function toggleNextStep(from: string, to: string, e: Event) {
  const input = e.target as HTMLInputElement
  if (!canWrite.value) { error.value = '当前账号没有配置写入权限'; input.checked = hasEdge(from, to); return }
  mutate(() => {
    if (input.checked) {
      if (!hasEdge(from, to)) topoEdges.value.push({ from, to })
      notice.value = `已连接「${nodeLabel(nodeByStep(from))}」→「${nodeLabel(nodeByStep(to))}」；可继续连接其他目标形成并行或回路`
    } else {
      topoEdges.value = topoEdges.value.filter(edge => !(edge.from === from && edge.to === to))
      notice.value = '已断开连线'
    }
  })
  error.value = ''
  input.checked = hasEdge(from, to)
  nextTick(repositionInspector)
}
function closeInspector() { inspectorOpen.value = false; selectedEdgeIndex.value = -1; inspectorPos.value = null; inspectorAnchor.value = null; inspectorAvoid.value = null }
function clearSelection() { selectedNodeKey.value = ''; linkSourceKey.value = ''; closeInspector() }
function rolesText(n: TopoNode) { return (n.step.recipient_role_codes || []).join(',') }
function setRoles(n: TopoNode, value: string) { n.step.recipient_role_codes = value.split(',').map(v => v.trim()).filter(Boolean) }

/* 自动布局：Kahn 分层；环中节点无法入队，统一放到末列避免重叠 */
function autoLayout(record = true) {
  const apply = () => {
    const indeg: Record<string, number> = {}
    nodes.value.forEach(n => indeg[n.step.id] = 0)
    topoEdges.value.forEach(e => { if (indeg[e.to] !== undefined) indeg[e.to]++ })
    const layer: Record<string, number> = {}
    nodes.value.forEach(n => layer[n.step.id] = 0)
    const settled = new Set<string>()
    let queue = nodes.value.filter(n => indeg[n.step.id] === 0).map(n => n.step.id)
    let guard = 0
    while (queue.length && guard++ < 1000) {
      const cur = queue.shift()!
      if (settled.has(cur)) continue
      settled.add(cur)
      topoEdges.value.filter(e => e.from === cur).forEach(e => {
        layer[e.to] = Math.max(layer[e.to] || 0, (layer[cur] || 0) + 1)
        indeg[e.to]--
        if (indeg[e.to] === 0) queue.push(e.to)
      })
    }
    const remainder = nodes.value.filter(n => !settled.has(n.step.id))
    if (remainder.length) {
      const lastLayer = Math.max(0, ...Object.values(layer)) + 1
      remainder.forEach(n => { layer[n.step.id] = lastLayer })
    }
    nodes.value.forEach(n => { n.step.node_x = 40 + (layer[n.step.id] || 0) * (NODE_W + 90) })
    const byLayer: Record<number, TopoNode[]> = {}
    nodes.value.forEach(n => { const l = layer[n.step.id] || 0; (byLayer[l] ||= []).push(n) })
    Object.values(byLayer).forEach(group => group.forEach((n, i) => { n.step.node_y = 40 + i * (NODE_H + 30) }))
    refreshBounds()
  }
  if (record) mutate(apply); else apply()
}
function refreshBounds() {
  const geoms = edgeGeoms.value
  canvas_min_x.value = Math.round(Math.min(0, ...nodes.value.map(n => n.step.node_x), ...geoms.map(g => g.minX)) - 40)
  canvas_min_y.value = Math.round(Math.min(0, ...nodes.value.map(n => n.step.node_y), ...geoms.map(g => g.minY)) - 40)
  node_x_max.value = Math.round(Math.max(NODE_W, ...nodes.value.map(n => n.step.node_x + NODE_W), ...geoms.map(g => g.maxX)) + 120 - canvas_min_x.value)
  node_y_max.value = Math.round(Math.max(NODE_H, ...nodes.value.map(n => n.step.node_y + NODE_H), ...geoms.map(g => g.maxY)) + 120 - canvas_min_y.value)
}
function zoomBy(delta: number) { zoom.value = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, +(zoom.value + delta).toFixed(2))) }
function onWheel(e: WheelEvent) { zoomBy(e.deltaY < 0 ? 0.1 : -0.1) }
function zoomFit() {
  const wrap = document.querySelector('.topo-canvas-wrap') as HTMLElement | null
  if (!wrap || !nodes.value.length) { zoom.value = 1; return }
  const cw = wrap.clientWidth - 40, ch = wrap.clientHeight - 40
  zoom.value = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, +(Math.min(cw / node_x_max.value, ch / node_y_max.value)).toFixed(2)))
}

/* 拖拽 */
let drag: { key: string; startX: number; startY: number; ox: number; oy: number } | null = null
function onPointerDown(e: PointerEvent, node: TopoNode) {
  if (e.button !== 0 || e.ctrlKey || e.metaKey) return
  history.begin(snapshot())
  drag = { key: node.key, startX: e.clientX, startY: e.clientY, ox: node.step.node_x, oy: node.step.node_y }
  ;(e.currentTarget as Element).setPointerCapture?.(e.pointerId)
}
function onPointerMove(e: PointerEvent) {
  if (!drag) return
  const n = nodes.value.find(x => x.key === drag!.key)
  if (!n) return
  const scale = zoom.value || 1
  n.step.node_x = Math.max(0, drag.ox + (e.clientX - drag.startX) / scale)
  n.step.node_y = Math.max(0, drag.oy + (e.clientY - drag.startY) / scale)
  refreshBounds()
}
function onPointerUp() { if (drag) history.commit(snapshot()); drag = null }

/* 连线几何：侧边中点锚点，绕开其它节点，箭头按终点切线 */
function edgeGeometry(e: TopoEdge) {
  const boxes: TopoBox[] = nodes.value.map(n => ({
    id: n.step.id, x: n.step.node_x, y: n.step.node_y, width: NODE_W, height: NODE_H,
  }))
  return topoEdgeGeometry(e, boxes, topoEdges.value)
}
const edgeGeoms = computed(() => topoEdges.value.map(e => edgeGeometry(e)))
function edgePathAt(i: number) { return edgeGeoms.value[i]?.d || '' }
function edgeAngleAt(i: number) { return edgeGeoms.value[i]?.angle || '0deg' }
function edgeColor(e: TopoEdge) { return isBidirectionalSecondary(e, topoEdges.value) ? '#f59e0b' : '#2563eb' }

/* 保存：把拓扑写回三表（全量）；允许循环拓扑 */
async function save() {
  if (!canWrite.value) { error.value = '当前账号没有配置写入权限'; return }
  saving.value = true; error.value = ''; notice.value = ''
  try {
    // 新建流程尚未分配 id：先为其分配临时正式 id
    flows.value.forEach(f => { if (!f.id) f.id = `wbf-${Date.now()}-${f.code}` })
    if (currentFlowID.value && !flows.value.some(f => f.id === currentFlowID.value)) currentFlowID.value = flows.value[0]?.id || ''
    // 把当前拓扑节点坐标写回 steps，移除被删节点对应的 step
    const topoStepIDs = new Set(nodes.value.map(n => n.step.id))
    const currentStepIDs = new Set(steps.value.filter(s => s.flow_id === currentFlowID.value).map(s => s.id))
    const removed = [...currentStepIDs].filter(id => !topoStepIDs.has(id) && !nodes.value.some(n => n.step.id === id))
    // 新增节点（step.id 为空）分配 id 并写入 steps
    nodes.value.forEach(n => {
      if (!n.step.id) n.step.id = `wbs-${Date.now()}-${n.step.step_code}-${Math.random().toString(36).slice(2, 6)}`
      n.step.flow_id = currentFlowID.value
      n.step.sort_no = nodes.value.indexOf(n) + 1
      n.step.node_x = Math.round(n.step.node_x)
      n.step.node_y = Math.round(n.step.node_y)
      const idx = steps.value.findIndex(s => s.id === n.step.id)
      if (idx >= 0) steps.value[idx] = n.step
      else steps.value.push(n.step)
    })
    if (removed.length) steps.value = steps.value.filter(s => !removed.includes(s.id))
    // 边同步：当前流程的边以 topoEdges 为准
    edges.value = edges.value.filter(e => e.flow_id !== currentFlowID.value)
    topoEdges.value.forEach(e => edges.value.push({ id: '', flow_id: currentFlowID.value, from_step_id: e.from, to_step_id: e.to }))

    await api('/workflow-badge-config', { method: 'PUT', body: JSON.stringify({ flows: flows.value, steps: steps.value, edges: edges.value }) })
    notice.value = '流程拓扑已保存'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

function onKeydown(e: KeyboardEvent) {
  const t = e.target as HTMLElement | null
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return
  if (!(e.ctrlKey || e.metaKey)) return
  const key = e.key.toLowerCase()
  if (key === 'z') { e.preventDefault(); e.shiftKey ? redo() : undo() }
  else if (key === 'y') { e.preventDefault(); redo() }
}
onMounted(() => {
  void load().then(() => nextTick(zoomFit))
  window.addEventListener('resize', repositionInspector)
  window.addEventListener('keydown', onKeydown)
})
onUnmounted(() => {
  window.removeEventListener('resize', repositionInspector)
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div class="page-header">
    <div><h1>流程待办配置</h1><p>拓扑方式配置流程：环节为生产管理菜单项；左键选中环节，右键环节打开属性面板；Ctrl/Cmd 点击起点后可连续点击多个目标建立并行连线，也可在属性面板勾选多个后续环节。多线汇入即汇合，允许形成回路（如返工），保存与后端均不再做无环限制。上一步完成后相关角色在对应菜单看到待办气泡（超过 99 显示两个点）。</p></div>
    <div class="header-actions">
      <button class="button secondary" @click="load">刷新</button>
      <button class="button" :disabled="saving || !canWrite" @click="save">{{ saving ? '保存中…' : '保存配置' }}</button>
    </div>
  </div>
  <div v-if="error" class="notice error">{{ error }}</div>
  <div v-if="notice" class="notice success">{{ notice }}</div>
  <PageState :loading="loading" :error="error" @retry="load" />

  <template v-if="!loading">
    <div class="panel flow-bar">
      <label>流程 <select v-model="currentFlowID" @change="selectFlow(currentFlowID)"><option v-for="f in flows" :key="f.id" :value="f.id">{{ f.name }}</option></select></label>
      <label>名称 <input :value="currentFlow?.name || ''" :disabled="!canWrite || !currentFlow" placeholder="流程名称" style="width:180px" @input="setFlowName(($event.target as HTMLInputElement).value)"></label>
      <label>状态 <select :value="currentFlow?.status || 'active'" :disabled="!canWrite || !currentFlow" @change="setFlowStatus(($event.target as HTMLSelectElement).value)"><option value="active">启用</option><option value="disabled">停用</option></select></label>
      <button class="button secondary" :disabled="!canWrite" @click="addFlow">+ 新建流程</button>
      <button class="button secondary" :disabled="!canWrite || !currentFlow" @click="removeFlow">删除流程</button>
    </div>

    <div class="panel topo-panel">
      <div class="topo-toolbar">
        <button class="button" :disabled="!canWrite || !currentFlowID" @click="openPicker">+ 添加环节</button>
        <button class="button secondary" :disabled="!canWrite" @click="autoLayout()">自动布局</button>
        <button class="button secondary small" :disabled="!canUndo" title="撤销（Ctrl/Cmd+Z）" @click="undo">↶ 撤销</button>
        <button class="button secondary small" :disabled="!canRedo" title="重做（Ctrl/Cmd+Shift+Z 或 Ctrl+Y）" @click="redo">↷ 重做</button>
        <span class="topo-zoom">
          <button class="button secondary small" @click="zoomBy(-0.1)">−</button>
          <span>{{ Math.round(zoom * 100) }}%</span>
          <button class="button secondary small" @click="zoomBy(0.1)">+</button>
          <button class="button secondary small" @click="zoomFit">适应</button>
        </span>
        <span class="topo-legend"><i class="dot serial"></i>串行 <i class="dot parallel"></i>并行/汇合</span>
        <span class="legend-tip">左键选中 · 右键节点属性 · Ctrl+点击起点后连续点击多个目标连线 · 双向连线橙色区分 · Ctrl+Z 撤销 / Ctrl+Y 重做</span>
      </div>
      <div class="topo-canvas-wrap" @wheel.prevent="onWheel">
        <svg ref="svgRef" class="topo-canvas" :viewBox="`${canvas_min_x} ${canvas_min_y} ${node_x_max} ${node_y_max}`" :width="node_x_max * zoom" :height="node_y_max * zoom"
             @pointermove="onPointerMove" @pointerup="onPointerUp" @pointerleave="onPointerUp" @click.self="cancelLink">
          <defs>
            <marker v-for="(e, ei) in topoEdges" :key="'marker-'+ei" :id="'arrow-'+ei" markerWidth="10" markerHeight="8" refX="9" refY="4" :orient="edgeAngleAt(ei)"><path d="M0,0 L10,4 L0,8 Z" :fill="edgeColor(e)" /></marker>
          </defs>
          <g v-for="(e, ei) in topoEdges" :key="'e'+ei">
            <path :d="edgePathAt(ei)" class="topo-edge-hit" :class="{ active: selectedEdgeIndex === ei }" @click.stop="onEdgeClick(ei, $event)" />
            <path :d="edgePathAt(ei)" class="topo-edge" :style="{ stroke: edgeColor(e) }" :marker-end="'url(#arrow-'+ei+')'" />
          </g>
          <g v-for="n in nodes" :key="n.key" class="topo-node" :class="{ selected: selectedNodeKey === n.key, linking: linkSourceKey === n.key, parallel: isParallel(n) }"
             :transform="`translate(${n.step.node_x},${n.step.node_y})`" @pointerdown="onPointerDown($event, n)"
             @click.stop="onNodeClick(n.key, $event)" @contextmenu.prevent.stop="openNodeInspector(n.key, $event)">
            <rect class="node-box" :width="NODE_W" :height="NODE_H" rx="8" />
            <text class="node-code" x="10" y="18">{{ n.step.menu_path || '未关联菜单' }}</text>
            <text class="node-name" x="10" y="40">{{ nodeLabel(n) }}</text>
            <text class="node-meta" x="10" y="60">{{ n.step.permission_code || '—' }} · {{ countKinds.find(k => k[0] === n.step.count_kind)?.[1] || '不计数' }}</text>
            <circle v-if="isParallel(n)" class="node-flag" :cx="NODE_W-12" cy="12" r="5" />
          </g>
        </svg>
        <div v-if="!nodes.length" class="topo-empty">暂无环节。先新建流程并保存，再点击「+ 添加环节」从生产管理菜单中选择；右键环节可打开属性并配置多个后续环节。</div>
      </div>
      <div class="topo-footer"><span class="text-muted">共 {{ nodes.length }} 个环节 · {{ topoEdges.length }} 条连线</span></div>
    </div>
  </template>

  <!-- 节点/连线属性面板 -->
  <Teleport to="body">
    <div v-if="inspectorOpen && inspectorPos && selectedNodeKey" ref="inspectorRef" class="topo-inspector floating" :style="{ left: inspectorPos.x + 'px', top: inspectorPos.y + 'px' }">
      <div class="inspector-title">环节属性<button type="button" class="inspector-close" @click="closeInspector">×</button></div>
      <template v-for="n in nodes.filter(x => x.key === selectedNodeKey)" :key="n.key">
        <div class="inspector-grid">
          <label>名称<input :value="nodeLabel(n)" disabled></label>
          <label>菜单路径<input v-model.trim="n.step.menu_path" disabled></label>
          <label>权限码<input v-model.trim="n.step.permission_code" :disabled="!canWrite" placeholder="inventory:receipt"></label>
          <label>计数来源<select v-model="n.step.count_kind" :disabled="!canWrite"><option v-for="k in countKinds" :key="k[0]" :value="k[0]">{{ k[1] }}</option></select></label>
          <label>相关角色编码<input :value="rolesText(n)" :disabled="!canWrite" placeholder="warehouse,group_leader" @input="setRoles(n, ($event.target as HTMLInputElement).value)"></label>
          <label>触发事件<input v-model.trim="n.step.trigger_event" :disabled="!canWrite" placeholder="project.confirmed"></label>
          <label class="chk"><input v-model="n.step.enabled" type="checkbox" :disabled="!canWrite"> 启用</label>
        </div>
        <div class="inspector-section">
          <div class="section-title">后续环节（可多选，勾选多个即并行）</div>
          <label v-for="other in nodes.filter(x => x.key !== n.key)" :key="other.key" class="next-item">
            <input type="checkbox" :checked="hasEdge(n.step.id, other.step.id)" :disabled="!canWrite" @change="toggleNextStep(n.step.id, other.step.id, $event)">
            <span>{{ nodeLabel(other) }}</span>
            <small>{{ other.step.menu_path }}</small>
          </label>
          <div v-if="nodes.length <= 1" class="text-muted">暂无其他环节</div>
        </div>
        <p class="text-muted" style="margin:6px 0">并行/汇合由连线自动识别，无需手工设置。</p>
        <button class="button link text-danger" :disabled="!canWrite" @click="removeNode(n.key); closeInspector()">删除该环节</button>
      </template>
    </div>
    <div v-if="inspectorOpen && inspectorPos && selectedEdgeIndex >= 0 && topoEdges[selectedEdgeIndex]" ref="inspectorRef" class="topo-inspector floating" :style="{ left: inspectorPos.x + 'px', top: inspectorPos.y + 'px' }">
      <div class="inspector-title">连线属性<button type="button" class="inspector-close" @click="closeInspector">×</button></div>
      <div class="edge-info">
        <span>{{ nodeLabel(nodeByStep(topoEdges[selectedEdgeIndex].from)!) }}</span>
        <span class="edge-arrow">→</span>
        <span>{{ nodeLabel(nodeByStep(topoEdges[selectedEdgeIndex].to)!) }}</span>
      </div>
      <button class="button link text-danger" :disabled="!canWrite" @click="removeEdge(selectedEdgeIndex); closeInspector()">删除该连线</button>
    </div>
  </Teleport>

  <!-- 添加环节：生产管理菜单项 -->
  <ModalDialog v-if="pickerOpen" title="添加流程环节（生产管理菜单项）" width="70vw" height="80vh" body-class="picker-body" @close="pickerOpen = false">
    <div class="picker-toolbar">
      <input v-model.trim="pickerKeyword" placeholder="菜单名称/路径关键字">
      <span class="text-muted">已选 {{ pickerSelected.length }} 项</span>
    </div>
    <div class="picker-list">
      <label v-for="m in pickerItems" :key="m.path" class="picker-item">
        <input type="checkbox" :checked="pickerSelected.includes(m.path)" @change="togglePicker(m.path)">
        <span class="picker-name">{{ m.label }}</span>
      </label>
      <div v-if="!pickerItems.length" class="text-muted" style="padding:16px">没有可添加的菜单项</div>
    </div>
    <template #footer><button class="button secondary" @click="pickerOpen = false">取消</button><button class="button" :disabled="!pickerSelected.length" @click="confirmPicker">添加所选环节</button></template>
  </ModalDialog>
</template>

<style scoped>
.panel{margin-top:14px;border:1px solid var(--border);border-radius:10px;overflow:hidden}
.flow-bar{display:flex;flex-wrap:wrap;align-items:center;gap:12px;padding:10px 14px;background:#f8fafc}
.flow-bar label{display:inline-flex;align-items:center;gap:6px;font-size:13px;color:#41537e}
.flow-bar select,.flow-bar input{padding:6px 8px;border:1px solid var(--border);border-radius:8px;background:#fff;color:#1f2d4d}
.topo-panel{padding:0}
.topo-toolbar{display:flex;flex-wrap:wrap;align-items:center;gap:10px;padding:10px 14px;border-bottom:1px solid var(--border);background:#fafcff}
.topo-zoom{display:inline-flex;align-items:center;gap:6px}
.topo-zoom span{min-width:44px;text-align:center;font-size:13px;color:#41537e}
.topo-legend{display:inline-flex;align-items:center;gap:6px;font-size:13px;color:#41537e}
.topo-legend .dot{width:10px;height:10px;border-radius:50%;display:inline-block;background:#2563eb}
.topo-legend .dot.parallel{background:#f59e0b}
.legend-tip{font-size:12px;color:#8b9ac0}
.topo-canvas-wrap{position:relative;overflow:auto;max-height:60vh;background-color:#fbfdff;background-image:radial-gradient(#dbe4f5 1px,transparent 1px);background-size:18px 18px}
.topo-canvas{display:block}
.topo-empty{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;color:#8b9ac0;font-size:14px;pointer-events:none}
.topo-edge{fill:none;stroke:#2563eb;stroke-width:2;pointer-events:none}
.topo-edge-hit{fill:none;stroke:transparent;stroke-width:14;cursor:pointer}
.topo-edge-hit.active{stroke:#93c5fd}
.topo-node{cursor:grab;user-select:none}
.topo-node:active{cursor:grabbing}
.node-box{fill:#fff;stroke:#c7d5ef;stroke-width:1.5}
.topo-node.parallel .node-box{stroke:#f59e0b;stroke-width:2}
.topo-node.selected .node-box{stroke:#2563eb;stroke-width:2.5}
.topo-node.linking .node-box{stroke:#16a34a;stroke-width:2.5;stroke-dasharray:5 3}
.node-code{fill:#8b9ac0;font-size:10px}
.node-name{fill:#1f2d4d;font-size:16px;font-weight:700}
.node-meta{fill:#64748b;font-size:10px}
.node-flag{fill:#f59e0b}
.topo-footer{padding:8px 14px;border-top:1px solid var(--border);background:#f8fafc;font-size:13px}
.topo-inspector{position:fixed;z-index:10001;width:320px;max-width:calc(100vw - 24px);max-height:calc(100vh - 24px);overflow:auto;background:#fff;border:1px solid var(--border);border-radius:10px;box-shadow:0 12px 36px rgba(20,40,80,.16);padding:12px}
.inspector-title{display:flex;align-items:center;justify-content:space-between;font-weight:700;margin-bottom:8px}
.inspector-close{border:0;background:transparent;font-size:20px;cursor:pointer;color:#8b9ac0}
.inspector-grid{display:grid;grid-template-columns:1fr;gap:8px}
.inspector-grid label{display:flex;flex-direction:column;gap:4px;font-size:12px;color:#41537e}
.inspector-grid input,.inspector-grid select{padding:6px 8px;border:1px solid var(--border);border-radius:6px;font-size:13px;color:#1f2d4d}
.inspector-grid label.chk{flex-direction:row;align-items:center;gap:6px}
.inspector-section{margin-top:12px;padding-top:10px;border-top:1px dashed var(--border)}
.section-title{font-size:12px;font-weight:700;color:#41537e;margin-bottom:6px}
.next-item{display:flex;align-items:center;gap:6px;padding:4px 2px;font-size:12px;color:#1f2d4d;cursor:pointer}
.next-item span{flex:1}
.next-item small{color:#8b9ac0;font-size:10px}
.edge-info{display:flex;align-items:center;gap:8px;font-size:14px;margin-bottom:8px;flex-wrap:wrap}
.edge-arrow{color:#2563eb;font-weight:700}
:deep(.picker-body){display:flex!important;flex-direction:column;gap:10px;min-height:0}
.picker-toolbar{display:flex;align-items:center;gap:10px;flex:none}
.picker-toolbar input{flex:1;min-width:0}
.picker-list{flex:1;min-height:0;overflow:auto;display:grid;grid-template-columns:repeat(2,1fr);gap:8px}
.picker-item{display:flex;align-items:flex-start;gap:8px;padding:10px 12px;border:1px solid var(--border);border-radius:8px;cursor:pointer}
.picker-item input{flex:none;margin-top:2px}
.picker-item .picker-name{font-weight:600;color:#1f2d4d;flex:1;min-width:0;white-space:normal;word-break:break-word;line-height:1.4}
@media(max-width:900px){.picker-list{grid-template-columns:1fr}}
</style>
