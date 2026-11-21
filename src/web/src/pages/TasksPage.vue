<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api } from '../api'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import FilterSelect from '../components/FilterSelect.vue'

const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')

/* ===== 生产工单列表 + 查询 ===== */
const workOrders = ref<any[]>([])
const projects = ref<any[]>([])
const workshops = ref<any[]>([])
const lines = ref<any[]>([])
const cuttingSheets = ref<any[]>([])
const lineParticipants = ref<any[]>([])
const orderSheetIds = ref<string[]>([])
const detailSheets = ref<any[]>([])
const detailParticipants = ref<any[]>([])

const woKeyword = ref('')
const woFilter = ref('')
const filteredOrders = computed(() => {
  const kw = woFilter.value.trim().toLowerCase()
  if (!kw) return workOrders.value
  return workOrders.value.filter(o => [o.work_order_code, o.project_code, o.product_name].some(v => String(v || '').toLowerCase().includes(kw)))
})
function applySearch() { woFilter.value = woKeyword.value }

const workOrderStatusText: Record<string, string> = { pending_release: '待下发', in_production: '生产中', paused: '已暂停', completed: '已完成', cancelled: '已取消', draft: '草稿' }

async function load() {
  loading.value = true; error.value = ''
  try { workOrders.value = (await api<{ items: any[] }>('/work-orders', {}, { silent: true })).items } catch { workOrders.value = [] }
  loading.value = false
}
async function ensureBasics() {
  const loaders: Promise<void>[] = []
  if (!projects.value.length) loaders.push(api<{ items: any[] }>('/projects').then(r => { projects.value = r.items }))
  if (!workshops.value.length) loaders.push(api<{ items: any[] }>('/workshops').then(r => { workshops.value = r.items }))
  if (!lines.value.length) loaders.push(api<{ items: any[] }>('/lines').then(r => { lines.value = r.items }))
  if (!cuttingSheets.value.length) loaders.push(api<{ items: any[] }>('/cutting-sheets', {}, { silent: true }).then(r => { cuttingSheets.value = r.items }).catch(() => {}))
  try { await Promise.all(loaders) } catch (e) { error.value = (e as Error).message }
}

const projectOptions = computed(() => projects.value.map(p => ({ value: p.id, label: `${p.project_code} · ${p.product_name || ''}` })))
const workshopOptions = computed(() => workshops.value.map(w => ({ value: w.id, label: `${w.code} · ${w.name}` })))
const linesOf = (workshopID: string) => lines.value.filter(l => !workshopID || l.workshop_id === workshopID)
const createProjectSheets = computed(() => cuttingSheets.value.filter(s => s.project_id === orderForm.project_id))
const editProjectSheets = computed(() => cuttingSheets.value.filter(s => s.project_id === editOrder.value?.project_id))
function sheetText(s: any) { return `${s.cutting_no} · ${s.style_no || '—'} · ${s.color || '—'} · ${s.piece_qty ?? 0} 件` }
function toggle(list: string[], id: string, checked: boolean) {
  if (checked && !list.includes(id)) list.push(id)
  if (!checked) { const i = list.indexOf(id); if (i >= 0) list.splice(i, 1) }
}
async function loadLineParticipants(lineID: string) {
  if (!lineID) { lineParticipants.value = []; return }
  try {
    const res = await api<{ items: any[] }>(`/work-orders/line-participants?line_id=${lineID}`)
    lineParticipants.value = res.items
  } catch (e) { error.value = (e as Error).message }
}

/* ===== 新建生产工单 ===== */
const woModal = ref<'' | 'create' | 'edit' | 'detail' | 'dispatch'>('')
const orderForm = reactive({ project_id: '', work_order_code: '', planned_product_qty: 0, workshop_id: '', line_id: '' })
const showEmployeeNo = ref(true)

async function openCreateWorkOrder() {
  error.value = ''
  await ensureBasics()
  if (!projects.value.length) { error.value = '请先在「产品订单管理」中创建订单项目'; return }
  Object.assign(orderForm, { project_id: '', work_order_code: '', planned_product_qty: 0, workshop_id: '', line_id: '' })
  orderSheetIds.value = []; lineParticipants.value = []
  woModal.value = 'create'
}
function onOrderProjectChange() {
  const project = projects.value.find(p => p.id === orderForm.project_id)
  orderForm.planned_product_qty = Number(project?.order_qty || 0)
  orderSheetIds.value = []
}
function onOrderLineChange() { loadLineParticipants(orderForm.line_id) }
async function createWorkOrder() {
  if (!orderForm.project_id) { error.value = '请选择订单号'; return }
  if (!orderForm.planned_product_qty || orderForm.planned_product_qty <= 0) { error.value = '计划产品数必填'; return }
  saving.value = true; error.value = ''
  try {
    const created = await api<{ id: string }>('/work-orders', { method: 'POST', body: JSON.stringify({
      work_order_code: orderForm.work_order_code.trim() || `GD${Date.now()}`,
      project_id: orderForm.project_id,
      planned_product_qty: Number(orderForm.planned_product_qty),
      workshop_org_unit_id: orderForm.workshop_id || null,
      line_org_unit_id: orderForm.line_id || null,
    }) })
    await api(`/work-orders/${created.id}/cutting-sheets`, { method: 'PUT', body: JSON.stringify({ cutting_sheet_ids: orderSheetIds.value }) })
    woModal.value = ''; notice.value = '生产工单已创建'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

/* ===== 编辑 / 删除工单 ===== */
const editOrder = ref<any>(null)
const editForm = reactive({ planned_product_qty: 0, priority: 'normal', workshop_id: '', line_id: '', version: 0 })
const editSheetIds = ref<string[]>([])
async function openEditOrder(item: any) {
  error.value = ''
  await ensureBasics()
  try {
    const [detail, sheets] = await Promise.all([
      api<any>(`/work-orders/${item.id}`),
      api<{ items: any[] }>(`/work-orders/${item.id}/cutting-sheets`),
    ])
    editOrder.value = detail
    editSheetIds.value = sheets.items.map(s => s.id)
    lineParticipants.value = []
    Object.assign(editForm, {
      planned_product_qty: Number(detail.planned_product_qty || 0),
      priority: detail.priority || 'normal',
      workshop_id: detail.workshop_org_unit_id || '',
      line_id: detail.line_org_unit_id || '',
      version: detail.version,
    })
    await loadLineParticipants(editForm.line_id)
    woModal.value = 'edit'
  } catch (e) { error.value = (e as Error).message }
}
function onEditLineChange() { loadLineParticipants(editForm.line_id) }
async function saveEditOrder() {
  if (!editOrder.value) return
  if (!editForm.planned_product_qty || editForm.planned_product_qty <= 0) { error.value = '计划产品数必填'; return }
  saving.value = true; error.value = ''
  try {
    await api(`/work-orders/${editOrder.value.id}`, { method: 'PATCH', body: JSON.stringify({
      version: editForm.version,
      planned_product_qty: Number(editForm.planned_product_qty),
      priority: editForm.priority,
      workshop_org_unit_id: editForm.workshop_id || null,
      line_org_unit_id: editForm.line_id || null,
      planned_start_at: editOrder.value.planned_start_at ?? null,
      planned_end_at: editOrder.value.planned_end_at ?? null,
      reason: '工单编辑',
    }) })
    await api(`/work-orders/${editOrder.value.id}/cutting-sheets`, { method: 'PUT', body: JSON.stringify({ cutting_sheet_ids: editSheetIds.value }) })
    woModal.value = ''; notice.value = '工单已更新'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function removeOrder(item: any) {
  if (!window.confirm(`确认删除工单 ${item.work_order_code}？仅未下发且未流转到裁床环节的工单可删除。`)) return
  error.value = ''
  try { await api(`/work-orders/${item.id}`, { method: 'DELETE' }); notice.value = '工单已删除'; await load() } catch (e) { error.value = (e as Error).message }
}

/* ===== 工单明细：裁床单与生产线参与者 ===== */
const detailOrder = ref<any>(null)

async function openDetail(item: any) {
  error.value = ''
  await ensureBasics()
  try {
    const [order, sheets, participants] = await Promise.all([
      api<any>(`/work-orders/${item.id}`),
      api<{ items: any[] }>(`/work-orders/${item.id}/cutting-sheets`),
      api<{ items: any[] }>(`/work-orders/${item.id}/participants`),
    ])
    detailOrder.value = order
    detailSheets.value = sheets.items
    detailParticipants.value = participants.items
  } catch (e) { error.value = (e as Error).message; return }
  woModal.value = 'detail'
}

/* ===== 工序派工：裁床单 + 产品工序 → 生产线员工 ===== */
const dispatchTasks = ref<any[]>([])
const dispatchTaskId = ref('')
const dispatchSheetId = ref('')
const dispatchEmployeeId = ref('')
const dispatchQty = ref(0)
const dispatchRows = ref<any[]>([])
const dispatchSelected = reactive<Record<string, boolean>>({})
const dispatchQtys = reactive<Record<string, number>>({})
const dispatchTask = computed(() => dispatchTasks.value.find(t => t.id === dispatchTaskId.value))
const dispatchMembers = computed(() => detailParticipants.value)
const dispatchSheets = computed(() => detailSheets.value)

async function openDispatch() {
  if (!detailOrder.value) return
  error.value = ''
  dispatchTaskId.value = ''; dispatchSheetId.value = ''; dispatchEmployeeId.value = ''
  dispatchQty.value = 0; dispatchRows.value = []
  Object.keys(dispatchSelected).forEach(k => delete dispatchSelected[k])
  Object.keys(dispatchQtys).forEach(k => delete dispatchQtys[k])
  try {
    const items = (await api<{ items: any[] }>(`/production-tasks?work_order_id=${detailOrder.value.id}`)).items
    dispatchTasks.value = items.filter((t: any) => !t.parent_task_id)
  } catch (e) { error.value = (e as Error).message }
  woModal.value = 'dispatch'
}
async function loadDispatchBundles() {
  dispatchRows.value = []
  if (!detailOrder.value || !dispatchTask.value || dispatchTask.value.report_mode !== 'A') return
  try {
    const items = (await api<{ items: any[] }>(`/work-orders/${detailOrder.value.id}/assignable-bundles`)).items
    let rows = items.filter((r: any) => r.task_id === dispatchTask.value.id && r.assignable_qty > 0)
    if (dispatchSheetId.value) {
      const bundleNos = new Set<string>()
      const sheet = await api<{ beds: any[] }>(`/cutting-sheets/${dispatchSheetId.value}`)
      for (const bed of sheet.beds || []) {
        try {
          const d = await api<{ rows: any[] }>(`/cutting-sheets/${dispatchSheetId.value}/details?bed_id=${bed.id}`)
          ;(d.rows || []).forEach((row: any) => bundleNos.add(String(row.bundle_no)))
        } catch { /* 单床明细加载失败时不过滤 */ }
      }
      if (bundleNos.size) rows = rows.filter((r: any) => bundleNos.has(String(r.bundle_no)))
    }
    dispatchRows.value = rows
    for (const row of rows) dispatchQtys[row.bundle_id] = row.assignable_qty
  } catch (e) { error.value = (e as Error).message }
}
async function submitDispatch() {
  if (!detailOrder.value) return
  const task = dispatchTask.value
  if (!task) { error.value = '请选择产品工序'; return }
  if (!dispatchEmployeeId.value) { error.value = '请选择组员'; return }
  saving.value = true; error.value = ''
  try {
    if (task.report_mode === 'A') {
      const items = dispatchRows.value.filter(r => dispatchSelected[r.bundle_id]).map(r => ({ bundle_id: r.bundle_id, employee_id: dispatchEmployeeId.value, planned_qty: Number(dispatchQtys[r.bundle_id] || r.assignable_qty) }))
      if (!items.length) { saving.value = false; error.value = '请选择至少一个扎'; return }
      await api(`/production-tasks/${task.id}/bundle-assignments`, { method: 'POST', body: JSON.stringify({ version: task.version, items }) })
      notice.value = `已按裁床单扎号派工给组员，共 ${items.length} 个扎`
    } else {
      if (!dispatchQty.value || dispatchQty.value <= 0) { saving.value = false; error.value = '请填写派工数量'; return }
      await api(`/production-tasks/${task.id}/assign`, { method: 'POST', body: JSON.stringify({ version: task.version, assignments: [{ employee_id: dispatchEmployeeId.value, planned_qty: Number(dispatchQty.value) }] }) })
      notice.value = '已按工序派工给组员'
    }
    woModal.value = 'detail'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}

onMounted(load)
</script>
<template>
  <div class="page-header"><div><h1>生产工单管理</h1><p>列出全部生产工单；按订单订单号安排车间/生产线并分配裁床单，生产线员工全部参与工单并按工序派工</p></div><div class="header-actions"><button class="button" @click="openCreateWorkOrder">+ 新建生产工单</button><button class="button secondary" @click="load">刷新</button></div></div>
  <div v-if="error && !woModal" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>

  <section class="panel">
    <div class="toolbar"><input v-model="woKeyword" class="search-input" placeholder="搜索工单编号、订单号或产品" @keyup.enter="applySearch"><button class="button secondary" @click="applySearch">检索</button></div>
    <PageState :loading="loading" />
    <div v-if="!loading" class="table-wrap"><table><thead><tr><th>工单编号</th><th>订单号</th><th>产品</th><th>计划数</th><th>完成入库</th><th>状态</th><th>生产线</th><th>裁床单</th><th>操作</th></tr></thead><tbody><tr v-for="item in filteredOrders" :key="item.id"><td><strong>{{ item.work_order_code }}</strong></td><td>{{ item.project_code || '—' }}</td><td>{{ item.product_name }}</td><td>{{ item.planned_product_qty }}</td><td>{{ item.approved_good_qty }}</td><td><span class="status" :class="item.status">{{ workOrderStatusText[item.status] || item.status }}</span></td><td>{{ item.line_name || '—' }}</td><td>{{ item.sheet_count || 0 }}</td><td><button class="button link" @click="openEditOrder(item)">编辑</button><button class="button link text-danger" @click="removeOrder(item)">删除</button><button class="button link" @click="openDetail(item)">明细</button></td></tr></tbody></table><PageState :empty="!filteredOrders.length" /></div>
  </section>

  <!-- 新建生产工单：80vw × 90vh，表单固定、员工区固定、裁床单区占满剩余高度 -->
  <ModalDialog v-if="woModal === 'create'" title="新建生产工单" wide width="80vw" height="90vh" body-class="wo-create-body" @close="woModal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid wo-form-grid">
      <div class="field wo-project"><label class="required">订单号</label><FilterSelect v-model="orderForm.project_id" :options="projectOptions" placeholder="选择订单号" @change="onOrderProjectChange"/></div>
      <div class="field wo-code"><label>工单编号</label><input v-model.trim="orderForm.work_order_code" placeholder="留空自动生成"></div>
      <div class="field wo-qty"><label class="required">计划产品数</label><input v-model.number="orderForm.planned_product_qty" type="number" min="1"></div>
      <div class="field wo-workshop"><label>车间</label><FilterSelect v-model="orderForm.workshop_id" :options="workshopOptions" placeholder="选择车间" @change="orderForm.line_id = ''"/></div>
      <div class="field wo-line"><label>生产线</label><FilterSelect v-model="orderForm.line_id" :options="linesOf(orderForm.workshop_id).map(l => ({ value: l.id, label: l.name }))" placeholder="选择生产线" @change="onOrderLineChange"/></div>
    </div>
    <div class="resource-section employee-section">
      <div class="resource-head"><strong>生产线员工</strong><span class="text-muted">生产线管理中分配到该生产线的员工都会参与本工单，无需另建生产组</span><label class="inline-check"><input type="checkbox" v-model="showEmployeeNo">显示员工编号</label></div>
      <div v-if="!orderForm.line_id" class="text-muted">请先选择生产线</div>
      <div v-else class="check-list static-list">
        <span v-for="m in lineParticipants" :key="m.employee_id" class="check-item"><template v-if="showEmployeeNo">{{ m.employee_no }} · </template>{{ m.name }}<em v-if="m.is_manager">线长</em></span>
        <span v-if="!lineParticipants.length" class="text-muted">该生产线暂无在职员工</span>
      </div>
    </div>
    <div class="resource-section sheet-section">
      <div class="resource-head"><strong>分配裁床单</strong><span class="text-muted">选择订单号后显示对应裁床单：单号 / 款号 / 颜色 / 件数</span></div>
      <div v-if="!orderForm.project_id" class="text-muted">请先选择订单号</div>
      <div v-else class="check-list sheet-list">
        <label v-for="s in createProjectSheets" :key="s.id" class="check-item"><input type="checkbox" :checked="orderSheetIds.includes(s.id)" @change="toggle(orderSheetIds, s.id, ($event.target as HTMLInputElement).checked)">{{ sheetText(s) }}</label>
        <span v-if="!createProjectSheets.length" class="text-muted">该项目暂无裁床单</span>
      </div>
    </div>
    <template #footer><button class="button secondary" @click="woModal = ''">取消</button><button class="button" :disabled="saving" @click="createWorkOrder">创建工单</button></template>
  </ModalDialog>

  <!-- 编辑工单 -->
  <ModalDialog v-if="woModal === 'edit' && editOrder" :title="`编辑工单 · ${editOrder.work_order_code}`" wide @close="woModal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label>工单编号</label><input :value="editOrder.work_order_code" disabled></div>
      <div class="field"><label>订单号</label><input :value="projects.find(p => p.id === editOrder.project_id)?.project_code || editOrder.project_id || '—'" disabled></div>
      <div class="field"><label class="required">计划产品数</label><input v-model.number="editForm.planned_product_qty" type="number" min="1"></div>
      <div class="field"><label>紧急程度</label><FilterSelect v-model="editForm.priority" :options="[{value:'normal',label:'普通'},{value:'urgent',label:'紧急'}]"/></div>
      <div class="field"><label>车间</label><FilterSelect v-model="editForm.workshop_id" :options="workshopOptions" placeholder="选择车间" @change="editForm.line_id = ''"/></div>
      <div class="field"><label>生产线</label><FilterSelect v-model="editForm.line_id" :options="linesOf(editForm.workshop_id).map(l => ({ value: l.id, label: l.name }))" placeholder="选择生产线" @change="onEditLineChange"/></div>
    </div>
    <div class="resource-section">
      <div class="resource-head"><strong>生产线员工</strong><span class="text-muted">当前生产线的员工自动参与本工单</span></div>
      <div class="check-list static-list">
        <span v-for="m in lineParticipants" :key="m.employee_id" class="check-item">{{ m.employee_no }} · {{ m.name }}<em v-if="m.is_manager">线长</em></span>
        <span v-if="!lineParticipants.length" class="text-muted">该生产线暂无在职员工</span>
      </div>
    </div>
    <div class="resource-section">
      <div class="resource-head"><strong>分配裁床单</strong><span class="text-muted">仅显示该项目下的裁床单</span></div>
      <div class="check-list">
        <label v-for="s in editProjectSheets" :key="s.id" class="check-item"><input type="checkbox" :checked="editSheetIds.includes(s.id)" @change="toggle(editSheetIds, s.id, ($event.target as HTMLInputElement).checked)">{{ sheetText(s) }}</label>
        <span v-if="!editProjectSheets.length" class="text-muted">该项目暂无裁床单</span>
      </div>
    </div>
    <div class="notice">计划数受项目订单总量约束；已完成或已取消的工单不能编辑。</div>
    <template #footer><button class="button secondary" @click="woModal = ''">取消</button><button class="button" :disabled="saving" @click="saveEditOrder">保存修改</button></template>
  </ModalDialog>

  <!-- 工单明细：裁床单 + 生产线参与者 + 派工 -->
  <ModalDialog v-if="woModal === 'detail' && detailOrder" :title="`工单明细 · ${detailOrder.work_order_code}`" wide width="min(1100px, 96vw)" @close="woModal = ''">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="quantity-summary"><span>订单号<strong>{{ projects.find(p => p.id === detailOrder.project_id)?.project_code || detailOrder.project_id || '—' }}</strong></span><span>计划数<strong>{{ detailOrder.planned_product_qty }}</strong></span><span>状态<strong>{{ workOrderStatusText[detailOrder.status] || detailOrder.status }}</strong></span><span>生产线<strong>{{ lines.find(l => l.id === detailOrder.line_org_unit_id)?.name || '—' }}</strong></span></div>
    <div class="resource-section">
      <div class="resource-head"><strong>已分配裁床单</strong><button v-if="detailOrder.status === 'in_production'" class="button" @click="openDispatch">按工序派工</button></div>
      <div class="table-wrap"><table><thead><tr><th>裁床单号</th><th>款号</th><th>颜色</th><th>件数</th></tr></thead><tbody>
        <tr v-for="s in detailSheets" :key="s.id"><td><strong>{{ s.cutting_no }}</strong></td><td>{{ s.style_no || '—' }}</td><td>{{ s.color || '—' }}</td><td>{{ s.piece_qty }}</td></tr>
      </tbody></table><PageState :empty="!detailSheets.length" /></div>
    </div>
    <div class="resource-section">
      <div class="resource-head"><strong>生产线参与者</strong><span class="text-muted">来自生产线管理中的在职分配，全部可参与派工</span></div>
      <div class="table-wrap"><table><thead><tr><th>工号</th><th>姓名</th><th>岗位</th><th>线长</th></tr></thead><tbody>
        <tr v-for="m in detailParticipants" :key="m.employee_id"><td>{{ m.employee_no }}</td><td>{{ m.name }}</td><td>{{ m.position || '—' }}</td><td>{{ m.is_manager ? '是' : '—' }}</td></tr>
      </tbody></table><PageState :empty="!detailParticipants.length" /></div>
    </div>
    <template #footer><button class="button secondary" @click="woModal = ''">关闭</button></template>
  </ModalDialog>

  <!-- 工序派工：裁床单 + 工序 → 生产线员工 -->
  <ModalDialog v-if="woModal === 'dispatch'" title="工序派工" wide @close="woModal = 'detail'">
    <div class="quantity-summary"><span>工单<strong>{{ detailOrder?.work_order_code }}</strong></span><span>裁床单<strong>{{ dispatchSheets.map((s: any) => s.cutting_no).join('、') || '—' }}</strong></span></div>
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field"><label class="required">产品工序</label><FilterSelect v-model="dispatchTaskId" :options="dispatchTasks.map(t => ({ value: t.id, label: `${t.process_code} · ${t.process_name} · 剩余${t.remaining_qty}` }))" placeholder="选择工序" @change="loadDispatchBundles"/></div>
      <div class="field"><label>裁床单</label><FilterSelect v-model="dispatchSheetId" :options="dispatchSheets.map((s: any) => ({ value: s.id, label: sheetText(s) }))" placeholder="全部已分配裁床单" @change="loadDispatchBundles"/></div>
      <div class="field"><label class="required">参与员工</label><FilterSelect v-model="dispatchEmployeeId" :options="dispatchMembers.map((m: any) => ({ value: m.employee_id, label: `${m.employee_no} · ${m.name}` }))" placeholder="选择员工"/></div>
      <div v-if="dispatchTask && dispatchTask.report_mode !== 'A'" class="field"><label class="required">派工数量</label><input v-model.number="dispatchQty" type="number" min="1" :max="dispatchTask.remaining_qty"></div>
    </div>
    <template v-if="dispatchTask && dispatchTask.report_mode === 'A'">
      <div class="table-wrap"><table><thead><tr><th>选择</th><th>扎号</th><th>颜色/尺码</th><th>件数</th><th>已派</th><th>可派</th><th>本次数量</th></tr></thead><tbody><tr v-for="row in dispatchRows" :key="row.bundle_id"><td><input v-model="dispatchSelected[row.bundle_id]" type="checkbox"></td><td>{{ row.bundle_no }}</td><td>{{ row.color }}/{{ row.size }}</td><td>{{ row.bundle_qty }}</td><td>{{ row.assigned_qty }}</td><td>{{ row.assignable_qty }}</td><td><input v-model.number="dispatchQtys[row.bundle_id]" type="number" min="1" :max="row.assignable_qty" style="width:110px"></td></tr></tbody></table><PageState :empty="!dispatchRows.length" /></div>
      <div class="notice">模式 A：按裁床单的扎号派工，员工按扎报工。</div>
    </template>
    <div v-else-if="dispatchTask" class="notice">模式 B：按数量派工，无需选择扎。</div>
    <template #footer><button class="button secondary" @click="woModal = 'detail'">返回明细</button><button class="button" :disabled="saving" @click="submitDispatch">确认派工</button></template>
  </ModalDialog>
</template>
<style scoped>
.toolbar{display:flex;gap:10px;align-items:center;margin-bottom:12px;flex-wrap:wrap}
.resource-section{margin-top:14px;border-top:1px solid var(--border);padding-top:12px}
.resource-head{display:flex;align-items:center;gap:12px;margin-bottom:10px;flex-wrap:wrap}
.resource-head .text-muted{flex:1;font-size:12px}
.check-list{display:flex;flex-wrap:wrap;gap:6px 16px;max-height:140px;overflow-y:auto;border:1px solid var(--border);border-radius:8px;padding:8px 10px}
.check-item{display:flex;align-items:center;gap:5px;font-size:13px;white-space:nowrap}
.check-item input{width:auto;min-width:0}
.check-item em{font-style:normal;color:var(--primary);font-size:11px}
.static-list .check-item{padding:2px 6px;border-radius:6px;background:#f7f9fc}
.wo-form-grid{display:grid;grid-template-columns:repeat(12,minmax(0,1fr));gap:16px;flex:0 0 auto}
.wo-form-grid .field{min-width:0}
.wo-form-grid .wo-project,.wo-form-grid .wo-code{grid-column:span 6}
.wo-form-grid .wo-qty{grid-column:span 2}
.wo-form-grid .wo-workshop,.wo-form-grid .wo-line{grid-column:span 5}
.wo-form-grid :deep(.filter-select){display:block;width:100%}
.wo-form-grid :deep(.filter-select > input){width:100%;min-width:0}
.inline-check{display:flex;align-items:center;gap:4px;font-size:12px;color:#41537e;white-space:nowrap}
.inline-check input{width:14px;height:14px;min-width:0}
.employee-section{flex:0 0 auto}
.employee-section .check-list{height:148px;max-height:148px;align-content:flex-start}
.sheet-section{flex:1 1 auto;min-height:0;display:flex;flex-direction:column}
.sheet-section .check-list{flex:1 1 auto;max-height:none;align-content:flex-start;overflow-y:auto}
.sheet-section .check-item{white-space:normal;word-break:break-all}
</style>
<style>
.wo-create-body{display:flex;flex-direction:column;min-height:0;gap:0}
</style>
