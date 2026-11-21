<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api, command, statusText } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import DatePicker from '../components/DatePicker.vue'
import FilterSelect from '../components/FilterSelect.vue'
import ProductImage from '../components/ProductImage.vue'
import ImageViewer from '../components/ImageViewer.vue'

const { has, ready } = usePermissions()
const canProjectRead = computed(() => has('project:read'))
const canWorkOrderRead = computed(() => has('work_order:read'))
const canCuttingRead = computed(() => has('cutting:read'))
const canProjectWrite = computed(() => has('project:write'))
const canWorkOrderWrite = computed(() => has('work_order:write'))
const canCuttingWrite = computed(() => has('cutting:write'))
const canReview = computed(() => has('work_order:review'))

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
const projectAdjust = ref<any>(null)
const adjustForm = reactive({ customer_name: '', product_name: '', style_no: '', style_name: '', order_qty: 0, required_delivery_date: '', customer_unit_price: '', external_unit_price: '', version: 0, reason: '' })
const adjustImages = ref<any[]>([])
const adjustViewType = ref('standard')
const adjustUploading = ref(false)
const adjustFileInput = ref<HTMLInputElement>()
const adjustViewer = ref<{ attachment_id: string; title: string } | null>(null)
const viewNames: Record<string, string> = { standard: '标准图', front: '正面', side: '侧面', back: '后面', top: '顶面', bottom: '底面', interior: '内里', other: '其他' }
function resolveAdjustProductId(): string | null {
  if (projectAdjust.value?.product_id) return projectAdjust.value.product_id
  const name = projectAdjust.value?.product_name
  return products.value.find(p => p.name === name)?.id || null
}
async function loadAdjustImages() {
  const productId = resolveAdjustProductId()
  if (!productId) { adjustImages.value = []; return }
  try { const res = await api<{ items: any[] }>(`/products/${productId}/images`); adjustImages.value = res.items } catch { adjustImages.value = [] }
}
async function openAdjust(item: any) {
  projectAdjust.value = item
  Object.assign(adjustForm, {
    customer_name: item.customer_name || '', product_name: item.product_name || '',
    style_no: item.style_no || '', style_name: item.style_name || '',
    order_qty: item.order_qty, required_delivery_date: item.required_delivery_date || '',
    customer_unit_price: item.customer_unit_price || '', external_unit_price: item.external_unit_price || '',
    version: item.version, reason: ''
  })
  error.value = ''; adjustViewType.value = 'standard'
  await loadAdjustImages()
}
async function uploadAdjustImage(event: Event) {
  const productId = resolveAdjustProductId()
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file || !productId) { if (!productId) error.value = '该项目未关联产品，无法上传图片'; return }
  if (file.size > 20 * 1024 * 1024) { error.value = '图片不能超过20MB'; return }
  adjustUploading.value = true; error.value = ''
  try {
    const bytes = new Uint8Array(await file.arrayBuffer())
    const hashBuffer = await crypto.subtle.digest('SHA-256', bytes)
    const sha256 = Array.from(new Uint8Array(hashBuffer), b => b.toString(16).padStart(2, '0')).join('')
    const init = await api<{ attachment_id: string; chunk_size: number }>('/attachments/init', { method: 'POST', body: JSON.stringify({ sha256, size_bytes: file.size, mime_type: file.type || 'image/jpeg', owner_type: 'product' }) })
    const chunkSize = init.chunk_size || 4 * 1024 * 1024
    let index = 0
    for (let offset = 0; offset < file.size; offset += chunkSize) {
      const chunk = file.slice(offset, offset + chunkSize)
      await fetch(`/api/v1/attachments/${init.attachment_id}/chunks/${index++}`, { method: 'PUT', headers: { Authorization: `Bearer ${localStorage.getItem('clothp_access_token')}`, 'Content-Type': 'application/octet-stream' }, body: chunk }).then(async r => { if (!r.ok) throw new Error((await r.json()).message) })
    }
    await api(`/attachments/${init.attachment_id}/complete`, { method: 'POST', body: JSON.stringify({ total_chunks: index }) })
    await api(`/products/${productId}/images`, { method: 'POST', body: JSON.stringify({ attachment_id: init.attachment_id, view_type: adjustViewType.value, sort_no: adjustImages.value.length + 1 }) })
    notice.value = `${viewNames[adjustViewType.value]}上传成功`
    await loadAdjustImages()
  } catch (e) { error.value = (e as Error).message } finally {
    adjustUploading.value = false
    if (adjustFileInput.value) adjustFileInput.value.value = ''
  }
}
async function removeAdjustImage(image: any) {
  const productId = resolveAdjustProductId()
  if (!productId) return
  if (!window.confirm(`确认删除${viewNames[image.view_type] || image.view_type}图片？`)) return
  try { await api(`/products/${productId}/images/${image.link_id}`, { method: 'DELETE' }); notice.value = '图片已删除'; await loadAdjustImages() } catch (e) { error.value = (e as Error).message }
}
function openAdjustViewer(image: any) {
  adjustViewer.value = { attachment_id: image.attachment_id, title: (viewNames[image.view_type] || image.view_type) + ' · ' + (projectAdjust.value?.product_name || '') }
}
/* 新建订单项目弹窗：右侧产品多角度图片区（仅在选了已存在的产品时可上传/展示） */
const createImages = ref<any[]>([])
const createViewType = ref('standard')
const createUploading = ref(false)
const createFileInput = ref<HTMLInputElement>()
function resolveCreateProductId(): string | null {
  if (products.value.some(p => p.id === projectForm.product_id)) return projectForm.product_id
  return products.value.find(p => p.name === projectForm.product_id)?.id || null
}
async function loadCreateImages() {
  const productId = resolveCreateProductId()
  if (!productId) { createImages.value = []; return }
  try { const res = await api<{ items: any[] }>(`/products/${productId}/images`); createImages.value = res.items } catch { createImages.value = [] }
}
async function uploadCreateImage(event: Event) {
  const productId = resolveCreateProductId()
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file || !productId) { if (!productId) error.value = '请先在产品字典中选择已有产品，才能上传图片'; return }
  if (file.size > 20 * 1024 * 1024) { error.value = '图片不能超过20MB'; return }
  createUploading.value = true; error.value = ''
  try {
    const bytes = new Uint8Array(await file.arrayBuffer())
    const hashBuffer = await crypto.subtle.digest('SHA-256', bytes)
    const sha256 = Array.from(new Uint8Array(hashBuffer), b => b.toString(16).padStart(2, '0')).join('')
    const init = await api<{ attachment_id: string; chunk_size: number }>('/attachments/init', { method: 'POST', body: JSON.stringify({ sha256, size_bytes: file.size, mime_type: file.type || 'image/jpeg', owner_type: 'product' }) })
    const chunkSize = init.chunk_size || 4 * 1024 * 1024
    let index = 0
    for (let offset = 0; offset < file.size; offset += chunkSize) {
      const chunk = file.slice(offset, offset + chunkSize)
      await fetch(`/api/v1/attachments/${init.attachment_id}/chunks/${index++}`, { method: 'PUT', headers: { Authorization: `Bearer ${localStorage.getItem('clothp_access_token')}`, 'Content-Type': 'application/octet-stream' }, body: chunk }).then(async r => { if (!r.ok) throw new Error((await r.json()).message) })
    }
    await api(`/attachments/${init.attachment_id}/complete`, { method: 'POST', body: JSON.stringify({ total_chunks: index }) })
    await api(`/products/${productId}/images`, { method: 'POST', body: JSON.stringify({ attachment_id: init.attachment_id, view_type: createViewType.value, sort_no: createImages.value.length + 1 }) })
    notice.value = `${viewNames[createViewType.value]}上传成功`
    await loadCreateImages()
  } catch (e) { error.value = (e as Error).message } finally {
    createUploading.value = false
    if (createFileInput.value) createFileInput.value.value = ''
  }
}
async function removeCreateImage(image: any) {
  const productId = resolveCreateProductId()
  if (!productId) return
  if (!window.confirm(`确认删除${viewNames[image.view_type] || image.view_type}图片？`)) return
  try { await api(`/products/${productId}/images/${image.link_id}`, { method: 'DELETE' }); notice.value = '图片已删除'; await loadCreateImages() } catch (e) { error.value = (e as Error).message }
}
function openCreateViewer(image: any) {
  const productName = products.value.find(p => p.id === resolveCreateProductId())?.name || projectForm.product_id || ''
  adjustViewer.value = { attachment_id: image.attachment_id, title: (viewNames[image.view_type] || image.view_type) + ' · ' + productName }
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
const projectKeyword = ref('')
const projectFilter = ref('')
const filteredProjects = computed(() => {
  const kw = projectFilter.value.trim().toLowerCase()
  if (!kw) return data.projects
  return data.projects.filter((p: any) =>
    [p.project_code, p.customer_name, p.style_no, p.style_name, p.product_name]
      .some(v => String(v || '').toLowerCase().includes(kw)))
})
const orderPage = ref(1)
const orderPageSize = ref(10)
const pagedProjects = computed(() => {
  const start = (orderPage.value - 1) * orderPageSize.value
  return filteredProjects.value.slice(start, start + orderPageSize.value)
})
const orderPageCount = computed(() => Math.max(1, Math.ceil(filteredProjects.value.length / orderPageSize.value)))
function applyProjectSearch() { projectFilter.value = projectKeyword.value; orderPage.value = 1 }
function changeOrderPage(delta: number) { orderPage.value = Math.min(orderPageCount.value, Math.max(1, orderPage.value + delta)) }
function onOrderPageSizeChange() { orderPage.value = 1 }
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
  loading.value = true; error.value = ''; orderPage.value = 1
  const [projects, orders, cuttings, productData, customerData] = await Promise.all([
    canProjectRead.value ? loadSection('/projects') : Promise.resolve(null),
    canWorkOrderRead.value ? loadSection('/work-orders') : Promise.resolve(null),
    canCuttingRead.value ? loadSection('/cutting-orders') : Promise.resolve(null),
    loadSection('/products'),
    loadSection('/customers'),
  ])
  if (projects) data.projects = projects.items
  if (orders) data['work-orders'] = orders.items
  if (cuttings) data['cutting-orders'] = cuttings.items
  if (productData) products.value = productData.items
  if (customerData) customers.value = customerData.items
  loading.value = false
  loaded.value = true
}
/* 进入页面即加载：权限已就绪时立即加载，未就绪时等权限到位再加载 */
watch(ready, (r) => { if (r) void load() }, { immediate: true })
function open(type: string) {
  error.value = ''
  if (type === 'project' && !customers.value.length) { error.value = '请先在“员工与客户”中新增客户'; return }
  if (type === 'order' && !data.projects.length) { error.value = '请先创建项目'; return }
  if (type === 'project') { Object.assign(projectForm, { project_code: '', customer_id: '', product_id: '', style_no: '', style_name: '', order_qty: 900, customer_unit_price: '0', external_unit_price: '0', required_delivery_date: '' }); createImages.value = []; createViewType.value = 'standard' }
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
  if (!product) { createImages.value = []; return }
  if (!projectForm.style_no) projectForm.style_no = product.product_code || ''
  if (!projectForm.style_name) projectForm.style_name = product.name || ''
  void loadCreateImages()
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
  try { await api('/cutting-orders', { method: 'POST', body: JSON.stringify(payload) }); modal.value = ''; notice.value = '裁床草稿已创建，请核对后确认'; await load() } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
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
  <section class="panel"><div class="toolbar"><input v-model="projectKeyword" class="search-input" placeholder="搜索订单号或产品" @keyup.enter="applyProjectSearch"><button class="button secondary" @click="applyProjectSearch">检索</button><span class="spacer"></span><button class="button secondary" @click="load">刷新</button></div><PageState :loading="loading" />
    <div v-if="!loading">
      <div class="table-wrap"><table><thead><tr><th>订单号</th><th>客户</th><th>款号</th><th>款式</th><th>产品</th><th>订单数</th><th>未出货</th><th>货期</th><th>操作</th></tr></thead><tbody><tr v-for="item in pagedProjects" :key="item.id"><td>{{ item.project_code }}</td><td>{{ item.customer_name }}</td><td>{{ item.style_no || '—' }}</td><td>{{ item.style_name || '—' }}</td><td><div style="display:flex;align-items:center;gap:10px"><ProductImage :attachment-id="item.image_attachment_id" :width="40" :height="40" /><span>{{ item.product_name }}</span></div></td><td>{{ item.order_qty }}</td><td>{{ item.unshipped_qty }}</td><td>{{ item.required_delivery_date || '—' }}</td><td><button v-if="canProjectWrite" class="button link" @click="openAdjust(item)">编辑</button><button v-if="canProjectWrite" class="button link text-danger" @click="removeProject(item)">删除</button></td></tr></tbody></table><PageState :empty="!filteredProjects.length" /></div>
      <div v-if="filteredProjects.length" class="table-pager">
        <span class="pager-info">共 {{ filteredProjects.length }} 条 · 第 {{ orderPage }} / {{ orderPageCount }} 页</span>
        <select v-model.number="orderPageSize" class="pager-size" @change="onOrderPageSizeChange"><option :value="5">5 条/页</option><option :value="10">10 条/页</option><option :value="20">20 条/页</option><option :value="50">50 条/页</option></select>
        <button class="button secondary" :disabled="orderPage <= 1" @click="changeOrderPage(-1)">上一页</button>
        <button class="button secondary" :disabled="orderPage >= orderPageCount" @click="changeOrderPage(1)">下一页</button>
      </div>
    </div>
  </section>
  <ModalDialog v-if="modal === 'project'" title="新建订单项目" wide width="min(1360px, 96vw)" @close="modal = ''">
    <div class="adjust-wrap">
      <div class="adjust-form">
        <div class="form-grid">
          <div class="field full"><label>订单号</label><input v-model.trim="projectForm.project_code" placeholder="留空自动生成"></div>
          <div class="field full"><label class="required">客户</label><FilterSelect v-model="projectForm.customer_id" :options="customerOptions" allow-create wide placeholder="输入客户名称（可参照客户列表）"/></div>
          <div class="field full"><label class="required">产品</label><FilterSelect v-model="projectForm.product_id" :options="productOptions" allow-create wide placeholder="输入产品名称（可参照产品字典）" @change="onProjectProductChange"/></div>
          <div class="field"><label>款号</label><input v-model.trim="projectForm.style_no" placeholder="选产品自动带出，可修改"></div>
          <div class="field"><label>款式</label><input v-model.trim="projectForm.style_name" placeholder="选产品自动带出，可修改"></div>
          <div class="field"><label class="required">订单数</label><input v-model.number="projectForm.order_qty" type="number" min="1"></div>
          <div class="field"><label>要求货期</label><DatePicker v-model="projectForm.required_delivery_date"/></div>
          <div class="field"><label>客户单价</label><input v-model="projectForm.customer_unit_price"></div>
          <div class="field"><label>外发单价</label><input v-model="projectForm.external_unit_price"></div>
        </div>
      </div>
      <div class="adjust-images">
        <h3>产品图片</h3>
        <div class="image-toolbar">
          <FilterSelect v-model="createViewType" :options="Object.entries(viewNames).map(([value, label]) => ({ value, label }))" />
          <label class="button secondary">{{ createUploading ? '正在上传…' : '上传图片' }}<input ref="createFileInput" type="file" accept="image/*" hidden :disabled="createUploading" @change="uploadCreateImage"></label>
        </div>
        <div class="image-grid">
          <div v-for="image in createImages" :key="image.link_id" class="image-item">
            <ProductImage :attachment-id="image.attachment_id" :width="150" :height="150" style="cursor:zoom-in" @click="openCreateViewer(image)" />
            <button class="image-delete" title="删除图片" @click="removeCreateImage(image)">×</button>
            <p>{{ viewNames[image.view_type] || image.view_type }}</p>
          </div>
          <div v-if="!createImages.length" class="text-muted">{{ resolveCreateProductId() ? '暂无图片，可选择角度上传各角度图片' : '选择已有产品后可上传/展示各角度图片' }}</div>
        </div>
      </div>
    </div>
    <template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving" @click="createProject">保存项目</button></template>
  </ModalDialog>
  <ModalDialog v-if="modal === 'order'" title="新建生产工单" @close="modal = ''"><div class="form-grid"><div class="field full"><label class="required">工单编号</label><input v-model.trim="orderForm.work_order_code" placeholder="GD202611001"></div><div class="field full"><label class="required">所属项目</label><FilterSelect v-model="orderForm.project_id" :options="projectOptions"/></div><div class="field"><label class="required">计划产品数</label><input v-model.number="orderForm.planned_product_qty" type="number" min="1"><small>服务端按产品工序配比计算任务计划量</small></div><div class="field"><label>紧急程度</label><FilterSelect v-model="orderForm.priority" :options="[{value:'normal',label:'普通'},{value:'urgent',label:'紧急'}]"/></div></div><template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving" @click="createOrder">提交待下发</button></template></ModalDialog>
  <ModalDialog v-if="modal === 'cutting'" title="新建裁床单" @close="modal = ''"><div class="form-grid"><div class="field full"><label class="required">裁床单号</label><input v-model.trim="cuttingForm.cutting_no" placeholder="CC202611001"></div><div class="field full"><label class="required">生产工单</label><FilterSelect v-model="cuttingForm.work_order_id" :options="cuttingOrderOptions" @change="onCuttingOrderChange"/></div><div class="field full"><label class="required">唛架</label><FilterSelect v-model="cuttingForm.marker_id" :options="markerOptions" placeholder="选择已审核唛架"/><small v-if="cuttingForm.work_order_id && !markerOptions.length" class="text-danger">该工单没有已审核唛架，请先在唛架管理提交并审核</small></div><div class="field"><label class="required">裁片数</label><input v-model.number="cuttingForm.cut_qty" type="number" min="1"></div><div class="field"><label>扎数</label><input v-model.number="cuttingForm.bundle_count" type="number" min="1"></div><div class="field"><label>颜色</label><input v-model.trim="cuttingForm.color"></div><div class="field"><label>尺码</label><input v-model.trim="cuttingForm.size"></div></div><div class="notice" style="margin-top:16px">本页使用单颜色/尺码配码；明细合计必须等于裁片数。</div><template #footer><button class="button secondary" @click="modal = ''">取消</button><button class="button" :disabled="saving || !cuttingForm.marker_id" @click="createCutting">保存裁床草稿</button></template></ModalDialog>
  <ModalDialog v-if="projectAdjust" :title="`编辑订单 · ${projectAdjust.project_code}`" wide width="min(1360px, 96vw)" @close="projectAdjust = null">
    <div class="adjust-wrap">
      <div class="adjust-form">
        <div v-if="error" class="notice error">{{ error }}</div>
        <div class="form-grid">
          <div class="field full"><label>订单号</label><input :value="projectAdjust.project_code" readonly disabled></div>
          <div class="field full"><label class="required">客户</label><FilterSelect v-model="adjustForm.customer_name" :options="customerNameOptions" allow-create wide placeholder="输入客户名称（可参照客户列表）"/></div>
          <div class="field full"><label class="required">产品</label><FilterSelect v-model="adjustForm.product_name" :options="productNameOptions" allow-create wide placeholder="输入产品名称（可参照产品字典）"/></div>
          <div class="field"><label>款号</label><input v-model.trim="adjustForm.style_no" placeholder="选产品自动带出，可修改"></div>
          <div class="field"><label>款式</label><input v-model.trim="adjustForm.style_name" placeholder="选产品自动带出，可修改"></div>
          <div class="field"><label class="required">订单数</label><input v-model.number="adjustForm.order_qty" type="number" min="1"></div>
          <div class="field"><label>要求货期</label><DatePicker v-model="adjustForm.required_delivery_date"/></div>
          <div class="field"><label>客户单价</label><input v-model="adjustForm.customer_unit_price"></div>
          <div class="field"><label>外发单价</label><input v-model="adjustForm.external_unit_price"></div>
          <div class="field full"><label class="required">变更原因</label><input v-model.trim="adjustForm.reason" placeholder="如 客户追加订单"></div>
        </div>
        <div class="notice">订单号不可修改；客户/产品按名称保存；订单数不能小于已出货数或已排产数；修改会写入审计。</div>
      </div>
      <div class="adjust-images">
        <h3>产品图片</h3>
        <div class="image-toolbar">
          <FilterSelect v-model="adjustViewType" :options="Object.entries(viewNames).map(([value, label]) => ({ value, label }))" />
          <label class="button secondary">{{ adjustUploading ? '正在上传…' : '上传图片' }}<input ref="adjustFileInput" type="file" accept="image/*" hidden :disabled="adjustUploading" @change="uploadAdjustImage"></label>
        </div>
        <div class="image-grid">
          <div v-for="image in adjustImages" :key="image.link_id" class="image-item">
            <ProductImage :attachment-id="image.attachment_id" :width="150" :height="150" style="cursor:zoom-in" @click="openAdjustViewer(image)" />
            <button class="image-delete" title="删除图片" @click="removeAdjustImage(image)">×</button>
            <p>{{ viewNames[image.view_type] || image.view_type }}</p>
          </div>
          <div v-if="!adjustImages.length" class="text-muted">暂无图片，可选择角度上传各角度图片</div>
        </div>
      </div>
    </div>
    <template #footer><button class="button secondary" @click="projectAdjust = null">取消</button><button class="button" :disabled="saving || !adjustForm.reason" @click="saveAdjust">保存修改</button></template>
  </ModalDialog>
  <ImageViewer v-if="adjustViewer" :attachment-id="adjustViewer.attachment_id" :title="adjustViewer.title" @close="adjustViewer = null" />
</template>
<style scoped>
.table-pager{display:flex;gap:10px;align-items:center;justify-content:flex-end;padding:12px 20px;border-top:1px solid var(--border);background:rgba(255,255,255,.6)}
.pager-info{margin-right:auto;color:#667085;font-size:12px}
.pager-size{padding:6px 9px;min-width:auto}
.table-pager .button{padding:6px 12px;min-width:auto}
.adjust-wrap{display:flex;gap:20px;align-items:flex-start}
.adjust-form{flex:1;min-width:0}
.adjust-images{width:420px;flex:none;border-left:1px solid var(--border);padding-left:18px}
.adjust-images h3{margin:0 0 12px;font-size:14px;color:#17255f}
.image-toolbar{display:flex;gap:8px;align-items:center;margin-bottom:12px;flex-wrap:wrap}
.image-toolbar .button{position:relative}
.image-grid{display:flex;gap:12px;flex-wrap:wrap}
.image-item{text-align:center;position:relative}
.image-item p{margin:4px 0 0;font-size:12px;color:#667085}
.image-delete{position:absolute;top:-8px;right:-8px;width:22px;height:22px;border-radius:50%;border:0;background:#d92d20;color:#fff;font-size:13px;line-height:20px;cursor:pointer}
</style>
