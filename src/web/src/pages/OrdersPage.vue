<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api, command, statusText } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import DatePicker from '../components/DatePicker.vue'
import FilterSelect from '../components/FilterSelect.vue'
import ProductImage from '../components/ProductImage.vue'
import SceneBadge from '../components/SceneBadge.vue'

const { has, ready } = usePermissions()
const canProjectRead = computed(() => has('project:read'))
const canWorkOrderRead = computed(() => has('work_order:read'))
const canCuttingRead = computed(() => has('cutting:read'))
const canProjectWrite = computed(() => has('project:write'))
const canWorkOrderWrite = computed(() => has('work_order:write'))
const canCuttingWrite = computed(() => has('cutting:write'))
const canReview = computed(() => has('work_order:review'))

type TabKey = 'projects' | 'work-orders' | 'cutting-orders'
const availableTabs = computed<TabKey[]>(() => {
  const tabs: TabKey[] = []
  if (canWorkOrderRead.value) tabs.push('work-orders')
  if (canProjectRead.value) tabs.push('projects')
  if (canCuttingRead.value) tabs.push('cutting-orders')
  return tabs
})
const tab = ref<TabKey>('work-orders')
const data = reactive<Record<string, any[]>>({ projects: [], 'work-orders': [], 'cutting-orders': [] })
const products = ref<any[]>([])
const customers = ref<any[]>([])
const loading = ref(false)
const loaded = ref(false)
const error = ref('')
const notice = ref('')
const modal = ref('')
const saving = ref(false)
const projectForm = reactive({ project_code: '', customer_id: '', product_id: '', style_no: '', style_name: '', order_qty: 900, customer_unit_price: '0', external_unit_price: '0', required_delivery_date: '' })
const orderForm = reactive({ work_order_code: '', project_id: '', planned_product_qty: 900, priority: 'normal' })
const cuttingForm = reactive({ cutting_no: '', work_order_id: '', marker_id: '', cut_qty: 900, color: '黑色', size: 'M', bundle_count: 10 })
const markers = ref<any[]>([])
const markerOptions = computed(() => markers.value.map(m => ({ value: m.id, label: `${m.marker_no} · ${m.color}${m.review_status === 'approved' ? '' : '（未审核）'}` })))
const statusFilter = ref('')
/* 场景化菜单（设计 6.12.7）：委托方视角工单列表分「本厂/外协」，行带场景徽标与外协角标 */
const partyFilter = ref('')
const sceneFilter = ref('')
const projectAdjust = ref<any>(null)
const adjustForm = reactive({ customer_name: '', product_name: '', style_no: '', style_name: '', order_qty: 0, required_delivery_date: '', customer_unit_price: '', external_unit_price: '', version: 0, reason: '' })
function openAdjust(item: any) {
  projectAdjust.value = item
  Object.assign(adjustForm, {
    customer_name: item.customer_name || '', product_name: item.product_name || '',
    style_no: item.style_no || '', style_name: item.style_name || '',
    order_qty: item.order_qty, required_delivery_date: item.required_delivery_date || '',
    customer_unit_price: item.customer_unit_price || '', external_unit_price: item.external_unit_price || '',
    version: item.version, reason: ''
  })
  error.value = ''
}
async function saveAdjust() {
  if (!projectAdjust.value) return
  if (!adjustForm.reason.trim()) { error.value = '变更原因必填'; return }
  if (!adjustForm.customer_name.trim() || !adjustForm.product_name.trim()) { error.value = '客户和产品名称必填'; return }
  saving.value = true; error.value = ''
  try {
    await api(`/projects/${projectAdjust.value.id}`, { method: 'PATCH', body: JSON.stringify({
      customer_name: adjustForm.customer_name.trim(), product_name: adjustForm.product_name.trim(),
      style_no: adjustForm.style_no.trim(), style_name: adjustForm.style_name.trim(),
      order_qty: Number(adjustForm.order_qty), required_delivery_date: adjustForm.required_delivery_date || null,
      customer_unit_price: adjustForm.customer_unit_price, external_unit_price: adjustForm.external_unit_price,
      version: adjustForm.version, reason: adjustForm.reason
    }) })
    projectAdjust.value = null; notice.value = '项目已更新'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
/* 委托方视角：列表分「本厂/外协」，并支持场景筛选（6.12.7） */
const visibleWorkOrders = computed(() => data['work-orders'].filter(o =>
  (!partyFilter.value || o.party === partyFilter.value) && (!sceneFilter.value || o.scene_type === sceneFilter.value)))
const projectOptions = computed(() => data.projects.map(item => ({ value: item.id, label: `${item.project_code} · ${item.product_name}` })))
const productOptions = computed(() => products.value.map(item => ({ value: item.id, label: `${item.product_code} · ${item.name}` })))
const customerOptions = computed(() => customers.value.map(item => ({ value: item.id, label: item.name })))
/* 编辑弹窗按名称存取（value=名称），避免选中字典项后写入 UUID */
const customerNameOptions = computed(() => customers.value.map(item => ({ value: item.name, label: item.name })))
const productNameOptions = computed(() => products.value.map(item => ({ value: item.name, label: item.name })))
const cuttingOrderOptions = computed(() => data['work-orders'].filter(o => o.status === 'pending_release').map(item => ({ value: item.id, label: `${item.work_order_code} · ${item.product_name}` })))

/* 各区块独立加载：无权限的接口直接跳过，其余接口互不影响，避免整页因某个 403 报错 */
async function loadSection(path: string): Promise<{ items: any[] } | null> {
  try { return await api<{ items: any[] }>(path, {}, { silent: true }) } catch { return null }
}
async function load() {
  if (!ready.value) return
  loading.value = true; error.value = ''
  const [projects, orders, cuttings, productData, customerData] = await Promise.all([
    canProjectRead.value ? loadSection('/projects') : Promise.resolve(null),
    canWorkOrderRead.value ? loadSection('/work-orders' + (statusFilter.value ? '?status=' + statusFilter.value : '')) : Promise.resolve(null),
    canCuttingRead.value ? loadSection('/cutting-orders') : Promise.resolve(null),
    loadSection('/products'),
    loadSection('/customers'),
  ])
  if (projects) data.projects = projects.items
  if (cuttings) data['cutting-orders'] = cuttings.items
  if (productData) products.value = productData.items
  if (customerData) customers.value = customerData.items
  /* 补充场景元数据：本厂/外协分组与场景徽标不改变原有工单数据结构 */
  const sceneRows = canWorkOrderRead.value ? await loadSection('/scene/work-orders') : null
  if (orders) {
    const sceneMap = new Map((sceneRows?.items || []).map((s: any) => [s.id, s]))
    data['work-orders'] = orders.items.map((o: any) => {
      const s: any = sceneMap.get(o.id)
      return { ...o, scene_type: s?.scene_type || 'A', scene_sub: s?.scene_sub || 'A1',
        party: s?.party || '本厂', outsource: !!s?.outsource, counterparty: s?.counterparty || '' }
    })
  }
  loading.value = false
  loaded.value = true
}
/* 进入页面即加载：权限已就绪时立即加载，未就绪时等权限到位再加载。
 * 当前 tab 无权限时切到第一个有权限的 tab。 */
watch(ready, (r) => {
  if (!r) return
  if (availableTabs.value.length && !availableTabs.value.includes(tab.value)) tab.value = availableTabs.value[0]
  void load()
}, { immediate: true })
/* 切换 tab 时确保数据已加载（进入页面 watch 通常已完成，这里兜底防止空列表） */
function switchTab(next: TabKey) {
  tab.value = next
  if (!loaded.value && !loading.value) void load()
}
function open(type: string) {
  error.value = ''
  if (type === 'project' && !customers.value.length) { error.value = '请先在“员工与客户”中新增客户'; return }
  if (type === 'order' && !data.projects.length) { error.value = '请先创建项目'; return }
  if (type === 'project') Object.assign(projectForm, { project_code: '', customer_id: '', product_id: '', style_no: '', style_name: '', order_qty: 900, customer_unit_price: '0', external_unit_price: '0', required_delivery_date: '' })
  if (type === 'cutting') { cuttingForm.marker_id = ''; markers.value = [] }
  modal.value = type
}
async function loadMarkers() {
  markers.value = []
  if (!cuttingForm.work_order_id) return
  try { markers.value = (await api<{ items: any[] }>(`/markers?work_order_id=${cuttingForm.work_order_id}&review_status=approved`)).items } catch { markers.value = [] }
  if (markers.value.length && !cuttingForm.marker_id) cuttingForm.marker_id = markers.value[0].id
}
function onCuttingOrderChange() {
  cuttingForm.cut_qty = data['work-orders'].find(o => o.id === cuttingForm.work_order_id)?.planned_product_qty || 0
  cuttingForm.marker_id = ''
  loadMarkers()
}
async function reviewOrder(item: any) {
  if (!window.confirm(`确认审核通过工单 ${item.work_order_code}？通过后才能下发排产。`)) return
  error.value = ''
  try { await api(`/work-orders/${item.id}/review`, { method: 'POST', body: JSON.stringify({ version: item.version, decision: 'approve' }) }); notice.value = '工单审核通过，可以下发'; await load() } catch (e) { error.value = (e as Error).message }
}
function onProjectProductChange() {
  const product = products.value.find(item => item.id === projectForm.product_id)
  if (!product) return
  if (!projectForm.style_no) projectForm.style_no = product.product_code || ''
  if (!projectForm.style_name) projectForm.style_name = product.name || ''
}
async function createProject() {
  saving.value = true; error.value = ''
  try {
    // 客户/产品按自由文本落库：下拉选中时取显示名称，手填时原样使用，均不写字典 ID 引用
    const customerName = customers.value.find(item => item.id === projectForm.customer_id)?.name || projectForm.customer_id
    const productName = products.value.find(item => item.id === projectForm.product_id)?.name || projectForm.product_id
    await api('/projects', { method: 'POST', body: JSON.stringify({ ...projectForm, customer_name: customerName, product_name: productName, customer_id: '', product_id: '', order_qty: Number(projectForm.order_qty), required_delivery_date: projectForm.required_delivery_date || null }) })
    modal.value = ''; notice.value = '项目创建成功'; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function createOrder() {
  saving.value = true; error.value = ''
  try { await api('/work-orders', { method: 'POST', body: JSON.stringify({ ...orderForm, planned_product_qty: Number(orderForm.planned_product_qty) }) }); modal.value = ''; notice.value = '工单已创建，确认裁床后即可下发'; await load() } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function createCutting() {
  if (!cuttingForm.marker_id) { error.value = '请选择已审核的唛架'; return }
  saving.value = true; error.value = ''
  const payload = { cutting_no: cuttingForm.cutting_no, work_order_id: cuttingForm.work_order_id, marker_id: cuttingForm.marker_id, cutting_date: Date.now() * 1000, fabric_layers: 1, cut_qty: Number(cuttingForm.cut_qty), details: [{ color: cuttingForm.color, size: cuttingForm.size, bundle_count: Number(cuttingForm.bundle_count), piece_qty: Number(cuttingForm.cut_qty), sort_no: 1 }] }
  try { await api('/cutting-orders', { method: 'POST', body: JSON.stringify(payload) }); modal.value = ''; notice.value = '裁床草稿已创建，请核对后确认'; tab.value = 'cutting-orders'; await load() } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function confirmCutting(item: any) {
  if (!window.confirm(`确认裁床单 ${item.cutting_no}？将按配码生成扎和菲票。`)) return
  error.value = ''
  try { const result = await api<{ bundle_count: number }>(`/cutting-orders/${item.id}/confirm`, { method: 'POST', body: JSON.stringify({ version: item.version }) }); notice.value = `裁床已确认，生成 ${result.bundle_count} 个扎和菲票`; await load() } catch (e) { error.value = (e as Error).message }
}
async function removeProject(item: any) {
  if (!window.confirm(`确认删除项目 ${item.project_code}？只要后续任何环节（物料入库/裁床/唛架/工单/领料/巡检/汇报/出货/退货/委外等）已有数据就不能删除。`)) return
  error.value = ''
  try { await api(`/projects/${item.id}`, { method: 'DELETE' }); notice.value = '项目已删除'; await load() } catch (e) { error.value = (e as Error).message }
}
async function cancelOrder(item: any) {
  const reason = window.prompt(`请输入取消工单 ${item.work_order_code} 的原因`)
  if (!reason) return
  error.value = ''
  try { await api(`/work-orders/${item.id}/cancel`, { method: 'POST', body: JSON.stringify({ version: item.version, reason }) }); notice.value = '工单已取消'; await load() } catch (e) { error.value = (e as Error).message }
}
async function removeOrder(item: any) {
  if (!window.confirm(`确认删除工单 ${item.work_order_code}？仅未下发且未流转到裁床环节的工单可删除。`)) return
  error.value = ''
  try { await api(`/work-orders/${item.id}`, { method: 'DELETE' }); notice.value = '工单已删除'; await load() } catch (e) { error.value = (e as Error).message }
}
async function removeCutting(item: any) {
  if (!window.confirm(`确认删除裁床单 ${item.cutting_no}？已确认生效的裁床单不能删除。`)) return
  error.value = ''
  try { await api(`/cutting-orders/${item.id}`, { method: 'DELETE' }); notice.value = '裁床单已删除'; await load() } catch (e) { error.value = (e as Error).message }
}
async function release(item: any) {
  if (!window.confirm(`确认下发工单 ${item.work_order_code}？系统将冻结工序快照并生成全部任务。`)) return
  error.value = ''
  try { const result = await command<{ tasks: any[] }>(`/work-orders/${item.id}/release`, { version: item.version, remark: '裁床复核完成' }); notice.value = `工单下发成功，已生成 ${result.tasks.length} 道工序任务`; await load() } catch (e) { error.value = (e as Error).message }
}
</script>
<template>
  <div class="page-header"><div><h1>产品订单管理</h1><p>产品订单、裁床确认、工单下发与工序快照一体化管理</p></div><div class="header-actions"><button v-if="canProjectWrite" class="button secondary" @click="open('project')">+ 项目</button><button v-if="canWorkOrderWrite" class="button secondary" @click="open('order')">+ 工单</button><button v-if="canCuttingWrite" class="button" @click="open('cutting')">+ 裁床</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><div class="tabs"><button v-if="canWorkOrderRead" :class="{ active: tab === 'work-orders' }" @click="switchTab('work-orders')">生产工单</button><button v-if="canProjectRead" :class="{ active: tab === 'projects' }" @click="switchTab('projects')">产品订单</button><button v-if="canCuttingRead" :class="{ active: tab === 'cutting-orders' }" @click="switchTab('cutting-orders')">裁床与分扎</button></div><div class="toolbar"><FilterSelect v-model="statusFilter" :options="[{value:'',label:'全部状态'},{value:'pending_release',label:'待下发'},{value:'in_production',label:'生产中'}]" @change="load"/><select v-model="partyFilter" class="button secondary" title="委托方视角分组"><option value="">本厂 + 外协</option><option value="本厂">本厂</option><option value="外协">外协</option></select><select v-model="sceneFilter" class="button secondary" title="业务场景筛选"><option value="">全部场景</option><option value="A">A 接服贸客商单</option><option value="B">B 接外包单</option><option value="C">C 接外包工序</option></select><input class="search-input" placeholder="搜索编号或产品"><span class="spacer"></span><button class="button secondary" @click="load">刷新</button></div><PageState :loading="loading" />
    <div v-if="!loading && tab === 'work-orders'" class="table-wrap"><table><thead><tr><th>工单编号</th><th>场景 / 归属</th><th>产品</th><th>计划数</th><th>良品数</th><th>综合进度</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in visibleWorkOrders" :key="item.id"><td><strong>{{ item.outsource ? '[外协]' : '' }}{{ item.work_order_code }}</strong><small class="text-muted" style="display:block">优先级：{{ item.priority }}</small></td><td><SceneBadge :scene-type="item.scene_type" :scene-sub="item.scene_sub" :outsource="item.outsource" :party="item.party" compact /><small v-if="item.counterparty" class="text-muted" style="display:block">对方：{{ item.counterparty }}</small></td><td><div style="display:flex;align-items:center;gap:10px"><ProductImage :attachment-id="item.image_attachment_id" :width="40" :height="40" /><span>{{ item.product_name }}</span></div></td><td>{{ item.planned_product_qty }}</td><td>{{ item.approved_good_qty }}</td><td><div class="progress-track" style="width:110px"><div class="progress-bar" :style="{ width: `${Math.min(item.progress_ratio * 100, 100)}%` }"></div></div></td><td><span class="status" :class="item.status">{{ statusText[item.status] }}</span></td><td><span v-if="item.review_status" class="status" :class="item.review_status" style="margin-right:6px">{{ item.review_status === 'approved' ? '已审核' : (item.review_status === 'rejected' ? '已驳回' : '待审核') }}</span><button v-if="item.status === 'pending_release' && item.review_status !== 'approved' && canReview" class="button small" @click="reviewOrder(item)">审核</button><button v-if="item.status === 'pending_release' && item.review_status === 'approved'" class="button small" @click="release(item)">下发</button><button v-if="item.status === 'pending_release' && canWorkOrderWrite" class="button link text-danger" @click="removeOrder(item)">删除</button><button v-if="canWorkOrderWrite && ['pending_release','in_production'].includes(item.status)" class="button link text-danger" @click="cancelOrder(item)">取消</button><span v-if="item.status !== 'pending_release'" class="text-muted">v{{ item.version }}</span></td></tr></tbody></table><PageState :empty="!visibleWorkOrders.length" /></div>
    <div v-if="!loading && tab === 'projects'" class="table-wrap"><table><thead><tr><th>项目号</th><th>客户</th><th>款号</th><th>款式</th><th>产品</th><th>订单数</th><th>未出货</th><th>货期</th><th>操作</th></tr></thead><tbody><tr v-for="item in data.projects" :key="item.id"><td>{{ item.project_code }}</td><td>{{ item.customer_name }}</td><td>{{ item.style_no || '—' }}</td><td>{{ item.style_name || '—' }}</td><td><div style="display:flex;align-items:center;gap:10px"><ProductImage :attachment-id="item.image_attachment_id" :width="40" :height="40" /><span>{{ item.product_name }}</span></div></td><td>{{ item.order_qty }}</td><td>{{ item.unshipped_qty }}</td><td>{{ item.required_delivery_date || '—' }}</td><td><button v-if="canProjectWrite" class="button link" @click="openAdjust(item)">编辑</button><button v-if="canProjectWrite" class="button link text-danger" @click="removeProject(item)">删除</button></td></tr></tbody></table><PageState :empty="!data.projects.length" /></div>
    <div v-if="!loading && tab === 'cutting-orders'" class="table-wrap"><table><thead><tr><th>裁床单号</th><th>工单 ID</th><th>裁片数</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in data['cutting-orders']" :key="item.id"><td>{{ item.cutting_no }}</td><td class="text-muted">{{ item.work_order_id }}</td><td>{{ item.cut_qty }}</td><td><span class="status" :class="item.status">{{ statusText[item.status] || item.status }}</span></td><td><button v-if="item.status === 'draft'" class="button small" @click="confirmCutting(item)">确认并生成菲票</button><button v-if="item.status === 'draft' && canCuttingWrite" class="button link text-danger" @click="removeCutting(item)">删除</button><span v-if="item.status !== 'draft'" class="text-muted">扎/票已生成</span></td></tr></tbody></table><PageState :empty="!data['cutting-orders'].length" /></div>
  </section>
  <ModalDialog v-if="modal === 'project'" title="新建订单项目" @close="modal = ''"><div class="form-grid"><div class="field full"><label>项目号</label><input v-model.trim="projectForm.project_code" placeholder="留空自动生成"></div><div class="field"><label class="required">客户</label><FilterSelect v-model="projectForm.customer_id" :options="customerOptions" allow-create placeholder="输入客户名称（可参照客户列表）"/></div><div class="field full"><label class="required">产品</label><FilterSelect v-model="projectForm.product_id" :options="productOptions" allow-create wide placeholder="输入产品名称（可参照产品字典）" @change="onProjectProductChange"/></div><div class="field"><label>款号</label><input v-model.trim="projectForm.style_no" placeholder="选产品自动带出，可修改"></div><div class="field"><label>款式</label><input v-model.trim="projectForm.style_name" placeholder="选产品自动带出，可修改"></div><div class="field"><label class="required">订单数</label><input v-model.number="projectForm.order_qty" type="number" min="1"></div><div class="field"><label>要求货期</label><DatePicker v-model="projectForm.required_delivery_date"/></div><div class="field"><label>客户单价</label><input v-model="projectForm.customer_unit_price"></div><div class="field"><label>外发单价</label><input v-model="projectForm.external_unit_price"></div></div><template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving" @click="createProject">保存项目</button></template></ModalDialog>
  <ModalDialog v-if="modal === 'order'" title="新建生产工单" @close="modal = ''"><div class="form-grid"><div class="field full"><label class="required">工单编号</label><input v-model.trim="orderForm.work_order_code" placeholder="GD202611001"></div><div class="field full"><label class="required">所属项目</label><FilterSelect v-model="orderForm.project_id" :options="projectOptions"/></div><div class="field"><label class="required">计划产品数</label><input v-model.number="orderForm.planned_product_qty" type="number" min="1"><small>服务端按产品工序配比计算任务计划量</small></div><div class="field"><label>紧急程度</label><FilterSelect v-model="orderForm.priority" :options="[{value:'normal',label:'普通'},{value:'urgent',label:'紧急'}]"/></div></div><template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving" @click="createOrder">提交待下发</button></template></ModalDialog>
  <ModalDialog v-if="modal === 'cutting'" title="新建裁床单" @close="modal = ''"><div class="form-grid"><div class="field full"><label class="required">裁床单号</label><input v-model.trim="cuttingForm.cutting_no" placeholder="CC202611001"></div><div class="field full"><label class="required">生产工单</label><FilterSelect v-model="cuttingForm.work_order_id" :options="cuttingOrderOptions" @change="onCuttingOrderChange"/></div><div class="field full"><label class="required">唛架</label><FilterSelect v-model="cuttingForm.marker_id" :options="markerOptions" placeholder="选择已审核唛架"/><small v-if="cuttingForm.work_order_id && !markerOptions.length" class="text-danger">该工单没有已审核唛架，请先在唛架管理提交并审核</small></div><div class="field"><label class="required">裁片数</label><input v-model.number="cuttingForm.cut_qty" type="number" min="1"></div><div class="field"><label>扎数</label><input v-model.number="cuttingForm.bundle_count" type="number" min="1"></div><div class="field"><label>颜色</label><input v-model.trim="cuttingForm.color"></div><div class="field"><label>尺码</label><input v-model.trim="cuttingForm.size"></div></div><div class="notice" style="margin-top:16px">本页使用单颜色/尺码配码；明细合计必须等于裁片数。</div><template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || !cuttingForm.marker_id" @click="createCutting">保存裁床草稿</button></template></ModalDialog>
  <ModalDialog v-if="projectAdjust" :title="`编辑订单 · ${projectAdjust.project_code}`" @close="projectAdjust = null">
    <div v-if="error" class="notice error">{{ error }}</div>
    <div class="form-grid">
      <div class="field full"><label>项目号</label><input :value="projectAdjust.project_code" readonly disabled></div>
      <div class="field"><label class="required">客户</label><FilterSelect v-model="adjustForm.customer_name" :options="customerNameOptions" allow-create placeholder="输入客户名称（可参照客户列表）"/></div>
      <div class="field full"><label class="required">产品</label><FilterSelect v-model="adjustForm.product_name" :options="productNameOptions" allow-create wide placeholder="输入产品名称（可参照产品字典）"/></div>
      <div class="field"><label>款号</label><input v-model.trim="adjustForm.style_no" placeholder="选产品自动带出，可修改"></div>
      <div class="field"><label>款式</label><input v-model.trim="adjustForm.style_name" placeholder="选产品自动带出，可修改"></div>
      <div class="field"><label class="required">订单数</label><input v-model.number="adjustForm.order_qty" type="number" min="1"></div>
      <div class="field"><label>要求货期</label><DatePicker v-model="adjustForm.required_delivery_date"/></div>
      <div class="field"><label>客户单价</label><input v-model="adjustForm.customer_unit_price"></div>
      <div class="field"><label>外发单价</label><input v-model="adjustForm.external_unit_price"></div>
      <div class="field full"><label class="required">变更原因</label><input v-model.trim="adjustForm.reason" placeholder="如 客户追加订单"></div>
    </div>
    <div class="notice">项目号不可修改；客户/产品按名称保存；订单数不能小于已出货数或已排产数；修改会写入审计。</div>
    <template #footer><button class="button secondary" @click="projectAdjust = null">取消</button><button class="button" :disabled="saving || !adjustForm.reason" @click="saveAdjust">保存修改</button></template>
  </ModalDialog>
</template>
