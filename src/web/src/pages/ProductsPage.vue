<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, statusText } from '../api'
import { usePermissions } from '../permissions'
import ModalDialog from '../components/ModalDialog.vue'
import PageState from '../components/PageState.vue'
import ProductImage from '../components/ProductImage.vue'
import ImageHoverPreview from '../components/ImageHoverPreview.vue'
import FilterSelect from '../components/FilterSelect.vue'

const processOptions = computed(() => processes.value.map(p => ({ value: p.id, label: `${p.process_code} · ${p.name}` })))

const tab = ref<'products' | 'processes'>('products')
const products = ref<any[]>([])
const processes = ref<any[]>([])
const loading = ref(false)
const error = ref('')
const notice = ref('')
const showProduct = ref(false)
const showProcess = ref(false)
const showRoute = ref(false)
const saving = ref(false)
const productForm = reactive({ product_code: '', name: '', spec: '', unit: '件', sale_price: '0' })
const processForm = reactive({ process_code: '', name: '', default_unit_price: '0.000000', default_report_ratio: '1.000000', standard_work_seconds: 0 })
const current = ref<any>(null)
const editingProcess = ref<any>(null)
const routes = ref<any[]>([])
const routeMeta = ref<{ name?: string | null; at?: number | null }>({})
const { has } = usePermissions()
const canProductWrite = computed(() => has('product:write'))
const canProcessWrite = computed(() => has('process:write'))

async function load() {
  loading.value = true; error.value = ''
  try {
    const productData = await api<{ items: any[] }>('/products')
    products.value = productData.items
  } catch (e) {
    products.value = []; processes.value = []; error.value = (e as Error).message
    return
  } finally { loading.value = false }
  try {
    const processData = await api<{ items: any[] }>('/process-definitions')
    processes.value = processData.items
  } catch {
    processes.value = []
    notice.value = notice.value ? `${notice.value} 当前账号暂无工序库读取权限，工序相关操作不可用。` : '当前账号暂无工序库读取权限，工序相关操作不可用。'
  }
}
async function createProduct() {
  saving.value = true; error.value = ''
  try { await api('/products', { method: 'POST', body: JSON.stringify({ ...productForm, spec: productForm.spec || null, sale_price: productForm.sale_price || '0' }) }); showProduct.value = false; notice.value = '产品已保存为草稿，请继续配置工序。'; await load() } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
async function saveProcess() {
  saving.value = true; error.value = ''
  try {
    if (editingProcess.value) await api(`/process-definitions/${editingProcess.value.id}`, { method: 'PATCH', body: JSON.stringify({ id: editingProcess.value.id, ...processForm, version: editingProcess.value.version }) })
    else await api('/process-definitions', { method: 'POST', body: JSON.stringify(processForm) })
    showProcess.value = false; editingProcess.value = null; await load()
  } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
function openProcess(item?: any) {
  editingProcess.value = item || null
  Object.assign(processForm, item ? { process_code: item.process_code, name: item.name, default_unit_price: item.default_unit_price, default_report_ratio: item.default_report_ratio, standard_work_seconds: item.standard_work_seconds } : { process_code: '', name: '', default_unit_price: '0.000000', default_report_ratio: '1.000000', standard_work_seconds: 0 })
  showProcess.value = true
}
async function deleteProcess(item: any) {
  if (!window.confirm(`确认删除工序 ${item.name}？已用于生产的工序将被拒绝。`)) return
  error.value = ''
  try { await api(`/process-definitions/${item.id}`, { method: 'DELETE' }); notice.value = '工序已删除'; await load() } catch (e) { error.value = (e as Error).message }
}
async function editRoutes(product: any) {
  error.value = ''; current.value = product
  try {
    const [detail, routeData] = await Promise.all([api<any>(`/products/${product.id}`), api<{ items: any[]; configurer_name?: string | null; configured_at?: number | null }>(`/products/${product.id}/processes`)])
    current.value = detail; routes.value = routeData.items.map(item => ({ ...item })); routeMeta.value = { name: routeData.configurer_name, at: routeData.configured_at }
    showRoute.value = true
  } catch (e) { error.value = (e as Error).message }
}
function addRoute() {
  const process = processes.value[0]
  if (!process) { error.value = '请先建立工序库'; return }
  routes.value.push({ process_id: process.id, sequence_no: routes.value.length + 1, unit_price: process.default_unit_price, report_ratio: process.default_report_ratio, standard_work_seconds: process.standard_work_seconds, is_required: true, is_parallel: false, is_critical: false })
}
function fillDefaults(row: any) {
  const process = processes.value.find(p => p.id === row.process_id)
  if (process) { row.unit_price = process.default_unit_price; row.report_ratio = process.default_report_ratio; row.standard_work_seconds = process.standard_work_seconds }
}
async function saveRoutes() {
  if (!current.value) return
  saving.value = true; error.value = ''
  const items = routes.value.map(row => ({ process_id: row.process_id, sequence_no: Number(row.sequence_no), unit_price: String(row.unit_price), report_ratio: String(row.report_ratio), department_id: row.department_id || null, standard_work_seconds: Number(row.standard_work_seconds || 0), is_parallel: !!row.is_parallel, is_critical: !!row.is_critical, is_required: row.is_required !== false, remark: row.remark || null }))
  try { await api(`/products/${current.value.id}/processes`, { method: 'PUT', body: JSON.stringify({ version: current.value.version, items }) }); showRoute.value = false; notice.value = '产品工序已保存为新版本，历史工单快照不受影响。'; await load() } catch (e) { error.value = (e as Error).message } finally { saving.value = false }
}
onMounted(load)
</script>
<template>
  <div class="page-header"><div><h1>产品与工序</h1><p>工序先建库，产品按版本配置工价、配比和路线</p></div><div class="header-actions"><button v-if="canProcessWrite" class="button secondary" @click="openProcess()">+ 新建工序</button><button v-if="canProductWrite" class="button" @click="showProduct = true">+ 新建产品</button></div></div>
  <div v-if="error" class="notice error">{{ error }}</div><div v-if="notice" class="notice success">{{ notice }}</div>
  <section class="panel"><div class="tabs"><button :class="{ active: tab === 'products' }" @click="tab = 'products'">产品资料</button><button :class="{ active: tab === 'processes' }" @click="tab = 'processes'">工序库</button></div><div class="toolbar"><input class="search-input" placeholder="搜索产品编号或名称"><span class="spacer"></span><button class="button secondary" @click="load">刷新</button></div><PageState :loading="loading" />
    <div v-if="!loading && tab === 'products'" class="table-wrap"><table><thead><tr><th>图片</th><th>产品编号</th><th>名称/规格</th><th>工序数</th><th>理论工价</th><th>版本</th><th>状态</th><th>配置人</th><th>操作</th></tr></thead><tbody><tr v-for="product in products" :key="product.id"><td><ImageHoverPreview :attachment-id="product.image_attachment_id"><ProductImage :attachment-id="product.image_attachment_id" :width="52" :height="52" /></ImageHoverPreview></td><td><strong>{{ product.product_code }}</strong></td><td>{{ product.name }}<small class="text-muted" style="display:block">{{ product.spec || '—' }}</small></td><td>{{ product.process_count }}</td><td class="money">¥ {{ product.theoretical_price }}</td><td>v{{ product.version }}</td><td><span class="status" :class="product.status">{{ statusText[product.status] }}</span></td><td :title="product.configured_at ? new Date(product.configured_at/1000).toLocaleString('zh-CN') : ''">{{ product.configurer_name || '—' }}</td><td><RouterLink class="button link" :to="`/products/${product.id}`">资料/图片</RouterLink><button v-if="canProductWrite" class="button link" @click="editRoutes(product)">配置工序</button></td></tr></tbody></table><PageState :empty="!products.length" /></div>
    <div v-if="!loading && tab === 'processes'" class="table-wrap"><table><thead><tr><th>工序编码</th><th>名称</th><th>默认工价</th><th>报工配比</th><th>标准工时(秒)</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="process in processes" :key="process.id"><td>{{ process.process_code }}</td><td>{{ process.name }}</td><td>{{ process.default_unit_price }}</td><td>{{ process.default_report_ratio }}</td><td>{{ process.standard_work_seconds }}</td><td><span class="status active">{{ statusText[process.status] }}</span></td><td><button v-if="canProcessWrite" class="button link" @click="openProcess(process)">编辑</button><button v-if="canProcessWrite" class="button link text-danger" @click="deleteProcess(process)">删除</button></td></tr></tbody></table></div>
  </section>
  <ModalDialog v-if="showProduct" title="新建产品" @close="showProduct = false"><div class="form-grid"><div class="field"><label class="required">产品编号</label><input v-model.trim="productForm.product_code" placeholder="例如 CP001"></div><div class="field"><label class="required">产品名称</label><input v-model.trim="productForm.name"></div><div class="field"><label>规格</label><input v-model.trim="productForm.spec"></div><div class="field"><label class="required">单位</label><input v-model.trim="productForm.unit"></div><div class="field"><label>售价</label><input v-model="productForm.sale_price" inputmode="decimal"></div></div><template #footer><button class="button secondary" @click="showProduct = false">取消</button><button class="button" :disabled="saving" @click="createProduct">保存草稿</button></template></ModalDialog>
  <ModalDialog v-if="showProcess" :title="editingProcess ? '编辑工序' : '新建工序'" @close="showProcess = false"><div class="form-grid"><div class="field"><label class="required">工序编码</label><input v-model.trim="processForm.process_code" placeholder="例如 G001"></div><div class="field"><label class="required">工序名称</label><input v-model.trim="processForm.name" placeholder="例如 车耳朵"></div><div class="field"><label>默认工价</label><input v-model="processForm.default_unit_price" inputmode="decimal"></div><div class="field"><label>报工配比</label><input v-model="processForm.default_report_ratio" inputmode="decimal"><small>900 套 × 配比 4 = 3600 个加工单位</small></div><div class="field"><label>标准工时（秒/件）</label><input v-model.number="processForm.standard_work_seconds" type="number" min="0"></div></div><template #footer><button class="button secondary" @click="showProcess = false">取消</button><button class="button" :disabled="saving" @click="saveProcess">保存</button></template></ModalDialog>
  <ModalDialog v-if="showRoute" wide :title="`产品工序 · ${current?.name || ''}`" @close="showRoute = false"><div class="notice">保存后生成产品新版本。工价与配比只影响后续下发，不回写工序库。</div><p class="text-muted" style="margin:10px 0 0">最近配置：{{ routeMeta.name || '—' }}<template v-if="routeMeta.at"> · {{ new Date(routeMeta.at/1000).toLocaleString('zh-CN') }}</template></p><div v-for="(row, index) in routes" :key="index" class="route-row"><input v-model.number="row.sequence_no" type="number" min="1" title="序号"><FilterSelect v-model="row.process_id" :options="processOptions" @change="fillDefaults(row)"/><input v-model="row.unit_price" inputmode="decimal" title="工价"><input v-model="row.report_ratio" inputmode="decimal" title="配比"><label><input v-model="row.is_critical" type="checkbox"> 关键</label><label><input v-model="row.is_required" type="checkbox"> 必做</label><button class="button link text-danger" @click="routes.splice(index, 1)">×</button></div><PageState :empty="!routes.length" /><button class="button secondary" @click="addRoute">+ 添加工序</button><template #footer><span class="text-muted" style="margin-right:auto">共 {{ routes.length }} 道工序</span><button class="button secondary" @click="showRoute = false">取消</button><button class="button" :disabled="saving" @click="saveRoutes">保存</button></template></ModalDialog>
</template>
